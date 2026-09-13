# moxy

Surcouche web multi-cluster pour Proxmox VE. La spécification de référence est
`docs/PROXMOX_UI_HANDOFF.md` : lis-la avant toute tâche de fond. Les décisions de
design du §2 et les contraintes du §4 sont arrêtées, ne les rediscute pas sans
raison explicite.

## Langue

- **Le code source est en anglais**, sans exception : identifiants, commentaires,
  messages de log, chaînes d'erreur, noms de tests, scripts shell et workflows CI.
- **Les messages de commit sont en anglais**, pour la même raison que le code :
  ils décrivent le code et se lisent dans `git log` à côté de lui. Sujet à
  l'impératif, en minuscules, suivi d'un corps explicatif si le changement le
  mérite.
- **Les titres et descriptions de PR sont en anglais**, pour la même raison encore :
  GitHub recopie le titre de la PR dans le commit de fusion, sous le
  « Merge pull request #N », d'où il se lit dans `git log` comme n'importe quel
  autre sujet — et en devient le sujet lui-même si la PR est écrasée (`squash`).
  Le titre prend donc la forme d'un sujet de commit : impératif, en minuscules,
  sans point final. Le corps de la PR suit la langue de son titre. La revue en
  commentaires, elle, est une conversation et non du code : elle se tient dans la
  langue de l'échange.
- **La documentation est en français** : `README.md`, ce fichier, et `docs/`.
- **Les libellés de l'interface sont en français**, sentence case, comme l'impose
  le §2 du document de passation. Ce sont les seules chaînes françaises du dépôt,
  et elles vivent dans le frontend.
- Conséquence : le backend renvoie ses erreurs en anglais (`method not allowed`).
  La traduction vers l'utilisateur est la responsabilité du frontend, jamais de
  l'API.

## Suivi des tâches

Ce projet n'utilise pas `TASKS.md`. La convention globale de suivi des tâches dans
un `TASKS.md` ne s'applique pas ici : ne crée pas ce fichier, ne l'alimente pas,
n'y coche rien.

## Backend (`apps/api`)

- **Go, bibliothèque standard uniquement.** Aucune dépendance externe : c'est le
  principal levier de réduction de la surface d'attaque sur un service qui détient
  des tokens d'hyperviseur. `net/http`, `crypto/tls` et `encoding/json` couvrent
  l'intégralité des besoins du §4.
- Les scripts posent `GOPROXY=off` et `CGO_ENABLED=0`. Une dépendance introduite
  par inadvertance fait donc échouer la compilation — c'est voulu.
- `go test -race` n'est **pas** utilisable : le détecteur de courses exige CGO.
- Toolchain locale : Go 1.19.8, qui est aussi le niveau de langage de `go.mod`.
  Pas de `log/slog` (1.21), pas de `errors.Join` (1.20) ; une passe de CI en 1.19
  garde la contrainte mécanique plutôt que déclarative.
- **La directive `go 1.19` fixe le langage, pas la bibliothèque standard livrée.**
  L'image et la CI compilent avec la série Go supportée du moment ; compiler la
  livraison en 1.19 embarquerait une stdlib sans correctif depuis septembre 2023
  dans un démon qui termine du TLS et analyse des certificats fournis par ses
  pairs. Le `Containerfile` et la matrice de `ci.yml` bougent ensemble.

## Client PVE (`internal/proxmox`)

Pièges de l'API Proxmox déjà rencontrés, à ne pas redécouvrir :

- **`cpu` est une fraction `0..1`, pas un pourcentage.** `0.31` vaut 31 %. La même
  convention remonte telle quelle dans `/api/overview` ; la mise en forme est au
  frontend.
- **Les tailles sont en octets** (`mem`, `maxmem`, `disk`, `maxdisk`). Aucune
  conversion en GiB/TiB côté backend.
- **Toute réponse PVE est enveloppée dans `{"data": ...}`.** Le déballage est fait
  une fois pour toutes par le helper générique du client, pas dans chaque appelant.
- **Les types `Flex*`** (`FlexInt`, `FlexFloat`, `FlexBool`) existent parce que PVE
  sérialise ses nombres tantôt en nombre tantôt en chaîne, et ses booléens en `0`/`1`
  (`shared`, `template`, `quorate`, `online`). Ne pas les remplacer par des types
  natifs « parce que le schéma dit booléen » : le schéma ment.
- **`node_status` est imbriqué dans `manager_status`** sur un vrai cluster PVE 9,
  et non à plat comme le laisse croire le schéma. Le décodage accepte les deux
  formes ; ne le simplifie pas. Vérifié le 2026-09-12 sur un cluster à 6 nœuds.
