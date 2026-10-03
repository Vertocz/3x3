package main

import (
	"log"
	"math/rand"
	"net/http"
	"strings"
)

// handleTeams gère toutes les requêtes sur /api/teams
func handleTeams(w http.ResponseWriter, r *http.Request, store *Store) {
	switch r.Method {

	// GET /api/teams → liste toutes les équipes
	case http.MethodGet:
		writeJSON(w, 200, store.ListTeams())

	// POST /api/teams → créer ou mettre à jour une équipe
	case http.MethodPost:
		var t Team
		if err := decodeJSON(w, r, &t); err != nil {
			writeErr(w, 400, "bad request")
			return
		}
		t.Name = cleanName(strings.ToUpper(t.Name))
		if t.Name == "" {
			writeErr(w, 400, "nom requis")
			return
		}
		if !isHexColor(t.Color) {
			writeErr(w, 400, "couleur invalide (attendu #rrggbb)")
			return
		}
		saved, err := store.UpsertTeam(t)
		if err != nil {
			log.Printf("⚠️  Sauvegarde équipe: %v", err)
			writeErr(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, saved)

	// DELETE /api/teams?id=xxx → supprimer une équipe
	case http.MethodDelete:
		id := r.URL.Query().Get("id")
		if id == "" {
			writeErr(w, 400, "id requis")
			return
		}
		if err := store.DeleteTeam(id); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		writeOK(w)

	default:
		writeErr(w, 405, "method not allowed")
	}
}

// handleMatches gère les requêtes sur /api/matches (historique des scores)
func handleMatches(w http.ResponseWriter, r *http.Request, store *Store) {
	switch r.Method {

	// GET /api/matches → liste tous les matchs enregistrés, du plus récent
	// au plus ancien (le tri chronologique se fait côté serveur).
	case http.MethodGet:
		writeJSON(w, 200, store.ListMatches())

	// DELETE /api/matches?id=xxx → supprimer un match de l'historique
	// DELETE /api/matches?all=1  → vider tout l'historique
	case http.MethodDelete:
		if r.URL.Query().Get("all") == "1" {
			if err := store.ClearMatches(); err != nil {
				writeErr(w, 500, err.Error())
				return
			}
			writeOK(w)
			return
		}
		id := r.URL.Query().Get("id")
		if id == "" {
			writeErr(w, 400, "id requis")
			return
		}
		if err := store.DeleteMatch(id); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		writeOK(w)

	default:
		writeErr(w, 405, "method not allowed")
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
