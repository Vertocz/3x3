package main

import (
	"encoding/json"
	"log"
	"net/http"
)

// writeJSON écrit une réponse JSON avec le bon Content-Type et le bon code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("writeJSON: %v", err)
	}
}

func writeOK(w http.ResponseWriter) {
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"ok": false, "error": msg})
}

// requireMethod répond 405 et retourne false si la méthode ne correspond pas.
func requireMethod(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method != method {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return false
	}
	return true
}

// decodeJSON lit un corps JSON (limité à 1 Mo) dans v.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	return json.NewDecoder(r.Body).Decode(v)
}

// serveBytes sert un contenu embarqué avec un Content-Type et une politique
// de cache donnés.
func serveBytes(contentType, cache string, data []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", cache)
		w.Write(data)
	}
}
