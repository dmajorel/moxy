# ADR 0010 — La mise en maintenance s'exécute par SSH, sous une grammaire fermée

- **Statut** : acceptée
- **Date** : 2026-09-17
- **Portée** : `apps/api/internal/maintenance`, `apps/api/internal/config`,
  `apps/api/internal/detail`, `apps/api/internal/server/detail.go`, `apps/api/go.mod`
  et `apps/api/vendor/`, `deploy/`, `apps/web/src/screens/MaintenancePlanDialog.tsx`
- **Remplace** : [ADR 0003](0003-no-maintenance-execute.md)

## Contexte

L'ADR 0003 a constaté que PVE n'expose aucune route REST de mise en maintenance —
`node-maintenance-set` est une sous-commande de CLI qui écrit une commande CRM dans
le système de fichiers du cluster — et en a tiré qu'il n'y aurait pas d'exécution
dans moxy. Trois de ses constats restent vrais et ne sont pas contestés ici : il n'y
a toujours pas de route REST, le plan reste strictement en lecture seule, et le token
d'API du cluster reste en lecture seule.

Ce qui change est la demande, et elle est explicite : le plan s'arrête là où
l'opérateur a encore tout à faire — ouvrir un terminal, retrouver un nœud, recopier
une commande que moxy affiche déjà. L'ADR 0003 avait retiré le bouton faute de route,
pas faute de besoin.

L'objection qu'il opposait au SSH, elle, reste entièrement valable :

> moxy deviendrait un exécuteur de commandes distantes avec des identifiants shell
> sur chaque nœud, très au-delà d'un token d'API.

On ne la réfute pas, on la prend comme cahier des charges. C'est elle qui dicte la
forme de ce qui suit : ce que la clé de moxy ouvre ne doit pas être un shell, mais
deux verbes sur un nom de nœud existant.

## Décision

**moxy exécute la commande CRM par SSH, sur un nœud du cluster autre que la cible.**

La fonctionnalité est déclinée en quatre décisions, qui ne se séparent pas.

### 1. Une grammaire fermée, pas un accès shell

Le canal est dimensionné pour ce qu'il transporte et pour rien d'autre :

- un **compte de service sans shell atteignable** (`/usr/sbin/nologin`) sur chaque
  nœud ;
- un **`ForceCommand`** — `command=` dans `authorized_keys`, ou l'option critique
  `force-command` du certificat : ce que le client demande n'arrive jamais qu'en
  donnée, dans `$SSH_ORIGINAL_COMMAND` ;
- une **grammaire de trois mots**, `node-maintenance <enable|disable> <nœud>`,
  validée par un script appartenant à `root` que le compte de service ne peut pas
  réécrire, le nom de nœud étant vérifié contre les nœuds réels (`/etc/pve/nodes/`) ;
- un **`sudo` borné** au seul verbe `ha-manager crm-command node-maintenance`. Le `*`
  de `sudoers` accepte les espaces et ne borne donc pas le dernier argument : la vraie
  barrière est le validateur, `sudoers` ne fait que fermer le verbe ;
- **jamais de session vers le nœud que l'on met en maintenance** : c'est souvent celui
  qu'on veut arrêter, il peut déjà être injoignable, et la session mourrait avec lui.

Le SSH est un **second canal**, ajouté à côté du token PVE. Il ne sert pas à relâcher
les droits de ce token, qui reste en lecture seule.

### 2. Le transport : `golang.org/x/crypto/ssh`, vendoré

La bibliothèque standard n'a pas de client SSH, et l'image livrée n'a ni shell ni
binaire `ssh`. Le backend prend donc **sa première dépendance**, et l'ADR 0001 est
amendé en conséquence : stdlib seule, *sauf* `golang.org/x/crypto/ssh`.

Ce que ce choix coûte, écrit noir sur blanc :

- `apps/api/vendor/` apparaît dans le dépôt, avec `x/crypto` et sa transitive `x/sys`,
  et la compilation se fait en `-mod=vendor` — `GOPROXY=off` (`scripts/env.sh`) reste
  posé et reste satisfait, rien n'est téléchargé à la compilation ;
