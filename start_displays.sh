#!/bin/bash
# ══════════════════════════════════════════════════════════════════════════════
#  start_displays.sh — Tableau de marque 3×3
# ══════════════════════════════════════════════════════════════════════════════

LOG="/home/pi/scoreboard/start_displays.log"
exec >> "$LOG" 2>&1
echo "=== $(date) — démarrage start_displays.sh ==="

# ── 1. Variables Wayland ──────────────────────────────────────────────────────
UID_NUM=$(id -u)
export WAYLAND_DISPLAY="${WAYLAND_DISPLAY:-wayland-0}"
export XDG_RUNTIME_DIR="${XDG_RUNTIME_DIR:-/run/user/${UID_NUM}}"
export HOME="${HOME:-/home/pi}"

# ── 2. Attendre le serveur Go ─────────────────────────────────────────────────
echo "Attente du serveur Go..."
for i in $(seq 1 60); do
  curl -sf http://localhost:8000/ > /dev/null && break
  sleep 1
done
echo "Serveur prêt."

# ── 3. Attendre Wayland ───────────────────────────────────────────────────────
echo "Attente de Wayland ($XDG_RUNTIME_DIR/$WAYLAND_DISPLAY)..."
for i in $(seq 1 30); do
  [ -S "$XDG_RUNTIME_DIR/$WAYLAND_DISPLAY" ] && break
  sleep 1
done
echo "Wayland disponible."

# ── 4. Rotation DSI ───────────────────────────────────────────────────────────
wlr-randr --output DSI-2 --transform 270
sleep 2

# ── 5. Nettoyer les anciens profils ───────────────────────────────────────────
# Les profils HDMI sont nettoyés ici par précaution (ex: coupure de courant
# précédente), mais ce script ne lance plus jamais de fenêtre dessus : c'est
# désormais entièrement le rôle du bouton "lancer le tableau" (/api/launch,
# /api/launch-table), qui sait positionner correctement la fenêtre sur le bon
# écran — ce que ce script ne faisait pas, d'où des fenêtres HDMI fantômes
# qui entraient en conflit avec celles lancées depuis l'appli.
rm -rf /tmp/chromium-dsi /tmp/chromium-hdmi1 /tmp/chromium-hdmi2

# ── 6. Flags communs Chromium ─────────────────────────────────────────────────
CHROME_FLAGS=(
  --noerrdialogs
  --disable-infobars
  --no-first-run
  --disable-session-crashed-bubble
  --disable-restore-session-state
  --autoplay-policy=no-user-gesture-required
  --disable-features=TranslateUI,Translate
  --disable-translate
  --lang=fr
  --ozone-platform=wayland
  --enable-features=UseOzonePlatform
  --password-store=basic
)
# Important : PAS de --start-fullscreen ici. La mise en plein écran et le
# placement sur le bon écran sont désormais gérés par labwc via les
# windowRules (voir install_window_rules.sh) qui font MoveToOutput puis
# ToggleFullscreen dès l'apparition de la fenêtre. Si la fenêtre démarre
# déjà plein écran côté Chromium, ToggleFullscreen l'annule au lieu de
# l'activer, et MoveToOutput est ignoré pour une fenêtre déjà plein écran
# — résultat : la fenêtre reste en mode fenêtré sur le mauvais écran.

# ── 7. DSI → configuration ────────────────────────────────────────────────────
echo "Ouverture config sur DSI..."
chromium \
  "${CHROME_FLAGS[@]}" \
  --class=chromium-config \
  --user-data-dir="/tmp/chromium-dsi" \
  http://localhost:8000/ &

sleep 8

# ── 8. HDMI → lancement automatique via l'appli elle-même ────────────────────
# On ne duplique plus la logique de lancement ici (c'était la cause du
# conflit avec le bouton "lancer le tableau" : deux fenêtres Chromium sur le
# même dossier de profil). On appelle directement le même endpoint que le
# bouton, qui sait positionner correctement chaque fenêtre et n'entre donc
# plus en conflit avec un lancement manuel ultérieur.
echo "Lancement automatique des écrans HDMI (si branchés)..."
curl -sf -X POST http://localhost:8000/api/launch-table
echo "=== Fin start_displays.sh ==="