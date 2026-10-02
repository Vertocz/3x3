package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"
)

// serverPort est renseigné par StartWebServer au démarrage, pour que les
// lancements Chromium déclenchés en dehors d'une requête HTTP (branchement
// à chaud) sachent quelle URL locale viser.
var serverPort = 8000

// launchArmed passe à true dès qu'on a tenté un lancement au moins une fois
// (bouton "lancer le tableau" → /api/launch-table, ou appel direct à
// /api/launch). Tant qu'il est à false, brancher un câble ne déclenche
// rien tout seul : on évite que les fenêtres s'ouvrent automatiquement sur
// un vidéoprojecteur avant que la table soit prête pour un match.
var launchArmedMu sync.Mutex
var launchArmed bool

func armLaunch() {
	launchArmedMu.Lock()
	launchArmed = true
	launchArmedMu.Unlock()
}

func isLaunchArmed() bool {
	launchArmedMu.Lock()
	defer launchArmedMu.Unlock()
	return launchArmed
}

// chromiumAppID donne un identifiant stable (--class) par HDMI, pour
// pouvoir retrouver la fenêtre avec wlrctl sans dépendre du <title> de la
// page (qu'on ne maîtrise pas ici, static/ n'étant pas dans ce lot).
func chromiumAppID(hdmi int) string {
	return fmt.Sprintf("chromium-hdmi%d", hdmi)
}

// spawnChromiumKiosk lance (ou retrouve) la fenêtre Chromium en kiosk pour
// l'output HDMI donné : garde anti-doublons + nettoyage du profil avant
// chaque (re)lancement, PUIS positionne réellement la fenêtre sur le bon
// écran et la passe en plein écran (wlrctl) — c'est cette dernière étape
// qui manquait : sans elle, Chromium s'ouvre là où le compositeur décide
// par défaut, jamais forcément sur le bon HDMI, que l'écran soit branché
// au démarrage ou après. Partagé entre /api/launch et le branchement à chaud.
func spawnChromiumKiosk(hdmi int, pageURL string) (alreadyRunning bool, err error) {
	appID := chromiumAppID(hdmi)
	outputName := fmt.Sprintf("HDMI-A-%d", hdmi)

	chromiumLaunches.mu.Lock()
	if existing, ok := chromiumLaunches.procs[hdmi]; ok && processAlive(existing) {
		chromiumLaunches.mu.Unlock()
		// La fenêtre existe déjà : on ne relance pas Chromium, mais on
		// s'assure qu'elle est toujours bien placée (au cas où elle aurait
		// été déplacée manuellement entre-temps).
		if err := placeWindowOnOutput(appID, outputName); err != nil {
			log.Printf("Repositionnement de %s sur %s : %v", appID, outputName, err)
		}
		return true, nil
	}
	chromiumLaunches.mu.Unlock()

	profileDir := fmt.Sprintf("/tmp/chromium-hdmi%d", hdmi)
	os.RemoveAll(profileDir)

	cmd := exec.Command("chromium",
		"--noerrdialogs",
		"--disable-infobars",
		"--disable-session-crashed-bubble",
		"--disable-features=Translate,TranslateUI",
		"--disable-translate",
		"--autoplay-policy=no-user-gesture-required",
		"--password-store=basic",
		"--no-first-run",
		"--disable-restore-session-state",
		"--ozone-platform=wayland",
		"--enable-features=UseOzonePlatform",
		"--lang=fr",
		"--class="+appID,
		fmt.Sprintf("--user-data-dir=%s", profileDir),
		pageURL,
	)
	cmd.Env = append(os.Environ(), waylandEnv()...)

	if err := cmd.Start(); err != nil {
		return false, fmt.Errorf("impossible de lancer chromium: %w", err)
	}

	chromiumLaunches.mu.Lock()
	chromiumLaunches.procs[hdmi] = cmd.Process
	chromiumLaunches.mu.Unlock()

	launchedProc := cmd.Process
	go func() {
		cmd.Wait()
		chromiumLaunches.mu.Lock()
		if chromiumLaunches.procs[hdmi] == launchedProc {
			delete(chromiumLaunches.procs, hdmi)
		}
		chromiumLaunches.mu.Unlock()
	}()

	// La fenêtre met un instant à apparaître côté compositeur : on attend
	// qu'elle soit réellement mappée (wlrctl "waitfor") plutôt qu'un sleep
	// fixe, qui serait soit trop court (course), soit trop long (attente
	// inutile). 10s de plafond pour ne jamais bloquer indéfiniment si
	// Chromium plante avant d'afficher quoi que ce soit.
	if err := waitForWindow(appID, 10*time.Second); err != nil {
		return false, fmt.Errorf("la fenêtre chromium (%s) n'est jamais apparue: %w", appID, err)
	}

	if err := placeWindowOnOutput(appID, outputName); err != nil {
		return false, fmt.Errorf("impossible de positionner la fenêtre sur %s: %w", outputName, err)
	}

	return false, nil
}

