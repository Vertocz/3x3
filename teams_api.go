package main

import (
	"encoding/json"
	"math/rand"
	"net/http"
	"sort"
	"strings"
)

// handleTeams gère toutes les requêtes sur /api/teams
func handleTeams(w http.ResponseWriter, r *http.Request, store *Store) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {

	// GET /api/teams → liste toutes les équipes
	case http.MethodGet:
		json.NewEncoder(w).Encode(store.Teams)

	// POST /api/teams → créer ou mettre à jour une équipe
	case http.MethodPost:
		var t Team
		if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
			http.Error(w, "bad request", 400)
			return
		}
		if t.Name == "" {
			http.Error(w, "nom requis", 400)
			return
		}
		if t.ID == "" {
			t.ID = newID()
		}
		t.Name = strings.ToUpper(strings.TrimSpace(t.Name))
		store.UpsertTeam(t)
		if err := store.Save(); err != nil {
			w.WriteHeader(500)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(t)

	// DELETE /api/teams?id=xxx → supprimer une équipe
	case http.MethodDelete:
		id := r.URL.Query().Get("id")
		if id == "" {
			http.Error(w, "id requis", 400)
			return
		}
		store.DeleteTeam(id)
		if err := store.Save(); err != nil {
			w.WriteHeader(500)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		w.Write([]byte(`{"ok":true}`))

	default:
		http.Error(w, "method not allowed", 405)
	}
}

// handleMatches gère les requêtes sur /api/matches (historique des scores)
func handleMatches(w http.ResponseWriter, r *http.Request, store *Store) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {

	// GET /api/matches → liste tous les matchs enregistrés, du plus récent
	// au plus ancien (le tri chronologique se fait ici, pas côté front).
	case http.MethodGet:
		sorted := make([]MatchRecord, len(store.Matches))
		copy(sorted, store.Matches)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i].Date.After(sorted[j].Date) })
		json.NewEncoder(w).Encode(sorted)

	// DELETE /api/matches?id=xxx → supprimer un match de l'historique
	// DELETE /api/matches?all=1  → vider tout l'historique
	case http.MethodDelete:
		if r.URL.Query().Get("all") == "1" {
			store.Matches = []MatchRecord{}
			if err := store.Save(); err != nil {
				w.WriteHeader(500)
				json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
				return
			}
			w.Write([]byte(`{"ok":true}`))
			return
		}
		id := r.URL.Query().Get("id")
		if id == "" {
			http.Error(w, "id requis", 400)
			return
		}
		store.DeleteMatch(id)
		if err := store.Save(); err != nil {
			w.WriteHeader(500)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		w.Write([]byte(`{"ok":true}`))

	default:
		http.Error(w, "method not allowed", 405)
	}
}

// newID génère un ID court aléatoire (Go 1.20+ : rand global auto-seedé).
func newID() string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 8)
	for i := range b {
		b[i] = chars[rand.Intn(len(chars))]
	}
	return string(b)
}
