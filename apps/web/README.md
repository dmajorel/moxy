# apps/web

Frontend de moxy : l'interface multi-cluster construite sur `GET /api/overview`
pour la vue d'ensemble et sur les routes par objet — `.../nodes/{node}`,
`.../guests/{vmid}`, leurs `rrd` et leurs `tasks`, `.../maintenance/plan` — pour
le détail. Elle applique les décisions de design du §2 de
[`docs/PROXMOX_UI_HANDOFF.md`](../../docs/PROXMOX_UI_HANDOFF.md) — surfaces plates,
bordures fines, hiérarchie portée par la typographie.

## Ce qui est affiché à ce stade

- **La vue d'ensemble des clusters (écran 4)** : totaux inter-clusters, une carte
  par cluster avec son état, son graphe d'utilisation CPU et mémoire sur la
  dernière heure, sa barre de stockage, son compteur de VM, la liste de ses nœuds
  et son bandeau d'alerte. Le graphe remplace les jauges CPU et mémoire du §2 :
  une barre ne dit que l'instant, et la question de cet écran est de savoir si
  quelque chose dérive. Les valeurs instantanées restent en légende, et passent
  en ambre au-delà du seuil comme le faisait le remplissage des barres.
- **La vue nœud (écran 2)** : cartes CPU / mémoire / stockage local / load
  average, sparkline de charge à hauteur fixe, quorum, HA, noyau, mises à jour,
  et la liste des VM hébergées.
- **La vue VM (écran 1)** : état, uptime, CPU, mémoire, volumétrie allouée,
  mémoire hôte, adresse IPv4 quand l'agent la donne, étiquettes, la liste des
  disques et les tâches récentes de l'invité. Ces tâches viennent de sa propre
  route — `.../guests/{vmid}/tasks`, servie depuis le nœud hôte —, jamais d'un
  filtrage du journal du cluster : ce journal ne porte que ses dernières lignes,
  et une nuit de sauvegardes en chasse celles d'une machine donnée.
- **La carte « Volumétrie » compte tous les volumes, pas le disque de boot.**
  `maxdisk` ne désigne que le disque système d'une VM ou le `rootfs` d'un
  conteneur : une VM portant 32 Gio de système et 2 Tio de données s'affichait
  à 32 Gio. Le tableau « Disques » détaille chaque volume, son stockage et sa
  taille ; une taille inconnue — un périphérique passé tel quel, un volume
  détaché, pour lesquels PVE n'en enregistre aucune — rend le tiret cadratin et
  jamais un zéro. Un volume détaché est listé et marqué, mais reste hors du
  total : il occupe son stockage sans appartenir à l'invité. Quand la
  configuration n'a pas pu être lue, la carte retombe sur le disque de boot et
  le tableau disparaît.
- **Les étiquettes sont une liste clé/valeur, pas des pastilles après le nom.**
  La ligne du nom est réservée à l'état — un parc réel pose cinq à dix
  étiquettes par invité, qui la feraient passer sur trois lignes. Une étiquette
  PVE est déjà une paire : `splitTag` la coupe au **dernier** point, si bien que
  `ha.state.started` se lit `ha.state` / `started` et non `ha` /
  `state.started`. Une étiquette sans point — `production` — est un drapeau :
  elle reste entière en clé et rend le tiret cadratin en valeur. L'ordre servi
  par l'API est conservé, jamais trié : un tri inventerait une hiérarchie que le
  parc n'a pas.
- **Le plan de maintenance (écran 3)** : la modal qui nomme chaque invité à
  déplacer, sa destination et l'état de cette destination après coup.
- **Le journal du cluster** : les tâches récentes, avec leur durée calculée côté
  backend et leur état.
