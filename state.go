package main

import (
	"encoding/json"
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
	Period            int     `json:"period"`
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
}

type StateManager struct {
	mu        sync.RWMutex
	state     GameState
	clients   map[chan []byte]bool
	clientsMu sync.Mutex
}

func NewStateManager() *StateManager {
	sm := &StateManager{
		state: GameState{
			GameTime:      600,
			PossTime:      12,
			Period:        1,
			TeamHomeName:  "ÉQUIPE 1",
			TeamAwayName:  "ÉQUIPE 2",
			TeamHomeColor: "#e63946",
			TeamAwayColor: "#457b9d",
			MatchDuration: 600,
			PossMax:       12,
			TargetScore:   21,
			WinMargin:     2,
		},
		clients: make(map[chan []byte]bool),
	}

	// Restaurer l'état depuis le disque si disponible
	sm.loadState()

	// Chrono mis en pause après un redémarrage (sécurité)
	sm.state.TimeRunning = false
	sm.state.PossRunning = false

	// Flush périodique toutes les 30s
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			sm.saveState()
		}
	}()

	return sm
}

func stateFile() string {
	return filepath.Join(dataDir(), "match_state.json")
}

func (sm *StateManager) saveState() {
	sm.mu.RLock()
	data, err := json.MarshalIndent(sm.state, "", "  ")
	sm.mu.RUnlock()
	if err != nil {
		return
	}
	os.MkdirAll(dataDir(), 0755)
	os.WriteFile(stateFile(), data, 0644)
}

func (sm *StateManager) loadState() {
	data, err := os.ReadFile(stateFile())
	if err != nil {
		return // première utilisation
	}
	var s GameState
	if err := json.Unmarshal(data, &s); err != nil {
		return
	}
	// On ne restaure que les infos de match (équipes, config)
	// Les scores/fautes sont remis à zéro pour éviter la confusion
	sm.state.TeamHomeName = s.TeamHomeName
	sm.state.TeamAwayName = s.TeamAwayName
	sm.state.TeamHomeColor = s.TeamHomeColor
	sm.state.TeamAwayColor = s.TeamAwayColor
	sm.state.MatchDuration = s.MatchDuration
	sm.state.PossMax = s.PossMax
	sm.state.GameTime = float64(s.MatchDuration)
	sm.state.PossTime = s.PossMax
}

func (sm *StateManager) Get() GameState {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.state
}

func (sm *StateManager) Update(fn func(*GameState)) {
	sm.mu.Lock()
	fn(&sm.state)
	sm.mu.Unlock()
	sm.Broadcast()
}

// Tick avance les chronos de 100ms.
func (sm *StateManager) Tick() {
	sm.mu.Lock()

	changed := false
	if sm.state.TimeRunning && sm.state.GameTime > 0 {
		sm.state.GameTime = math.Max(0, sm.state.GameTime-0.1)
		if sm.state.GameTime == 0 {
			sm.state.TimeRunning = false
			sm.state.PossRunning = false
		}
		changed = true
	}

	if sm.state.PossRunning && sm.state.PossTime > 0 {
		sm.state.PossTime = math.Max(0, sm.state.PossTime-0.1)
		if sm.state.PossTime <= 0 {
			sm.state.PossTime = 0
			sm.state.PossRunning = false
		}
		changed = true
	}
	sm.mu.Unlock()

	if changed {
		sm.Broadcast()
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

// Broadcast envoie l'état courant à tous les clients WebSocket.
func (sm *StateManager) Broadcast() {
	data, err := json.Marshal(sm.Get())
	if err != nil {
		return
	}
	sm.BroadcastRaw(data)
}

// BroadcastRaw envoie un message brut à tous les clients.
func (sm *StateManager) BroadcastRaw(data []byte) {
	sm.clientsMu.Lock()
	for ch := range sm.clients {
		select {
		case ch <- data:
		default:
		}
	}
	sm.clientsMu.Unlock()
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
