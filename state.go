package main

import (
	"bytes"
	"encoding/json"
	"log"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type GameState struct {
	ScoreHome         int     `json:"score_home"`
	ScoreAway         int     `json:"score_away"`
	GameTime          float64 `json:"game_time"`
	PossTime          float64 `json:"poss_time"`
	FoulsHome         int     `json:"fouls_home"`
	FoulsAway         int     `json:"fouls_away"`
	TimeRunning       bool    `json:"time_running"`
	PossRunning       bool    `json:"poss_running"`
	TeamHomeName      string  `json:"team_home_name"`
	TeamAwayName      string  `json:"team_away_name"`
	TeamHomeColor     string  `json:"team_home_color"`
	TeamAwayColor     string  `json:"team_away_color"`
	MatchDuration     int     `json:"match_duration"`
	PossMax           float64 `json:"poss_max"`
	ShowDecScoreboard bool    `json:"show_dec_scoreboard"`
	ShowDecPossession bool    `json:"show_dec_possession"`
	TargetScore       int     `json:"target_score"`
	WinMargin         int     `json:"win_margin"`

	// Prolongation (règle 3x3) : en cas d'égalité à la fin du temps, pause
	// d'une minute puis la première équipe qui marque OvertimePoints points
	// gagne, sans chrono de jeu.
	Overtime       bool    `json:"overtime"`
	OvertimeBase   int     `json:"overtime_base"`   // score commun au début de la prolongation
	OvertimePoints int     `json:"overtime_points"` // points à marquer pour gagner (2)
	BreakTime      float64 `json:"break_time"`      // secondes restantes de la pause (0 = pause finie)
}

// resetMatch remet score, fautes, période et chronos à leur valeur de début
// de match. La configuration (équipes, durées, règles) n'est pas touchée.
func (s *GameState) resetMatch() {
	s.ScoreHome = 0
	s.ScoreAway = 0
	s.FoulsHome = 0
	s.FoulsAway = 0
	s.Overtime = false
	s.OvertimeBase = 0
	s.BreakTime = 0
	s.TimeRunning = false
	s.PossRunning = false
	s.GameTime = float64(s.MatchDuration)
	s.PossTime = s.PossMax
}

// persistedConfig est la partie de l'état qui survit à un redémarrage :
// les réglages, pas le score en cours (voir loadState).
type persistedConfig struct {
	TeamHomeName      string  `json:"team_home_name"`
	TeamAwayName      string  `json:"team_away_name"`
	TeamHomeColor     string  `json:"team_home_color"`
	TeamAwayColor     string  `json:"team_away_color"`
	MatchDuration     int     `json:"match_duration"`
	PossMax           float64 `json:"poss_max"`
	ShowDecScoreboard bool    `json:"show_dec_scoreboard"`
	ShowDecPossession bool    `json:"show_dec_possession"`
	TargetScore       int     `json:"target_score"`
	WinMargin         int     `json:"win_margin"`

	// Match en cours (absent quand le tableau est à zéro). Restauré après une
	// coupure de courant, chronos à l'arrêt.
	Match *persistedMatch `json:"match,omitempty"`
}

// persistedMatch est l'état d'un match commencé mais pas terminé.
type persistedMatch struct {
	ScoreHome    int     `json:"score_home"`
	ScoreAway    int     `json:"score_away"`
	FoulsHome    int     `json:"fouls_home"`
	FoulsAway    int     `json:"fouls_away"`
	GameTime     float64 `json:"game_time"`
	PossTime     float64 `json:"poss_time"`
	Overtime     bool    `json:"overtime"`
	OvertimeBase int     `json:"overtime_base"`
	BreakTime    float64 `json:"break_time"`
}

const (
	// Règles de la prolongation au 3x3.
	overtimeBreakSecs = 60.0 // pause avant la prolongation
	overtimePoints    = 2    // premier à marquer 2 points

	// tickInterval : cadence de rafraîchissement des chronos.
	tickInterval = 100 * time.Millisecond
	// maxTickDelta : plafond du temps réel pris en compte en un tick (si le
	// process a été figé, on ne vide pas le chrono d'un coup).
	maxTickDelta = 2.0
	// resyncEvery : tous les N ticks, l'état complet est rediffusé même
	// sans changement. Un écran qui aurait raté un message se rattrape
	// ainsi en moins de 2 secondes.
	resyncEvery = 20
)

type StateManager struct {
	mu        sync.RWMutex
	state     GameState
	lastTick  time.Time
	tickCount int

	clients   map[chan []byte]bool
	clientsMu sync.Mutex

	saveCh    chan struct{}
	saveMu    sync.Mutex
	lastSaved []byte
}

func NewStateManager() *StateManager {
	sm := &StateManager{
		state: GameState{
			GameTime:       600,
			PossTime:       12,
			OvertimePoints: overtimePoints,
			TeamHomeName:   "ÉQUIPE 1",
			TeamAwayName:   "ÉQUIPE 2",
			TeamHomeColor:  "#e63946",
			TeamAwayColor:  "#457b9d",
			MatchDuration:  600,
			PossMax:        12,
			TargetScore:    21,
			WinMargin:      2,
		},
		clients:  make(map[chan []byte]bool),
		saveCh:   make(chan struct{}, 1),
		lastTick: time.Now(),
	}

	// Restaurer la configuration depuis le disque si disponible.
	sm.loadState()

	// Chrono toujours à l'arrêt après un redémarrage (sécurité).
	sm.state.TimeRunning = false
	sm.state.PossRunning = false

	go sm.saver()
	return sm
}

func stateFile() string {
	return filepath.Join(dataDir(), "match_state.json")
}

// snapshotConfig extrait la configuration persistante. L'appelant détient mu.
func (sm *StateManager) snapshotConfig() persistedConfig {
	s := sm.state
	c := persistedConfig{
		TeamHomeName:      s.TeamHomeName,
		TeamAwayName:      s.TeamAwayName,
		TeamHomeColor:     s.TeamHomeColor,
		TeamAwayColor:     s.TeamAwayColor,
		MatchDuration:     s.MatchDuration,
		PossMax:           s.PossMax,
		ShowDecScoreboard: s.ShowDecScoreboard,
		ShowDecPossession: s.ShowDecPossession,
		TargetScore:       s.TargetScore,
		WinMargin:         s.WinMargin,
	}
	// On ne mémorise le match que s'il est réellement commencé : un tableau à
	// zéro ne laisse aucune trace dans le fichier.
	inProgress := s.ScoreHome > 0 || s.ScoreAway > 0 || s.FoulsHome > 0 || s.FoulsAway > 0 ||
		s.Overtime || s.GameTime < float64(s.MatchDuration) || s.PossTime < s.PossMax
	if inProgress {
		c.Match = &persistedMatch{
			ScoreHome: s.ScoreHome, ScoreAway: s.ScoreAway,
			FoulsHome: s.FoulsHome, FoulsAway: s.FoulsAway,
			GameTime: s.GameTime, PossTime: s.PossTime,
			Overtime: s.Overtime, OvertimeBase: s.OvertimeBase, BreakTime: s.BreakTime,
		}
	}
	return c
}

// saveState écrit la configuration sur disque, mais seulement si elle a
// changé depuis la dernière écriture (ménage la carte SD) et de façon
// atomique (jamais de fichier tronqué après une coupure de courant).
func (sm *StateManager) saveState() {
	sm.mu.RLock()
	data, err := json.MarshalIndent(sm.snapshotConfig(), "", "  ")
	sm.mu.RUnlock()
	if err != nil {
		return
	}

	sm.saveMu.Lock()
	defer sm.saveMu.Unlock()
	if bytes.Equal(data, sm.lastSaved) {
		return
	}
	if err := writeFileAtomic(stateFile(), data, 0644); err != nil {
		log.Printf("⚠️  Sauvegarde de la configuration impossible: %v", err)
		return
	}
	sm.lastSaved = data
}

// requestSave demande une sauvegarde en arrière-plan, sans bloquer le
// handler HTTP (un fsync sur carte SD peut prendre quelques dizaines de ms).
// Les demandes rapprochées sont regroupées.
func (sm *StateManager) requestSave() {
	select {
	case sm.saveCh <- struct{}{}:
	default:
	}
}

func (sm *StateManager) saver() {
	// Toutes les 10 s : pendant un match, le temps restant est ainsi
	// toujours à jour à 10 s près après une coupure ; hors match, rien n'est
	// écrit (comparaison avec la dernière sauvegarde).
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-sm.saveCh:
		case <-t.C:
		}
		sm.saveState()
	}
}