- **Un stockage `shared` apparaît une fois par nœud** dans `/cluster/resources` : le
  dédoublonner par nom, sans quoi la capacité est multipliée par le nombre de nœuds.
  Clé de dédoublonnage : `storage` si partagé, `node/storage` sinon.
- **Les stockages Ceph (`rbd`, `cephfs`) adossés au même Ceph rapportent le même
  espace libre** : ils forment un seul backend dans le calcul de capacité
  (`deriveStorage`), jamais une somme. Vérifié le 2026-09-12 : 7 stockages Ceph
  additionnés donnaient 262 TiB pour 37 TiB réels. La clé de groupement est donc
  `ceph/<espace libre>` et non la seule constante `ceph` : un pool adossé à un
  **second** Ceph (Ceph mutualisé) rapporte un libre différent et compte à part,
  sans quoi sa capacité disparaissait derrière celle du premier.
- **Un stockage partagé sans taille (`maxdisk: 0`) n'est pas un backend.** Une
  cible iSCSI exposée directement accepte `images` sans rapporter de taille ;
  la compter donnait « 0 o / 0 o » au lieu du repli sur les stockages locaux.
- **`maxdisk` n'est pas la volumétrie d'un invité** : c'est le disque de boot
  d'une VM, le `rootfs` d'un conteneur, et rien d'autre. Le reste n'est que dans
  `/nodes/{node}/{kind}/{vmid}/config`, une clé par volume. Et **les tailles
  d'une configuration ne sont pas en octets**, à l'inverse de tout le reste de
  l'API : elles portent un suffixe 1024 (`size=32G`, `size=528K`). Un lecteur
  optique occupe une clé de disque sans rien allouer (`media=cdrom`, ISO ou
  non) ; un périphérique passé tel quel et un volume `unused` ne déclarent
  aucune taille, qui est donc inconnue et jamais nulle. Voir
  `GuestConfig.Disks`.
- **Sans `Sys.Audit` sur `/nodes/{node}`, PVE renvoie la ligne `node` sans
  `cpu`/`maxcpu`/`mem`/`maxmem`**, sans erreur. Un nœud en ligne sans mesures est
  donc « inconnu » (`cpu`/`memory` à `nil`, alerte `node_stats_unavailable`),
  jamais un nœud vide.
- **`/cluster/tasks` n'accepte aucun paramètre de requête**, et les refuse au lieu
  de les ignorer : son schéma est vide et interdit les propriétés additionnelles,
  si bien qu'un `?limit=25` vaut un `400 Parameter verification failed` et non une
  liste tronquée. Seule la route par nœud `/nodes/{node}/tasks` prend `limit`,
  `start` et les filtres. Le plafonnement du journal du cluster se fait donc côté
  moxy, après tri, dans `detail.Service.Tasks`. Vérifié le 2026-09-12 sur un
  cluster à 6 nœuds.
- **Les tâches d'un invité se lisent sur `/nodes/{node}/tasks?vmid=…`**, jamais en
  filtrant `/cluster/tasks` : ce journal ne porte que sa queue, et une nuit de
  `vzdump` en chasse les lignes de la machine qu'on regarde. `proxmox.NodeTasks`
  et `detail.Service.GuestTasks` en sont les deux moitiés. Contrepartie assumée :
  un invité qui a migré laisse son passé sur son ancien nœud.
- **Le paramètre `source` de `/nodes/{node}/tasks` vaut `archive` par défaut**, et
  `archive` ne contient que les tâches **terminées** : sans `source=all`, une
  sauvegarde en cours est absente de la liste censée la montrer. Vérifié dans
  `PVE/API2/Tasks.pm` le 2026-09-13 (énumération `archive`, `active`, `all`).
- **`Secret.Reveal()` est réservé au transport d'authentification du paquet
  `proxmox`** — il n'a qu'un seul appelant légitime, celui qui pose l'en-tête
  `Authorization`. Partout ailleurs, un `Secret` se rédige en `***` via ses méthodes
  `String`, `GoString`, `MarshalJSON` et `MarshalText`.
- **Interdit de journaliser ou de formater une `*http.Request`**, `httputil.DumpRequestOut`
  en tête : l'en-tête `Authorization` porte le secret. De même, une erreur ne doit
  jamais embarquer le corps ni les en-têtes d'une requête — seulement l'identifiant
  de cluster, le chemin, le code HTTP et la cause.

## API de détail (`internal/detail`)

