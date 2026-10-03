package main

import (
	"log"
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
		writeJSON(w, 200, map[string]bool{
			"show_dec_scoreboard": s.ShowDecScoreboard,
			"show_dec_possession": s.ShowDecPossession,
		})
	case http.MethodPost:
		var body struct {
			ShowDecScoreboard bool `json:"show_dec_scoreboard"`
			ShowDecPossession bool `json:"show_dec_possession"`
		}
		if err := decodeJSON(w, r, &body); err != nil {
			writeErr(w, 400, "bad request")
			return
		}
		sm.Update(func(s *GameState) {
			s.ShowDecScoreboard = body.ShowDecScoreboard
			s.ShowDecPossession = body.ShowDecPossession
		})
		sm.requestSave()
		writeOK(w)
	default:
		writeErr(w, 405, "method not allowed")
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
		writeJSON(w, 200, map[string]int{
			"target_score": s.TargetScore,
			"win_margin":   s.WinMargin,
		})
	case http.MethodPost:
		var body struct {
			TargetScore int `json:"target_score"`
			WinMargin   int `json:"win_margin"`
		}
		if err := decodeJSON(w, r, &body); err != nil {
			writeErr(w, 400, "bad request")
			return
		}
		if body.TargetScore <= 0 || body.TargetScore > maxTargetScore ||
			body.WinMargin <= 0 || body.WinMargin > maxWinMargin {
			writeErr(w, 400, "valeurs invalides")
			return
		}
		sm.Update(func(s *GameState) {
			s.TargetScore = body.TargetScore
			s.WinMargin = body.WinMargin
		})
		sm.requestSave()
		writeOK(w)
	default:
		writeErr(w, 405, "method not allowed")
	}
}

func handleGetIPs(w http.ResponseWriter, r *http.Request) {
	ifaces, err := net.Interfaces()
	if err != nil {
		writeErr(w, 500, "erreur")
		return
	}
	type ipInfo struct {
		Name string `json:"name"`
		IP   string `json:"ip"`
	}
	result := []ipInfo{}
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
	writeJSON(w, 200, result)
}

// handleEndMatch — POST /api/end-match
// Enregistre le score courant dans l'historique des matchs, puis remet le
// match à zéro. La lecture du score et la remise à zéro se font dans le même
// verrou : aucun point marqué entre les deux ne peut être perdu ou compté
// dans le match suivant.
func handleEndMatch(w http.ResponseWriter, r *http.Request, sm *StateManager, store *Store) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	var record MatchRecord
	sm.Update(func(gs *GameState) {
		record = MatchRecord{
			ID:        newID(),
			Date:      time.Now(),
			HomeTeam:  gs.TeamHomeName,
			AwayTeam:  gs.TeamAwayName,
			HomeColor: gs.TeamHomeColor,
			AwayColor: gs.TeamAwayColor,
			ScoreHome: gs.ScoreHome,
			ScoreAway: gs.ScoreAway,
			Overtime:  gs.Overtime,
		}
		gs.resetMatch()
	})

	sm.requestSave() // le tableau est à zéro : on ne garde plus le match en cours

	if err := store.AddMatch(record); err != nil {
		// On ne bloque pas la remise à zéro pour autant : le match doit
		// pouvoir se terminer même si l'historique n'a pas pu être écrit.
		log.Printf("⚠️  Impossible d'enregistrer l'historique du match: %v", err)
	}

	writeJSON(w, 200, map[string]any{"ok": true, "record": record})
}

func handleSaveConfig(w http.ResponseWriter, r *http.Request, sm *StateManager) {
	if !requireMethod(w, r, http.MethodPost) {
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
	if err := decodeJSON(w, r, &cfg); err != nil {
		writeErr(w, 400, "bad request")
		return
	}

	home, away := cleanName(cfg.TeamHomeName), cleanName(cfg.TeamAwayName)
	log.Printf("📥 /api/config : home=%q away=%q", home, away)

	sm.Update(func(s *GameState) {
		if home != "" {
			s.TeamHomeName = home
		}
		if away != "" {
			s.TeamAwayName = away
		}
		if isHexColor(cfg.TeamHomeColor) {
			s.TeamHomeColor = cfg.TeamHomeColor
		}
		if isHexColor(cfg.TeamAwayColor) {
			s.TeamAwayColor = cfg.TeamAwayColor
		}
		if cfg.MatchDuration >= minMatchSecs && cfg.MatchDuration <= maxMatchSecs {
			s.MatchDuration = cfg.MatchDuration
		}
		if cfg.PossMax >= minPossSecs && cfg.PossMax <= maxPossSecs {
			s.PossMax = cfg.PossMax
		}
		if cfg.ResetMatch {
			s.resetMatch()
		}
	})
	sm.requestSave()

	writeOK(w)
}
