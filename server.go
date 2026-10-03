package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

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

// Polices embarquées (Inconsolata, licence OFL) : le Pi sert son propre
// hotspot Wi-Fi, donc sans accès Internet. Avec Google Fonts, la police ne
// se chargeait jamais et la feuille de style externe pouvait retarder
// l'affichage de plusieurs secondes au démarrage.
//
//go:embed static/fonts
var fontsFS embed.FS

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

const (
	wsWriteWait = 5 * time.Second
	wsPongWait  = 45 * time.Second
	wsPingEvery = 20 * time.Second
)

// chromiumLaunches suit, par numéro de HDMI (1 ou 2), le process Chromium
// lancé par /api/launch. Objectif : éviter d'empiler plusieurs fenêtres sur
// le même écran (double clic sur "lancer le tableau", ou auto-lancement au
// branchement à chaud) — chaque instance Chromium supplémentaire coûte cher
// en RAM sur un Raspberry Pi.
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
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var cfg struct {
		HDMI1 string `json:"hdmi1"`
		HDMI2 string `json:"hdmi2"`
	}
	if err := decodeJSON(w, r, &cfg); err != nil {
		writeErr(w, 400, "bad request")
		return
	}

	apply := func(output, res string) error {
		if res == "" {
			return nil
		}
		if !resolutionRe.MatchString(res) {
			return fmt.Errorf("résolution invalide pour %s: %q", output, res)
		}
		parts := strings.Split(res, "@")
		return runWayland(5*time.Second, "wlr-randr", "--output", output, "--mode", parts[0], "--refresh", parts[1])
	}

	var errs []string
	if err := apply("HDMI-A-1", cfg.HDMI1); err != nil {
		errs = append(errs, err.Error())
	}
	if err := apply("HDMI-A-2", cfg.HDMI2); err != nil {
		errs = append(errs, err.Error())
	}
	if len(errs) > 0 {
		writeErr(w, 500, strings.Join(errs, " ; "))
		return
	}
	writeOK(w)
}

// shutdownNow sauvegarde la configuration puis éteint le Pi.
func shutdownNow(sm *StateManager) {
	sm.saveState()
	if out, err := exec.Command("sudo", "shutdown", "-h", "now").CombinedOutput(); err != nil {
		log.Printf("❌ shutdown impossible: %v (%s)", err, strings.TrimSpace(string(out)))
	}
}

// handleShutdown — appelé par buttons.py (bouton power physique).
// force=false : broadcast WS → modale de confirmation sur toutes les pages.
// force=true : shutdown immédiat (appui long >= 3s).
func handleShutdown(w http.ResponseWriter, r *http.Request, sm *StateManager) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var body struct {
		Force bool `json:"force"`
	}
	decodeJSON(w, r, &body) // corps vide = force=false
	writeOK(w)

	if body.Force {
		log.Println("Shutdown immédiat (appui long)")
		go shutdownNow(sm)
		return
	}
	msg, _ := json.Marshal(map[string]string{"type": "shutdown_request"})
	sm.BroadcastRaw(msg)
	log.Println("shutdown_request broadcasté")
}

// handleShutdownConfirm — appelé par la page web après confirmation utilisateur.
func handleShutdownConfirm(w http.ResponseWriter, r *http.Request, sm *StateManager) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	log.Println("Shutdown confirmé (interface web)")
	writeOK(w)
	go shutdownNow(sm)
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
	mux.HandleFunc("/api/shutdown/confirm", func(w http.ResponseWriter, r *http.Request) {
		handleShutdownConfirm(w, r, sm)
	})
	mux.HandleFunc("/api/action", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodPost) {
			return
		}
		var a ClientAction
		if err := decodeJSON(w, r, &a); err != nil {
			writeErr(w, 400, "bad request")
			return
		}
		handleAction(a, sm)
		writeOK(w)
	})

	// Fichiers statiques embarqués (images, son, polices : cache long).
	const assetCache = "public, max-age=86400"
	mux.HandleFunc("/lancer.png", serveBytes("image/png", assetCache, lancerPNG))
	mux.HandleFunc("/ball.png", serveBytes("image/png", assetCache, ballPNG))
	mux.HandleFunc("/logo.png", serveBytes("image/png", assetCache, logoPNG))
	mux.HandleFunc("/horn.mp3", serveBytes("audio/mpeg", assetCache, hornMP3))
	if sub, err := fs.Sub(fontsFS, "static/fonts"); err == nil {
		fonts := http.StripPrefix("/fonts/", http.FileServer(http.FS(sub)))
		mux.Handle("/fonts/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "public, max-age=604800")
			fonts.ServeHTTP(w, r)
		}))
	}

	// Pages : pas de cache, pour qu'une mise à jour du binaire soit visible
	// au prochain rechargement.
	const pageCache = "no-cache"
	const htmlType = "text/html; charset=utf-8"
	mux.HandleFunc("/scoreboard", serveBytes(htmlType, pageCache, indexHTML))
	mux.HandleFunc("/possession", serveBytes(htmlType, pageCache, possessionHTML))
	page := serveBytes(htmlType, pageCache, configHTML)
	mux.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		page(w, r)
	})

	addr := fmt.Sprintf("0.0.0.0:%d", port)
	log.Printf("Écoute sur %s", addr)
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	return srv.ListenAndServe()
}

