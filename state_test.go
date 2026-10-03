package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// newTestManager crée un StateManager isolé (dossier de données temporaire).
func newTestManager(t *testing.T) *StateManager {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	return NewStateManager()
}

func TestScoreNeverNegative(t *testing.T) {
	sm := newTestManager(t)
	handleAction(ClientAction{Action: "score_home_dec"}, sm)
	handleAction(ClientAction{Action: "fouls_away_dec"}, sm)
	s := sm.Get()
	if s.ScoreHome != 0 || s.FoulsAway != 0 {
		t.Fatalf("valeurs négatives: %+v", s)
	}
	for i := 0; i < 15; i++ {
		handleAction(ClientAction{Action: "fouls_home_inc"}, sm)
	}
	if got := sm.Get().FoulsHome; got != 10 {
		t.Fatalf("fautes plafonnées à 10, obtenu %d", got)
	}
}

func TestTickUsesRealElapsedTime(t *testing.T) {
	sm := newTestManager(t)
	base := time.Now()
	sm.lastTick = base
	handleAction(ClientAction{Action: "toggle_timer"}, sm)

	// Un tick en retard (350 ms) doit retirer 350 ms, pas 100 ms.
	sm.tickAt(base.Add(350 * time.Millisecond))
	got := sm.Get().GameTime
	want := 600 - 0.35
	if got < want-1e-9 || got > want+1e-9 {
		t.Fatalf("GameTime = %v, attendu %v", got, want)
	}
}

func TestTimerStopsAtZero(t *testing.T) {
	sm := newTestManager(t)
	sm.Update(func(s *GameState) { s.GameTime = 0.05 })
	base := time.Now()
	sm.lastTick = base
	handleAction(ClientAction{Action: "toggle_timer"}, sm)
	sm.tickAt(base.Add(100 * time.Millisecond))
	s := sm.Get()
	if s.GameTime != 0 || s.TimeRunning || s.PossRunning {
		t.Fatalf("le chrono aurait dû s'arrêter à 0: %+v", s)
	}
}

func TestCannotStartTimerAtZero(t *testing.T) {
	sm := newTestManager(t)
	sm.Update(func(s *GameState) { s.GameTime = 0 })
	handleAction(ClientAction{Action: "toggle_timer"}, sm)
	if sm.Get().TimeRunning {
		t.Fatal("le chrono ne doit pas démarrer à 0")
	}
}

func TestTimerStopsWhenAdjustedToZeroWhileRunning(t *testing.T) {
	sm := newTestManager(t)
	base := time.Now()
	sm.lastTick = base
	handleAction(ClientAction{Action: "toggle_timer"}, sm)
	handleAction(ClientAction{Action: "game_time_adj", Value: -100000}, sm)
	sm.tickAt(base.Add(100 * time.Millisecond))
	if sm.Get().TimeRunning {
		t.Fatal("le chrono doit s'arrêter quand le temps est ramené à 0")
	}
}

func TestHugeTickGapIsCapped(t *testing.T) {
	sm := newTestManager(t)
	base := time.Now()
	sm.lastTick = base
	handleAction(ClientAction{Action: "toggle_timer"}, sm)
	sm.tickAt(base.Add(10 * time.Minute)) // process figé
	if got := sm.Get().GameTime; got < 600-maxTickDelta-1e-9 {
		t.Fatalf("un gel du process ne doit pas vider le chrono: %v", got)
	}
}

func TestPossessionResetFollowsTimer(t *testing.T) {
	sm := newTestManager(t)
	handleAction(ClientAction{Action: "toggle_timer"}, sm)
	sm.Update(func(s *GameState) { s.PossTime = 3 })
	handleAction(ClientAction{Action: "reset_poss"}, sm)
	s := sm.Get()
	if s.PossTime != s.PossMax || !s.PossRunning {
		t.Fatalf("reset_poss: %+v", s)
	}
}

