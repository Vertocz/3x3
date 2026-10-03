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
# Le fichier du dépôt est un modèle (__USER__ / __HOME__) : on l'adapte à
# l'utilisateur courant plutôt que de supposer "pi".
sed -e "s|__USER__|$(whoami)|g" -e "s|__HOME__|$HOME|g" \
  "$HOME/scoreboard/buttons.service" | sudo tee /etc/systemd/system/buttons.service > /dev/null
sudo systemctl daemon-reload
sudo systemctl enable buttons.service
sudo systemctl restart buttons.service

echo ""
echo "✅ Boutons actifs !"
echo ""
echo "   Pour vérifier que ça tourne :"
echo "   sudo systemctl status buttons"
echo ""
echo "   Pour voir les logs en direct :"
echo "   sudo journalctl -fu buttons"
