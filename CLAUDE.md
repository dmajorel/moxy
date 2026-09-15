# moxy

Surcouche web multi-cluster pour Proxmox VE. La spécification de référence est
`docs/PROXMOX_UI_HANDOFF.md` : lis-la avant toute tâche de fond. Les décisions de
design du §2 et les contraintes du §4 sont arrêtées, ne les rediscute pas sans
raison explicite.

## Décisions (ADR)

Le *pourquoi* des choix structurants vit dans `docs/adr/` : un fichier court par
décision (contexte, décision, conséquences, alternatives écartées, vérification).
Ce fichier-ci garde la **règle applicable** et renvoie à l'ADR, qui garde le
raisonnement et ce qu'on a écarté : on lit un ADR pour remettre une règle en cause,
pas pour l'appliquer, et une issue cite « ADR 0003 » au lieu de recopier le
paragraphe. Index : `docs/adr/README.md`.

## Langue

- **Le code source est en anglais**, sans exception : identifiants, commentaires,
  messages de log, chaînes d'erreur, noms de tests, scripts shell et workflows CI.
- **Les messages de commit sont en anglais**, pour la même raison que le code : ils
  décrivent le code et se lisent dans `git log` à côté de lui. Sujet à l'impératif,
  en minuscules, corps explicatif si le changement le mérite.
- **Les titres et descriptions de PR sont en anglais**, pour la même raison encore :
  GitHub recopie le titre de la PR dans le commit de fusion, sous le
  « Merge pull request #N », d'où il se lit dans `git log` comme n'importe quel
  autre sujet — et en devient le sujet lui-même si la PR est écrasée (`squash`).
  Le titre prend donc la forme d'un sujet de commit : impératif, en minuscules,
  sans point final. Le corps de la PR suit la langue de son titre. La revue en
  commentaires, elle, est une conversation et non du code : elle se tient dans la
  langue de l'échange.
- **La documentation est en français** : `README.md`, ce fichier, `docs/` — les ADR
  de `docs/adr/` compris — et les commandes de `.claude/commands/*.md`.
- **L'interface est bilingue, et ses libellés sources sont en français**, sentence
  case, comme l'impose le §2. Ils ne vivent plus dans les composants mais dans le
  catalogue `apps/web/src/i18n/messages.ts`, qui porte chaque chaîne dans les deux
  langues ; le français y est la source, et la complétude de l'anglais est tenue par
  le compilateur. La langue suit le navigateur, avec un sélecteur mémorisé comme
  celui du thème. → ADR 0008.
- Conséquence : le backend renvoie ses erreurs en anglais (`method not allowed`) ; la
  traduction vers l'utilisateur est au frontend, jamais à l'API.

## Suivi des tâches

Ce projet n'utilise pas `TASKS.md` : ne crée pas ce fichier, ne l'alimente pas, n'y
coche rien. La convention globale ne s'applique pas ici.

## Backend (`apps/api`)

- **Go, bibliothèque standard uniquement** : `GOPROXY=off` et `CGO_ENABLED=0` dans les
  scripts font échouer la compilation sur une dépendance — c'est voulu. → ADR 0001.
- `go test -race` n'est **pas** utilisable : le détecteur de courses exige CGO.
- **La directive `go 1.19` fixe le langage, pas la stdlib livrée** : pas de
  `log/slog` (1.21) ni d'`errors.Join` (1.20), et une passe de CI en 1.19 — le niveau
  de la toolchain locale — garde la contrainte mécanique ; mais l'image et la CI
  compilent avec la série supportée du moment, et le `Containerfile` et la matrice de
  `ci.yml` bougent ensemble. → ADR 0001.

## Client PVE (`internal/proxmox`)

Pièges de l'API Proxmox déjà rencontrés, à ne pas redécouvrir :

- **`cpu` est une fraction `0..1`, pas un pourcentage** (`0.31` = 31 %), y compris
  dans `/api/overview` : la mise en forme est au frontend.
- **Les tailles sont en octets** (`mem`, `maxmem`, `disk`, `maxdisk`) : aucune
  conversion en GiB/TiB côté backend.