func handleWebSocket(w http.ResponseWriter, r *http.Request, sm *StateManager) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	// Détection des connexions mortes (écran HDMI débranché, Wi-Fi coupé,
	// onglet gelé) : sans ping/pong, la goroutine et la file du client
	// restaient en mémoire indéfiniment.
	conn.SetReadLimit(4096)
	conn.SetReadDeadline(time.Now().Add(wsPongWait))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(wsPongWait))
		return nil
	})

	// S'abonner AVANT de lire l'état initial : aucun changement ne peut
	// se glisser entre les deux.
	ch := sm.Subscribe()
	defer sm.Unsubscribe(ch)

	write := func(msgType int, data []byte) error {
		conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
		return conn.WriteMessage(msgType, data)
	}

	initialState := sm.Get()
	log.Printf("🔌 WS connecté (%s) — home=%q away=%q", r.RemoteAddr, initialState.TeamHomeName, initialState.TeamAwayName)
	initial, _ := json.Marshal(initialState)
	if err := write(websocket.TextMessage, initial); err != nil {
		return
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			conn.SetReadDeadline(time.Now().Add(wsPongWait))
			handleClientAction(msg, sm)
		}
	}()

	ping := time.NewTicker(wsPingEvery)
	defer ping.Stop()

	for {
		select {
		case <-done:
			return
		case data, ok := <-ch:
			if !ok {
				return
			}
			if err := write(websocket.TextMessage, data); err != nil {
				return
			}
		case <-ping.C:
			if err := write(websocket.PingMessage, nil); err != nil {
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
	handleAction(a, sm)
}

// handleAction applique une action (bouton physique, page web ou clavier).
func handleAction(a ClientAction, sm *StateManager) {
	if a.Action == "manual_horn" {
		// Pas une mutation d'état à retenir/rediffuser en continu : un
		// simple événement ponctuel, sur le même principe que
		// shutdown_request.
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
			if s.Overtime {
				// En prolongation il n'y a plus de chrono de jeu : le bouton
				// principal lance/arrête le chrono de possession. Rien pendant
				// la pause d'une minute.
				if s.BreakTime > 0 {
					return
				}
				if !s.PossRunning && s.PossTime <= 0 {
					return
				}
				s.PossRunning = !s.PossRunning
				return
			}
			if !s.TimeRunning && s.GameTime <= 0 {
				return // rien à faire démarrer : le temps est écoulé
			}
			s.TimeRunning = !s.TimeRunning
			s.PossRunning = s.TimeRunning
		case "reset_game":
			s.resetMatch()
		case "game_time_adj":
			if s.Overtime {
				return // pas de chrono de jeu en prolongation
			}
			s.GameTime = clamp(s.GameTime+float64(a.Value), 0, float64(s.MatchDuration))
		case "toggle_poss":
			if s.Overtime && s.BreakTime > 0 {
				return
			}
			if !s.PossRunning && s.PossTime <= 0 {
				return
			}
			s.PossRunning = !s.PossRunning
		case "reset_poss":
			s.PossTime = s.PossMax
			if !s.Overtime {
				s.PossRunning = s.TimeRunning
			}
			// En prolongation : le chrono de possession garde son état (en
			// marche → repart à 12 s, à l'arrêt → reste à l'arrêt).
		case "poss_time_adj":
			s.PossTime = clamp(s.PossTime+float64(a.Value), 0, s.PossMax)
		case "fouls_home_inc":
			if s.FoulsHome < maxFouls {
				s.FoulsHome++
			}
		case "fouls_home_dec":
			if s.FoulsHome > 0 {
				s.FoulsHome--
			}
		case "fouls_away_inc":
			if s.FoulsAway < maxFouls {
				s.FoulsAway++
			}
		case "fouls_away_dec":
			if s.FoulsAway > 0 {
				s.FoulsAway--
			}

		// ── Prolongation ─────────────────────────────────────────────
		case "start_overtime":
			// Uniquement si le temps est écoulé sur une égalité : le bouton
			// n'existe d'ailleurs à l'écran que dans ce cas.
			if s.Overtime || s.TimeRunning || s.GameTime > 0 || s.ScoreHome != s.ScoreAway {
				return
			}
			s.Overtime = true
			s.OvertimeBase = s.ScoreHome
			s.BreakTime = overtimeBreakSecs
			s.PossTime = s.PossMax
			s.PossRunning = false
		case "skip_break":
			if s.Overtime {
				s.BreakTime = 0
			}
		case "cancel_overtime":
			// Annulation en cas de fausse manœuvre : retour à l'égalité à 0:00.
			s.Overtime = false
			s.OvertimeBase = 0
			s.BreakTime = 0
			s.PossRunning = false
		}
	})
	sm.requestSave() // mémorise le match en cours (reprise après coupure)
}
