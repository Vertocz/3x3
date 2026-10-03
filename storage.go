package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"time"
)

// Team représente une équipe enregistrée
type Team struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

// MatchRecord représente un match enregistré
type MatchRecord struct {
	ID        string    `json:"id"`
	Date      time.Time `json:"date"`
	HomeTeam  string    `json:"home_team"`
	AwayTeam  string    `json:"away_team"`
	HomeColor string    `json:"home_color"`
	AwayColor string    `json:"away_color"`
	ScoreHome int       `json:"score_home"`
	ScoreAway int       `json:"score_away"`
	Overtime  bool      `json:"overtime,omitempty"` // terminé après prolongation
}

// Store contient toutes les données persistantes (équipes + historique).
// Toutes les méthodes sont sûres pour un usage concurrent : les handlers
// HTTP tournent chacun dans leur goroutine.
type Store struct {
	mu      sync.RWMutex
	Teams   []Team        `json:"teams"`
	Matches []MatchRecord `json:"matches"`
}

// dataDir retourne le dossier de données — séparé du binaire
// pour survivre aux mises à jour.
func dataDir() string {
	switch runtime.GOOS {
	case "linux":
		// Sur Pi : ~/scoreboard-data/
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "scoreboard-data")
	default:
		// Sur Windows/Mac : dossier "data" à côté de l'exe
		exe, _ := os.Executable()
		return filepath.Join(filepath.Dir(exe), "data")
	}
}

func dataFile() string {
	return filepath.Join(dataDir(), "teams.json")
}

// writeFileAtomic écrit data dans path sans jamais laisser un fichier à
// moitié écrit : écriture dans un fichier temporaire du même dossier,
// fsync, puis rename (atomique sous POSIX). Une coupure de courant — le
// quotidien d'un Raspberry Pi débranché à la main — laisse donc soit
// l'ancien fichier, soit le nouveau, jamais un fichier tronqué.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("impossible de créer %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".tmp-"+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() { os.Remove(tmpName) }

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return err
	}
	// Meilleur effort : fsync du dossier pour que le rename survive aussi
	// à une coupure de courant immédiate.
	if d, err := os.Open(dir); err == nil {
		d.Sync()
		d.Close()
	}
	return nil
}

// LoadStore charge les données depuis le disque.
// Si le fichier n'existe pas, retourne un Store vide. Si le fichier est
// illisible (corrompu), il est mis de côté (.corrupt-<date>) au lieu d'être
// écrasé silencieusement à la prochaine sauvegarde.
func LoadStore() *Store {
	store := &Store{Teams: []Team{}, Matches: []MatchRecord{}}

	data, err := os.ReadFile(dataFile())
	if err != nil {
		return store // fichier inexistant = première utilisation
	}
	if err := json.Unmarshal(data, store); err != nil {
		backup := fmt.Sprintf("%s.corrupt-%s", dataFile(), time.Now().Format("20060102-150405"))
		log.Printf("⚠️  Erreur lecture données (%v) — fichier mis de côté : %s", err, backup)
		os.Rename(dataFile(), backup)
		return &Store{Teams: []Team{}, Matches: []MatchRecord{}}
	}
	if store.Teams == nil {
		store.Teams = []Team{}
	}
	if store.Matches == nil {
		store.Matches = []MatchRecord{}
	}
	log.Printf("✅ %d équipe(s), %d match(s) chargé(s) depuis %s", len(store.Teams), len(store.Matches), dataFile())
	return store
}

// saveLocked sérialise et écrit le store. L'appelant doit détenir s.mu.
func (s *Store) saveLocked() error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(dataFile(), data, 0644)
}

// ListTeams retourne une copie de la liste des équipes.
func (s *Store) ListTeams() []Team {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Team, len(s.Teams))
	copy(out, s.Teams)
	return out
}

// UpsertTeam ajoute ou met à jour une équipe. Sans ID, une équipe portant
// déjà le même nom est mise à jour au lieu d'être dupliquée. Retourne
// l'équipe telle qu'enregistrée.
func (s *Store) UpsertTeam(t Team) (Team, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if t.ID == "" {
		for _, existing := range s.Teams {
			if existing.Name == t.Name {
				t.ID = existing.ID
				break
			}
		}
	}
	if t.ID == "" {
		t.ID = newID()
	}

	replaced := false
	for i, existing := range s.Teams {
		if existing.ID == t.ID {
			s.Teams[i] = t
			replaced = true
			break
		}
	}
	if !replaced {
		s.Teams = append(s.Teams, t)
	}
	return t, s.saveLocked()
}

// DeleteTeam supprime une équipe par ID.
func (s *Store) DeleteTeam(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	filtered := make([]Team, 0, len(s.Teams))
	for _, t := range s.Teams {
		if t.ID != id {
			filtered = append(filtered, t)
		}
	}
	s.Teams = filtered
	return s.saveLocked()
}

// ListMatches retourne l'historique du plus récent au plus ancien.
func (s *Store) ListMatches() []MatchRecord {
	s.mu.RLock()
	out := make([]MatchRecord, len(s.Matches))
	copy(out, s.Matches)
	s.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Date.After(out[j].Date) })
	return out
}

// AddMatch ajoute un match à l'historique.
func (s *Store) AddMatch(m MatchRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Matches = append(s.Matches, m)
	return s.saveLocked()
}

// DeleteMatch supprime un match de l'historique par ID.
func (s *Store) DeleteMatch(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	filtered := make([]MatchRecord, 0, len(s.Matches))
	for _, m := range s.Matches {
		if m.ID != id {
			filtered = append(filtered, m)
		}
	}
	s.Matches = filtered
	return s.saveLocked()
}

// ClearMatches vide tout l'historique.
func (s *Store) ClearMatches() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Matches = []MatchRecord{}
	return s.saveLocked()
}