- **Toute réponse PVE est enveloppée dans `{"data": ...}`** ; le déballage est fait
  une fois pour toutes par le helper générique du client, pas dans chaque appelant.
- **Les types `Flex*`** (`FlexInt`, `FlexFloat`, `FlexBool`) existent parce que PVE
  sérialise ses nombres tantôt en nombre tantôt en chaîne, et ses booléens en `0`/`1`
  (`shared`, `template`, `quorate`, `online`). Ne pas les remplacer par des types
  natifs « parce que le schéma dit booléen » : le schéma ment.
- **`node_status` est imbriqué dans `manager_status`** sur un vrai cluster PVE 9, et
  non à plat comme le laisse croire le schéma. Le décodage accepte les deux formes ;
  ne le simplifie pas (vérifié le 2026-09-12, cluster à 6 nœuds).
- **La capacité partagée se compte par backend, pas par ligne** : un stockage
  `shared` apparaît une fois par nœud, les pools d'un même Ceph rapportent tous le
  même espace libre, et un partagé sans taille (`maxdisk: 0`) n'est pas un backend.
  Clés de groupement et repli sur le local dans `deriveStorage`. → ADR 0002.
- **`maxdisk` n'est pas la volumétrie d'un invité** : c'est le disque de boot d'une
  VM, le `rootfs` d'un conteneur, rien d'autre. Le reste est dans
  `/nodes/{node}/{kind}/{vmid}/config`, une clé par volume, et **les tailles d'une
  configuration ne sont pas en octets** : suffixe 1024 (`size=32G`, `size=528K`). Un
  lecteur optique occupe une clé sans rien allouer (`media=cdrom`, ISO ou non) ; un
  périphérique passé tel quel et un volume `unused` ne déclarent aucune taille, qui
  est donc inconnue et jamais nulle. Voir `GuestConfig.Disks`.
- **Les deux genres d'invité écrivent `netN` différemment**, sous la même clé. QEMU
  met le modèle et la MAC dans un raccourci — `net0: virtio=BC:24:11:AA:BB:CC,
  bridge=vmbr0` — où le **modèle est la clé de la paire** ; il accepte aussi
  `model=` et `macaddr=` séparés. LXC exige `name=eth0`, le nom de l'interface **vu
  par le conteneur**, qu'une VM ne déclare jamais, et range sa MAC dans `hwaddr=`.
  `tag=` est un VLAN : absent veut dire non étiqueté, jamais VLAN zéro. Voir
  `GuestConfig.Nets`.
- **L'alias d'un réseau ne vit pas dans la configuration de l'invité** : `bridge=`
  nomme un pont Linux ou un VNet SDN sans dire lequel, et le nom lisible se résout
  ailleurs. Deux sources, lues **par cluster et par nœud, jamais par invité**
  (`detail.Service.netAliases`) : `/cluster/sdn/vnets` porte `alias`, et
  `/nodes/{node}/network` porte `comments` — la colonne « Comment » de l'UI native,
  que PVE stocke avec son retour à la ligne. L'alias SDN l'emporte, un VNet
  apparaissant aussi comme pont généré côté nœud. **Un pont sans alias n'est pas un
  inconnu** : l'UI affiche son identifiant, et réserve le tiret cadratin à une carte
  attachée à rien. Vérifié dans le schéma publié de PVE 9 le 2026-09-14 : contrairement
  à presque tout le reste, `/cluster/sdn/vnets` **ne renvoie pas 403** à un token sans
  `SDN.Audit`, il renvoie une **liste filtrée**, et `/nodes/{node}/network` n'exige
  aucun droit particulier.
- **Sans `Sys.Audit` sur `/nodes/{node}`, PVE renvoie la ligne `node` sans
  `cpu`/`maxcpu`/`mem`/`maxmem`**, et sans erreur : un nœud en ligne sans mesures est
  « inconnu » (`cpu`/`memory` à `nil`, alerte `node_stats_unavailable`), jamais vide.
