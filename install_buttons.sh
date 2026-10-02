#!/bin/bash
# ══════════════════════════════════════════════════════
#  Installation des boutons GPIO (Pi 5 — lgpio)
#  Lancer sur le Pi : bash ~/scoreboard/install_buttons.sh
# ══════════════════════════════════════════════════════
set -e

echo "▶ Installation de gpiozero, lgpio et requests..."
# Sur Pi 5, rpi.gpio ne fonctionne pas — lgpio est le backend requis.
sudo apt install -y python3-gpiozero python3-lgpio python3-requests

echo "▶ Installation du service boutons..."
sudo cp /home/pi/scoreboard/buttons.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable buttons.service
sudo systemctl start buttons.service

echo ""
echo "✅ Boutons actifs !"
echo ""
echo "   Pour vérifier que ça tourne :"
echo "   sudo systemctl status buttons"
echo ""
echo "   Pour voir les logs en direct :"
echo "   sudo journalctl -fu buttons"