- la série courante de `x/crypto` déclare une directive `go` supérieure à `1.19`. Le
  relèvement de la directive du module est **la bonne branche** : épingler une version
  ancienne reviendrait à vivre avec ses avis de sécurité dans le paquet cryptographique
  d'un démon qui ouvre des sessions privilégiées. Le garde-fou de l'ADR 0001 sur
  `log/slog`, `errors.Join` et la passe de CI en 1.19 tombe avec lui ;
- `govulncheck`, déjà dans `scripts/analyze.sh`, surveille pour la première fois une
  dépendance réelle, et Dependabot doit se voir déclarer l'écosystème `gomod`.

Ce qu'il rapporte est le contrôle fin dont le reste dépend : pas de PTY, budgets
explicites, vérification de clé d'hôte **sans TOFU et sans mode permissif**, clé privée
en mémoire, sortie plafonnée.

### 3. Deux modes de fourniture de la clé, exclusifs, à portée processus

La clé qui ouvre les sessions vient soit d'un **fichier local** (`mode: "ssh-key"`),
soit d'un **certificat SSH signé par OpenBao** (`mode: "openbao"`), engendré à chaque
exécution à partir d'une paire ed25519 éphémère. Le premier est celui par lequel on
commence, le second celui vers lequel on bascule quand le durcissement le justifie :
les contraintes changent alors de porteur, elles passent d'un fichier que chaque nœud
héberge — réécrivable par qui obtient un pied sur le nœud, oubliable sur le nœud ajouté
six mois plus tard — à un rôle, à un seul endroit.

**Le mode est une propriété du processus, jamais d'un cluster ni d'un nœud**, et
l'exclusivité est structurelle plutôt que documentaire :

- `mode` est obligatoire dès que le bloc `maintenance` existe, et **sans valeur par
  défaut** : celui qui décide où vit un secret l'écrit de sa main ;
- le bloc du mode non choisi doit être **absent**, pas seulement ignoré — même règle et
  même formulation que `config/auth.go` pour `tokenEnv` ;
- `clusters[].maintenance` **n'a aucun champ de mode**. Le parc mixte n'est pas interdit
  par une validation : il est impossible à écrire, et une règle qu'aucune configuration
  ne peut enfreindre n'a pas besoin d'être vérifiée ;
- côté code, un **seul `KeyProvider`**, choisi au chargement. Pas de
  `map[cluster]KeyProvider`, pas de paramètre de mode qui redescende : la fonction qui
  ouvre une session ignore quel mode est actif, et c'est ce qui garantit qu'elle ne
  pourra jamais en mélanger deux.

`KeyProvider` est appelé **à chaque exécution**, pas une fois au démarrage : un
certificat mis en cache serait expiré au moment de servir.

**Il n'y a pas de repli automatique de `openbao` vers `ssh-key`.** Ce serait exactement
le parc mixte que la configuration rend impossible, et un repli silencieux vers une clé
locale au pire moment est une régression de sécurité déguisée en robustesse. Le recours
existe déjà et il est ailleurs : la commande `ha-manager` que la modale continue
d'afficher. Revenir en `ssh-key` reste possible, mais c'est un changement de
configuration délibéré, écrit et redémarré — pas quelque chose que le démon décide seul
à trois heures du matin.

### 4. Le démarrage ne dépend pas d'OpenBao, et `/readyz` non plus

La forme de la configuration est validée et les fichiers sont lus au chargement ; la
joignabilité d'OpenBao ne l'est pas. Une sonde au démarrage peut journaliser un
avertissement, elle n'échoue pas. **`/readyz` ne reflète pas la santé d'OpenBao** :
sortir du service un démon de supervision — dont la vue d'ensemble, elle, fonctionne —
parce qu'un service tiers est scellé, c'est perdre les deux au lieu d'un, pour une
fonctionnalité utilisée quelques fois par mois.

## Modèle de menace

Trois compromissions, et ce que chacune donne exactement.

**L'attaquant obtient la clé privée de moxy** (mode `ssh-key`, fichier lu au
démarrage). Il peut ouvrir une session sur les nœuds, depuis l'adresse que `from=`
autorise s'il y parvient, et **rien d'autre que** `node-maintenance enable|disable` sur
un nœud que le cluster a réellement. Pas de shell, pas de copie de fichier, pas de
redirection de port, pas de PTY : `restrict` et `ForceCommand` sont posés du côté du
nœud, où l'attaquant ne les choisit pas. Le pouvoir de nuisance est une **mise en
maintenance non désirée** — un drain, donc une indisponibilité, pas une exfiltration.
En mode `openbao`, il n'y a pas de clé durable à voler : la paire naît et meurt avec
l'exécution, et le certificat expire au TTL que le rôle fixe.

