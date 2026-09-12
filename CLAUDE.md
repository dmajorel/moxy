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
- Toolchain locale : Go 1.19.8. Pas de `log/slog` (1.21), pas de `errors.Join` (1.20).

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
- **`Secret.Reveal()` est réservé au transport d'authentification du paquet
  `proxmox`** — il n'a qu'un seul appelant légitime, celui qui pose l'en-tête
  `Authorization`. Partout ailleurs, un `Secret` se rédige en `***` via ses méthodes
  `String`, `GoString`, `MarshalJSON` et `MarshalText`.
- **Interdit de journaliser ou de formater une `*http.Request`**, `httputil.DumpRequestOut`
  en tête : l'en-tête `Authorization` porte le secret. De même, une erreur ne doit
  jamais embarquer le corps ni les en-têtes d'une requête — seulement l'identifiant
  de cluster, le chemin, le code HTTP et la cause.

## Frontend (`apps/web`)

React 19 + TypeScript 6 `strict` + Vite 8 + Tailwind 4 + Vitest 5 + ESLint 10, avec
`@tabler/icons-react`. Le document de référence est `apps/web/README.md` ; ce qui
suit est ce qu'une session doit savoir pour ne pas se tromper.

- **Aucune couleur en dur dans un composant.** Les tokens du §2 vivent dans
  `src/styles/tokens.css` et sont exposés en utilitaires Tailwind par `@theme
  inline` : `bg-surface-2`, `text-text-muted`, `border-border`, `rounded-card`.
  Ni `#1D9E75`, ni `bg-green-500`, ni `style={{ color }}`. Une couleur qui manque
  s'ajoute à `tokens.css`, jamais au fond d'un JSX. L'orange `#D85A30` est réservé
  au logo : jamais un statut, jamais un bouton.
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
- **Ne documente ni n'échafaude ce qui n'existe pas.** Les écrans VM et nœud, les
  sparklines et les tâches attendent des endpoints backend (`/nodes/{node}/status`,
  `rrddata`, `/cluster/tasks`) hors périmètre de l'étape 2.

## Vérifications

```sh
./scripts/check.sh       # gofmt, go vet, go test
./scripts/build.sh       # compile bin/moxyd
./scripts/check-web.sh   # typecheck, eslint, vitest
./scripts/build-web.sh   # bundle dans apps/web/dist
./scripts/build-image.sh # image OCI (podman ou docker), voir Containerfile
```

Le produit se livre en conteneur : une image unique où `moxyd -web` sert le bundle
du frontend sous la même origine que l'API. Sans `-web`, `moxyd` reste API seule,
c'est le mode de développement avec le serveur Vite.

`make` n'est pas disponible dans l'environnement de développement ; tout passe par
`scripts/`.

## Règles de sécurité

- Un token d'API Proxmox ne doit jamais atteindre le navigateur, ni un log, ni un
  message d'erreur renvoyé au client.
- La vérification TLS peut être assouplie **par cluster** (certificats auto-signés),
  jamais globalement, et toujours avec un avertissement explicite.
- Les actions destructrices (maintenance, migration, redémarrage) exigent une
  autorisation vérifiée côté backend, jamais seulement masquée côté UI.
