#!/bin/bash
# ══════════════════════════════════════════════════════════════════════════════
#  install_autostart.sh — Tableau de marque 3×3
#  Configure le démarrage automatique via UN SEUL service systemd utilisateur.
#  Lancer UNE FOIS sur le Pi :
#
#    bash ~/scoreboard/install_autostart.sh
# ══════════════════════════════════════════════════════════════════════════════

set -e
echo ""
echo "▶ Installation de l'autostart (service systemd utilisateur)..."

# ── 1. Rendre start_displays.sh exécutable ────────────────────────────────────
chmod +x ~/scoreboard/start_displays.sh
echo "  ✅ start_displays.sh rendu exécutable"

# ── 2. Supprimer les anciens autostart qui causaient des doublons ─────────────
rm -f ~/.config/autostart/scoreboard-displays.desktop
echo "  ✅ Ancien autostart GNOME/labwc .desktop supprimé"

# Supprimer l'entrée Wayfire si elle existe (évite le double démarrage)
WAYFIRE_AUTOSTART="$HOME/.config/wayfire/autostart"
if [ -f "$WAYFIRE_AUTOSTART" ]; then
  sed -i '/^scoreboard\s*=/d' "$WAYFIRE_AUTOSTART"
  echo "  ✅ Entrée Wayfire supprimée de $WAYFIRE_AUTOSTART"
fi

# Supprimer l'entrée labwc si elle existe
LABWC_AUTOSTART="$HOME/.config/labwc/autostart"
if [ -f "$LABWC_AUTOSTART" ]; then
  grep -v "start_displays" "$LABWC_AUTOSTART" > /tmp/_labwc_tmp 2>/dev/null || true
  mv /tmp/_labwc_tmp "$LABWC_AUTOSTART" 2>/dev/null || true
  echo "  ✅ Entrée labwc supprimée de $LABWC_AUTOSTART"
fi

# ── 3. Service systemd utilisateur (méthode unique et fiable) ─────────────────
mkdir -p ~/.config/systemd/user

cat > ~/.config/systemd/user/scoreboard-displays.service << 'EOF'
[Unit]
Description=Scoreboard Chromium Displays
After=graphical-session.target
Wants=graphical-session.target

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStartPre=/bin/sleep 5
ExecStart=/home/pi/scoreboard/start_displays.sh
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=graphical-session.target
EOF

systemctl --user daemon-reload
systemctl --user enable scoreboard-displays.service
echo "  ✅ Service systemd utilisateur activé"

# ── 4. Activer le linger (nécessaire pour les services user au boot) ──────────
sudo loginctl enable-linger "$(whoami)" 2>/dev/null || true
echo "  ✅ linger activé pour l'utilisateur $(whoami)"

echo ""
echo "╔══════════════════════════════════════════╗"
echo "║       AUTOSTART INSTALLÉ                 ║"
echo "║                                          ║"
echo "║  Au prochain démarrage, Chromium         ║"
echo "║  s'ouvrira automatiquement sur :         ║"
echo "║   • DSI       → Configuration            ║"
echo "║   • HDMI-A-1  → Tableau de marque        ║"
echo "║   • HDMI-A-2  → Possession (si branché)  ║"
echo "║                                          ║"
echo "║  Logs : ~/scoreboard/start_displays.log  ║"
echo "║  ou   : journalctl --user -u scoreboard-displays"
echo "╚══════════════════════════════════════════╝"
echo ""
read -p "Redémarrer maintenant pour tester ? (o/N) " ans
if [[ "$ans" == "o" || "$ans" == "O" ]]; then
  sudo reboot
fi