**L'attaquant prend le compte `moxy` sur un nœud.** Il hérite d'un compte sans shell
dont le seul droit `sudo` est ce même verbe. Il ne peut pas réécrire le validateur
(`root:root`), ni le `sudoers` (`0440`, `root`). En mode `ssh-key` il peut lire et
réécrire l'`authorized_keys` de ce nœud, donc s'y donner un accès persistant sous ce
compte : c'est précisément la faiblesse que le mode `openbao` supprime, puisqu'il n'y a
plus de fichier de contraintes sur le nœud.

**L'attaquant prend le processus `moxyd`.** C'est le cas le plus grave, et il l'était
déjà avant cet ADR : le processus détient les tokens d'API de tout le parc. Le SSH
ajoute à ce butin la clé — ou le pouvoir d'en faire signer une — et donc la capacité de
drainer des nœuds. L'ajout est réel mais borné par la grammaire du point 1 : un
`moxyd` compromis peut provoquer une indisponibilité, il n'obtient pas d'exécution
arbitraire sur les hyperviseurs. C'est cette borne, et non la confiance dans le démon,
qui rend la décision acceptable — et c'est pourquoi la grammaire fermée n'est pas un
raffinement mais la condition de l'ADR.

## Conséquences

- **L'ADR 0003 est remplacé** : le bouton d'exécution existe, sous la modale de plan
  qui reste la confirmation. Le commentaire de `DetailSource` — « Executing the drain
  is not part of this interface, and cannot be » — est réécrit et renvoie ici.
- La règle de l'ADR 0003 qui survit : **pas de bouton désactivé assorti d'une
  infobulle.** Sur un cluster sans bloc `maintenance`, la route répond `404` et le
  bouton n'existe pas ; le payload du nœud porte `maintenanceExecutable` pour cela.
- **C'est la première route en écriture du service**, donc la première exposée au CSRF.
  Le mode `token` est couvert par son cookie `SameSite=Strict` ; le mode
  `proxy-header` ne l'est pas, et la route exige donc `Content-Type: application/json`
  et une `Origin` attendue.
- **`auth.mode: "none"` interdit l'exécution**, et le démarrage échoue franchement si
  un cluster porte un bloc `maintenance` sans authentification : sinon quiconque atteint
  le port draine la production.
- La réponse dit « la demande est passée », **jamais « le nœud est drainé »** : la
  commande CRM écrit une intention, le drain suit de façon asynchrone, et l'état réel
  continue de se lire par la scrutation existante (ADR 0006).
- Un échec de **transport** essaie le nœud suivant ; un échec **applicatif** n'en essaie
  aucun, parce que la commande a peut-être pris effet.
- Un **mutex par (cluster, nœud)** et un `409` plutôt qu'une attente : c'est l'inverse
  du verrou anti-troupeau de l'ADR 0005, qui coalesce des lectures ; ici deux écritures
  concurrentes se refusent.
- **Un échec reste rattrapable à la main** : la commande `ha-manager` affichée dans la
  modale n'est pas un vestige, c'est le plan B, et elle reste.
- `deploy/` gagne de quoi préparer un nœud, dans les deux variantes, et la préparation
  devient un prérequis d'exploitation là où la fonctionnalité ne demandait rien.
- **Une CA SSH de confiance sur tous les nœuds est une autorité nouvelle dans le
  parc** : elle remplace une clé, mais mal cadrée elle accorde bien plus qu'elle.
  Son cadrage s'arbitre dans son propre ADR, pas en passant.
- `Secret.Reveal()` reste réservé au transport d'authentification du paquet `proxmox` :
  la clé SSH est un autre secret, avec son propre porteur, et l'exception documentée ne
  s'étend pas à elle par ressemblance.

## Alternatives écartées

- **Le statu quo de l'ADR 0003** — afficher la commande et s'arrêter : c'était la bonne
  réponse tant que la seule voie était une route REST inexistante. La demande est
  explicite, et la forme ci-dessus répond à l'objection au lieu de l'ignorer.
