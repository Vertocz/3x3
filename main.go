package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	store := LoadStore()
	sm := NewStateManager()

	// Tick toutes les 100ms. Le temps retiré aux chronos est le temps réel
	// écoulé (voir tickAt), pas 100 ms fixes : pas de dérive sous charge.
	go func() {
		ticker := time.NewTicker(tickInterval)
		defer ticker.Stop()
		for range ticker.C {
			sm.Tick()
		}
	}()

	// À l'arrêt du service (mise à jour, redémarrage, extinction) : on
	// sauvegarde la configuration avant de quitter.
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
		s := <-sig
		log.Printf("Signal %v reçu, sauvegarde puis arrêt", s)
		sm.saveState()
		os.Exit(0)
	}()

	port := 8000
	fmt.Printf("Serveur lancé sur http://0.0.0.0:%d\n", port)
	log.Fatal(StartWebServer(port, sm, store))
}
