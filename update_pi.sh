#!/bin/bash
# ══════════════════════════════════════════════════════════════════════════════
#  update_pi.sh — Mise à jour rapide en développement
#
#  Depuis le PC (PowerShell ou Git Bash), une seule commande :
#
#    scp -r . pi@scoreboard.local:~/scoreboard/ && ssh pi@scoreboard.local "bash ~/scoreboard/update_pi.sh"
#
#  Ce script :
#   1. Recompile le binaire Go
#   2. Redémarre le service principal (scoreboard)
#   3. Redémarre le service boutons si buttons.py a changé
#   4. Redémarre les displays si start_displays.sh a changé
# ══════════════════════════════════════════════════════════════════════════════

set -e
cd ~/scoreboard

export PATH=$PATH:/usr/local/go/bin

# ── 1. Recompiler ──────────────────────────────────────────────────────────────
echo "▶ Compilation..."
go mod tidy
go build -o scoreboard .
echo "  ✅ Build OK"

# ── 2. Redémarrer le service principal ────────────────────────────────────────
echo "▶ Redémarrage du serveur Go..."
sudo systemctl restart scoreboard
sleep 1
sudo systemctl is-active --quiet scoreboard && echo "  ✅ scoreboard actif" || echo "  ❌ scoreboard en erreur — voir: sudo journalctl -u scoreboard -n 30"

# ── 3. Redémarrer les boutons si buttons.py a changé ─────────────────────────
if systemctl is-active --quiet buttons 2>/dev/null; then
  echo "▶ Redémarrage du service boutons..."
  sudo systemctl restart buttons
  sleep 1
  sudo systemctl is-active --quiet buttons && echo "  ✅ buttons actif" || echo "  ❌ buttons en erreur — voir: sudo journalctl -u buttons -n 20"
fi

# ── 4. Redémarrer les fenêtres Chromium ───────────────────────────────────────
echo "▶ Redémarrage des displays Chromium..."
# Fermer les Chromium existants proprement
pkill -f "chromium.*localhost:8000" 2>/dev/null || true
sleep 1
# Relancer via le service systemd user
systemctl --user restart scoreboard-displays.service 2>/dev/null \
  && echo "  ✅ Displays relancés" \
  || echo "  ⚠️  Service displays non trouvé — lancer manuellement : bash ~/scoreboard/start_displays.sh"

echo ""
echo "╔══════════════════════════════════════════╗"
echo "║   MISE À JOUR TERMINÉE                   ║"
echo "║   Données équipes conservées ✓           ║"
echo "╚══════════════════════════════════════════╝"
