# Composants tiers

Ces composants ne sont pas couverts par la licence du projet (voir `LICENSE`) :
chacun reste soumis à sa propre licence. Les licences indiquées sont celles
connues à la date de rédaction : **à re-vérifier avant toute commercialisation**
(texte exact, version utilisée, obligations de mention).

## Embarqués dans le dépôt

| Composant | Où | Licence | Remarque |
|---|---|---|---|
| Inconsolata (polices `.woff2`) | `static/fonts/` | SIL Open Font License 1.1 | Texte de licence fourni (`static/fonts/LICENSE-Inconsolata-OFL.txt`) : à conserver avec les polices. |

## Dépendances Go (`go.mod`)

| Composant | Version | Licence |
|---|---|---|
| github.com/gorilla/websocket | v1.5.1 | BSD-2-Clause |
| golang.org/x/net | v0.17.0 (indirecte) | BSD-3-Clause |
| Chaîne d'outils Go | 1.24.x | BSD-3-Clause |

## Dépendances Python (`buttons.py`)

| Composant | Licence |
|---|---|
| requests | Apache-2.0 |
| gpiozero | BSD-3-Clause |
| lgpio | domaine public (Unlicense) |

## Paquets système installés par `setup_pi.sh` / `install_buttons.sh`

`hostapd`, `dnsmasq` (licences GNU GPL), `wlr-randr`, `wlrctl`, Chromium,
Raspberry Pi OS (Debian). Ils sont exécutés comme programmes distincts, pas
liés au code du projet. **Si vous livrez un Raspberry Pi ou une carte SD
préinstallés, vous redistribuez ces composants** : prévoyez la liste des
paquets, leurs licences et l'accès aux sources lorsque la licence l'exige
(GPL).

## Médias du projet : origine à documenter

`static/ball.png`, `static/lancer.png`, `static/logo.png`, `static/station12.png`,
`static/horn.mp3` : indiquer ici l'auteur et la licence de chacun (création
personnelle, banque libre de droits, achat…) et conserver les justificatifs.