- **`os/exec` vers `ssh(1)`** : impose de quitter `distroless/static` — le binaire est
  lié dynamiquement — et d'installer un exécuteur de commandes distantes dans l'image.
  Le `Containerfile` explique déjà pourquoi `curl` n'y est que dans l'étape de débogage
  (« une primitive d'exfiltration toute prête ») ; un `ssh` serait pire.
- **Écrire un client SSH en bibliothèque standard** : plusieurs milliers de lignes de
  cryptographie de transport à écrire et à maintenir, et `crypto/ecdh` n'existe qu'à
  partir de Go 1.20. Le gain — zéro dépendance — se paierait dans la seule partie du
  produit où un défaut est immédiatement exploitable.
- **Un petit agent HTTP/mTLS sur chaque nœud** : resterait en stdlib pure, avec une
  surface minimale et sans `sudo`. Mais ce n'est plus du SSH, c'est un binaire de plus à
  déployer, à mettre à jour et à surveiller sur tout le parc, et la demande porte
  explicitement sur SSH. Écarté ici, pas disqualifié : c'est l'alternative à rouvrir si
  la dépendance devenait insoutenable.
- **Ranger une clé permanente dans un KV OpenBao** plutôt que de faire signer un
  certificat : plus simple, et rapporterait peu — la clé resterait permanente, on aurait
  seulement échangé un fichier contre un identifiant d'amorçage.
- **Un mode de fourniture par cluster**, pour migrer un cluster à la fois : rendrait
  écrivable le parc mixte que le point 3 rend impossible, et il n'en a pas besoin — les
  deux variantes coexistent **sur le nœud**, `sshd` acceptant une clé listée *ou* un
  certificat signé par une CA de confiance, ce qui permet la bascule sans coupure.
- **Un mode permissif pour les clés d'hôte**, par symétrie avec `tls.mode: "insecure"` :
  ce dernier assouplit une *lecture* de mesures, tandis qu'une clé d'hôte non vérifiée
  fait exécuter une commande privilégiée sur une machine usurpée. Il n'y en a pas, et il
  n'y en aura pas.

## Vérification

Ce que l'ADR 0003 avait vérifié tient : sources PVE lues le 2026-09-12
(`PVE/CLI/ha_manager.pm`, `PVE/API2/HA/*`, `PVE/API2/Nodes.pm`) — aucune route REST de
maintenance n'est apparue depuis, et c'est bien pour cela que le canal est un second
canal.

Fichiers concernés : `apps/api/internal/config` (bloc `maintenance`, modes exclusifs,
lecture et vérification de la clé, `Secret`), `apps/api/internal/maintenance`
(`KeyProvider`, `Credential`, `Runner`, `Target`, vocabulaire d'erreur et coupure
transport / applicatif), `apps/api/internal/detail/model.go` et
`apps/web/src/api/types.ts` (contrat, qui bougent dans le même changement),
`apps/api/internal/server/detail.go` (`matchDetailPath`, méthode par route, `Allow`),
`apps/api/internal/metrics` (étiquettes à ensemble fermé, jamais un nom de nœud),
`deploy/` (compte de service, validateur, `sudoers`, `authorized_keys` ou CA),
`README.md` (les deux modes côte à côte), `apps/api/go.mod` et `apps/api/vendor/`.

**Le transport SSH concret n'est pas encore écrit.** L'environnement de développement
courant n'a pas d'accès réseau — TLS intercepté, `x509: certificate signed by unknown
authority` sur `proxy.golang.org` —, si bien que `golang.org/x/crypto/ssh` n'y est pas
vendorable et que rien de ce qui l'importe ne compile. La conséquence est assumée et
délimitée : le dial, la poignée de main, la vérification `known_hosts` et l'exécution
vivent derrière l'interface `Runner`, dont la seule implémentation livrée pour l'instant
est celle du mode mock. Un fichier de plus l'ajoutera quand le vendoring sera possible ;
d'ici là, aucun code du dépôt n'importe `x/crypto` et aucun stub ne prétend parler SSH.
La décision de transport, elle, est prise : c'est ce que cet ADR acte, et c'est ce qui
permettait d'écrire les lots suivants sans attendre.
