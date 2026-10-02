package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"
)

// handleDisplayOptions — GET/POST /api/display-options
// Réglages persistants (pas liés à un match précis) : faut-il afficher la
// décimale sous 10s sur le tableau général et/ou sur l'écran de
// possession. Appliqués immédiatement à tous les écrans déjà ouverts via
// la diffusion WebSocket normale de l'état.
func handleDisplayOptions(w http.ResponseWriter, r *http.Request, sm *StateManager) {
	switch r.Method {
	case http.MethodGet:
		s := sm.Get()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{
			"show_dec_scoreboard": s.ShowDecScoreboard,
			"show_dec_possession": s.ShowDecPossession,
		})
	case http.MethodPost:
		var body struct {
			ShowDecScoreboard bool `json:"show_dec_scoreboard"`
			ShowDecPossession bool `json:"show_dec_possession"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad request", 400)
			return
		}
		sm.Update(func(s *GameState) {
			s.ShowDecScoreboard = body.ShowDecScoreboard
			s.ShowDecPossession = body.ShowDecPossession
		})
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	default:
		http.Error(w, "method not allowed", 405)
	}
}

// handleMatchRules — GET/POST /api/match-rules
// Règle de fin de match configurable (par défaut 21 points, 2 d'écart) —
// utilisée pour calculer la "balle de match" affichée sur le tableau.
// Persistant comme les réglages d'affichage : pas remis à zéro entre deux
// matchs, contrairement au score.
func handleMatchRules(w http.ResponseWriter, r *http.Request, sm *StateManager) {
	switch r.Method {
	case http.MethodGet:
		s := sm.Get()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]int{
			"target_score": s.TargetScore,
			"win_margin":   s.WinMargin,
		})
	case http.MethodPost:
		var body struct {
			TargetScore int `json:"target_score"`
			WinMargin   int `json:"win_margin"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad request", 400)
			return
		}
		if body.TargetScore <= 0 || body.WinMargin <= 0 {
			http.Error(w, "valeurs invalides", 400)
			return
		}
		sm.Update(func(s *GameState) {
			s.TargetScore = body.TargetScore
			s.WinMargin = body.WinMargin
		})
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	default:
		http.Error(w, "method not allowed", 405)
	}
}

func handleGetIPs(w http.ResponseWriter, r *http.Request) {
	ifaces, err := net.Interfaces()
	if err != nil {
		http.Error(w, "erreur", 500)
		return
	}
	type ipInfo struct {
		Name string `json:"name"`
		IP   string `json:"ip"`
	}
	var result []ipInfo
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				if ipnet.IP.To4() != nil {
					result = append(result, ipInfo{Name: iface.Name, IP: ipnet.IP.String()})
				}
			}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// handleEndMatch — POST /api/end-match
// Enregistre le score courant dans l'historique des matchs (jusqu'ici
// jamais alimenté malgré la structure MatchRecord déjà prévue dans
// storage.go), puis remet le match à zéro — équivalent du reset_match de
// /api/config, mais avec la sauvegarde du résultat en plus.
func handleEndMatch(w http.ResponseWriter, r *http.Request, sm *StateManager, store *Store) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}

	s := sm.Get()
	record := MatchRecord{
		ID:        newID(),
		Date:      time.Now(),
		HomeTeam:  s.TeamHomeName,
		AwayTeam:  s.TeamAwayName,
		HomeColor: s.TeamHomeColor,
		AwayColor: s.TeamAwayColor,
		ScoreHome: s.ScoreHome,
		ScoreAway: s.ScoreAway,
	}
	store.Matches = append(store.Matches, record)
	if err := store.Save(); err != nil {
		// On ne bloque pas la remise à zéro pour autant : le match doit
		// pouvoir se terminer même si l'historique n'a pas pu être écrit.
		fmt.Println("⚠️  Impossible d'enregistrer l'historique du match:", err)
	}

	sm.Update(func(gs *GameState) {
		gs.ScoreHome = 0
		gs.ScoreAway = 0
		gs.FoulsHome = 0
		gs.FoulsAway = 0
		gs.Period = 1
		gs.TimeRunning = false
		gs.PossRunning = false
		gs.GameTime = float64(gs.MatchDuration)
		gs.PossTime = gs.PossMax
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":     true,
		"record": record,
	})
}

func handleSaveConfig(w http.ResponseWriter, r *http.Request, sm *StateManager) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	var cfg struct {
		TeamHomeName  string  `json:"team_home_name"`
		TeamAwayName  string  `json:"team_away_name"`
		TeamHomeColor string  `json:"team_home_color"`
		TeamAwayColor string  `json:"team_away_color"`
		MatchDuration int     `json:"match_duration"`
		PossMax       float64 `json:"poss_max"`
		ResetMatch    bool    `json:"reset_match"`
	}
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		http.Error(w, "bad request", 400)
		return
	}
	fmt.Printf("📥 /api/config reçu : home=%q away=%q\n", cfg.TeamHomeName, cfg.TeamAwayName)

	sm.Update(func(s *GameState) {
		if cfg.TeamHomeName != "" {
			s.TeamHomeName = cfg.TeamHomeName
		}
		if cfg.TeamAwayName != "" {
			s.TeamAwayName = cfg.TeamAwayName
		}
		if cfg.TeamHomeColor != "" {
			s.TeamHomeColor = cfg.TeamHomeColor
		}
		if cfg.TeamAwayColor != "" {
			s.TeamAwayColor = cfg.TeamAwayColor
		}
		if cfg.MatchDuration > 0 {
			s.MatchDuration = cfg.MatchDuration
		}
		if cfg.PossMax > 0 {
			s.PossMax = cfg.PossMax
		}
		if cfg.ResetMatch {
			s.ScoreHome = 0
			s.ScoreAway = 0
			s.FoulsHome = 0
			s.FoulsAway = 0
			s.Period = 1
			s.TimeRunning = false
			s.PossRunning = false
			s.GameTime = float64(s.MatchDuration)
			s.PossTime = s.PossMax
		}
	})

	fmt.Println("✅ Config mise à jour")
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"ok":true}`))
}