// loadState restaure la configuration (équipes, durées, règles, options
// d'affichage). Les scores et fautes repartent à zéro pour éviter la
// confusion. Chaque valeur est validée : un fichier abîmé ou d'une ancienne
// version ne peut pas produire un chrono à 0 ou un nom vide.
func (sm *StateManager) loadState() {
	data, err := os.ReadFile(stateFile())
	if err != nil {
		return // première utilisation
	}
	var c persistedConfig
	if err := json.Unmarshal(data, &c); err != nil {
		log.Printf("⚠️  match_state.json illisible, valeurs par défaut utilisées: %v", err)
		return
	}

	s := &sm.state
	if n := cleanName(c.TeamHomeName); n != "" {
		s.TeamHomeName = n
	}
	if n := cleanName(c.TeamAwayName); n != "" {
		s.TeamAwayName = n
	}
	if isHexColor(c.TeamHomeColor) {
		s.TeamHomeColor = c.TeamHomeColor
	}
	if isHexColor(c.TeamAwayColor) {
		s.TeamAwayColor = c.TeamAwayColor
	}
	if c.MatchDuration >= minMatchSecs && c.MatchDuration <= maxMatchSecs {
		s.MatchDuration = c.MatchDuration
	}
	if c.PossMax >= minPossSecs && c.PossMax <= maxPossSecs {
		s.PossMax = c.PossMax
	}
	s.ShowDecScoreboard = c.ShowDecScoreboard
	s.ShowDecPossession = c.ShowDecPossession
	if c.TargetScore > 0 && c.TargetScore <= maxTargetScore {
		s.TargetScore = c.TargetScore
	}
	if c.WinMargin > 0 && c.WinMargin <= maxWinMargin {
		s.WinMargin = c.WinMargin
	}
	s.GameTime = float64(s.MatchDuration)
	s.PossTime = s.PossMax

	// Match en cours au moment de la coupure : score, fautes, temps restants.
	// Les chronos restent À L'ARRÊT (forcé par NewStateManager) : l'opérateur
	// vérifie puis relance. Chaque valeur est bornée, un fichier abîmé ne peut
	// donc pas produire un affichage absurde.
	if m := c.Match; m != nil {
		s.ScoreHome = clampInt(m.ScoreHome, 0, maxTargetScore)
		s.ScoreAway = clampInt(m.ScoreAway, 0, maxTargetScore)
		s.FoulsHome = clampInt(m.FoulsHome, 0, maxFouls)
		s.FoulsAway = clampInt(m.FoulsAway, 0, maxFouls)
		s.GameTime = clamp(m.GameTime, 0, float64(s.MatchDuration))
		s.PossTime = clamp(m.PossTime, 0, s.PossMax)
		if m.Overtime {
			s.Overtime = true
			s.OvertimeBase = clampInt(m.OvertimeBase, 0, maxTargetScore)
			s.BreakTime = clamp(m.BreakTime, 0, overtimeBreakSecs)
			s.GameTime = 0
		}
	}
}