- **La recherche globale filtre l'arbre**, par nom de cluster, de nœud ou
  d'invité, et par `vmid` — insensible à la casse et aux accents, de sorte que
  « prepro » trouve « Préproduction ». Une ligne est gardée si elle répond, si
  un **ancêtre** répond (un cluster nommé montre tout ce qu'il contient), ou si
  un **descendant** répond, ce qui garde le chemin jusqu'au résultat visible.
  Ce qui reste est déplié : un résultat enfoui dans une branche repliée est un
  résultat que personne ne voit. `Entrée` ouvre le premier résultat, `Échap`
  vide le champ avant de le quitter, et un `aria-live` annonce le nombre de
  résultats — un filtre qui vide une liste en silence ne laisse aucun moyen à
  un lecteur d'écran de savoir pourquoi.
- **La carte affiche toutes les alertes du cluster**, empilées dans l'ordre de
  gravité que le backend sert, et non la seule première. L'en-tête les comptait
  toutes pendant que la carte en montrait une : l'opérateur cherchait la
  seconde sur une autre carte et ne la trouvait pas. Empilées plutôt que
  repliées derrière un « +1 », pour la raison qui fait déjà lister tous les
  nœuds : l'intérêt de cet écran est de repérer ce qui demande attention, et ce
  qui est à un clic est ce sur quoi personne n'a cliqué.
- **La cloche ouvre la liste de toutes les alertes**, tous clusters confondus,
  chaque ligne menant à son cluster — la même information, rassemblée, quand on
  ne veut pas parcourir les cartes.
- **Le layout** : barre supérieure (logo, sélecteur de cluster, recherche,
  notifications, thème), arbre latéral, zone contextuelle, chacune des deux
  colonnes défilant pour son compte. Le panneau de gauche part des 190 px
  nominaux de l'annexe A.2 et se redimensionne entre 150 et 420 px, à la souris
  depuis le séparateur ou au clavier ; la largeur choisie est mémorisée dans
  `localStorage` et un double-clic revient à la largeur nominale.
- **L'arbre** des clusters, de leurs nœuds et de leurs invités, avec la sélection
  partagée entre la barre supérieure et l'arbre.

### Ce qui n'est pas là, et pourquoi

Rien de ce qui suit n'est échafaudé ici — mieux vaut une vue absente qu'une vue
qui ment.

- **Pas de barre d'onglets sur les vues nœud et VM.** Le §2 en dessine six
  (Résumé, Matériel/VM, Cloud-init/Disques, Snapshots/Réseau, Pare-feu,
  Options/Mises à jour) ; une seule a du contenu aujourd'hui. Cinq onglets morts
  promettraient ce qui n'existe pas. La barre s'ajoutera quand un deuxième
  onglet aura de quoi s'afficher.
- **Pas de bouton d'exécution de la mise en maintenance**, même désactivé. Ce
  n'est pas un manque côté frontend : **PVE n'expose aucune route REST** pour
  vidanger un nœud. `node-maintenance-set` vit dans la CLI `ha-manager`, et
  l'API2 HA ne propose que `current`, `manager_status`, `disarm-ha` et `arm-ha`.
  La modal donne donc la commande exacte et s'arrête là ; le calcul du plan,
  lui, est en lecture seule.
- **Pas de bouton « Ajouter un cluster »**, et il n'y en aura pas. Déclarer un
  cluster, c'est fournir une URL, un `tokenId` et le secret d'un token
  d'hyperviseur : le faire depuis le navigateur ferait transiter ce secret par
  le client, ce que la règle de sécurité du `CLAUDE.md` interdit, et obligerait
  moxy à le stocker. Un cluster se déclare dans la configuration du serveur,
  décrite dans le `README.md` à la racine. C'est un écart assumé au §2, qui
  dessine ce bouton dans l'en-tête de la vue d'ensemble et au pied de l'arbre.
- **Pas d'avatar dans la barre.** Le §2 en dessine un ; il affichait un « ? »
  avec l'infobulle « Authentification non configurée », c'est-à-dire un
  contrôle représentant une identité qui n'existe pas. Il reviendra le jour où
  il y aura un nom à y mettre.
- **La recherche ne cherche pas dans les tâches.** Elle filtre l'arbre, et
  l'arbre ne contient pas de tâche ; le placeholder le dit — « Rechercher une
  VM ou un nœud… » — plutôt que de promettre autre chose.
- **Pas de flux poussé.** Le temps quasi réel se fait par scrutation — 5 s pour
  la vue d'ensemble et pour le détail, 60 s pour les séries RRD, que le cache
  court du backend absorbe. SSE et WebSocket restent à venir.

## Démarrage

```sh
npm install
npm run dev          # http://127.0.0.1:5173
```

**Il faut moxyd en face.** Le serveur de développement ne sert que le frontend ;
les données viennent du backend. Le plus simple, sans cluster Proxmox joignable,
est le mode mock :

```sh
# terminal 1, à la racine du dépôt
./scripts/build.sh && ./bin/moxyd -mock

# terminal 2
cd apps/web && npm run dev
```

Le serveur de dev **proxie `/api` vers `http://127.0.0.1:8080`**. La cible se
surcharge par la variable d'environnement `MOXY_API` :

```sh
MOXY_API=http://127.0.0.1:9090 npm run dev
```

Le proxy n'est pas un confort : **le backend ne sert délibérément aucun en-tête
CORS**. Navigateur et API vivent sous la même origine, ce qui évite d'ouvrir une
surface inutile sur un service qui détient des tokens d'hyperviseur. Appeler
`moxyd` depuis une autre origine ne marchera pas, et c'est voulu.

## Scripts

| Commande | Rôle |
|---|---|
| `npm run dev` | Serveur de développement Vite, port 5173, proxy `/api`. |
| `npm run build` | `tsc -b` puis `vite build` → `dist/`. |
| `npm run preview` | Sert le bundle produit, pour vérifier un build. |
| `npm run lint` | ESLint sur tout le paquet. |
| `npm run test` | Vitest, une passe (`vitest run`), environnement jsdom. |
| `npm run typecheck` | `tsc -b --noEmit`. |

Depuis la racine du dépôt, deux scripts enveloppent tout cela — ils installent les
dépendances si `node_modules` est absent, en préférant `npm ci` quand le lockfile
est là :

```sh
./scripts/check-web.sh   # typecheck, lint, tests
./scripts/build-web.sh   # bundle dans apps/web/dist
```

Ce sont les pendants de `./scripts/check.sh` et `./scripts/build.sh` du backend, et
`make check-web` / `make build-web` les exposent depuis la racine.

`npm run preview` ne sert qu'à vérifier un build. En production, c'est `moxyd` qui
sert le bundle (`./bin/moxyd -web apps/web/dist`, et l'image de conteneur fait de
même) : frontend et API partagent alors une seule origine, ce qui est exactement ce
que suppose l'absence de CORS.

## Stack

- **React 19** et **TypeScript 6** en mode `strict`, avec en plus
  `noUncheckedIndexedAccess` (un accès par index rend `T | undefined` : le payload
  a des tableaux qui peuvent être vides) et `verbatimModuleSyntax` (les imports de
  types s'écrivent `import type`, sans exception).
- **Vite 8** pour le dev et le build, avec l'alias `@` → `src/`.
- **Tailwind 4**, chargé par `@tailwindcss/vite`, configuré en CSS et non en
  fichier JS.
- **Vitest 5** en environnement jsdom, avec Testing Library et `jest-dom`.
- **ESLint 10** en flat config (`eslint.config.js`), lint *type-aware* : les règles
  qui attrapent un `null` mal traité sont précisément celles qui ont besoin des
  types. Les deux règles `react-hooks` sont des erreurs, pas des avertissements —
  un effet aux dépendances fausses fait rater ou dupliquer un rafraîchissement,
  ce qui est un bug de correction dans une UI de supervision.
- **Icônes** : `@tabler/icons-react` (les `ti ti-*` du §2, en composants React).

## Thème

Les tokens du §2 vivent dans [`src/styles/tokens.css`](src/styles/tokens.css), en
variables CSS sur `:root`, et sont exposés en utilitaires Tailwind par un bloc
`@theme inline`. Tailwind les lit alors comme des valeurs de thème de premier
rang : `bg-surface-2`, `text-text-muted`, `border-border`, `rounded-card`,
`bg-bg-warning`, `font-mono` résolvent vers les tokens, et non vers une palette
parallèle.

### Couleurs sémantiques

| Rôle | Trait | Fond | Texte | Sens |
|---|---|---|---|---|
| Succès | `#1D9E75` | `#E1F5EE` | `#085041` | Sain, running |
| Avertissement | `#EF9F27` | `#FAEEDA` | `#633806` / `#854F0B` | Maintenance, dégradé, action à conséquence, tâche terminée avec avertissements |
| Échec | `#C8393A` | `#FBE8E8` | `#7A1F20` | Une tâche qui n'a pas fait ce qu'on lui demandait |
| Voile | — | `--scrim` | — | Le fond assombri derrière une modale. Un token, et non `bg-black/45` : noir à 45 % sur le `#101216` du thème sombre est presque invisible, et le dialogue flottait sans rien derrière lui. |
| Accent | `#378ADD` | `#E6F1FB` | `#1B5E9E` | Données neutres : barres, graphes, sélection. **Jamais un statut.** |
| Marque | `#2D679C` | — | — | Bleu de la marque, **réservé au logo** : jamais un statut, jamais un bouton |

**Aucune couleur en dur dans un composant, jamais.** Pas de `#1D9E75`, pas de
`bg-green-500`, pas de `style={{ color: ... }}`. Une couleur qui manque se
rajoute dans `tokens.css`, où elle est visible, nommée et revue — pas au fond d'un
JSX. Même règle pour les rayons (`rounded-card`, `rounded-panel`) et pour la
bordure 0,5 px, qui est un choix de design et non un arrondi de pixel.

La seule couleur qui peut arriver au runtime est `clusters[].color`, configurée
côté backend par cluster et passée telle quelle.

Le rouge est le seul ajout à la palette du §2, qui n'en prévoit pas : ses
maquettes ne montrent aucune tâche en échec. Il est devenu nécessaire le jour
où le journal a dû distinguer trois fins de tâche — `ok`, `warnings`, `failed`
— car un échec et un avertissement rendus tous deux en ambre ne se seraient
différenciés que par leur libellé, sur la ligne même qui rapporte ce qui s'est
mal passé. Il reste réservé à cela : un état de fait négatif, jamais une action
à conséquence, qui est l'ambre.

### Clair, sombre, système

Le thème sombre est **un second jeu de valeurs derrière les mêmes noms de
tokens**, au bas de `tokens.css` : aucun composant ne sait quel thème est actif,
il nomme un rôle (`bg-surface-1`, `text-text-muted`) et le rôle est résolu là.
C'est la raison pratique de la règle ci-dessus : une couleur écrite dans un JSX
casse le thème sombre par construction, et pas seulement par principe.

Les rôles sont conservés, pas inversés : `--surface-0` reste la page,
`--surface-1` la colonne latérale, `--surface-2` le chrome surélevé ; ce qui
change, c'est que « surélevé » veut maintenant dire plus clair au lieu de plus
blanc.

**Les deux thèmes portent la même garantie de contraste** : tous les couples
texte / fond dépassent 4,5:1 sur les trois surfaces, et tous les aplats d'état
3:1 — à une exception près, l'ambre `#EF9F27` du §2, qui tombe à 2,0:1 sur fond
clair (8,0:1 en sombre). C'est une couleur imposée par la spécification,
toujours employée en aplat (barre, point) et toujours doublée d'un équivalent
textuel : elle est assumée et consignée comme telle dans le test.

`src/styles/tokens.test.ts` lit `tokens.css`, en dérive les deux palettes et
calcule chaque couple avec [`src/lib/contrast.ts`](src/lib/contrast.ts) (formules
WCAG 2.1). Un token qui repasse sous son seuil fait donc échouer `make
check-web`, au lieu de partir en production. C'est ce test qui a chassé
`--text-muted: #8b919c` du thème clair : 2,98:1 sur `--surface-1` pour du 11 px,
remplacé par `#6b7280` (4,55:1 sur la même surface, 4,83:1 sur `--surface-0`,
4,67:1 sur `--surface-2`).

Trois états, pas deux. Le contrat avec la feuille de style tient en un attribut
sur `<html>` :

| `data-theme` | Effet |
|---|---|
| `"light"` | Clair explicite, l'emporte sur un système sombre |
| `"dark"` | Sombre explicite, l'emporte sur un système clair |
| absent | Suit `prefers-color-scheme` |

« Suit le système » est donc l'état par défaut, et il est traité par la media
query de `tokens.css` — sans JavaScript, ce qui veut dire que le bon thème est
peint même si le bundle est lent ou bloqué. Le choix explicite l'emporte grâce au
garde `:not([data-theme="light"])` sur la règle sombre.

Le choix est conservé dans `localStorage` sous la clé `moxy.theme`, et **tout
accès est protégé** : lire `window.localStorage` lève carrément dans une fenêtre
où les données de site sont bloquées, écrire lève en navigation privée une fois
le quota épuisé. Un échec dégrade vers le thème système, jamais vers une page
blanche. Revenir à « Système » efface l'entrée plutôt que d'écrire le mot :
l'absence de préférence *est* le défaut.

Le **flash de thème clair** au chargement est évité par un petit script,
`public/theme-boot.js`, que `index.html` charge dans `<head>` par un `<script
src>` classique et bloquant : il pose l'attribut avant la première peinture — un
module serait différé, donc trop tard. C'est un fichier et non un script inline
pour que la `Content-Security-Policy` servie par `moxyd` n'ait besoin ni
d'`'unsafe-inline'` ni d'une empreinte à tenir à jour. Il ne peut pas importer
`src/lib/theme.ts` puisqu'il s'exécute avant le bundle : il redit la clé et
l'attribut à la main, et `theme.test.ts` vérifie que les deux orthographes n'ont
pas divergé.

Côté React, `src/lib/theme.ts` porte la logique pure et le stockage,
`src/lib/useTheme.ts` l'état, et `src/components/ThemeToggle.tsx` le contrôle de
la barre supérieure (menu à trois entrées : « Clair », « Sombre », « Système »).
La préférence est tenue par la racine de l'application, comme la sélection de
cluster : la barre supérieure reste un composant contrôlé.

## Organisation du code

| Chemin | Contenu |
|---|---|
| `src/api/types.ts` | Les types du payload, **miroir de `apps/api/internal/aggregate/model.go` et de `internal/detail/model.go`** |
| `src/api/client.ts` | `fetchOverview()`, `fetchHealth()`, les lectures de détail (`fetchNode`, `fetchGuest`, les séries, les tâches, le plan), la construction des chemins et les erreurs typées `ApiRequestError` (dont le `path` est obligatoire) / `ApiParseError` |
| `src/api/usePolledResource.ts` | Le socle de scrutation commun : dernier instantané conservé, `isStale`, rafraîchissement manuel |
| `src/api/useOverview.ts` | Le hook (5 s) qui alimente la vue d'ensemble et l'arbre |
| `src/api/useDetail.ts` | Les hooks par objet : `useNode`, `useGuest`, les séries (60 s), `useTasks`, `useMaintenancePlan` |
| `src/api/useHealth.ts` | La version servie par `/healthz`, lue une seule fois au montage et jamais scrutée |
| `src/lib/format.ts` | Tout le formatage d'affichage |
| `src/lib/errors.ts` | La classification d'un échec d'API (`classifyError`) et la phrase française qui lui correspond (`explainError`) |
| `src/lib/overview.ts` | Le filtrage de la vue d'ensemble sur le cluster sélectionné |
| `src/lib/contrast.ts` | Luminance relative et ratio de contraste WCAG 2.1, dont vit `styles/tokens.test.ts` |
| `src/lib/theme.ts` | Préférence de thème : lecture, stockage, pose sur le document |
| `src/lib/useTheme.ts` | La préférence de thème en état React |
| `src/components/ui` | Primitives : `StatusDot`, `Tag`, `UsageBar`, `MetricCard`, `AlertBanner`, `KeyValue`, `Sparkline` |
| `src/components` | Barre supérieure, sélecteur de cluster, bascule de thème, arbre, coquille applicative, vues d'état, en-tête d'objet, tableau des tâches |
| `src/screens` | Les écrans : vue d'ensemble et carte de cluster, vue nœud, vue VM, journal du cluster, modal de plan de maintenance, et les conteneurs qui les alimentent (`DetailRoutes`) |
| `src/styles` | `tokens.css` (le thème) et `index.css` (le point d'entrée Tailwind) |
| `public` | Les fichiers copiés tels quels à la racine du bundle : `theme-boot.js`, le script anti-flash chargé avant lui |

### La règle qui structure tout

**Le backend ne renvoie que des octets et des ratios**, jamais une chaîne
localisée : `memory.used` est un entier d'octets, `cpu.ratio` une fraction dans
`[0,1]`, `uptime` des secondes. Le choix de l'unité, l'arrondi, la virgule
décimale et l'espace fine insécable avant le `%` sont la responsabilité du
frontend.

Conséquence pratique : **toute mise en forme passe par `src/lib/format.ts`**
(`formatBytes`, `formatUsage`, `formatRatio`, `formatCores`, `formatVcpus`,
`formatUptime`, `formatLoadAverage`, `formatRelativeTime`, `formatTime`,
`formatDateTime`, `formatGuestName`, `formatGuestRef`, `formatGuestKind`,
`formatGuestStatus`, `formatNodeStatus`, `formatClusterStatus`, `formatQuorum`,
`formatAlert`, `formatTaskLabel`, `formatTaskOutcome`, `plural`,
`formatInteger`). Un composant qui écrit `${Math.round(ratio * 100)} %`
introduit une seconde convention typographique qui divergera de la première ; il
n'y a qu'un seul endroit où l'on décide comment s'écrit une taille.

Cela vaut aussi pour les **libellés**, et pas seulement pour les chiffres : un
état porte **un seul mot**. « template », « Template » et « Modèle » ont nommé
le même état dans trois fichiers, ce qui se lit comme trois états ; c'est
`formatGuestStatus` qui le décide, et `StatusDot` comme l'arbre le lisent de
là. De même, un conteneur LXC n'est pas une machine virtuelle : `formatGuestRef`
écrit « CT 105 » comme PVE et `pct`, jamais « VM 105 ».

Le tiret cadratin s'écrit `FALLBACK`, jamais `"—"` : c'est la même valeur, mais
la constante dit ce qu'elle signifie — « inconnu » — et se retrouve par une
recherche.

`format.ts` ne lève jamais : une valeur inexploitable (NaN, infinie, négative)
rend le tiret cadratin `—`, qui se lit « inconnu » dans l'interface. C'est aussi
ce qu'il faut afficher pour un `null` du payload, qui signifie « inconnu » et non
« zéro ».

### Deux règles qui se redécouvriraient mal

- **La sparkline ne s'auto-échelonne jamais.** `Sparkline` fixe son axe à
  `[0, scaleMax]`, `1` par défaut. C'est la correction du défaut central de
  l'interface native, qui redimensionne à la donnée et transforme un nœud à
  0,6 % en chaîne de montagnes. L'échelle ne se dérive donc pas des points, et
  un trou RRD coupe la courbe au lieu d'être tracé à zéro — un `null` reste un
  « inconnu » là aussi.
- **Rien de plein ne survit à `preserveAspectRatio="none"`.** La `viewBox` fait
  300 de large et s'étire à la largeur du conteneur ; `vectorEffect` protège
  les **traits**, jamais la géométrie de remplissage. Le point terminal était
  un `<circle r={3}>` : une ellipse 9×3 sur une carte de 900 px, dont la forme
  changeait avec le panneau redimensionnable. C'est désormais un trait de
  longueur nulle à extrémités rondes (`M x y h0`), exempt de l'étirement. Toute
  forme ajoutée à ce composant suit la même règle.
- **Les écrans nœud et VM portent trois repères horaires** sous la courbe —
  début, milieu et fin de la fenêtre, comme l'annexe A.1 les dessine. Un graphe
  sans axe horaire ne dit pas **quand** a eu lieu le pic qu'il montre. Le
  repère médian est l'échantillon médian et non le milieu des deux instants :
  les marques s'alignent sur les points réellement tracés. Les cartes, hautes
  de 48 px, gardent « Dernière heure » en légende.
- **Un échec d'API se classe avant de se raconter.** `ErrorView` ne compose
  aucune phrase : `src/lib/errors.ts` traduit la classe de l'échec — plus de
  réponse du tout (`status: 0`), objet disparu (`404`), refus de droits (`403`),
  cluster injoignable (`502`), délai dépassé (`504`), écran indisponible sans
  cluster (`501`), paramètre refusé (`400`), autre `5xx`, ou réponse illisible
  (`ApiParseError`) — en titre et en texte. Un seul libellé pour tout envoyait
  vérifier que `moxyd` tourne alors qu'il venait de répondre `404`. Seul le
  `404` propose « Retour à la vue d'ensemble » : c'est le seul échec que
  réessayer ne répare pas.
- **Une exception de rendu ne vide jamais la page non plus.** React démonte
  l'arbre **entier** quand un rendu lève et que rien ne l'attrape : un champ
  manquant dans un payload — backend en avance d'une version, proxy qui
  tronque, clé renommée — emportait la barre supérieure et l'arbre avec
  l'écran qui l'avait lu. `ErrorBoundary` (le seul composant de classe du
  dépôt : `getDerivedStateFromError` n'a pas d'équivalent en hook) entoure le
  contenu de `AppShell` et chaque écran de détail. Le bouton « Réessayer »
  **remonte** le sous-arbre par une clé : effacer l'erreur seule re-rendrait
  les composants qui ont levé, avec leur état, et ils lèveraient de nouveau.
  La frontière ne remplace pas les gardes de forme d'`api/client.ts`, qui
  transforment un payload malformé en `ApiParseError` expliquée ; elle attrape
  ce qu'elles n'ont pas prévu — c'est-à-dire, par définition, l'imprévu.
- **Une erreur de scrutation ne vide jamais la vue.** `usePolledResource`, et
  donc `useOverview` comme les hooks de détail, conserve le dernier instantané
  connu et lève `isStale` : le bandeau dit depuis quand la donnée date et offre
  de réessayer. C'est le pendant du backend, qui sert le dernier état connu d'un
  cluster injoignable plutôt qu'une page vide.
- **Rien n'est scruté pendant qu'un onglet est caché**, et le retour déclenche
  une requête **immédiate**. Un onglet en arrière-plan continuait de demander,
  ralenti par le navigateur à environ un tick par minute : au retour,
  l'opérateur regardait une donnée pouvant avoir une minute sans qu'aucun tick
  ne la rafraîchisse, si bien qu'un nœud tombé il y a cinquante secondes était
  encore vert. Le retour en ligne (`online`) fait la même chose.
- **L'intervalle double après chaque échec consécutif**, jusqu'à une minute, et
  revient à sa valeur nominale au premier succès comme à un « Réessayer »
  explicite : dix échecs valaient dix requêtes par minute contre quelque chose
  qui ne répond pas. La chaîne est faite de `setTimeout` et non d'un
  `setInterval`, parce qu'un intervalle ne change pas de durée.
- **Un seul bandeau de connexion perdue à l'écran.** Celui de la vue d'ensemble
  parle pour elle ; sur une vue d'objet, c'est celui de l'objet. Les deux
  tombaient en `isStale` ensemble et s'empilaient à l'identique.
- **Les séries des cartes ne sont demandées que quand les cartes sont
  affichées.** Une vue nœud ou VM n'en rend aucune, et la liste était tout de
  même scrutée : N appels `/rrd` par minute, chacun une lecture RRD amont, pour
  un graphe que personne ne regardait.
- **`useNow` fait vivre les libellés relatifs.** « il y a 12 s » n'était
  recalculé qu'au rendu suivant — c'est-à-dire, pendant une panne, plus du
  tout : l'âge de la lecture se figeait au moment précis où il devenait
  intéressant. Une seule horloge pour toute la grille, arrêtée quand l'onglet
  est caché.

### L'URL est la sélection

Quatre routes, et rien d'autre dans le chemin :

```
/                                   tous les clusters
/clusters/{id}                      un cluster
/clusters/{id}/nodes/{node}         un nœud
/clusters/{id}/guests/{vmid}        une machine
```

Elles reprennent la forme de l'API : un opérateur qui lit un lien moxy et un
opérateur qui lit un journal `moxyd` voient la même chose. `src/lib/routes.ts`
les analyse et les écrit, `src/lib/useLocation.ts` branche `pushState` et
`popstate` sur React par `useSyncExternalStore`. **Aucune dépendance de
routage** : l'ensemble tient en quarante lignes, moins à auditer qu'une
bibliothèque.

Trois règles qui se redécouvriraient mal :

- **La route de la machine ne porte pas son nœud.** Une VM migre, et un lien
  qui aurait épinglé le nœud où elle était pourrirait à la première migration.
  Le nœud hôte se retrouve dans la vue d'ensemble, qui est l'endroit où cette
  vérité vit. L'arbre, lui, a besoin du nœud pour déplier la bonne branche :
  `App` fait la réconciliation.
- **Le filtre de cluster fait un `replaceState`, pas un `pushState`.** Réduire
  la vue affine l'écran où l'on est déjà ; l'empiler ferait défaire un choix de
  menu clic par clic au bouton précédent.
- **La modale de maintenance n'entre pas dans l'URL.** C'est un état
  transitoire, pas un lieu.

Tout segment est validé au décodage — segment vide, `.`, `..`, encodage
invalide, `vmid` hors des bornes de PVE — et rend « Objet introuvable » plutôt
qu'une requête construite dessus. C'est la règle du backend sur les mêmes
formes, pour la même raison. Le titre de l'onglet suit
(`prox-pprd-2302-cit · Préproduction · moxy`) : dix onglets appelés « moxy »
sont dix onglets indiscernables, ce qui est exactement l'état où un incident
les laisse.

Côté serveur, rien à faire : `moxyd -web` sert déjà `index.html` pour tout
chemin sans extension, et le serveur de développement Vite fait de même.

### Le contrat avec le backend, vérifié et non promis

`src/api/types.ts` est le miroir de `apps/api/internal/aggregate/model.go`,
`detail/model.go` et `plan.go`. Cette correspondance n'est plus tenue par la
seule discipline :

- **Les fixtures de `src/test/fixtures/` sont générées** par
  `TestMockMatchesWebFixtures`, côté Go, à partir du démon mock sur une horloge
  figée. Elles ne se recopient pas à la main — elles l'étaient, et avaient
  dérivé d'un cluster entier. Pour les régénérer :
  `cd apps/api && go test ./internal/server -update`.
- **`src/api/types.contract.test.ts` vérifie les deux sens.** À la compilation,
  affecter une fixture à son interface échoue si `types.ts` déclare un champ
  que le payload ne porte pas. À l'exécution, la comparaison des jeux de clés
  échoue si le payload porte un champ que `types.ts` ne déclare pas. Le
  `satisfies Record<keyof T, true>` est ce qui rend la liste de clés
  obligatoirement exhaustive.
- Un dernier passage de CI vérifie que l'arbre de travail est propre, pour
  attraper la régénération faite en local et non commise.

Ajouter une route au backend veut donc dire l'ajouter à `webFixtures` dans
`fixtures_test.go` : c'est la seule façon qu'elle soit surveillée.

## Conventions

- **Le code et les commentaires sont en anglais**, sans exception : identifiants,
  noms de tests, messages d'erreur techniques.
- **Les libellés d'interface sont en français, sentence case** (« Plan de
  maintenance », « Tous les clusters », « Aucun cluster à afficher »). Ce sont les
  seules chaînes françaises du code, et elles vivent ici — le backend renvoie ses
  erreurs en anglais avec un `kind` traduisible, et c'est le frontend qui traduit.
- La documentation, elle, est en français : ce fichier compris.

## Accessibilité

Ce qui est attendu de tout composant ajouté ici :

- **L'arbre latéral est un vrai `role="tree"`**, navigable au clavier : flèches
  haut/bas pour se déplacer, gauche/droite pour replier et déplier, `Entrée` pour
  sélectionner, un seul point d'entrée dans l'ordre de tabulation. Une liste de
  `<div>` cliquables ne convient pas.
- **Tout menu ou popover se ferme à `Échap`** et rend le focus à l'élément qui
  l'a ouvert — c'est le cas du sélecteur de cluster, du panneau d'alertes et du
  champ de recherche.
- **Une modale piège le clavier et le rend.** `useFocusTrap` fait les deux :
  `Tab` et `Maj+Tab` cyclent sur les focalisables du dialogue, et le focus
  revient à la fermeture sur l'élément qui l'avait — le déclencheur, quel qu'il
  soit, puisque le hook le retient lui-même plutôt que de le recevoir en prop.
  Sans piège, `Tab` sort vers l'arbre et la barre, ce qui est une modale qui
  n'en est pas une ; sans retour, la fermeture laisse le focus sur `<body>`, et
  un utilisateur au clavier est renvoyé en haut du document. Le dialogue est
  nommé par son `<h2>` via `aria-labelledby`, jamais par un `aria-label` qui le
  répéterait.
- **Une information portée par une couleur a toujours un équivalent textuel.**
  Un point d'état vert n'est pas un statut pour un lecteur d'écran : il porte un
  `aria-label`, ou bien le texte à côté dit la même chose. Idem pour une barre
  au-delà du seuil, qui vire à l'ambre *et* déclenche une alerte nommée.
- **La poignée de redimensionnement du panneau est un `role="separator"`
  focalisable** (`aria-orientation="vertical"`, `aria-valuenow` / `aria-valuemin`
  / `aria-valuemax`) : flèches gauche/droite pour ajuster, `Origine` et `Fin`
  pour aller d'une borne à l'autre. Ce qui se fait à la souris se fait au
  clavier, sans exception.
- **Un conteneur ne prend jamais un rôle interactif.** `role="button"` porte
  « Children Presentational: true » dans WAI-ARIA : tout ce qu'il contient
  disparaît de l'arbre d'accessibilité. La carte de cluster l'a porté, et
  rendait donc la vue d'ensemble — le seul écran affiché en permanence —
  inaudible : « Cluster Qualification, bouton », et ni le statut, ni la mémoire
  à 83 %, ni « Quorum perdu ». Le motif à reprendre est celui de `ClusterCard` :
  l'`<article>` est nommé par son titre (`aria-labelledby`), le titre contient
  un vrai `<button>`, et la zone cliquable de ce bouton est étendue à la carte
  par un `::after` en `absolute inset-0`. Un point de tabulation, tout le
  contenu exposé, l'affordance souris conservée — au prix de la sélection de
  texte dans la carte, que le recouvrement absorbe.
- **Une décoration est `aria-hidden`, jamais nommée à vide.** `StatusDot` prend
  un `decorative` quand le libellé voisin dit déjà l'état ; `title=""` n'est pas
  nullish et produisait `aria-label=""` sur un `role="img"` — une image **sans
  nom** dans l'arbre d'accessibilité, ce qui est pire que la lecture en double
  qu'on voulait éviter.
- **Une barre annonce un pourcentage, pas un ratio.** `aria-valuetext` porte
  « 83 % » là où `aria-valuenow` se lirait « 0,83 ». Un ratio non fini
  n'annonce **rien** : omettre `aria-valuenow` est la façon dont un
  `progressbar` ARIA dit « indéterminé », et lire « 0 » sur un nœud non mesuré
  serait le même mensonge qu'afficher 0 %.
- **Un bandeau qui apparaît en cours de session porte `role="status"`.** Poli et
  non assertif : les données restent à l'écran, il n'y a rien à interrompre.
- **Chaque tableau déclare ses en-têtes** (`scope="col"`) et porte un
  `<caption class="sr-only">` : sans quoi il est annoncé « tableau » et rien
  d'autre.
- **Une zone défilante est focalisable.** Une liste que le clavier ne peut pas
  atteindre est une liste dont un utilisateur au clavier ne voit que le haut.
- **Un nom tronqué porte un `title`** : c'est la seule façon de lire la suite
  sans ouvrir l'objet.
- Un lien d'évitement (« Aller au contenu ») ouvre la coquille, et les icônes
  purement décoratives sont `aria-hidden`.
