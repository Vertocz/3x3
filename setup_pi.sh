#!/bin/bash
# ══════════════════════════════════════════════════════
#  SETUP COMPLET — Tableau de marque Basketball 3x3
#  À lancer UNE SEULE FOIS sur le Raspberry Pi
#  ssh pi@scoreboard.local puis : bash setup_pi.sh
# ══════════════════════════════════════════════════════

set -e
echo ""
echo "╔══════════════════════════════════════════╗"
echo "║   INSTALLATION TABLEAU DE MARQUE 3x3     ║"
echo "╚══════════════════════════════════════════╝"
echo ""

# ── 1. Mise à jour système ──
echo "▶ Mise à jour du système..."
sudo apt update && sudo apt upgrade -y

# ── 2. Installer Go ──
echo "▶ Installation de Go..."
GO_VERSION="1.24.1"
wget -q https://go.dev/dl/go${GO_VERSION}.linux-arm64.tar.gz
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf go${GO_VERSION}.linux-arm64.tar.gz
rm go${GO_VERSION}.linux-arm64.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
export PATH=$PATH:/usr/local/go/bin
go version

# ── 3. Compiler l'application ──
echo "▶ Compilation du scoreboard..."
cd ~/scoreboard
go build -o scoreboard .
echo "✅ Compilation OK"

# ── 4. Rotation écran en paysage (DSI 7") ──
echo "▶ Configuration de l'écran DSI 7\"..."
BOOT_CONFIG="/boot/firmware/config.txt"
if ! grep -q "display_rotate" $BOOT_CONFIG; then
  echo "display_rotate=0" | sudo tee -a $BOOT_CONFIG
fi
if ! grep -q "dtoverlay=vc4-kms-dsi-7inch" $BOOT_CONFIG; then
  printf "\n# Écran tactile DSI 7\"\ndtoverlay=vc4-kms-dsi-7inch\n" | sudo tee -a $BOOT_CONFIG
fi

# ── 5. Hotspot WiFi ──
# Nom et mot de passe du hotspot : modifiables sans toucher au script
#   WIFI_PASS="monmotdepasse" bash setup_pi.sh
WIFI_SSID="${WIFI_SSID:-Scoreboard3x3}"
WIFI_PASS="${WIFI_PASS:-basketball3x3}"
echo "▶ Configuration du hotspot WiFi..."

if systemctl is-active --quiet NetworkManager 2>/dev/null; then
  # Raspberry Pi OS Bookworm (obligatoire sur Pi 5) : NetworkManager gère le
  # Wi-Fi. dhcpcd/hostapd n'y sont pas utilisés.
  echo "  (NetworkManager détecté)"
  sudo nmcli connection delete "$WIFI_SSID" >/dev/null 2>&1 || true
  sudo nmcli connection add type wifi ifname wlan0 con-name "$WIFI_SSID" \
    autoconnect yes ssid "$WIFI_SSID" \
    802-11-wireless.mode ap 802-11-wireless.band bg \
    ipv4.method shared ipv4.addresses 192.168.4.1/24 \
    wifi-sec.key-mgmt wpa-psk wifi-sec.psk "$WIFI_PASS"
  sudo nmcli connection up "$WIFI_SSID" || echo "  ⚠️  hotspot non démarré maintenant — il le sera au prochain redémarrage"
else
  # Anciens Raspberry Pi OS : dhcpcd + hostapd + dnsmasq
  sudo apt install -y hostapd dnsmasq
  sudo systemctl stop hostapd dnsmasq 2>/dev/null || true

  # IP fixe sur wlan0
  if ! grep -q "interface wlan0" /etc/dhcpcd.conf 2>/dev/null; then
    cat << EOF | sudo tee -a /etc/dhcpcd.conf

interface wlan0
static ip_address=192.168.4.1/24
nohook wpa_supplicant
EOF
  fi

  # DHCP
  sudo mv /etc/dnsmasq.conf /etc/dnsmasq.conf.backup 2>/dev/null || true
  cat << EOF | sudo tee /etc/dnsmasq.conf