func (sm *StateManager) Get() GameState {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.state
}

// Update applique fn à l'état puis diffuse le nouvel état. La diffusion se
// fait sous le verrou : l'ordre des messages reçus par les écrans est ainsi
// toujours l'ordre réel des changements (jamais un état ancien reçu après un
// état récent).
func (sm *StateManager) Update(fn func(*GameState)) {
	sm.mu.Lock()
	fn(&sm.state)
	sm.broadcastLocked()
	sm.mu.Unlock()
}

// Tick fait avancer les chronos du temps réellement écoulé.
func (sm *StateManager) Tick() { sm.tickAt(time.Now()) }

// tickAt est Tick avec une horloge injectable (tests).
//
// Le temps retiré est le temps réel écoulé depuis le tick précédent, et non
// une constante de 100 ms : si le Pi est chargé et que des ticks sont en
// retard, le chrono ne prend pas de retard sur la vraie horloge.
func (sm *StateManager) tickAt(now time.Time) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	dt := now.Sub(sm.lastTick).Seconds()
	sm.lastTick = now
	if dt < 0 {
		dt = 0
	}
	if dt > maxTickDelta {
		dt = maxTickDelta
	}
	sm.tickCount++

	s := &sm.state
	changed := false

	if s.Overtime && s.BreakTime > 0 {
		s.BreakTime = math.Max(0, s.BreakTime-dt)
		changed = true
	}

	if s.TimeRunning {
		if s.GameTime > 0 {
			s.GameTime = math.Max(0, s.GameTime-dt)
			changed = true
		}
		if s.GameTime <= 0 { // fin du temps (ou temps ramené à 0 à la main)
			s.TimeRunning = false
			s.PossRunning = false
			changed = true
		}
	}

	if s.PossRunning {
		if s.PossTime > 0 {
			s.PossTime = math.Max(0, s.PossTime-dt)
			changed = true
		}
		if s.PossTime <= 0 {
			s.PossRunning = false
			changed = true
		}
	}

	if changed || sm.tickCount%resyncEvery == 0 {
		sm.broadcastLocked()
	}
}

func (sm *StateManager) Subscribe() chan []byte {
	ch := make(chan []byte, 16)
	sm.clientsMu.Lock()
	sm.clients[ch] = true
	sm.clientsMu.Unlock()
	return ch
}

func (sm *StateManager) Unsubscribe(ch chan []byte) {
	sm.clientsMu.Lock()
	delete(sm.clients, ch)
	sm.clientsMu.Unlock()
	close(ch)
}

// broadcastLocked diffuse l'état courant. L'appelant détient mu (R ou W).
func (sm *StateManager) broadcastLocked() {
	data, err := json.Marshal(sm.state)
	if err != nil {
		return
	}
	sm.BroadcastRaw(data)
}

// Broadcast envoie l'état courant à tous les clients WebSocket.
func (sm *StateManager) Broadcast() {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	sm.broadcastLocked()
}

// BroadcastRaw envoie un message brut à tous les clients. Un client lent
// ne bloque jamais les autres : si sa file est pleine, on écarte son message
// le plus ancien pour garder les plus récents (un tableau de score veut
// l'état le plus frais, pas l'historique).
func (sm *StateManager) BroadcastRaw(data []byte) {
	sm.clientsMu.Lock()
	for ch := range sm.clients {
		select {
		case ch <- data:
		default:
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- data:
			default:
			}
		}
	}
	sm.clientsMu.Unlock()
}

func clampInt(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func clamp(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