func TestEndMatchRecordsAndResets(t *testing.T) {
	sm := newTestManager(t)
	store := LoadStore()
	handleAction(ClientAction{Action: "score_home_inc"}, sm)
	handleAction(ClientAction{Action: "score_home_inc"}, sm)
	handleAction(ClientAction{Action: "score_away_inc"}, sm)

	rec := httptest.NewRecorder()
	handleEndMatch(rec, httptest.NewRequest(http.MethodPost, "/api/end-match", nil), sm, store)
	if rec.Code != 200 {
		t.Fatalf("code %d", rec.Code)
	}
	ms := store.ListMatches()
	if len(ms) != 1 || ms[0].ScoreHome != 2 || ms[0].ScoreAway != 1 {
		t.Fatalf("historique: %+v", ms)
	}
	if s := sm.Get(); s.ScoreHome != 0 || s.ScoreAway != 0 {
		t.Fatalf("score non remis à zéro: %+v", s)
	}
}

func TestConfigPersistsAndIsValidated(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	sm := NewStateManager()

	post := func(path string, h http.HandlerFunc, body string) int {
		rec := httptest.NewRecorder()
		h(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
		return rec.Code
	}
	post("/api/config", func(w http.ResponseWriter, r *http.Request) { handleSaveConfig(w, r, sm) },
		`{"team_home_name":"  Les Aigles  ","team_home_color":"#112233","team_away_color":"pas-une-couleur","match_duration":420,"poss_max":99999}`)
	post("/api/match-rules", func(w http.ResponseWriter, r *http.Request) { handleMatchRules(w, r, sm) },
		`{"target_score":15,"win_margin":1}`)
	post("/api/display-options", func(w http.ResponseWriter, r *http.Request) { handleDisplayOptions(w, r, sm) },
		`{"show_dec_scoreboard":true,"show_dec_possession":true}`)

	s := sm.Get()
	if s.TeamHomeName != "Les Aigles" || s.TeamHomeColor != "#112233" {
		t.Fatalf("équipe: %+v", s)
	}
	if s.TeamAwayColor != "#457b9d" {
		t.Fatalf("couleur invalide acceptée: %q", s.TeamAwayColor)
	}
	if s.MatchDuration != 420 || s.PossMax != 12 {
		t.Fatalf("durée/possession: %d / %v", s.MatchDuration, s.PossMax)
	}

	sm.saveState() // synchrone, comme à l'arrêt du service

	// "Redémarrage" : un nouveau manager relit le disque.
	sm2 := NewStateManager()
	s2 := sm2.Get()
	if s2.TeamHomeName != "Les Aigles" || s2.MatchDuration != 420 || s2.GameTime != 420 {
		t.Fatalf("config non restaurée: %+v", s2)
	}
	if s2.TargetScore != 15 || s2.WinMargin != 1 || !s2.ShowDecScoreboard || !s2.ShowDecPossession {
		t.Fatalf("règles/options non restaurées: %+v", s2)
	}
	if s2.TimeRunning || s2.ScoreHome != 0 {
		t.Fatalf("le chrono/score ne doivent pas être restaurés: %+v", s2)
	}
}

func TestCorruptStateFileFallsBackToDefaults(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, "scoreboard-data")
	os.MkdirAll(dir, 0755)
	// Ancien format / valeurs absurdes : ne doit jamais donner un chrono à 0.
	os.WriteFile(filepath.Join(dir, "match_state.json"),
		[]byte(`{"match_duration":0,"poss_max":-5,"team_home_name":"","team_home_color":"x"}`), 0644)
	s := NewStateManager().Get()
	if s.MatchDuration != 600 || s.PossMax != 12 || s.GameTime != 600 || s.TeamHomeName != "ÉQUIPE 1" {
		t.Fatalf("valeurs par défaut attendues: %+v", s)
	}
}

func TestBroadcastKeepsNewestForSlowClient(t *testing.T) {
	sm := newTestManager(t)
	ch := sm.Subscribe()
	for i := 0; i < 50; i++ {
		sm.BroadcastRaw([]byte{byte(i)})
	}
	var last byte
	for len(ch) > 0 {
		last = (<-ch)[0]
	}
	if last != 49 {
		t.Fatalf("le dernier message reçu doit être le plus récent, obtenu %d", last)
	}
}