interface=wlan0
dhcp-range=192.168.4.10,192.168.4.50,255.255.255.0,24h
address=/scoreboard.local/192.168.4.1
EOF

  # Hotspot
  cat << EOF | sudo tee /etc/hostapd/hostapd.conf
interface=wlan0
ssid=${WIFI_SSID}
hw_mode=g
channel=7
wpa=2
wpa_passphrase=${WIFI_PASS}
wpa_key_mgmt=WPA-PSK
rsn_pairwise=CCMP
EOF

  sudo sed -i 's|#DAEMON_CONF=""|DAEMON_CONF="/etc/hostapd/hostapd.conf"|' /etc/default/hostapd
  sudo systemctl unmask hostapd
  sudo systemctl enable hostapd dnsmasq
fi

# ── 6. Service systemd — application Go ──
echo "▶ Service systemd scoreboard..."
# Le modèle versionné (scoreboard.service) est adapté à l'utilisateur courant.
sed -e "s|__USER__|$(whoami)|g" -e "s|__HOME__|$HOME|g" -e "s|__UID__|$(id -u)|g" \
  ~/scoreboard/scoreboard.service | sudo tee /etc/systemd/system/scoreboard.service > /dev/null

sudo systemctl daemon-reload
sudo systemctl enable scoreboard
sudo systemctl restart scoreboard

# ── 7. Sudoers — extinction sans mot de passe ──
# Le serveur Go exécute "sudo shutdown -h now" (bouton power physique et
# confirmation web). Sans règle sudoers dédiée, sudo resterait bloqué en
# attente d'un mot de passe qu'aucun processus non-interactif ne peut
# fournir, et l'extinction ne se ferait jamais silencieusement.
# La règle est volontairement restreinte à cette commande exacte (pas de
# NOPASSWD général) pour limiter ce qu'un accès au service peut déclencher.
echo "▶ Autorisation d'extinction sans mot de passe..."
SHUTDOWN_BIN="$(command -v shutdown)"
SUDOERS_FILE="/etc/sudoers.d/scoreboard-shutdown"
echo "$(whoami) ALL=(root) NOPASSWD: ${SHUTDOWN_BIN} -h now" | sudo tee "$SUDOERS_FILE" > /dev/null
sudo chmod 440 "$SUDOERS_FILE"
if sudo visudo -cf "$SUDOERS_FILE"; then
  echo "  ✅ Règle sudoers installée (${SUDOERS_FILE})"
else
  echo "  ⚠️  Règle sudoers invalide générée — fichier supprimé par sécurité"
  echo "     (l'extinction depuis l'appli redemandera un mot de passe)"
  sudo rm -f "$SUDOERS_FILE"
fi

# ── 8. Script de démarrage des fenêtres Chromium ──
echo "▶ Script de démarrage Chromium..."
# On copie start_displays.sh depuis le repo (ne pas le réécrire ici)
chmod +x ~/scoreboard/start_displays.sh

# ── 9. Autostart via service systemd utilisateur (méthode unique) ──
echo "▶ Autostart Chromium..."
bash ~/scoreboard/install_autostart.sh

# ── 10. Outils Wayland ──
echo "▶ Installation de wlr-randr et wlrctl..."
sudo apt install -y wlr-randr wlrctl 2>/dev/null || echo "  ⚠️  wlrctl non dispo via apt — à installer manuellement si nécessaire"

# ── 11. Dépendances boutons GPIO ──
echo "▶ Dépendances GPIO (optionnel si boutons physiques présents)..."
sudo apt install -y python3-gpiozero python3-lgpio python3-requests

echo ""
echo "╔══════════════════════════════════════════╗"
echo "║         INSTALLATION TERMINÉE            ║"
echo "║                                          ║"
echo "║  WiFi : Scoreboard3x3                    ║"
echo "║  MDP  : ${WIFI_PASS}"
echo "║  URL  : http://192.168.4.1:8000          ║"
echo "║                                          ║"
echo "║  Redémarrage nécessaire pour appliquer   ║"
echo "╚══════════════════════════════════════════╝"
echo ""
read -p "Redémarrer maintenant ? (o/N) " ans
if [[ "$ans" == "o" || "$ans" == "O" ]]; then
  sudo reboot
fi