- **`/cluster/tasks` n'accepte aucun paramètre de requête** et les refuse au lieu de
  les ignorer : `?limit=25` vaut un `400 Parameter verification failed`, pas une liste
  tronquée. Seule `/nodes/{node}/tasks` prend `limit`, `start` et les filtres ; le
  plafonnement du journal du cluster se fait donc côté moxy, après tri, dans
  `detail.Service.Tasks` (vérifié le 2026-09-12).
- **Les tâches d'un invité se lisent sur `/nodes/{node}/tasks?vmid=…`**, jamais en
  filtrant `/cluster/tasks` : ce journal ne porte que sa queue, qu'une nuit de
  `vzdump` suffit à remplir. `proxmox.NodeTasks` et `detail.Service.GuestTasks` en
  sont les deux moitiés ; contrepartie assumée, un invité qui a migré laisse son
  passé sur son ancien nœud.
- **Le paramètre `source` de `/nodes/{node}/tasks` vaut `archive` par défaut**, qui ne
  contient que les tâches **terminées** : sans `source=all`, une sauvegarde en cours
  est absente de la liste censée la montrer (`PVE/API2/Tasks.pm`, 2026-09-13).
- **`Secret.Reveal()` est réservé au transport d'authentification du paquet
  `proxmox`**, son unique appelant légitime, celui qui pose l'en-tête
  `Authorization`. Partout ailleurs un `Secret` se rédige en `***`, via `String`,
  `GoString`, `MarshalJSON` et `MarshalText`.
- **Interdit de journaliser ou de formater une `*http.Request`**,
  `httputil.DumpRequestOut` en tête : l'en-tête `Authorization` porte le secret. Une
  erreur ne doit pas davantage embarquer le corps ni les en-têtes d'une requête —
  seulement l'identifiant de cluster, le chemin, le code HTTP et la cause.

## API de détail (`internal/detail`)

Les routes par objet obéissent à d'autres règles que la vue d'ensemble. Elles
sont **huit**, toutes sous `/api/clusters/{cluster}/` (`server/detail.go`,
`matchDetailPath`) : `rrd` et `tasks` du cluster, `nodes/{node}` et son `rrd`,
`nodes/{node}/maintenance/plan`, `guests/{vmid}` et ses `rrd` et `tasks`. Ce qui
suit se redécouvrirait douloureusement.

- **La vue d'ensemble est scrutée, le détail est à la demande** : ces routes
  appellent PVE au moment de la requête, amorties par un **cache court** (5 s) et un
  **verrou anti-troupeau** — dix onglets sur le même nœud ne déclenchent qu'un appel
  amont. Ne pas « uniformiser » en ajoutant ces objets au scrutateur. → ADR 0005.
- **`null` signifie « inconnu », jamais « zéro »** — points RRD compris : une lacune
  remplie de `0` inventerait une chute qui n'a pas eu lieu. Même règle pour `ipv4`
  sans agent invité, `haState` sans gestionnaire HA, `pendingUpdates` sans
  `Sys.Modify`, `quorum` sur un nœud seul.
- **Un appel facultatif qui échoue laisse son champ à `nil` sans faire échouer la
  réponse** : un 403 sur `apt/update` ou un agent invité absent dégrade un champ, pas
  la requête. Seuls les appels essentiels propagent leur erreur.
- **PVE n'a pas de RRD de cluster** : la série de la carte est repliée nœud par nœud
  — moyenne CPU pondérée par les cœurs (`deriveCPUAndMemory`), mémoire sommée, points
  appariés **sur l'horodatage** et jamais sur l'indice —, en partageant l'entrée de
  cache de la vue nœud. Un nœud illisible perd sa part de courbe ; l'erreur n'est
  propagée que si aucun nœud n'a répondu. → ADR 0004.
- **Les séries RRD portent des points, pas une image** : le backend sert les fractions
  brutes, l'échelle est décidée au frontend (→ ADR 0004). De même, la **durée d'une
  tâche est calculée ici**, en secondes — le défaut exact de l'UI native est
  d'afficher un début et une fin à soustraire de tête.