func TestStoreConcurrentAndAtomic(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	store := LoadStore()
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			store.UpsertTeam(Team{Name: "T" + string(rune('A'+i%5)), Color: "#ffffff"})
			store.AddMatch(MatchRecord{ID: newID(), Date: time.Now()})
			store.ListTeams()
			store.ListMatches()
		}(i)
	}
	wg.Wait()

	if n := len(store.ListTeams()); n != 5 {
		t.Fatalf("5 équipes distinctes attendues (dédoublonnage par nom), obtenu %d", n)
	}
	// Le fichier écrit doit être un JSON valide, sans résidu temporaire.
	data, err := os.ReadFile(dataFile())
	if err != nil {
		t.Fatal(err)
	}
	var back Store
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("fichier invalide: %v", err)
	}
	if len(back.Matches) != 30 {
		t.Fatalf("30 matchs attendus, obtenu %d", len(back.Matches))
	}
	entries, _ := os.ReadDir(dataDir())
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tmp-") {
			t.Fatalf("fichier temporaire oublié: %s", e.Name())
		}
	}
}

func TestCorruptTeamsFileIsKept(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	os.MkdirAll(dataDir(), 0755)
	os.WriteFile(dataFile(), []byte("{pas du json"), 0644)
	store := LoadStore()
	if len(store.ListTeams()) != 0 {
		t.Fatal("store vide attendu")
	}
	entries, _ := os.ReadDir(dataDir())
	found := false
	for _, e := range entries {
		if strings.Contains(e.Name(), ".corrupt-") {
			found = true
		}
	}
	if !found {
		t.Fatal("le fichier corrompu doit être conservé, pas écrasé")
	}
}

func TestTeamsAPIValidation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	store := LoadStore()
	do := func(method, target, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		handleTeams(rec, httptest.NewRequest(method, target, strings.NewReader(body)), store)
		return rec
	}
	if c := do("POST", "/api/teams", `{"name":"","color":"#ffffff"}`).Code; c != 400 {
		t.Fatalf("nom vide: %d", c)
	}
	if c := do("POST", "/api/teams", `{"name":"A","color":"red"}`).Code; c != 400 {
		t.Fatalf("couleur invalide: %d", c)
	}
	rec := do("POST", "/api/teams", `{"name":"  l'aigle \"x\" ","color":"#AABBCC"}`)
	if rec.Code != 200 {
		t.Fatalf("création: %d %s", rec.Code, rec.Body)
	}
	var tm Team
	json.Unmarshal(rec.Body.Bytes(), &tm)
	if tm.Name != `L'AIGLE "X"` || tm.ID == "" {
		t.Fatalf("équipe: %+v", tm)
	}
	if c := do("PATCH", "/api/teams", "").Code; c != 405 {
		t.Fatalf("méthode: %d", c)
	}
}

func TestCleanName(t *testing.T) {
	if got := cleanName("  a\x00b\n  "); got != "ab" {
		t.Fatalf("got %q", got)
	}
	long := strings.Repeat("é", 100)
	if n := len([]rune(cleanName(long))); n != maxNameRunes {
		t.Fatalf("longueur %d", n)
	}
}

func TestResolutionValidation(t *testing.T) {
	for _, ok := range []string{"1920x1080@60", "1280x720@59.94"} {
		if !resolutionRe.MatchString(ok) {
			t.Errorf("%q devrait être valide", ok)
		}
	}
	for _, bad := range []string{"--mode@1", "1920x1080", "1920x1080@60; rm", "1920x1080@"} {
		if resolutionRe.MatchString(bad) {
			t.Errorf("%q devrait être refusé", bad)
		}
	}
}

// ── Prolongation (règles 3x3) ───────────────────────────────────────────

// tieAtZero amène un match à 0:00 sur une égalité 14-14.
func tieAtZero(t *testing.T, sm *StateManager) {
	t.Helper()
	sm.Update(func(s *GameState) { s.ScoreHome, s.ScoreAway, s.GameTime = 14, 14, 0 })
}

