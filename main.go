package main

import (
	"fmt"
	"log"
	"time"
)

func main() {
	store := LoadStore()
	sm := NewStateManager()

	// Tick toutes les 100ms
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for range ticker.C {
			sm.Tick()
		}
	}()

	port := 8000
	fmt.Printf("Serveur lancé sur http://0.0.0.0:%d\n", port)
	log.Fatal(StartWebServer(port, sm, store))
}
