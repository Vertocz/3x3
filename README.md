# Tableau de marque Basketball 3x3

Tableau de score, chrono de match et chrono de possession (12 s) pour le 3x3,
sur Raspberry Pi 5 : un serveur Go, des pages web affichées en Chromium
plein écran (écran tactile de contrôle + 1 ou 2 écrans HDMI) et des boutons
physiques sur les GPIO.

```
 boutons GPIO ──► buttons.py ──► /api/action ─┐
 écran tactile (/) ──► WebSocket /ws ─────────┼──► serveur Go (état + chronos)
 page contrôle (/scoreboard?ctrl=1) ──────────┘            │ diffusion 10×/s
                                                            ▼
                                  HDMI-1 /scoreboard    HDMI-2 /possession
```

## Matériel
- Raspberry Pi 5 (Raspberry Pi OS Bookworm, session Wayland / labwc)
- Écran DSI 7" tactile (configuration), 1 écran HDMI « tableau », 1 écran HDMI « possession » (optionnel)
- 16 boutons de jeu + 1 bouton power sur GPIO (voir `buttons.py` pour le câblage)

## Installation
Copier le dossier dans `~/scoreboard` sur le Pi puis :

```bash
bash ~/scoreboard/setup_pi.sh
# Mot de passe du hotspot personnalisé :
# WIFI_PASS="monmotdepasse" bash ~/scoreboard/setup_pi.sh
bash ~/scoreboard/install_buttons.sh      # si les boutons physiques sont branchés
```

Le Pi crée le Wi-Fi `Scoreboard3x3` ; l'interface est sur `http://192.168.4.1:8000`.

## Mise à jour (développement)
Depuis le PC :

```bash
scp -r . pi@scoreboard.local:~/scoreboard/ && ssh pi@scoreboard.local "bash ~/scoreboard/update_pi.sh"
```

`update_pi.sh` compile vers un fichier temporaire (un échec de build laisse l'ancienne
version en marche), relance serveur,
boutons et écrans. Les équipes, l'historique et les réglages sont conservés
(`~/scoreboard-data/`).

## Fichiers
| Fichier | Rôle |
|---|---|
| `main.go` | démarrage, cadence des chronos, sauvegarde à l'arrêt |
| `state.go` | état du match, chronos (temps réel), diffusion aux écrans, persistance des réglages |
| `server.go` | routes HTTP, WebSocket (ping/pong), actions de jeu |
| `config.go`, `teams_api.go` | API réglages, équipes, historique |
| `storage.go` | équipes et matchs (`teams.json`), écriture atomique |
| `launch.go`, `hotplug.go` | détection des écrans (wlr-randr), lancement Chromium, branchement à chaud |
| `validate.go` | bornes et validation des entrées |
| `buttons.py` | boutons GPIO → API |
| `static/` | pages web, images, son, polices (embarquées, fonctionne hors ligne) |
| `scoreboard.service`, `buttons.service` | modèles systemd (`__USER__`, `__HOME__`) |

## Règles gérées
- Match de 10 min (réglable), chrono de possession de 12 s, fautes d'équipe, fin à 21 points avec écart minimum (réglables) : balle de match affichée.
- **Prolongation (3x3)** : égalité à la fin du temps → pause d'1 minute, puis première équipe à 2 points. L'opérateur la déclenche depuis la tactile (bouton visible seulement sur une égalité à 0:00).
- **Coupure de courant** : le match en cours est restauré (score, fautes, temps, prolongation), chronos à l'arrêt.

## Choix de conception
- **Chrono exact** : le temps retiré est le temps réellement écoulé entre deux ticks, pas
  100 ms fixes — pas de dérive si le Pi est chargé.
- **Résistant aux coupures de courant** : toutes les écritures sur disque sont atomiques
  (fichier temporaire + rename). Un fichier corrompu est mis de côté, jamais écrasé.
- **Écrans qui se rattrapent seuls** : l'état complet est rediffusé toutes les 2 s ; les
  connexions mortes sont détectées (ping/pong) ; les pages se reconnectent automatiquement.
- **Ouverture des fenêtres Chromium** : conservée telle quelle (lancement `--class`, règles labwc
  de `install_window_rules.sh`, mise au point par tâtonnement). Ne pas la modifier sans test sur le Pi.
- **Hors ligne** : polices embarquées, aucune dépendance à Internet à l'exécution.
- **API volontairement ouverte** (pas d'authentification) : pratique pour tester. À ne pas
  exposer hors du hotspot du Pi.

## Développement
```bash
go vet ./... && go test -race ./...
```
La CI GitHub (`.github/workflows/ci.yml`) fait la même chose et vérifie aussi la
compilation arm64 et la syntaxe des scripts.