func TestOvertimeOnlyOnTieAtZero(t *testing.T) {
	sm := newTestManager(t)

	// Temps non écoulé : refusé.
	sm.Update(func(s *GameState) { s.ScoreHome, s.ScoreAway = 5, 5 })
	handleAction(ClientAction{Action: "start_overtime"}, sm)
	if sm.Get().Overtime {
		t.Fatal("prolongation refusée tant qu'il reste du temps")
	}
	// Temps écoulé mais score différent : refusé.
	sm.Update(func(s *GameState) { s.GameTime, s.ScoreHome = 0, 6 })
	handleAction(ClientAction{Action: "start_overtime"}, sm)
	if sm.Get().Overtime {
		t.Fatal("prolongation refusée si pas d'égalité")
	}
	// Égalité à 0:00 : acceptée.
	tieAtZero(t, sm)
	handleAction(ClientAction{Action: "start_overtime"}, sm)
	s := sm.Get()
	if !s.Overtime || s.OvertimeBase != 14 || s.BreakTime != overtimeBreakSecs || s.PossRunning {
		t.Fatalf("démarrage de la prolongation: %+v", s)
	}
	// Un second appui ne relance rien.
	sm.Update(func(s *GameState) { s.BreakTime = 20 })
	handleAction(ClientAction{Action: "start_overtime"}, sm)
	if sm.Get().BreakTime != 20 {
		t.Fatal("start_overtime ne doit pas relancer la pause en cours")
	}
}

func TestOvertimeBreakCountsDownThenPossessionRuns(t *testing.T) {
	sm := newTestManager(t)
	tieAtZero(t, sm)
	handleAction(ClientAction{Action: "start_overtime"}, sm)

	base := time.Now()
	sm.lastTick = base
	sm.tickAt(base.Add(10 * time.Second)) // 10 s de pause écoulées (cap 2 s par tick)
	if got := sm.Get().BreakTime; got < overtimeBreakSecs-maxTickDelta-1e-9 || got >= overtimeBreakSecs {
		t.Fatalf("la pause doit décompter: %v", got)
	}

	// Pendant la pause, le bouton principal et la possession sont inertes.
	handleAction(ClientAction{Action: "toggle_timer"}, sm)
	handleAction(ClientAction{Action: "toggle_poss"}, sm)
	if sm.Get().PossRunning {
		t.Fatal("rien ne démarre pendant la pause")
	}

	// Fin de la pause (sautée) : le bouton principal pilote la possession
	// et ne touche jamais au chrono de jeu.
	handleAction(ClientAction{Action: "skip_break"}, sm)
	handleAction(ClientAction{Action: "toggle_timer"}, sm)
	s := sm.Get()
	if s.BreakTime != 0 || !s.PossRunning || s.TimeRunning || s.GameTime != 0 {
		t.Fatalf("après la pause: %+v", s)
	}
	// Reset possession : repart à 12 s et reste en marche.
	sm.Update(func(s *GameState) { s.PossTime = 3 })
	handleAction(ClientAction{Action: "reset_poss"}, sm)
	s = sm.Get()
	if s.PossTime != s.PossMax || !s.PossRunning {
		t.Fatalf("reset_poss en prolongation: %+v", s)
	}
	// Le chrono de jeu ne se règle plus.
	handleAction(ClientAction{Action: "game_time_adj", Value: 5}, sm)
	if sm.Get().GameTime != 0 {
		t.Fatal("pas de réglage du chrono de jeu en prolongation")
	}
}

func TestOvertimeCancelAndReset(t *testing.T) {
	sm := newTestManager(t)
	tieAtZero(t, sm)
	handleAction(ClientAction{Action: "start_overtime"}, sm)
	handleAction(ClientAction{Action: "cancel_overtime"}, sm)
	s := sm.Get()
	if s.Overtime || s.BreakTime != 0 || s.OvertimeBase != 0 || s.ScoreHome != 14 || s.GameTime != 0 {
		t.Fatalf("annulation: %+v", s)
	}
	handleAction(ClientAction{Action: "start_overtime"}, sm)
	handleAction(ClientAction{Action: "reset_game"}, sm)
	if s := sm.Get(); s.Overtime || s.BreakTime != 0 || s.ScoreHome != 0 {
		t.Fatalf("reset_game doit sortir de la prolongation: %+v", s)
	}
}

