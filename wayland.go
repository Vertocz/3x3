package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// runWayland exécute une commande dans l'environnement Wayland avec un délai
// maximum. Utilisé uniquement par /api/display (réglage de résolution), pas
// par la chaîne d'ouverture des fenêtres Chromium.
func runWayland(timeout time.Duration, name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), waylandEnv()...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("%s: délai dépassé (%s)", name, timeout)
		}
		if msg := strings.TrimSpace(string(out)); msg != "" {
			return fmt.Errorf("%s: %w (%s)", name, err, msg)
		}
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}
