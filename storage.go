package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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
}

// Store contient toutes les données persistantes
type Store struct {
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

// LoadStore charge les données depuis le disque.
// Si le fichier n'existe pas, retourne un Store vide.
func LoadStore() *Store {
	store := &Store{Teams: []Team{}, Matches: []MatchRecord{}}

	data, err := os.ReadFile(dataFile())
	if err != nil {
		return store // fichier inexistant = première utilisation
	}
	if err := json.Unmarshal(data, store); err != nil {
		fmt.Println("⚠️  Erreur lecture données:", err)
		return store
	}
	fmt.Printf("✅ %d équipe(s) chargée(s) depuis %s\n", len(store.Teams), dataFile())
	return store
}

// Save sauvegarde les données sur le disque.
func (s *Store) Save() error {
	if err := os.MkdirAll(dataDir(), 0755); err != nil {
		return fmt.Errorf("impossible de créer le dossier data: %w", err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(dataFile(), data, 0644)
}

// UpsertTeam ajoute ou met à jour une équipe (par ID).
func (s *Store) UpsertTeam(t Team) {
	for i, existing := range s.Teams {
		if existing.ID == t.ID {
			s.Teams[i] = t
			return
		}
	}
	s.Teams = append(s.Teams, t)
}

// DeleteTeam supprime une équipe par ID.
func (s *Store) DeleteTeam(id string) {
	filtered := s.Teams[:0]
	for _, t := range s.Teams {
		if t.ID != id {
			filtered = append(filtered, t)
		}
	}
	s.Teams = filtered
}

// DeleteMatch supprime un match de l'historique par ID.
func (s *Store) DeleteMatch(id string) {
	filtered := s.Matches[:0]
	for _, m := range s.Matches {
		if m.ID != id {
			filtered = append(filtered, m)
		}
	}
	s.Matches = filtered
}
