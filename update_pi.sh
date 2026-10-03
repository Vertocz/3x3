#!/bin/bash
# ══════════════════════════════════════════════════════════════════════════════
#  update_pi.sh — Mise à jour rapide en développement
#
#  Depuis le PC (PowerShell ou Git Bash), une seule commande :
#
#    scp -r . pi@scoreboard.local:~/scoreboard/ && ssh pi@scoreboard.local "bash ~/scoreboard/update_pi.sh"
#
#  Ce script :
#   1. Recompile le binaire Go (sans toucher au binaire actuel si ça échoue)
#   2. Redémarre le service principal (scoreboard)
#   3. Redémarre le service boutons
#   4. Redémarre les displays
# ══════════════════════════════════════════════════════════════════════════════

set -e
cd ~/scoreboard

export PATH=$PATH:/usr/local/go/bin

# ── 1. Recompiler ──────────────────────────────────────────────────────────────
# Pas de "go mod tidy" : il peut vouloir joindre Internet, absent quand on est
# connecté au hotspot du Pi. go.sum est versionné, "go build" suffit.
# On compile vers un fichier temporaire puis on remplace : un échec de
# compilation laisse l'ancien binaire intact et le service tourne toujours.
echo "▶ Compilation..."
go build -o scoreboard.new .
mv -f scoreboard.new scoreboard
echo "  ✅ Build OK"

# ── 2. Redémarrer le service principal ────────────────────────────────────────
echo "▶ Redémarrage du serveur Go..."
sudo systemctl restart scoreboard
sleep 1
sudo systemctl is-active --quiet scoreboard && echo "  ✅ scoreboard actif" || echo "  ❌ scoreboard en erreur — voir: sudo journalctl -u scoreboard -n 30"

# ── 3. Redémarrer les boutons ─────────────────────────────────────────────────
if systemctl is-active --quiet buttons 2>/dev/null; then
  echo "▶ Redémarrage du service boutons..."
  sudo systemctl restart buttons
  sleep 1
  sudo systemctl is-active --quiet buttons && echo "  ✅ buttons actif" || echo "  ❌ buttons en erreur — voir: sudo journalctl -u buttons -n 20"
fi

# ── 4. Redémarrer les fenêtres Chromium ───────────────────────────────────────
# Le binaire a changé : les pages embarquées aussi, il faut les recharger.
echo "▶ Redémarrage des displays Chromium..."
pkill -f "chromium.*localhost:8000" 2>/dev/null || true
sleep 1
systemctl --user restart scoreboard-displays.service 2>/dev/null \
  && echo "  ✅ Displays relancés" \
  || echo "  ⚠️  Service displays non trouvé — lancer manuellement : bash ~/scoreboard/start_displays.sh"

echo ""
echo "╔══════════════════════════════════════════╗"
echo "║   MISE À JOUR TERMINÉE                   ║"
echo "║   Données équipes conservées ✓           ║"
echo "╚══════════════════════════════════════════╝"
