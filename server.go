package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/gorilla/websocket"
)

//go:embed static/index.html
var indexHTML []byte

//go:embed static/config.html
var configHTML []byte

//go:embed static/possession.html
var possessionHTML []byte

//go:embed static/lancer.png
var lancerPNG []byte

//go:embed static/ball.png
var ballPNG []byte

//go:embed static/logo.png
var logoPNG []byte

//go:embed static/horn.mp3
var hornMP3 []byte

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// chromiumLaunches suit, par numéro de HDMI (1 ou 2), le process Chromium
// lancé par /api/launch. Objectif : éviter d'empiler plusieurs fenêtres sur
// le même écran (double clic sur "lancer le tableau", ou futur
// auto-lancement au branchement à chaud) — chaque instance Chromium
// supplémentaire coûte cher en RAM sur un Raspberry Pi.
type launchState struct {
	mu    sync.Mutex
	procs map[int]*os.Process
}

var chromiumLaunches = &launchState{procs: make(map[int]*os.Process)}

// processAlive teste l'existence du process en lui envoyant le signal 0
// (convention POSIX : ne tue rien, sert uniquement à sonder le PID).
func processAlive(p *os.Process) bool {
	if p == nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}

// waylandEnv retourne les variables d'environnement Wayland en lisant
// l'environnement courant plutôt qu'en codant l'UID en dur.
func waylandEnv() []string {
	xdgRuntime := os.Getenv("XDG_RUNTIME_DIR")
	if xdgRuntime == "" {
		uid := os.Getuid()
		xdgRuntime = fmt.Sprintf("/run/user/%d", uid)
	}
	home := os.Getenv("HOME")
	if home == "" {
		home = "/home/pi"
	}
	waylandDisplay := os.Getenv("WAYLAND_DISPLAY")
	if waylandDisplay == "" {
		waylandDisplay = "wayland-0"
	}
	return []string{
		"WAYLAND_DISPLAY=" + waylandDisplay,
		"XDG_RUNTIME_DIR=" + xdgRuntime,
		"HOME=" + home,
	}
}

func handleSetDisplay(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	var cfg struct {
		HDMI1 string `json:"hdmi1"`
		HDMI2 string `json:"hdmi2"`
	}
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		http.Error(w, "bad request", 400)
		return
	}
	applyRes := func(output, res string) {
		parts := strings.Split(res, "@")
		if len(parts) != 2 {
			return
		}
		cmd := exec.Command("wlr-randr", "--output", output, "--mode", parts[0], "--refresh", parts[1])
		cmd.Env = append(os.Environ(), waylandEnv()...)
		cmd.Run()
	}
	if cfg.HDMI1 != "" {
		applyRes("HDMI-A-1", cfg.HDMI1)
	}
	if cfg.HDMI2 != "" {
		applyRes("HDMI-A-2", cfg.HDMI2)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"ok":true}`))
}

// handleShutdown — appelé par buttons.py (bouton power physique).
// force=false : broadcast WS → modale de confirmation sur toutes les pages.
// force=true  : shutdown immédiat (appui long >= 3s).
func handleShutdown(w http.ResponseWriter, r *http.Request, sm *StateManager) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	var body struct {
		Force bool `json:"force"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"ok":true}`))
	if body.Force {
		log.Println("Shutdown immédiat (appui long)")
		go exec.Command("sudo", "shutdown", "-h", "now").Run()
		return
	}
	msg, _ := json.Marshal(map[string]string{"type": "shutdown_request"})
	sm.BroadcastRaw(msg)
	log.Println("shutdown_request broadcasté")
}

// handleShutdownConfirm — appelé par la page web après confirmation utilisateur.
func handleShutdownConfirm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	log.Println("Shutdown confirmé (interface web)")
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"ok":true}`))
	go exec.Command("sudo", "shutdown", "-h", "now").Run()
}