- **Le `ServeMux` de Go 1.19 n'a pas de paramètres de chemin** (arrivés en 1.22) : les
  segments sont découpés à la main, et tout ajout de route doit **refaire la
  validation** — segments vides, `.` et `..`, décodage percent, nombre exact de
  segments. Aucun routeur ne l'attrapera.
- **`detail/model.go` est un contrat**, comme `aggregate/model.go` : miroir de
  `apps/web/src/api/types.ts`, les deux bougent dans le même changement.
- **Les règles partagées avec la vue d'ensemble vivent dans `aggregate/rules.go`**,
  exportées, et ne se recopient pas : statut d'un nœud, quorum, genre et état d'un
  invité, `UsageOf`, `AsBytes`. Un opérateur qui lit « en maintenance » sur une carte
  et « hors ligne » sur la page du même nœud a été menti par l'une des deux, sans
  moyen de savoir laquelle.
- **Le mode mock répond aussi sur ces routes** (`detail.NewMock`), en dérivant ses
  réponses de la vue d'ensemble de démonstration : un nœud ouvert depuis l'arbre porte
  les chiffres de sa carte. Ses séries ont des trous et sa tâche la plus récente est
  en cours — un mock trop propre laisserait passer une UI incapable de les afficher.
- **`maintenance/plan` existe, `maintenance/execute` n'existera pas** : PVE n'expose
  aucune route REST de maintenance. Pas de bouton d'exécution, même désactivé ; l'UI
  donne la commande `ha-manager` et s'arrête là, le plan restant en lecture seule.
  → ADR 0003.
- Le temps quasi réel se fait **par scrutation**, pas par flux poussé : le journal du
  cluster relit `.../tasks` toutes les 5 s, ce que le cache court absorbe. → ADR 0006.

## Frontend (`apps/web`)

React 19 + TypeScript 6 `strict` + Vite 8 + Tailwind 4 + Vitest 5 + ESLint 10, avec
`@tabler/icons-react`. Document de référence : `apps/web/README.md`. Le choix d'une SPA
sur API JSON plutôt que d'un rendu HTML côté Go (HTMX) est motivé dans l'ADR 0007.

- **Aucune couleur en dur dans un composant.** Les tokens du §2 vivent dans
  `src/styles/tokens.css` et sont exposés en utilitaires Tailwind par `@theme inline`
  (`bg-surface-2`, `text-text-muted`, `border-border`, `rounded-card`). Ni `#1D9E75`,
  ni `bg-green-500`, ni `style={{ color }}` : une couleur qui manque s'ajoute à
  `tokens.css`. Le bleu `#2D679C` est réservé au logo, jamais un statut ni un bouton.
- **Le logo est la marque en X de `ui/Logo.tsx`**, pas le carré orange du §2 : quatre
  barres à 45°, deux par diagonale, séparées par deux fentes fines qui se croisent au
  centre, chaque bras coupé à plat. Dérogation assumée au §2, demandée explicitement.
  Les fentes sont des trous, non des traits blancs — la page les traverse, la marque
  tient donc sur n'importe quel fond ; tracé plein et jamais au trait ; peint en
  `currentColor` depuis `text-brand`.
- **Aucun formatage ad hoc** : octets, secondes, ratios et libellés d'état passent
  tous par `src/lib/format.ts`. Un `Math.round(ratio * 100)` dans un composant crée
  une seconde convention typographique qui divergera de la première.
- **`src/api/types.ts` est le miroir de `apps/api/internal/aggregate/model.go`** : les
  deux bougent dans le même changement, sans quoi le contrat est rompu en silence.
- **`null` signifie « inconnu », pas « zéro »** — `updates: null` veut dire que la
  question n'a pas pu être posée, `pendingUpdates: null` de même par nœud,
  `quorum: null` désigne un nœud seul : l'UI rend le tiret cadratin `—`, pas un `0`.
- **`useOverview` ne vide jamais ses données sur erreur** : il conserve le dernier
  instantané connu et signale `isStale`, comme le backend sert le dernier état connu
  d'un cluster injoignable. → ADR 0006.
