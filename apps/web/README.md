# apps/web

Frontend de moxy : l'interface multi-cluster construite sur `GET /api/overview`.
Elle applique les décisions de design du §2 de
[`docs/PROXMOX_UI_HANDOFF.md`](../../docs/PROXMOX_UI_HANDOFF.md) — surfaces plates,
bordures fines, hiérarchie portée par la typographie.

## Ce qui est affiché à ce stade

- **La vue d'ensemble des clusters (écran 4)** : totaux inter-clusters, une carte
  par cluster avec son état, ses barres CPU / mémoire / stockage, son compteur de
  VM, la liste de ses nœuds et son bandeau d'alerte.
- **Le layout** : barre supérieure (logo, sélecteur de cluster, recherche,
  notifications, thème), arbre latéral, zone contextuelle, chacune des deux
  colonnes défilant pour son compte.
- **L'arbre** des clusters et de leurs nœuds, avec la sélection partagée entre la
  barre supérieure et l'arbre.

### Ce qui n'est pas là, et pourquoi

**L'écran 1 (vue VM) et l'écran 2 (vue nœud) ne sont pas implémentés.** Ce n'est
pas un oubli de mise en forme : ils réclament des données que le backend n'expose
pas encore. `/api/overview` est la seule route applicative de l'étape 2, et elle
ne porte ni le détail matériel d'un nœud, ni l'historique de charge, ni les
tâches.

| Écran | Ce qui manque côté backend |
|---|---|
| Vue nœud (écran 2) | `/nodes/{node}/status` — kernel, load average, stockage local, version PVE |
| Sparklines CPU | `/nodes/{node}/rrddata` — les séries temporelles du graphe à hauteur fixe du §2 |
| Tableau des tâches | `/cluster/tasks` — heure, description, durée calculée, état |

Ces trois endpoints sont **explicitement hors du périmètre de l'étape 2** (§6 du
document de passation). De même, le bouton et la modal de mise en maintenance
(écran 3) sont à venir : ils dépendent des routes `maintenance/plan` et
`maintenance/execute` de l'étape 4. Rien de tout cela n'est échafaudé ici — mieux
vaut une vue absente qu'une vue qui ment.

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
| Avertissement | `#EF9F27` | `#FAEEDA` | `#633806` / `#854F0B` | Maintenance, dégradé, action à conséquence |
| Accent | `#378ADD` | `#E6F1FB` | `#1B5E9E` | Données neutres : barres, graphes, sélection. **Jamais un statut.** |
| Marque | `#D85A30` | — | — | Orange Proxmox, **réservé au logo** : jamais un statut, jamais un bouton |

**Aucune couleur en dur dans un composant, jamais.** Pas de `#1D9E75`, pas de
`bg-green-500`, pas de `style={{ color: ... }}`. Une couleur qui manque se
rajoute dans `tokens.css`, où elle est visible, nommée et revue — pas au fond d'un
JSX. Même règle pour les rayons (`rounded-card`, `rounded-panel`) et pour la
bordure 0,5 px, qui est un choix de design et non un arrondi de pixel.

La seule couleur qui peut arriver au runtime est `clusters[].color`, configurée
côté backend par cluster et passée telle quelle.

### Clair, sombre, système

Le thème sombre est **un second jeu de valeurs derrière les mêmes noms de
tokens**, au bas de `tokens.css` : aucun composant ne sait quel thème est actif,
il nomme un rôle (`bg-surface-1`, `text-text-muted`) et le rôle est résolu là.
C'est la raison pratique de la règle ci-dessus : une couleur écrite dans un JSX
casse le thème sombre par construction, et pas seulement par principe.

Les rôles sont conservés, pas inversés : `--surface-0` reste la page,
`--surface-1` la colonne latérale, `--surface-2` le chrome surélevé ; ce qui
change, c'est que « surélevé » veut maintenant dire plus clair au lieu de plus
blanc. Tous les couples texte / fond du thème sombre dépassent 4,5:1, et tous les
aplats d'état 3:1 sur les trois surfaces — l'ambre compris, qui est le couple le
plus faible du thème clair (2,0:1 là-bas, 8,0:1 ici).

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