// handleLaunchHDMI — POST /api/launch {"hdmi": 1|2}
// Applique la résolution native puis ouvre Chromium kiosk plein écran
// via wlrctl (compositeur Wayland existant — Wayfire ou labwc).
func handleLaunchHDMI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}

	var req struct {
		HDMI int `json:"hdmi"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", 400)
		return
	}
	if req.HDMI != 1 && req.HDMI != 2 {
		http.Error(w, "hdmi must be 1 or 2", 400)
		return
	}

	armLaunch()

	outputName := fmt.Sprintf("HDMI-A-%d", req.HDMI)

	// Trouver l'écran dans wlr-randr
	posX, width, height, refreshHz := 0, 1920, 1080, "60"
	for _, s := range parseScreens() {
		if s.Name == outputName {
			posX = s.PosX
			width = s.Width
			height = s.Height
			refreshHz = s.RefreshHz
			break
		}
	}
	// Fallback position HDMI-2 si non détecté
	if req.HDMI == 2 && posX == 0 {
		posX = 1920
	}

	// URL selon le rôle. On cible toujours localhost (Chromium tourne sur
	// la même machine que le serveur) plutôt que r.Host, pour un
	// comportement identique que le lancement vienne d'une requête HTTP ou
	// du branchement à chaud (qui n'a pas de requête associée).
	pageURL := fmt.Sprintf("http://localhost:%d/scoreboard", serverPort)
	if req.HDMI == 2 {
		pageURL = fmt.Sprintf("http://localhost:%d/possession", serverPort)
	}

	// 1. Appliquer la résolution native
	modeStr := fmt.Sprintf("%dx%d", width, height)
	refreshInt := "60"
	if f, err := strconv.ParseFloat(refreshHz, 64); err == nil {
		refreshInt = strconv.Itoa(int(f + 0.5))
	}
	randrCmd := exec.Command("wlr-randr",
		"--output", outputName,
		"--mode", modeStr,
		"--refresh", refreshInt,
	)
	randrCmd.Env = append(os.Environ(), waylandEnv()...)
	randrCmd.Run()

	// 2. Lancer (ou retrouver) la fenêtre Chromium en kiosk. Garde
	// anti-doublons et nettoyage du profil gérés par spawnChromiumKiosk
	// (voir hotplug.go), partagé avec le branchement à chaud.
	alreadyRunning, err := spawnChromiumKiosk(req.HDMI, pageURL)
	if err != nil {
		log.Printf("❌ /api/launch HDMI-%d : %v", req.HDMI, err)
		http.Error(w, err.Error(), 500)
		return
	}
	if alreadyRunning {
		log.Printf("↻ /api/launch HDMI-%d : déjà lancée, repositionnée", req.HDMI)
	} else {
		log.Printf("✅ /api/launch HDMI-%d positionné avec succès", req.HDMI)
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(fmt.Sprintf(
		`{"ok":true,"already_running":%t,"url":"%s","output":"%s","resolution":"%dx%d"}`,
		alreadyRunning, pageURL, outputName, width, height,
	)))
}

// handleMoveToHDMI — POST /api/move-to-hdmi {"output":"HDMI-A-2","title":"Possession"}
func handleMoveToHDMI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	var req struct {
		Output string `json:"output"`
		Title  string `json:"title"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.Output == "" {
		req.Output = "HDMI-A-2"
	}
	if req.Title == "" {
		req.Title = "Possession"
	}

	env := append(os.Environ(), waylandEnv()...)

	focusCmd := exec.Command("wlrctl", "window", "focus", "title:"+req.Title)
	focusCmd.Env = env
	focusCmd.Run()

	moveCmd := exec.Command("wlrctl", "window", "move", "output:"+req.Output)
	moveCmd.Env = env
	if err := moveCmd.Run(); err != nil {
		http.Error(w, fmt.Sprintf("wlrctl error: %v", err), 500)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(fmt.Sprintf(`{"ok":true,"output":"%s"}`, req.Output)))
}

// handleFullscreen — POST /api/fullscreen {"title":"Possession","output":"HDMI-A-2"}
func handleFullscreen(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	var req struct {
		Title  string `json:"title"`
		Output string `json:"output"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	env := append(os.Environ(), waylandEnv()...)

	focus := exec.Command("wlrctl", "window", "focus", "title:"+req.Title)
	focus.Env = env
	focus.Run()

	if req.Output != "" {
		move := exec.Command("wlrctl", "window", "move", "output:"+req.Output)
		move.Env = env
		move.Run()
	}

	fs := exec.Command("wlrctl", "window", "set", "fullscreen")
	fs.Env = env
	if err := fs.Run(); err != nil {
		http.Error(w, fmt.Sprintf("wlrctl error: %v", err), 500)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"ok":true}`))
}

