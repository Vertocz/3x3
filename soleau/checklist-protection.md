# Station 12 : plan de protection (28/10/2026)

Ce document est une aide pratique, pas un avis juridique. Pour la vente, une heure avec un
conseil en propriété industrielle (CPI) ou un conseil en PI vaut le coup.

## 1. Cette semaine

- [ ] **Passer le dépôt GitHub en privé** (Settings > General > Danger Zone > Change visibility).
      Au moment de mon analyse : 0 fork, 0 étoile, 0 observateur, donc exposition faible à ce jour.
- [ ] **Compléter les champs `[Prénom NOM]`, `[adresse]`, `[e-mail]`, `[lieu]`, `[date]`** dans
      `LICENSE`, dans les en-têtes de fichiers et dans `dossier-descriptif-e-soleau.pdf`.
      Le PDF doit être régénéré ou édité après avoir rempli ces champs, avant dépôt.
- [ ] **Confirmer ou adapter le paragraphe sur l'assistance d'IA** (section 6 du PDF, encadré jaune).
      Le commit 56a04f6 est intitulé « version claude ». Une déclaration inexacte affaiblirait la preuve.
- [ ] **Déposer l'e-Soleau** sur le portail INPI avec : `3x3-snapshot-89a3c15.zip` +
      `dossier-descriptif-e-soleau.pdf` (+ `SHA256SUMS-fichiers.txt`). Choisir une conservation de
      10 à 20 ans. Garder l'attestation et le code confidentiel de restitution.
      Je ne peux pas créer de compte ni payer à votre place.
- [ ] **Appliquer le patch** `licence-et-en-tetes.patch` à un clone du dépôt
      (`git apply licence-et-en-tetes.patch`) une fois les champs remplis, puis commit et push.
      Il ajoute `LICENSE`, `THIRD_PARTY_NOTICES.md`, une section Licence au README et un en-tête
      de copyright aux fichiers `.go`, `.py` et `.sh` (uniquement des ajouts : aucune ligne existante n'est supprimée ni modifiée).

## 2. À traiter avant de vendre

### Droits sur les contenus
- [ ] **Origine des médias** : `ball.png`, `lancer.png`, `logo.png`, `station12.png`, `horn.mp3`.
      Qui les a créés ? Sous quelle licence ? Conserver les justificatifs. Le son (`horn.mp3`) est
      le plus souvent issu d'une banque de sons : vérifier que l'usage commercial est autorisé.
- [ ] **Témoignages** (`Station 12 - présentation.md`, section 6) : noms et citations de trois
      joueurs de l'équipe de France de Parabasket Adapté, publiés dans un dépôt public.
      Obtenir leur **accord écrit** pour l'usage commercial (nom, citation, éventuellement le club
      ou la fédération). Ce sont des données personnelles (RGPD).
      Le fichier est exclu de l'archive e-Soleau pour cette raison.
- [ ] **Licences tierces** : voir `THIRD_PARTY_NOTICES.md`. Si vous livrez un Pi avec carte SD
      préinstallée, vous redistribuez des paquets GPL (hostapd, dnsmasq…) : prévoir la liste et les
      sources à disposition.

### Protection du nom et du design
- [ ] **Nom « Station 12 »** : rechercher les antériorités (base de l'INPI, EUIPO, Google), puis
      déposer la marque (classes 9 et 28, éventuellement 41) et réserver le nom de domaine.
      Ne pas utiliser « Raspberry Pi » dans le nom commercial.
- [ ] **Design** (boîtier, interface) : dépôt de dessin et modèle à l'INPI ou à l'EUIPO.
      Le délai de grâce est de 12 mois à compter de la première divulgation (à confirmer avec
      l'INPI). Première divulgation possible : 02/10/2026, date du premier commit si le dépôt était
      déjà public. Viser un dépôt avant fin 2026.
- [ ] **Brevet** : probablement hors de portée pour ce qui est déjà public. Un CPI peut dire en
      une heure si un aspect non divulgué mérite d'être protégé.

### Produit
- [ ] **Sécurité de l'API** (voir section 3).
- [ ] **Conformité** : marquage CE, CEM, RoHS, DEEE, directive RED (Wi-Fi), alimentation, notice
      d'utilisation en français. Assurance responsabilité civile produit.
- [ ] **Contrat de vente / licence utilisateur final** (CGV, garantie, mises à jour).

## 3. Failles relevées dans le code (pertinentes pour un produit vendu)

| Constat | Où | Risque | Piste |
|---|---|---|---|
| API sans authentification, serveur sur `0.0.0.0` | `server.go`, README | Aucune restriction à localhost : quiconque est sur le Wi-Fi peut modifier le score et les réglages, lancer des écrans ou arrêter le Pi (l'arrêt demande deux appels, `/api/shutdown` puis `/confirm`, mais rien ne l'empêche) | Jeton partagé ou code PIN pour les routes qui modifient l'état, au minimum `/api/shutdown`, `/api/config`, `/api/action` depuis le réseau Wi-Fi |
| `CheckOrigin` renvoie toujours `true` (WebSocket) | `server.go:51` | Une page web tierce ouverte sur un appareil connecté peut piloter le Pi | Vérifier l'en-tête `Origin` |
| Mot de passe Wi-Fi par défaut en clair dans le dépôt | `setup_pi.sh:50` | Même mot de passe sur tous les appareils vendus | Générer un mot de passe aléatoire par appareil à l'installation, l'afficher sur l'écran ou l'étiquette |
| Règle `sudoers` pour `shutdown` | `setup_pi.sh:124` | Limitée à une commande : acceptable, à garder ainsi | Rien à changer |
| E-mail personnel visible dans les commits | historique Git | Spam, identification | Utiliser l'adresse `noreply` de GitHub pour les prochains commits |

## 4. Ce qui ne protège pas (rappel)

- L'e-Soleau prouve une date et une possession ; elle ne donne pas de monopole et n'empêche
  personne de refaire un outil équivalent sans copier votre code.
- Elle ne couvre que l'état déposé : refaire un dépôt à chaque version majeure.
- L'originalité du code s'apprécie sur l'apport humain : conservez cahiers des charges, croquis,
  échanges et décisions de conception qui montrent vos choix (utile aussi si une partie du code
  a été produite avec une IA).
- Durée maximale de conservation : 20 ans. Notez l'échéance dans votre agenda.
