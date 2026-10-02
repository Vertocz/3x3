# Mise à jour du Raspberry Pi

## Lot 3 (dernier en date) — bouton fin de match, équipes, logo, couleurs

Fichiers concernés :
- `server.go` (modifié — nouvelles routes `/logo.png`, `/match`, `/api/end-match`)
- `config.go` (modifié — nouvelle fonction `handleEndMatch`)
- `static/config.html` (modifié — palette de couleurs fixe, clavier tactile intégré, équipes enfin persistées côté serveur)
- `static/match.html` (**nouveau** — copie de `index.html` avec un bouton "Terminer le match")

**⚠️ Il te manque un fichier pour compiler : `static/logo.png`**. `config.html` demande déjà `/logo.png` (c'était ça, le bug du logo qui ne s'affichait pas — la route n'existait tout simplement pas côté serveur). Dépose ton fichier logo, au format PNG, exactement à `~/scoreboard/static/logo.png` **avant** de recompiler — sans lui, `go build` échouera (`go:embed` exige que le fichier existe).

Ce que ça change concrètement :
- **Bouton "Terminer le match"** : sur la nouvelle page `/match` (celle qui s'ouvre sur la tactile après "lancer le tableau", à la place de `/scoreboard`), un bouton vert enregistre le score dans un historique côté serveur (jusque-là jamais utilisé malgré la structure déjà prévue), remet le match à zéro, puis referme l'onglet pour revenir sur `/config` — sans souris.
- **Équipes enfin sauvegardées pour de vrai** : `config.html` utilisait le `localStorage` du navigateur, complètement déconnecté du serveur — d'où l'impression que ça ne marchait pas (probablement perdu à chaque redémarrage du Pi si le profil Chromium de la tactile est dans `/tmp`). C'est maintenant branché sur `/api/teams`, qui écrit dans `~/scoreboard-data/teams.json` et survit aux redémarrages.
- **Palette de couleurs fixe** : blanc (gris très clair `#d9d9d9`), noir (gris très foncé `#1c1c1c`), rouge, bleu, vert, jaune, rose, violet, orange, marron. Modifiable directement dans `config.html` (`const COLORS = [...]`) si les teintes ne conviennent pas.
- **Clavier tactile intégré** pour le nom des équipes (AZERTY, pavé de touches dans la modale) — pas de dépendance à un clavier virtuel système, pour éviter un nouveau point de fragilité Wayland.

### Déploiement de ce lot
```bash
cp -r ~/scoreboard ~/scoreboard.bak-$(date +%Y%m%d)
# copier server.go, config.go dans ~/scoreboard/
# copier static/config.html, static/match.html dans ~/scoreboard/static/
# déposer TON logo réel dans ~/scoreboard/static/logo.png
cd ~/scoreboard
go build -o scoreboard .
sudo systemctl restart scoreboard
```

### Tests à faire
- [ ] Le logo s'affiche sur `config.html`.
- [ ] Enregistrer une équipe, redémarrer le service (`sudo systemctl restart scoreboard`), rouvrir `config.html` → l'équipe est toujours là.
- [ ] Modifier une équipe déjà enregistrée (même nom) → ça met à jour l'entrée existante, ça n'en crée pas une deuxième.
- [ ] Taper un nom d'équipe uniquement au tactile, via le clavier intégré (pas de clavier physique).
- [ ] Lancer un match, cliquer "Terminer le match" → retour sur `/config` sans toucher une souris.

---

## Lot 2 — robustesse + positionnement HDMI

Fichiers concernés par ce lot :
- `server.go` (modifié)
- `launch.go` (modifié)
- `hotplug.go` (**nouveau**)
- `setup_pi.sh` (modifié — script de référence, pas besoin de le relancer en entier)

## Le vrai bug trouvé

Ce n'était pas un problème de détection de branchement : **rien ne disait jamais explicitement à Chromium sur quel écran s'afficher**. `/api/launch` (et `start_displays.sh` au démarrage) lançaient Chromium pointé sur la bonne URL, mais sans jamais appeler le déplacement vers l'output HDMI ni le passage en plein écran — d'où le fait que ça ne marchait pas non plus avec un écran déjà branché au démarrage.

Le correctif : chaque fenêtre Chromium lancée par `/api/launch` ou `/api/launch-table` reçoit maintenant une étiquette stable (`--class=chromium-hdmi1` / `chromium-hdmi2`), on attend qu'elle soit réellement apparue (`wlrctl window waitfor`), puis on la positionne explicitement sur le bon écran et on la passe en plein écran (`wlrctl window focus/move/set fullscreen`) — exactement la séquence que faisaient déjà `/api/move-to-hdmi` et `/api/fullscreen`, mais déclenchée automatiquement.

**`start_displays.sh` (le script de démarrage automatique du Pi) a probablement le même défaut** — il lance Chromium sur HDMI-1/2 sans jamais les positionner non plus. Je ne l'ai pas touché pour l'instant : dis-moi si tu veux qu'on le corrige aussi, ou qu'on le simplifie pour qu'il délègue à `/api/launch-table` maintenant que ça fonctionne côté serveur Go.

## Ce qui a changé côté comportement

- `POST /api/launch` et `POST /api/launch-table` positionnent maintenant réellement la fenêtre sur le bon HDMI et la passent en plein écran.
- Ne relance plus une fenêtre déjà en cours sur ce HDMI ; si elle existe déjà, on se contente de la repositionner (au cas où elle aurait bougé).
- Le nettoyage du profil Chromium se fait avant chaque relance, pas seulement au démarrage du Pi.
- `sudo shutdown -h now` ne demande plus de mot de passe (règle sudoers dédiée, restreinte à cette seule commande).
- Le branchement à chaud (écran connecté après coup) reste expérimental : voir l'échange précédent sur les limites connues du pilote vc4-kms pour ça spécifiquement — le correctif ci-dessus concerne le *positionnement*, pas la *détection*.

## Étapes de déploiement

1. **Sauvegarder l'existant sur le Pi**, au cas où :
   ```bash
   cp -r ~/scoreboard ~/scoreboard.bak-$(date +%Y%m%d)
   ```

2. **Copier les fichiers modifiés** (`server.go`, `launch.go`, `hotplug.go`) dans `~/scoreboard/`, en écrasant les anciennes versions de `server.go` et `launch.go`. `hotplug.go` est un fichier en plus, pas un remplacement.

3. **Recompiler** :
   ```bash
   cd ~/scoreboard
   go build -o scoreboard .
   ```
   Si ça ne compile pas, ne pas aller plus loin — regarder l'erreur, elle indique généralement le fichier et la ligne en cause.

4. **Vérifier que `wlrctl` supporte bien `waitfor`** (nécessaire au nouveau code) :
   ```bash
   wlrctl window waitfor --help 2>&1 | head -5
   ```
   Si la commande n'existe pas dans votre version de wlrctl, prévenez-moi : il faudra remplacer `waitfor` par une petite boucle de sondage.

5. **Appliquer la règle sudoers pour l'extinction** (nouvelle étape 7 de `setup_pi.sh` — pas besoin de relancer tout le script, ce bloc suffit) :
   ```bash
   SHUTDOWN_BIN="$(command -v shutdown)"
   echo "$(whoami) ALL=(root) NOPASSWD: ${SHUTDOWN_BIN} -h now" | sudo tee /etc/sudoers.d/scoreboard-shutdown > /dev/null
   sudo chmod 440 /etc/sudoers.d/scoreboard-shutdown
   sudo visudo -cf /etc/sudoers.d/scoreboard-shutdown && echo "Règle sudoers OK"
   ```

6. **Redémarrer le service** :
   ```bash
   sudo systemctl restart scoreboard
   journalctl -u scoreboard -f
   ```
   Garder ce terminal de logs ouvert pendant les tests ci-dessous.

## Tests à faire sur place

- [ ] Les deux écrans HDMI branchés **avant** d'allumer le Pi (ou avant de redémarrer le service), cliquer sur "lancer le tableau" (ou `curl -X POST http://localhost:8000/api/launch-table`) → les deux fenêtres doivent apparaître **sur les bons écrans**, en plein écran.
- [ ] Cliquer une seconde fois → aucune nouvelle fenêtre ne s'ouvre, celle qui existe reste bien en place.
- [ ] Si une fenêtre apparaît mais pas sur le bon écran, ou pas en plein écran : regarder les logs (`journalctl -u scoreboard -f`) au moment du clic — les erreurs de `focus`/`move`/`fullscreen` y apparaîtront explicitement, ça aidera à savoir laquelle des trois étapes échoue.
- [ ] Tester l'extinction (bouton power ou confirmation depuis la page web) → le Pi doit s'éteindre sans mot de passe.

## Pour brancher le bouton "lancer le tableau" sur le nouvel endpoint (optionnel)

L'ancien comportement continue de fonctionner sans rien changer côté page web. Si un jour tu veux que le bouton appelle directement le nouvel endpoint dédié plutôt que d'appeler `/api/launch` séparément pour chaque écran, il suffit d'un appel :

```js
fetch('/api/launch-table', { method: 'POST' })
  .then(r => r.json())
  .then(data => console.log(data));
```

## En cas de souci — revenir en arrière

```bash
rm -rf ~/scoreboard
mv ~/scoreboard.bak-AAAAMMJJ ~/scoreboard
cd ~/scoreboard
go build -o scoreboard .
sudo systemctl restart scoreboard
```
(remplacer `AAAAMMJJ` par la date de la sauvegarde faite à l'étape 1)