Les routes par objet — `/api/clusters/{cluster}/nodes/{node}`, `.../guests/{vmid}`,
leurs `rrd`, et `.../tasks` — obéissent à d'autres règles que la vue d'ensemble.
Ce qui suit se redécouvrirait douloureusement.

- **La vue d'ensemble est scrutée, le détail est à la demande.** Un scrutateur
  d'arrière-plan rafraîchit `/api/overview` toutes les 5 s pour tous les clusters,
  parce que c'est le seul document affiché en permanence. Scruter au même rythme
  six nœuds et cent cinquante invités coûterait bien plus que ça ne vaut, et
  personne ne regarde plus d'un objet à la fois. Les routes de détail appellent
  donc PVE au moment de la requête, amorties par un **cache court** et un **verrou
  anti-troupeau** : dix onglets ouverts sur le même nœud ne déclenchent qu'un seul
  appel amont. Ne pas « uniformiser » en ajoutant ces objets au scrutateur.
- **`null` signifie « inconnu », jamais « zéro »** — y compris dans les points RRD.
  RRD renvoie des lacunes ; les remplir de `0` inventerait une chute qui n'a jamais
  eu lieu. Même règle pour `ipv4` sans agent invité, `haState` sans gestionnaire HA,
  `pendingUpdates` sans `Sys.Modify`, `quorum` sur un nœud seul.
- **Un appel facultatif qui échoue laisse son champ à `nil` sans faire échouer la
  réponse.** Seuls les appels essentiels propagent leur erreur. Un 403 sur
  `apt/update` ou un agent invité absent dégrade un champ, pas la requête : c'est
  la même philosophie que la vue d'ensemble, qui sert son dernier état connu
  plutôt qu'une page vide.
- **PVE n'a pas de RRD de cluster.** La série de la carte
  (`/api/clusters/{cluster}/rrd`) est repliée nœud par nœud : moyenne CPU
  pondérée par les cœurs — la règle de `deriveCPUAndMemory` —, mémoire sommée,
  pas appariés **sur l'horodatage** et jamais sur l'indice, un nœud entré en
  cours d'heure ayant moins de points. Les lectures par nœud passent par la même
  entrée de cache que la vue nœud, si bien qu'une carte et un onglet ouvert sur
  le même nœud ne coûtent qu'un appel. Un nœud illisible perd sa part de courbe ;
  l'erreur n'est propagée que si aucun nœud n'a répondu.
- **Les séries RRD portent des points, pas une image.** Le §2 impose une sparkline
  à hauteur fixe, jamais auto-échelonnée — l'auto-échelle transforme 0,6 % en pic.
  Le backend sert donc les fractions brutes et l'échelle est décidée au frontend.
  De même, la **durée d'une tâche est calculée ici**, en secondes : le défaut exact
  de l'interface native est d'afficher un début et une fin à soustraire de tête.
- **Le `ServeMux` de Go 1.19 n'a pas de paramètres de chemin** — ils sont arrivés
  en 1.22, et la toolchain locale est en 1.19.8. Les segments sont donc découpés à
  la main. Tout ajout de route doit **refaire la validation** : segments vides,
  `.` et `..`, décodage percent, et le nombre exact de segments attendus. Il n'y a
  pas de routeur pour l'attraper.
- **`detail/model.go` est un contrat**, comme `aggregate/model.go` : miroir de
  `apps/web/src/api/types.ts`, les deux fichiers bougent dans le même changement.
  Un champ ajouté côté Go sans son pendant TypeScript rompt le contrat en silence.
- **Les règles partagées avec la vue d'ensemble vivent dans
  `aggregate/rules.go`**, exportées, et ne se recopient pas : statut d'un nœud,
  quorum, genre et état d'un invité, `UsageOf`, `AsBytes`. Les deux vues
  décrivent les mêmes objets, et un opérateur qui lit « en maintenance » sur une
  carte et « hors ligne » sur la page du même nœud a été menti par l'une des
  deux, sans moyen de savoir laquelle. Elles ont été dupliquées, sous un
  commentaire demandant qu'on ne les change jamais d'un seul côté ; rien ne le
  faisait respecter.
- **Le mode mock répond aussi sur ces routes** (`detail.NewMock`), en dérivant
  ses réponses de la vue d'ensemble de démonstration plutôt qu'en inventant des
  objets à côté : un nœud ouvert depuis l'arbre porte les chiffres de sa carte.
  Ses séries comportent des trous et sa tâche la plus récente est en cours — un
  mock trop propre laisserait passer une UI incapable de les afficher.
