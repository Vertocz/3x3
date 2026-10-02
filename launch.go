package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// ScreenInfo décrit une sortie HDMI détectée par wlr-randr.
type ScreenInfo struct {
	Name      string `json:"name"`
	Connected bool   `json:"connected"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	RefreshHz string `json:"refresh_hz"`
	PosX      int    `json:"pos_x"`
	Role      string `json:"role"`
	HDMI      int    `json:"hdmi"`
}

// filterEnv retire du tableau d'environnement les variables dont la clé
// figure dans keys. Sert à garantir qu'une valeur ajoutée ensuite (ex:
// LC_ALL=C) prend effet, sans dépendre du comportement — non garanti selon
// les libc — face à des clés dupliquées dans le tableau d'environnement.
func filterEnv(env []string, keys ...string) []string {
	out := env[:0:0]
	for _, kv := range env {
		skip := false
		for _, k := range keys {
			if strings.HasPrefix(kv, k+"=") {
				skip = true
				break
			}
		}
		if !skip {
			out = append(out, kv)
		}
	}
	return out
}

// parseScreens interroge wlr-randr et retourne les sorties HDMI actives.
func parseScreens() []ScreenInfo {
	cmd := exec.Command("wlr-randr")
	env := append(os.Environ(), waylandEnv()...)
	// wlr-randr formate les Hz avec un printf("%f", ...) dont le séparateur
	// décimal dépend de la locale du processus. En locale française
	// (LC_ALL/LANG=fr_FR...), "60.000000" devient "60,000000" et casse le
	// parsing ci-dessous. On force une locale C pour cet appel précis,
	// après avoir retiré les LC_*/LANG hérités de l'environnement du service.
	// (On n'a pas basculé sur `wlr-randr --json` : le flag n'existe pas dans
	// toutes les versions packagées pour Raspberry Pi OS, et se tromper sur
	// le schéma JSON casserait la détection en silence — plus risqué que ce
	// correctif ciblé.)
	env = filterEnv(env, "LC_ALL", "LANG", "LC_NUMERIC")
	env = append(env, "LC_ALL=C", "LANG=C")
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		return []ScreenInfo{}
	}

	lines := strings.Split(string(out), "\n")
	var screens []ScreenInfo
	var cur *ScreenInfo

	for _, raw := range lines {
		if raw == "" {
			continue
		}

		// Ligne de sortie = commence sans indentation
		if len(raw) > 0 && raw[0] != ' ' && raw[0] != '\t' {
			if cur != nil && cur.Connected {
				screens = append(screens, *cur)
			}
			cur = nil

			fields := strings.Fields(raw)
			if len(fields) == 0 {
				continue
			}
			name := fields[0]
			if name != "HDMI-A-1" && name != "HDMI-A-2" {
				continue
			}
			hdmi := 1
			if name == "HDMI-A-2" {
				hdmi = 2
			}
			role := "scoreboard"
			if hdmi == 2 {
				role = "possession"
			}
			cur = &ScreenInfo{
				Name:      name,
				HDMI:      hdmi,
				Role:      role,
				Width:     1920,
				Height:    1080,
				RefreshHz: "60",
			}
			continue
		}

		if cur == nil {
			continue
		}

		trimmed := strings.TrimSpace(raw)

		// Résolution courante : "1920x1080 px, 60.000000 Hz (preferred, current)"
		if strings.Contains(trimmed, "current") && strings.Contains(trimmed, "px,") {
			var w, h int
			fmt.Sscanf(trimmed, "%dx%d px,", &w, &h)
			if w > 0 && h > 0 {
				cur.Width = w
				cur.Height = h
				cur.Connected = true
			}
			commaIdx := strings.Index(trimmed, ", ")
			hzIdx := strings.Index(trimmed, " Hz")
			if commaIdx >= 0 && hzIdx > commaIdx+2 {
				rr := strings.TrimSpace(trimmed[commaIdx+2 : hzIdx])
				// Filet de sécurité : si un séparateur décimal virgule passe
				// malgré la locale forcée plus haut, on le normalise avant parsing.
				rr = strings.ReplaceAll(rr, ",", ".")
				if f, err := strconv.ParseFloat(rr, 64); err == nil {
					cur.RefreshHz = strconv.FormatFloat(f, 'f', 2, 64)
				} else {
					cur.RefreshHz = rr
				}
			}
		}

		// Position : "Position: 1280,0"
		if strings.HasPrefix(trimmed, "Position:") {
			var px, py int
			fmt.Sscanf(trimmed, "Position: %d,%d", &px, &py)
			cur.PosX = px
		}
	}

	// Dernière sortie
	if cur != nil && cur.Connected {
		screens = append(screens, *cur)
	}

	return screens
}

// handleGetScreens — GET /api/screens
func handleGetScreens(w http.ResponseWriter, r *http.Request) {
	screens := parseScreens()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(screens)
}