- **Aucune chaîne visible dans un composant** : tout passe par `useT()`, qui lit
  `src/i18n/messages.ts`. Sentence case dans les deux langues ; code et commentaires
  en anglais. Le backend renvoie ses erreurs en anglais avec un `kind` traduisible, ce
  qui est précisément ce qui rend la traduction possible côté frontend. → ADR 0008.
- **La typographie d'un nombre dépend de la langue, et n'est pas dans le catalogue** :
  virgule décimale et espace fine insécable U+202F en français (`1,2 TiB`, `31 %`,
  `1 024`), point et virgule en anglais (`1.2 TiB`, `31%`, `1,024`), unité de base `o`
  contre `B`. La table `TYPOGRAPHY` de `lib/format.ts` en décide. Les fonctions dont
  la sortie dépend de la langue pendent à `createFormat(locale)` et s'obtiennent par
  `useFormat()` ; les autres restent des exports de module.
- **Les noms de langue ne se traduisent pas** : « Français » reste « Français » dans
  un menu anglais. L'attribut `lang` de `<html>` est toujours posé, contrairement à
  `data-theme` que le mode système retire.
- Accessibilité : l'arbre est un vrai `role="tree"` navigable au clavier, les menus se
  ferment à `Échap` en rendant le focus, et une information portée par une couleur a
  toujours un équivalent textuel.
- **Les cartes de cluster portent un graphe d'utilisation sur la dernière heure, pas
  de jauges CPU et mémoire** : valeurs instantanées en légende, ambre au-delà du
  seuil, et le stockage garde sa barre faute d'historique côté PVE. Dérogation
  assumée au §2. → ADR 0004.
- **Le bandeau d'alerte est sous le nom du cluster, pas en bas de carte** : c'est la
  seule ligne qui dise *ce qu'il y a à faire*, et comme aucun nœud n'est tronqué, la
  liste intercalée la repoussait sous la ligne de flottaison sur un cluster à six
  nœuds. L'ordre est donc : en-tête, bandeau, graphe, stockage, VM, nœuds, fraîcheur.
  Le bandeau neutre (« Quorum 3/3 · aucune alerte ») occupe le **même** emplacement,
  sans quoi la position dépendrait du contenu et l'œil devrait le chercher d'une carte
  à l'autre ; la ligne de fraîcheur reste en bas, elle dit depuis quand la lecture
  date et non quoi en faire. Une seule règle d'espacement sépare le nom du bandeau :
  l'en-tête n'a pas de marge basse. Dérogation assumée au §A.4, demandée
  explicitement.
- **La sparkline ne s'auto-échelonne jamais** : `Sparkline` fixe son axe à
  `[0, scaleMax]`, défaut 1, et prend des séries de ratios (`lib/series.ts` traduit
  les points RRD), une ou deux, jamais des `Point` bruts. Un trou RRD coupe la courbe
  au lieu d'être tracé à zéro. → ADR 0004.
- **Pas de barre d'onglets sur les vues nœud et VM.** Le §2 en dessine six, une seule
  a du contenu ; elle s'ajoutera quand un deuxième onglet aura de quoi s'afficher.
- **Ne documente ni n'échafaude ce qui n'existe pas.** La mise en maintenance et le
  temps réel sont encore à venir.

## Vérifications

```sh
make check       # tout : backend et frontend
make check-api   # gofmt, go vet, go test
make check-web   # typecheck, eslint, vitest
make fmt         # gofmt -w apps/api, ce que check exige
make analyze     # shellcheck, staticcheck, govulncheck, npm audit (outils à part)
make build       # compile bin/moxyd
make build-web   # bundle dans apps/web/dist
make image       # image OCI (podman ou docker), voir Containerfile
make image-debug # variante de débogage (bash + curl), taguée -debug
make mock        # compile puis lance moxyd sur les données d'exemple
make dev         # démon mock + serveur Vite, un seul terminal
make probe       # sonde un cluster réel (URL=, TOKEN=, MOXY_SECRET=)
```