// waitForWindow bloque jusqu'à ce qu'une fenêtre avec cet app_id apparaisse,
// avec un délai maximum pour ne jamais bloquer indéfiniment.
func waitForWindow(appID string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "wlrctl", "window", "waitfor", "app_id:"+appID)
	cmd.Env = append(os.Environ(), waylandEnv()...)
	return cmd.Run()
}

// placeWindowOnOutput reprend exactement la séquence déjà utilisée par
// /api/move-to-hdmi + /api/fullscreen (focus → move → set fullscreen),
// mais en ciblant la fenêtre par app_id (fiable, sous notre contrôle)
// plutôt que par le <title> de la page (qu'on ne connaît pas ici).
func placeWindowOnOutput(appID, outputName string) error {
	env := append(os.Environ(), waylandEnv()...)

	// IMPORTANT : wlrctl ne sait PAS déplacer une fenêtre vers un output
	// précis — "window move output:X" n'existe pas dans son jeu d'actions
	// (seulement minimize/maximize/fullscreen/focus/find/wait/waitfor).
	// Le placement sur le bon écran est donc délégué à labwc lui-même,
	// via une <windowRule identifier="..."> dans rc.xml qui exécute
	// "MoveToOutput" + "ToggleFullscreen" automatiquement dès que la
	// fenêtre apparaît (voir install_window_rules.sh). Ici, on se
	// contente de :
	//   1. focus (au cas où la règle labwc n'aurait pas mis le focus,
	//      p.ex. pour un repositionnement ultérieur) ;
	//   2. fullscreen, avec la bonne syntaxe wlrctl : l'action est
	//      "fullscreen" directement (pas "set fullscreen"), et on cible
	//      explicitement la fenêtre par app_id plutôt que de compter sur
	//      un focus implicite.
	//
	// Note : la windowRule ne se déclenche qu'au *premier* affichage
	// (first map) de la fenêtre. Si jamais quelqu'un déplace la fenêtre
	// manuellement ensuite, ce code ne peut pas la replacer sur le bon
	// écran (wlrctl n'en est pas capable) — seul un relancement de
	// Chromium (donc un nouveau "first map") redéclenchera la règle.

	focus := exec.Command("wlrctl", "window", "focus", "app_id:"+appID)
	focus.Env = env
	if err := focus.Run(); err != nil {
		return fmt.Errorf("focus: %w", err)
	}

	var fsErr error
	for attempt := 0; attempt < 6; attempt++ {
		fs := exec.Command("wlrctl", "window", "fullscreen", "app_id:"+appID)
		fs.Env = env
		fsErr = fs.Run()
		if fsErr == nil {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if fsErr != nil {
		return fmt.Errorf("fullscreen: %w", fsErr)
	}
	return nil
}

// launchOnHDMIIfConnected applique la résolution native et lance Chromium
// sur l'output HDMI donné, à condition qu'il soit actuellement détecté par
// wlr-randr. Retourne une erreur sinon — c'est watchHotplug qui se chargera
// de le lancer plus tard, dès qu'il sera effectivement branché.
func launchOnHDMIIfConnected(hdmi int) error {
	outputName := fmt.Sprintf("HDMI-A-%d", hdmi)

	var target *ScreenInfo
	for _, s := range parseScreens() {
		s := s
		if s.Name == outputName {
			target = &s
			break
		}
	}
	if target == nil {
		return fmt.Errorf("output %s non branché", outputName)
	}

	pageURL := fmt.Sprintf("http://localhost:%d/scoreboard", serverPort)
	if hdmi == 2 {
		pageURL = fmt.Sprintf("http://localhost:%d/possession", serverPort)
	}

	modeStr := fmt.Sprintf("%dx%d", target.Width, target.Height)
	refreshInt := "60"
	if f, err := strconv.ParseFloat(target.RefreshHz, 64); err == nil {
		refreshInt = strconv.Itoa(int(f + 0.5))
	}
	randrCmd := exec.Command("wlr-randr", "--output", outputName, "--mode", modeStr, "--refresh", refreshInt)
	randrCmd.Env = append(os.Environ(), waylandEnv()...)
	randrCmd.Run()

	_, err := spawnChromiumKiosk(hdmi, pageURL)
	return err
}

// watchHotplug surveille les sorties HDMI toutes les 2 secondes et lance
// automatiquement Chromium sur un écran qui vient d'être branché — mais
// seulement une fois la table "armée" (cf. armLaunch). On ne réagit qu'aux
// vraies transitions débranché→branché, pas à un écran déjà branché
// (sinon on rappellerait /api/launch en boucle sans raison).
func watchHotplug() {
	knownConnected := map[int]bool{1: false, 2: false}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		nowConnected := map[int]bool{1: false, 2: false}
		for _, s := range parseScreens() {
			if s.HDMI == 1 || s.HDMI == 2 {
				nowConnected[s.HDMI] = true
			}
		}

		if isLaunchArmed() {
			for hdmi := 1; hdmi <= 2; hdmi++ {
				if nowConnected[hdmi] && !knownConnected[hdmi] {
					log.Printf("Branchement détecté sur HDMI-%d, lancement automatique", hdmi)
					if err := launchOnHDMIIfConnected(hdmi); err != nil {
						log.Printf("Échec du lancement automatique sur HDMI-%d: %v", hdmi, err)
					}
				}
			}
		}

		// Toujours mis à jour, même non armé, pour ne pas confondre au
		// moment de l'armement un écran déjà branché avec un nouveau
		// branchement.
		knownConnected = nowConnected
	}
}

// handleLaunchTable — POST /api/launch-table
// Point d'entrée pensé pour le bouton "lancer le tableau" : arme le
// branchement à chaud puis lance immédiatement Chromium sur les écrans
// HDMI déjà branchés. Un écran pas encore branché sera lancé tout seul dès
// qu'on le connectera (cf. watchHotplug).
func handleLaunchTable(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}

	armLaunch()
	log.Println("📥 /api/launch-table appelé")

	type outputStatus struct {
		HDMI      int    `json:"hdmi"`
		Connected bool   `json:"connected"`
		Started   bool   `json:"started"`
		Error     string `json:"error,omitempty"`
	}
	var results []outputStatus

	connected := map[int]bool{}
	for _, s := range parseScreens() {
		if s.HDMI == 1 || s.HDMI == 2 {
			connected[s.HDMI] = true
		}
	}

	for hdmi := 1; hdmi <= 2; hdmi++ {
		status := outputStatus{HDMI: hdmi, Connected: connected[hdmi]}
		if connected[hdmi] {
			if err := launchOnHDMIIfConnected(hdmi); err != nil {
				status.Error = err.Error()
				log.Printf("❌ HDMI-%d : %v", hdmi, err)
			} else {
				status.Started = true
				log.Printf("✅ HDMI-%d positionné avec succès", hdmi)
			}
		} else {
			log.Printf("⏭️  HDMI-%d non branché, ignoré", hdmi)
		}
		results = append(results, status)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":      true,
		"armed":   true,
		"outputs": results,
	})
}