func TestEndMatchRecordsOvertime(t *testing.T) {
	sm := newTestManager(t)
	store := LoadStore()
	tieAtZero(t, sm)
	handleAction(ClientAction{Action: "start_overtime"}, sm)
	handleAction(ClientAction{Action: "score_home_inc"}, sm)
	handleAction(ClientAction{Action: "score_home_inc"}, sm)

	rec := httptest.NewRecorder()
	handleEndMatch(rec, httptest.NewRequest(http.MethodPost, "/api/end-match", nil), sm, store)
	ms := store.ListMatches()
	if len(ms) != 1 || !ms[0].Overtime || ms[0].ScoreHome != 16 || ms[0].ScoreAway != 14 {
		t.Fatalf("historique: %+v", ms)
	}
	if sm.Get().Overtime {
		t.Fatal("le tableau doit être remis à zéro")
	}
}

// ── Reprise d'un match après coupure de courant ─────────────────────────

func TestMatchSurvivesPowerCut(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	sm := NewStateManager()
	for i := 0; i < 7; i++ {
		handleAction(ClientAction{Action: "score_home_inc"}, sm)
	}
	for i := 0; i < 4; i++ {
		handleAction(ClientAction{Action: "score_away_inc"}, sm)
	}
	handleAction(ClientAction{Action: "fouls_home_inc"}, sm)
	handleAction(ClientAction{Action: "fouls_away_inc"}, sm)
	handleAction(ClientAction{Action: "fouls_away_inc"}, sm)
	base := time.Now()
	sm.lastTick = base
	handleAction(ClientAction{Action: "toggle_timer"}, sm) // chrono en marche au moment de la coupure
	sm.tickAt(base.Add(1500 * time.Millisecond))
	want := sm.Get()
	sm.saveState() // dernier enregistrement avant la coupure

	sm2 := NewStateManager() // redémarrage du Pi
	got := sm2.Get()
	if got.ScoreHome != 7 || got.ScoreAway != 4 || got.FoulsHome != 1 || got.FoulsAway != 2 {
		t.Fatalf("score/fautes non restaurés: %+v", got)
	}
	if got.GameTime != want.GameTime || got.PossTime != want.PossTime {
		t.Fatalf("temps non restaurés: got %v/%v want %v/%v", got.GameTime, got.PossTime, want.GameTime, want.PossTime)
	}
	if got.TimeRunning || got.PossRunning {
		t.Fatal("les chronos doivent rester à l'arrêt après une coupure")
	}
}

func TestOvertimeSurvivesPowerCut(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	sm := NewStateManager()
	tieAtZero(t, sm)
	handleAction(ClientAction{Action: "start_overtime"}, sm)
	handleAction(ClientAction{Action: "skip_break"}, sm)
	handleAction(ClientAction{Action: "score_away_inc"}, sm)
	sm.saveState()
	got := NewStateManager().Get()
	if !got.Overtime || got.OvertimeBase != 14 || got.ScoreAway != 15 || got.BreakTime != 0 || got.GameTime != 0 {
		t.Fatalf("prolongation non restaurée: %+v", got)
	}
}

func TestCleanBoardLeavesNoMatchInFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	sm := NewStateManager()
	handleAction(ClientAction{Action: "score_home_inc"}, sm)
	sm.saveState()
	handleAction(ClientAction{Action: "reset_game"}, sm)
	sm.saveState()
	data, _ := os.ReadFile(filepath.Join(home, "scoreboard-data", "match_state.json"))
	if strings.Contains(string(data), `"match"`) {
		t.Fatalf("un tableau à zéro ne doit laisser aucun match en cours: %s", data)
	}
	got := NewStateManager().Get()
	if got.ScoreHome != 0 || got.GameTime != float64(got.MatchDuration) {
		t.Fatalf("état propre attendu: %+v", got)
	}
}

func TestAbsurdSavedMatchIsClamped(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, "scoreboard-data")
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "match_state.json"), []byte(
		`{"match_duration":600,"poss_max":12,"match":{"score_home":-5,"score_away":99999,"fouls_home":50,"game_time":99999,"poss_time":-3}}`), 0644)
	s := NewStateManager().Get()
	if s.ScoreHome != 0 || s.ScoreAway != maxTargetScore || s.FoulsHome != maxFouls || s.GameTime != 600 || s.PossTime != 0 {
		t.Fatalf("valeurs non bornées: %+v", s)
	}
}