- **`maintenance/plan` existe, `maintenance/execute` n'existera pas.** Vérifié
  dans les sources (2026-09-12) : `node-maintenance-set` est enregistré dans
  `PVE/CLI/ha_manager.pm` et écrit une commande CRM dans le système de fichiers
  du cluster ; l'API2 HA n'expose que `current`, `manager_status`, `disarm-ha`
  et `arm-ha`, et `PVE/API2/Nodes.pm` ne contient pas une occurrence de
  « maintenance ». **Il n'y a donc aucune route REST à appeler.** Ne pas
  ajouter de bouton d'exécution, même désactivé : l'UI donne la commande
  `ha-manager` et s'arrête là. Le calcul du plan, lui, est en lecture seule.
- Le temps quasi réel se fait **par scrutation**, pas par flux poussé : le
  journal du cluster relit `.../tasks` toutes les 5 s, ce que le cache court du
  service absorbe.

## Frontend (`apps/web`)

React 19 + TypeScript 6 `strict` + Vite 8 + Tailwind 4 + Vitest 5 + ESLint 10, avec
`@tabler/icons-react`. Le document de référence est `apps/web/README.md` ; ce qui
suit est ce qu'une session doit savoir pour ne pas se tromper.

- **Aucune couleur en dur dans un composant.** Les tokens du §2 vivent dans
  `src/styles/tokens.css` et sont exposés en utilitaires Tailwind par `@theme
  inline` : `bg-surface-2`, `text-text-muted`, `border-border`, `rounded-card`.
  Ni `#1D9E75`, ni `bg-green-500`, ni `style={{ color }}`. Une couleur qui manque
  s'ajoute à `tokens.css`, jamais au fond d'un JSX. Le bleu `#2D679C` est réservé
  au logo : jamais un statut, jamais un bouton.
- **Le logo est la marque en X de `ui/Logo.tsx`**, pas le carré orange du §2 :
  quatre barres à 45°, deux par diagonale, séparées par deux fentes fines qui se
  croisent au centre, chaque bras coupé à plat à l'horizontale. C'est une
  dérogation assumée au §2, demandée explicitement. Les fentes sont des trous et
  non des traits blancs — la page les traverse —, si bien que la marque tient sur
  n'importe quel fond ; elle se peint en `currentColor` depuis `text-brand`, qui
  s'éclaircit en thème sombre. Tracé plein et jamais au trait : un X au trait
  épaissirait avec la taille et brouillerait ses bouts à 22 px.
- **Aucun formatage ad hoc.** Octets, secondes, ratios et libellés d'état passent
  tous par `src/lib/format.ts`. Un `Math.round(ratio * 100)` écrit dans un
  composant crée une seconde convention typographique qui divergera de la
  première ; il n'y a qu'un endroit où l'on décide comment s'écrit une taille.
- **`src/api/types.ts` est le miroir de `apps/api/internal/aggregate/model.go`.**
  Les deux fichiers bougent ensemble, dans le même changement : un champ ajouté
  côté Go sans son pendant TypeScript est un contrat rompu silencieusement.
- **`null` signifie « inconnu », pas « zéro ».** `updates: null` veut dire que la
  question n'a pas pu être posée, `pendingUpdates: null` de même par nœud,
  `quorum: null` désigne un nœud seul. L'UI rend alors le tiret cadratin `—`, pas
  un `0` qui affirmerait quelque chose de faux.
- **`useOverview` ne vide jamais ses données sur erreur.** Il conserve le dernier
  instantané connu et signale `isStale`, à l'image du backend qui sert le dernier
  état connu d'un cluster injoignable. Une erreur de scrutation ne doit jamais
  faire disparaître la vue.
- **Libellés d'interface en français, sentence case ; code et commentaires en
  anglais.** Le backend renvoie ses erreurs en anglais avec un `kind` traduisible :
  la traduction est la responsabilité du frontend.
- Accessibilité : l'arbre est un vrai `role="tree"` navigable au clavier, les menus
  se ferment à `Échap` en rendant le focus, et une information portée par une
  couleur a toujours un équivalent textuel.
- **Les cartes de cluster portent un graphe d'utilisation sur la dernière heure,
  pas de jauges CPU et mémoire.** Une barre ne dit que l'instant ; la vue
  d'ensemble sert à repérer une dérive. Les valeurs instantanées restent en
  légende et virent à l'ambre au-delà du seuil, le stockage garde sa barre (PVE
  n'expose pas d'historique de capacité partagée). C'est une dérogation assumée
  au §2, demandée explicitement.
