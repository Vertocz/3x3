#!/bin/bash
# ══════════════════════════════════════════════════════════════════════════════
#  install_window_rules.sh — Tableau de marque 3×3
#
#  wlrctl ne sait pas déplacer une fenêtre vers un écran précis (pas d'action
#  "move" côté sortie/output). C'est labwc qui doit le faire, via des
#  <windowRule> dans rc.xml qui se déclenchent automatiquement au premier
#  affichage de chaque fenêtre Chromium (identifiée par son --class).
#
#  Ce script ajoute ces règles à ~/.config/labwc/rc.xml (idempotent : on peut
#  le relancer sans dupliquer les règles), puis recharge labwc.
#
#  Lancer UNE FOIS sur le Pi :
#    bash ~/scoreboard/install_window_rules.sh
# ══════════════════════════════════════════════════════════════════════════════

set -e
RC="$HOME/.config/labwc/rc.xml"

echo ""
echo "▶ Installation des règles de placement d'écran (labwc)..."

mkdir -p "$HOME/.config/labwc"

# ── 1. Créer rc.xml à partir du fichier système s'il n'existe pas encore ──────
if [ ! -f "$RC" ]; then
  if [ -f /etc/xdg/labwc/rc.xml ]; then
    cp /etc/xdg/labwc/rc.xml "$RC"
    echo "  ✅ rc.xml créé à partir de /etc/xdg/labwc/rc.xml"
  else
    cat > "$RC" << 'EOF'
<?xml version="1.0"?>
<labwc_config>
</labwc_config>
EOF
    echo "  ✅ rc.xml minimal créé"
  fi
fi

# ── 2. Insérer/remplacer le bloc windowRules du tableau de marque ─────────────
# On utilise python3 (présent par défaut sur Raspberry Pi OS) pour éditer le
# XML proprement plutôt qu'avec sed, afin de ne jamais casser un rc.xml déjà
# personnalisé (thème, keybinds, etc.).
python3 - "$RC" << 'PYEOF'
import sys
import xml.etree.ElementTree as ET

path = sys.argv[1]
MARKER = "scoreboard-3x3-window-rules"

tree = ET.parse(path)
root = tree.getroot()

# Supprimer un éventuel bloc précédemment inséré par ce script (identifié par
# un commentaire marqueur), pour rester idempotent.
for child in list(root):
    pass  # ElementTree ne gère pas bien les commentaires ; on régénère à neuf ci-dessous.

windowRules = root.find("windowRules")
if windowRules is None:
    windowRules = ET.SubElement(root, "windowRules")

# Retirer les anciennes règles posées par ce script (identifiants connus).
for wr in list(windowRules):
    if wr.get("identifier") in ("chromium-config", "chromium-hdmi1", "chromium-hdmi2"):
        windowRules.remove(wr)

def add_rule(identifier, output):
    wr = ET.SubElement(windowRules, "windowRule")
    wr.set("identifier", identifier)
    a1 = ET.SubElement(wr, "action")
    a1.set("name", "MoveToOutput")
    a1.set("output", output)
    a2 = ET.SubElement(wr, "action")
    a2.set("name", "ToggleFullscreen")

add_rule("chromium-config", "DSI-2")
add_rule("chromium-hdmi1", "HDMI-A-1")
add_rule("chromium-hdmi2", "HDMI-A-2")

ET.indent(tree, space="  ")
tree.write(path, encoding="unicode", xml_declaration=False)

with open(path, "r") as f:
    content = f.read()
if not content.startswith("<?xml"):
    content = '<?xml version="1.0"?>\n' + content
    with open(path, "w") as f:
        f.write(content)

print("  ✅ Règles ajoutées : chromium-config→DSI-2, chromium-hdmi1→HDMI-A-1, chromium-hdmi2→HDMI-A-2")
PYEOF

# ── 3. Recharger labwc (recharge rc.xml sans redémarrer la session) ───────────
if pgrep -x labwc > /dev/null; then
  pkill -HUP -x labwc
  echo "  ✅ labwc rechargé (SIGHUP)"
else
  echo "  ⚠️  labwc n'est pas en cours d'exécution, les règles seront actives au prochain démarrage"
fi

echo ""
echo "╔══════════════════════════════════════════════════════════╗"
echo "║  Règles de placement installées.                         ║"
echo "║  Redémarrez (ou relancez le tableau) pour vérifier que    ║"
echo "║  chaque écran passe bien en plein écran automatiquement.  ║"
echo "╚══════════════════════════════════════════════════════════╝"
echo ""