`scripts/analyze.sh` est volontairement distinct de `check.sh` : `check.sh` doit
rester exécutable avec le seul Go local, hors ligne, alors que `staticcheck` et
`govulncheck` exigent tous deux une série Go plus récente que le `go 1.19` du
module et se téléchargent depuis le proxy que `env.sh` coupe. Le script n'installe
rien, il exécute ce qu'il trouve et **saute en le disant** ce qui manque ;
`MOXY_ANALYZE_REQUIRE=1` (posé par la CI) transforme chaque saut en échec. La CI
les installe **hors du module**, `GOPROXY` réactivé pour ce seul step : rien
n'entre dans `go.mod`, aucun `go.sum` n'apparaît, `bin/moxyd` est inchangé. La
couverture est mesurée en CI seulement — `MOXY_COVER` désigne le profil que
`check.sh` écrit — et publiée en artefact.

`make help` liste les cibles. **Le Makefile n'est qu'une enveloppe autour de
`scripts/`** : ce sont les scripts qui font foi, puisque la CI les appelle
directement, et dupliquer leur logique dans le Makefile les ferait diverger. Une
nouvelle vérification s'ajoute dans un script, que le Makefile ne fait qu'exposer.

### Les fixtures du frontend sont générées

`apps/web/src/test/fixtures/*.json` **ne se recopient pas à la main** : elles sont
produites par `TestMockMatchesWebFixtures` (`apps/api/internal/server/fixtures_test.go`)
à partir du démon mock, sur une horloge figée. Le test échoue sur une fixture
périmée ; pour la régénérer :

```sh
cd apps/api && go test ./internal/server -update
```

C'est ce qui rend mécanique la règle du contrat : un champ renommé dans `model.go`
fait échouer `make check-api` tant que les fixtures ne sont pas régénérées, et
`types.contract.test.ts` fait échouer `make check-web` tant que `types.ts` ne suit
pas. Une route de plus se déclare dans `webFixtures`, faute de quoi c'est elle que
personne ne verra dériver.

Le produit se livre en conteneur : une image unique où `moxyd -web` sert le bundle du
frontend sous la même origine que l'API. Sans `-web`, `moxyd` reste API seule, c'est
le mode de développement avec le serveur Vite.

## Observabilité (`internal/metrics`)

`GET /metrics` sert une exposition Prometheus écrite à la main, en bibliothèque
standard comme le reste. Trois règles qui se redécouvriraient mal :

- **Aucune étiquette de cardinalité libre.** Jamais un nom de nœud, jamais un `vmid`,
  jamais une URL : chaque valeur vient d'un ensemble fermé — identifiant de cluster,
  famille d'endpoint (`metrics.ClassifyPath`), issue du vocabulaire `proxmox.Kind`.
  C'est à la fois une question de cardinalité et la règle qui tient les noms d'hôte
  hors d'un document qui sort du processus.
- **`/metrics` est authentifié**, contrairement à `/healthz` et `/readyz` :
  l'exposition nomme le parc.
- **L'appel PVE se mesure dans `read`, pas autour de `fetch`.** `read` est le point de
  passage unique d'un appel logique — bascule d'URL *et* décodage compris —, si bien
  qu'un cluster qui répond `200` avec un corps illisible compte `outcome="protocol"`
  et non `outcome="ok"` : le seul échec que le transport ne voit pas, et le compter en
  succès masquait un cluster cassé derrière un taux d'erreur plat. Ne pas redescendre
  la mesure dans `fetch` « parce que c'est là que part la requête ».

Une route PVE ajoutée se classe dans `ClassifyPath`, faute de quoi elle compte sous
`other`.

## Règles de sécurité

- Un token d'API Proxmox ne doit jamais atteindre le navigateur, ni un log, ni un
  message d'erreur renvoyé au client.
- La vérification TLS peut être assouplie **par cluster** (certificats auto-signés),
  jamais globalement, et toujours avec un avertissement explicite.
- Les actions destructrices (maintenance, migration, redémarrage) exigent une
  autorisation vérifiée côté backend, jamais seulement masquée côté UI.