Le **flash de thème clair** au chargement est évité par un petit script inline et
synchrone dans `index.html`, qui pose l'attribut avant la première peinture — un
module serait différé, donc trop tard. Il ne peut pas importer `src/lib/theme.ts`
puisqu'il s'exécute avant le bundle : il redit la clé et l'attribut à la main, et
`theme.test.ts` vérifie que les deux orthographes n'ont pas divergé.

Côté React, `src/lib/theme.ts` porte la logique pure et le stockage,
`src/lib/useTheme.ts` l'état, et `src/components/ThemeToggle.tsx` le contrôle de
la barre supérieure (menu à trois entrées : « Clair », « Sombre », « Système »).
La préférence est tenue par la racine de l'application, comme la sélection de
cluster : la barre supérieure reste un composant contrôlé.

## Organisation du code

| Chemin | Contenu |
|---|---|
| `src/api/types.ts` | Les types du payload, **miroir de `apps/api/internal/aggregate/model.go`** |
| `src/api/client.ts` | `fetchOverview()`, les erreurs typées `ApiRequestError` / `ApiParseError` |
| `src/api/useOverview.ts` | Le hook de scrutation (5 s) qui alimente toute l'application |
| `src/lib/format.ts` | Tout le formatage d'affichage |
| `src/lib/theme.ts` | Préférence de thème : lecture, stockage, pose sur le document |
| `src/lib/useTheme.ts` | La préférence de thème en état React |
| `src/components/ui` | Primitives : `StatusDot`, `Tag`, `UsageBar`, `MetricCard`, `AlertBanner` |
| `src/components` | Barre supérieure, sélecteur de cluster, bascule de thème, arbre, coquille applicative, vues d'état |
| `src/screens` | Les écrans, à commencer par la vue d'ensemble |
| `src/styles` | `tokens.css` (le thème) et `index.css` (le point d'entrée Tailwind) |

### La règle qui structure tout

**Le backend ne renvoie que des octets et des ratios**, jamais une chaîne
localisée : `memory.used` est un entier d'octets, `cpu.ratio` une fraction dans
`[0,1]`, `uptime` des secondes. Le choix de l'unité, l'arrondi, la virgule
décimale et l'espace fine insécable avant le `%` sont la responsabilité du
frontend.

Conséquence pratique : **toute mise en forme passe par `src/lib/format.ts`**
(`formatBytes`, `formatUsage`, `formatRatio`, `formatUptime`,
`formatRelativeTime`, `truncateGuestLabel`, `formatNodeStatus`,
`formatClusterStatus`, `formatAlert`). Un composant qui écrit
`${Math.round(ratio * 100)} %` introduit une seconde convention typographique qui
divergera de la première ; il n'y a qu'un seul endroit où l'on décide comment
s'écrit une taille.

`format.ts` ne lève jamais : une valeur inexploitable (NaN, infinie, négative)
rend le tiret cadratin `—`, qui se lit « inconnu » dans l'interface. C'est aussi
ce qu'il faut afficher pour un `null` du payload, qui signifie « inconnu » et non
« zéro ».

## Conventions

- **Le code et les commentaires sont en anglais**, sans exception : identifiants,
  noms de tests, messages d'erreur techniques.
- **Les libellés d'interface sont en français, sentence case** (« Mettre en
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
  l'a ouvert — c'est déjà le cas du sélecteur de cluster et du champ de recherche.
- **Une information portée par une couleur a toujours un équivalent textuel.**
  Un point d'état vert n'est pas un statut pour un lecteur d'écran : il porte un
  `aria-label`, ou bien le texte à côté dit la même chose. Idem pour une barre
  au-delà du seuil, qui vire à l'ambre *et* déclenche une alerte nommée.
- Un lien d'évitement (« Aller au contenu ») ouvre la coquille, et les icônes
  purement décoratives sont `aria-hidden`.