// StartWebServer démarre le serveur HTTP et retourne une erreur en cas d'échec.
func StartWebServer(port int, sm *StateManager, store *Store) error {
	serverPort = port
	go watchHotplug()

	mux := http.NewServeMux()

	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		handleWebSocket(w, r, sm)
	})
	mux.HandleFunc("/api/teams", func(w http.ResponseWriter, r *http.Request) {
		handleTeams(w, r, store)
	})
	mux.HandleFunc("/api/matches", func(w http.ResponseWriter, r *http.Request) {
		handleMatches(w, r, store)
	})
	mux.HandleFunc("/api/config", func(w http.ResponseWriter, r *http.Request) {
		handleSaveConfig(w, r, sm)
	})
	mux.HandleFunc("/api/end-match", func(w http.ResponseWriter, r *http.Request) {
		handleEndMatch(w, r, sm, store)
	})
	mux.HandleFunc("/api/display-options", func(w http.ResponseWriter, r *http.Request) {
		handleDisplayOptions(w, r, sm)
	})
	mux.HandleFunc("/api/match-rules", func(w http.ResponseWriter, r *http.Request) {
		handleMatchRules(w, r, sm)
	})
	mux.HandleFunc("/api/ips", handleGetIPs)
	mux.HandleFunc("/api/screens", handleGetScreens)
	mux.HandleFunc("/api/move-to-hdmi", handleMoveToHDMI)
	mux.HandleFunc("/api/fullscreen", handleFullscreen)
	mux.HandleFunc("/api/display", handleSetDisplay)
	mux.HandleFunc("/api/launch", handleLaunchHDMI)
	mux.HandleFunc("/api/launch-table", handleLaunchTable)
	mux.HandleFunc("/api/shutdown", func(w http.ResponseWriter, r *http.Request) {
		handleShutdown(w, r, sm)
	})
	mux.HandleFunc("/api/shutdown/confirm", handleShutdownConfirm)
	mux.HandleFunc("/api/action", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", 405)
			return
		}
		var a ClientAction
		if err := json.NewDecoder(r.Body).Decode(&a); err != nil {
			http.Error(w, "bad request", 400)
			return
		}
		msg, _ := json.Marshal(a)
		handleClientAction(msg, sm)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	})
	mux.HandleFunc("/lancer.png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(lancerPNG)
	})
	mux.HandleFunc("/ball.png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(ballPNG)
	})
	mux.HandleFunc("/logo.png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(logoPNG)
	})
	mux.HandleFunc("/horn.mp3", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.Write(hornMP3)
	})
	mux.HandleFunc("/scoreboard", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(indexHTML)
	})
	mux.HandleFunc("/possession", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(possessionHTML)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(configHTML)
	})

	addr := fmt.Sprintf("0.0.0.0:%d", port)
	log.Printf("Écoute sur %s", addr)
	return http.ListenAndServe(addr, mux)
}

func handleWebSocket(w http.ResponseWriter, r *http.Request, sm *StateManager) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	ch := sm.Subscribe()
	defer sm.Unsubscribe(ch)

	initialState := sm.Get()
	fmt.Printf("🔌 Nouvelle connexion WS — home=%q away=%q\n", initialState.TeamHomeName, initialState.TeamAwayName)
	initial, _ := json.Marshal(initialState)
	conn.WriteMessage(websocket.TextMessage, initial)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			handleClientAction(msg, sm)
		}
	}()

	for {
		select {
		case <-done:
			return
		case data, ok := <-ch:
			if !ok {
				return
			}
			if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
				return
			}
		}
	}
}

type ClientAction struct {
	Action string `json:"action"`
	Value  int    `json:"value,omitempty"`
}

func handleClientAction(msg []byte, sm *StateManager) {
	var a ClientAction
	if err := json.Unmarshal(msg, &a); err != nil {
		return
	}

	if a.Action == "manual_horn" {
		// Pas une mutation d'état à retenir/rediffuser en continu : un
		// simple événement ponctuel, sur le même principe que
		// shutdown_request plus haut.
		hornMsg, _ := json.Marshal(map[string]string{"type": "horn"})
		sm.BroadcastRaw(hornMsg)
		return
	}

	sm.Update(func(s *GameState) {
		switch a.Action {
		case "score_home_inc":
			s.ScoreHome++
		case "score_home_dec":
			if s.ScoreHome > 0 {
				s.ScoreHome--
			}
		case "score_away_inc":
			s.ScoreAway++
		case "score_away_dec":
			if s.ScoreAway > 0 {
				s.ScoreAway--
			}
		case "toggle_timer":
			s.TimeRunning = !s.TimeRunning
			s.PossRunning = s.TimeRunning
		case "reset_game":
			s.GameTime = float64(s.MatchDuration)
			s.ScoreHome = 0
			s.ScoreAway = 0
			s.FoulsHome = 0
			s.FoulsAway = 0
			s.Period = 1
			s.TimeRunning = false
			s.PossRunning = false
			s.PossTime = s.PossMax
		case "game_time_adj":
			s.GameTime = clamp(s.GameTime+float64(a.Value), 0, float64(s.MatchDuration))
		case "toggle_poss":
			s.PossRunning = !s.PossRunning
		case "reset_poss":
			s.PossTime = s.PossMax
			s.PossRunning = s.TimeRunning
		case "poss_time_adj":
			s.PossTime = clamp(s.PossTime+float64(a.Value), 0, s.PossMax)
		case "fouls_home_inc":
			if s.FoulsHome < 10 {
				s.FoulsHome++
			}
		case "fouls_home_dec":
			if s.FoulsHome > 0 {
				s.FoulsHome--
			}
		case "fouls_away_inc":
			if s.FoulsAway < 10 {
				s.FoulsAway++
			}
		case "fouls_away_dec":
			if s.FoulsAway > 0 {
				s.FoulsAway--
			}
		}
	})
}