- **La sparkline ne s'auto-échelonne jamais.** `Sparkline` fixe son axe à
  `[0, scaleMax]`, défaut 1. Elle prend des séries de ratios (`lib/series.ts`
  traduit les points RRD), une ou deux, jamais des `Point` bruts. C'est la correction du défaut central de l'UI
  native, qui redimensionne à la donnée et transforme un nœud à 0,6 % en chaîne
  de montagnes. Ne dérive jamais l'échelle des points, et laisse un trou RRD
  couper la courbe plutôt que de le tracer à zéro.
- **Pas de barre d'onglets sur les vues nœud et VM.** Le §2 en dessine six, une
  seule a du contenu ; cinq onglets morts promettraient ce qui n'existe pas.
  Elle s'ajoutera quand un deuxième onglet aura de quoi s'afficher.
- **Ne documente ni n'échafaude ce qui n'existe pas.** La mise en maintenance et
  le temps réel sont encore à venir.

## Vérifications

```sh
make check       # tout : backend et frontend
make check-api   # gofmt, go vet, go test
make check-web   # typecheck, eslint, vitest
make build       # compile bin/moxyd
make build-web   # bundle dans apps/web/dist
make image       # image OCI (podman ou docker), voir Containerfile
make mock        # compile puis lance moxyd sur les données d'exemple
```

`make help` liste les cibles. **Le Makefile n'est qu'une enveloppe autour de
`scripts/`** : ce sont les scripts qui font foi, puisque la CI les appelle
directement. Dupliquer leur logique dans le Makefile les ferait diverger. Une
nouvelle vérification s'ajoute donc dans un script, et le Makefile ne fait que
l'exposer.

### Les fixtures du frontend sont générées

`apps/web/src/test/fixtures/*.json` **ne se recopient pas à la main** : elles
sont produites par `TestMockMatchesWebFixtures`
(`apps/api/internal/server/fixtures_test.go`) à partir du démon mock, sur une
horloge figée. Le test échoue sur une fixture périmée ; pour la régénérer :

```sh
cd apps/api && go test ./internal/server -update
```

C'est le mécanisme qui rend mécanique la règle du contrat énoncée plus haut.
Un champ renommé dans `model.go` fait échouer `make check-api` tant que les
fixtures ne sont pas régénérées, et `types.contract.test.ts` fait échouer
`make check-web` tant que `types.ts` ne suit pas. Une route de plus se déclare
dans `webFixtures`, faute de quoi c'est elle que personne ne verra dériver.

Le produit se livre en conteneur : une image unique où `moxyd -web` sert le bundle
du frontend sous la même origine que l'API. Sans `-web`, `moxyd` reste API seule,
c'est le mode de développement avec le serveur Vite.

## Observabilité (`internal/metrics`)

`GET /metrics` sert une exposition Prometheus écrite à la main, en bibliothèque
standard comme le reste. Trois règles qui se redécouvriraient mal :

- **Aucune étiquette de cardinalité libre.** Jamais un nom de nœud, jamais un
  `vmid`, jamais une URL : chaque valeur vient d'un ensemble fermé —
  identifiant de cluster, famille d'endpoint (`metrics.ClassifyPath`), issue du
  vocabulaire `proxmox.Kind`. C'est à la fois une question de cardinalité et la
  règle qui tient les noms d'hôte hors d'un document qui sort du processus.
- **`/metrics` est authentifié**, contrairement à `/healthz` et `/readyz` :
  l'exposition nomme le parc.
- **L'appel PVE se mesure dans `read`, pas autour de `fetch`.** `read` est le
  point de passage unique d'un appel logique : il englobe la bascule d'URL *et*
  le décodage, si bien qu'un cluster qui répond `200` avec un corps qui ne
  s'analyse pas compte `outcome="protocol"` et non `outcome="ok"`. C'est le seul
  échec que le transport ne voit pas, et le compter en succès masquait un
  cluster cassé derrière un taux d'erreur plat. Ne pas redescendre la mesure
  dans `fetch` « parce que c'est là que part la requête ».

Une route PVE ajoutée se classe dans `ClassifyPath`, faute de quoi elle compte
sous `other`.

## Règles de sécurité

- Un token d'API Proxmox ne doit jamais atteindre le navigateur, ni un log, ni un
  message d'erreur renvoyé au client.
- La vérification TLS peut être assouplie **par cluster** (certificats auto-signés),
  jamais globalement, et toujours avec un avertissement explicite.
- Les actions destructrices (maintenance, migration, redémarrage) exigent une
  autorisation vérifiée côté backend, jamais seulement masquée côté UI.
