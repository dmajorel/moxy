# moxy

Surcouche web moderne et **multi-cluster** pour Proxmox VE 9.2.x.

L'interface native est mono-cluster : chaque cluster est un endpoint API distinct,
sans vue agrégée. moxy interroge N clusters et les présente dans une seule UI, en
mettant en avant le statut et les métriques plutôt que des tableaux clé/valeur.

La spécification complète — décisions de design, contraintes de l'API Proxmox et
maquettes de référence — est dans [`docs/PROXMOX_UI_HANDOFF.md`](docs/PROXMOX_UI_HANDOFF.md).

## Structure

| Chemin | Rôle |
|---|---|
| `apps/api` | Backend agrégateur (Go, bibliothèque standard uniquement) |
| `apps/web` | Frontend (React 19 + Tailwind 4) — vue d'ensemble des clusters, voir [`apps/web/README.md`](apps/web/README.md) |
| `docs` | Document de passation et spécifications |
| `scripts` | Build et vérifications |
| `Containerfile` | Image OCI unique (`moxyd` + bundle du frontend), voir [Déploiement en conteneur](#déploiement-en-conteneur) |

## Développement

Prérequis : Go ≥ 1.19.

```sh
./scripts/check.sh   # gofmt, go vet, go test
./scripts/build.sh   # compile bin/moxyd
./bin/moxyd          # écoute sur 127.0.0.1:8080 par défaut
```

L'adresse d'écoute se règle via `-addr` ou la variable `MOXY_ADDR`.

Par défaut `moxyd` ne sert que l'API : en développement, c'est le serveur Vite qui
sert le frontend (voir plus bas). Le drapeau `-web` (ou la variable `MOXY_WEB`)
désigne un répertoire contenant le bundle produit par `./scripts/build-web.sh` ;
`moxyd` le sert alors lui-même sous la même origine que l'API, ce qui est le mode
de l'image de conteneur. Le démarrage échoue si le répertoire n'a pas d'`index.html`.

> **Avertissement — écoute loopback, sans authentification.**
> `moxyd` écoute sur `127.0.0.1` et **n'a pas encore d'authentification propre** :
> quiconque atteint le port lit la vue d'ensemble de tous les clusters configurés.
> Il ne doit pas être exposé tel quel, ni derrière un simple reverse proxy ouvert.
> L'authentification de moxy (OIDC/SSO) fera l'objet d'une étape dédiée.

## Frontend

Le frontend vit dans [`apps/web`](apps/web/README.md), qui est son document de
référence : démarrage, thème, organisation du code et conventions. La CI
construit avec Node 22.

```sh
./bin/moxyd -mock                         # terminal 1 — API sur 127.0.0.1:8080
cd apps/web && npm install && npm run dev  # terminal 2 — UI sur 127.0.0.1:5173
```

Le serveur de développement proxie `/api` vers `http://127.0.0.1:8080`
(surchargeable par `MOXY_API`), parce qu'il n'y a pas de CORS côté backend.
Vérifications : `./scripts/check-web.sh` et `./scripts/build-web.sh`.

## Configuration

moxy lit un fichier JSON décrivant les clusters à interroger. Le fichier de travail
est `config.local.json` à la racine : il est **déjà couvert par `.gitignore`**
(comme tout `*.local.json`), parce qu'il désigne les hôtes et les identifiants de
token d'un parc réel.

Résolution du chemin, dans l'ordre :

1. le drapeau `-config` ;
2. la variable d'environnement `MOXY_CONFIG` ;
3. `config.local.json` dans le répertoire courant.

Sans fichier de config et sans `-mock`, le démarrage échoue franchement plutôt que
de servir une vue vide.

```sh
cp config.example.json config.local.json   # puis adapter les hôtes et les tokenId
./bin/moxyd -config config.local.json
```

[`config.example.json`](config.example.json) est un exemple committé, sans aucun
secret, qui illustre les trois modes TLS et une liste d'URL à plusieurs entrées.

### Champs

| Champ | Obligatoire | Défaut | Description |
|---|---|---|---|
| `thresholds.memory` | non | `0.80` | Seuil du ratio mémoire au-delà duquel une alerte `memory_high` est levée. Fraction dans `]0,1]`. |
| `clusters` | oui | — | Au moins un cluster. |
| `clusters[].id` | oui | — | Identifiant stable, unique, de la forme `[a-z0-9-]+`. Sert de clé dans l'API et dans les logs. |
| `clusters[].name` | oui | — | Libellé affiché dans l'UI. |
| `clusters[].color` | non | `null` | Couleur d'accent du cluster, passée telle quelle au frontend (§4 du document de passation). |
| `clusters[].urls` | oui | — | Liste d'URL de nœuds du cluster, au moins une, toutes en `https` et sans chemin (le client ajoute `/api2/json`). moxy bascule d'une URL à l'autre en cas de panne d'un nœud. |
| `clusters[].tokenId` | oui | — | Identifiant du token PVE, forme `user@realm!tokenid` (ex. `moxy@pve!ro`). **Non sensible** — voir [Secrets](#secrets). |
| `clusters[].secretEnv` | oui | — | Nom de la variable d'environnement qui porte le secret du token. La variable doit être présente et non vide au démarrage, sinon échec franc. |
| `clusters[].tls.mode` | non | `system` | `system`, `pinned` ou `insecure` — voir [TLS](#tls). |
| `clusters[].tls.caFile` | si `pinned` | — | Chemin d'un fichier PEM lisible contenant le CA du cluster. |
| `clusters[].timeout` | non | `4s` | Délai par appel PVE, au format `time.Duration` (`4s`, `1500ms`…). Borne l'ensemble des tentatives de bascule d'URL. |

La configuration est validée au démarrage : identifiants uniques et bien formés,
URL en `https` sans chemin, `tokenId` conforme, `secretEnv` renseignée, `caFile`
lisible et PEM valide, seuil dans ses bornes.

## Secrets

Un token d'API Proxmox se présente en deux morceaux, et moxy les traite
différemment :

- **`tokenId`** — `moxy@pve!ro`. C'est un nom, pas une clé : il vit dans le fichier
  de configuration, en clair.
- **le secret** — l'UUID rendu une seule fois par Proxmox à la création du token.
  Il vit **uniquement** dans la variable d'environnement nommée par `secretEnv`,
  jamais dans la configuration.

Le motif est simple : un fichier de configuration se sauvegarde, se copie dans un
dépôt, se joint à un ticket. Aucun secret ne doit s'y trouver au repos. Le secret
n'est réassemblé avec le `tokenId` qu'au moment de poser l'en-tête `Authorization`,
dans le transport HTTP du paquet `proxmox` ; il n'est jamais journalisé ni renvoyé
dans un message d'erreur.

En développement :

```sh
export MOXY_SECRET_QUALIFICATION='00000000-0000-0000-0000-000000000000'
export MOXY_SECRET_PREPRODUCTION='00000000-0000-0000-0000-000000000000'
export MOXY_SECRET_PRODUCTION='00000000-0000-0000-0000-000000000000'
./bin/moxyd -config config.local.json
```

En production, passer par un `EnvironmentFile` en lecture seule pour le seul
utilisateur de service :

```ini
# /etc/systemd/system/moxyd.service
[Unit]
Description=moxy aggregator
After=network-online.target

[Service]
User=moxy
Group=moxy
EnvironmentFile=/etc/moxy/secrets.env
Environment=MOXY_CONFIG=/etc/moxy/config.json
ExecStart=/usr/local/bin/moxyd -addr 127.0.0.1:8080
Restart=on-failure
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes

[Install]
WantedBy=multi-user.target
```

```sh
# /etc/moxy/secrets.env — chmod 0600, propriétaire moxy:moxy
MOXY_SECRET_QUALIFICATION=...
MOXY_SECRET_PREPRODUCTION=...
MOXY_SECRET_PRODUCTION=...
```

## Privilèges PVE requis

Pour la vue d'ensemble, le rôle **`PVEAuditor` sur `/`** suffit. Il couvre les trois
appels du chemin critique :

| Endpoint | Usage | Privilège |
|---|---|---|
| `/cluster/resources` | nœuds, VM/CT, stockage | `Sys.Audit` (`PVEAuditor`) |
| `/cluster/status` | quorum, nœuds en ligne | `Sys.Audit` (`PVEAuditor`) |
| `/cluster/ha/status/manager_status` | nœuds en maintenance | `Sys.Audit` (`PVEAuditor`) |
| `/nodes/{node}/apt/update` | paquets en attente, version `pve-manager` | **`Sys.Modify` sur `/nodes`** |

Le bandeau « mise à jour disponible » repose sur `apt/update`, qui exige en plus
`Sys.Modify` sur `/nodes`. Sans ce droit, **moxy n'échoue pas** : l'appel renvoie
403, le cluster reste servi normalement, `updates` vaut `null` et le bandeau
n'apparaît pas. Donner ce privilège est donc un choix, pas une obligation.

Création du token côté Proxmox, sur un nœud du cluster :

```sh
# 1. un utilisateur dédié, sans mot de passe utilisable
pveum user add moxy@pve --comment "moxy aggregator (read-only)"

# 2. un token sans expiration, non privilégié (privsep=1 : ses droits propres)
#    la commande affiche le secret UNE SEULE FOIS : le placer dans secrets.env
pveum user token add moxy@pve ro --privsep 1

# 3. le rôle d'audit sur la racine, pour l'utilisateur et pour le token
pveum acl modify / --users moxy@pve --roles PVEAuditor
pveum acl modify / --tokens 'moxy@pve!ro' --roles PVEAuditor
```

Pour activer en plus le bandeau de mises à jour, ajouter un rôle portant
`Sys.Modify` sur `/nodes` :

```sh
pveum role add MoxyAptCheck --privs "Sys.Modify"
pveum acl modify /nodes --tokens 'moxy@pve!ro' --roles MoxyAptCheck
```

À répéter sur chaque cluster : Proxmox n'a pas de notion de multi-cluster, chaque
cluster a son propre utilisateur, son propre token et son propre secret.

## TLS

La vérification TLS se règle **par cluster**, jamais globalement, via
`clusters[].tls.mode` :

| Mode | Comportement | Quand |
|---|---|---|
| `system` | Vérification classique contre le magasin de CA du système. | Certificat émis par une CA d'entreprise ou publique (ACME). |
| `pinned` | Vérification contre le seul CA du fichier `caFile`. | **Le bon choix pour Proxmox par défaut** : chaque cluster signe les certificats de ses nœuds avec son `pve-root-ca`, un seul CA couvre donc toutes les `urls` du cluster. |
| `insecure` | Aucune vérification du certificat. | Développement uniquement. |

Pour le mode `pinned`, récupérer le CA du cluster depuis un de ses nœuds :

```sh
scp root@prox-pprd-2301-cit:/etc/pve/pve-root-ca.pem \
    /etc/moxy/ca/preproduction-pve-root-ca.pem
```

`insecure` journalise au démarrage un avertissement nommant le cluster concerné.
Il expose la connexion à une interception active — donc au vol du token — et ne
devrait jamais servir en production : `pinned` demande un fichier de plus et
supprime le risque.

## Mode mock

```sh
./bin/moxyd -mock
```

`-mock` sert le jeu de données figé de l'écran 4 (trois clusters, onze nœuds,
148 VM, deux alertes, un nœud en maintenance) **sans lire de configuration ni
ouvrir la moindre connexion réseau**. C'est le mode prévu pour développer le
frontend sans cluster joignable, et pour les tests de bout en bout du serveur.

## Déploiement en conteneur

Le produit se livre sous forme d'une **image OCI unique** : `moxyd` y sert l'API et
le bundle du frontend (`-web`), sous la même origine. L'image est publiée par la CI
sur `ghcr.io/dmajorel/moxy` avec les tags `edge` (dernier `main`), `X.Y.Z` / `X.Y` /
`latest` (tags `vX.Y.Z`) et `sha-<commit>`, pour `linux/amd64` et `linux/arm64`.

Construction locale, avec `podman` ou `docker` :

```sh
./scripts/build-image.sh                 # ghcr.io/dmajorel/moxy:dev
IMAGE=moxy TAG=test ./scripts/build-image.sh
```

Le [`Containerfile`](Containerfile) construit le bundle (Node 22), compile `moxyd`
(Go, `CGO_ENABLED=0`, `GOPROXY=off`, donc sans accès réseau) et assemble une image
`distroless/static` : pas de shell, utilisateur `nonroot` (uid 65532), bundle de CA
système présent (le mode `tls.mode: system` fonctionne). Le
[`.dockerignore`](.dockerignore) tient les artefacts locaux et les `*.local.json`
hors du contexte de build.

Essai sans cluster :

```sh
podman run --rm --read-only -p 127.0.0.1:8080:8080 ghcr.io/dmajorel/moxy:edge -mock
```

Exécution réelle : la configuration et les CA épinglés se montent en lecture seule
dans `/etc/moxy` (l'image attend `MOXY_CONFIG=/etc/moxy/config.json`), les secrets
arrivent par l'environnement, jamais dans l'image :

```sh
# /etc/moxy/config.json, /etc/moxy/ca/*.pem : lisibles par l'uid 65532
# /etc/moxy/secrets.env : MOXY_SECRET_...=..., chmod 0600
podman run --rm --read-only \
  -p 127.0.0.1:8080:8080 \
  -v /etc/moxy:/etc/moxy:ro \
  --env-file /etc/moxy/secrets.env \
  ghcr.io/dmajorel/moxy:edge
```

Points à connaître :

- **Le port doit rester privé.** Dans l'image, `MOXY_ADDR` vaut `0.0.0.0:8080`,
  sinon le port publié n'atteindrait jamais le processus. L'avertissement de la
  section [Développement](#développement) s'applique donc intégralement : tant que
  moxy n'a pas d'authentification propre, publier le port sur loopback
  (`-p 127.0.0.1:8080:8080`) ou sur un réseau privé, derrière un reverse proxy qui
  authentifie.
- **Sonde de vie** : `GET /healthz`. L'image ne déclare pas de `HEALTHCHECK`, faute
  de shell ou de client HTTP pour l'exécuter ; la sonde se déclare côté
  orchestrateur.
- **Système de fichiers en lecture seule** : `moxyd` n'écrit rien sur disque,
  `--read-only` fonctionne sans volume temporaire.
- L'unité systemd de la section [Secrets](#secrets) reste la voie de déploiement
  sans conteneur.

## API

### `GET /api/overview`

Unique route applicative de l'étape 2. Elle renvoie, en un seul document, de quoi
peindre l'écran 4 (vue d'ensemble) intégralement : totaux inter-clusters, carte de
chaque cluster, détail par nœud et alertes. Le backend scrute les clusters en
arrière-plan, la route se contente de servir le dernier état connu — elle ne bloque
donc pas sur un cluster injoignable. Réponse en `Cache-Control: no-store`.

**Le schéma est figé** : le frontend de l'étape 3 est construit dessus. Sa
définition de référence est `apps/api/internal/aggregate/model.go`.

Extrait abrégé :

```json
{
  "generatedAt": "2026-09-12T10:00:00Z",
  "thresholds": { "memory": 0.8 },
  "totals": { "clusters": 3, "nodes": 11, "nodesOnline": 11, "vms": 148, "alerts": 2 },
  "clusters": [
    {
      "id": "preproduction",
      "name": "Préproduction",
      "color": null,
      "status": "degraded",
      "fetchedAt": "2026-09-12T09:59:57Z",
      "error": null,
      "quorum": { "quorate": true, "nodes": 3, "online": 3 },
      "cpu": { "ratio": 0.31, "cores": 96 },
      "memory": { "used": 227633266688, "total": 274877906944, "ratio": 0.828 },
      "storage": { "used": 4288125337600, "total": 8796093022208, "ratio": 0.4875 },
      "vms": { "running": 44, "stopped": 2, "templates": 0, "total": 46 },
      "nodes": [
        {
          "name": "prox-pprd-2301-cit",
          "status": "online",
          "uptime": 3542400,
          "cpu": { "ratio": 0.42, "cores": 32 },
          "memory": { "used": 76000000000, "total": 91625968981, "ratio": 0.83 },
          "pendingUpdates": 0
        }
      ],
      "updates": null,
      "alerts": [
        { "kind": "memory_high", "nodes": ["prox-pprd-2301-cit"], "ratio": 0.828 }
      ]
    }
  ]
}
```

Conventions du payload :

- **Tailles en octets**, entiers bruts (`memory.used`, `storage.total`…). Le choix
  de l'unité (GiB, TiB), l'arrondi et la virgule décimale sont la responsabilité du
  frontend : aucune chaîne localisée ne sort du backend.
- **Ratios en fractions `0..1`**, jamais en pourcentage — `cpu.ratio: 0.31` vaut
  31 %. C'est aussi la convention de PVE lui-même.
- **`null` signifie « inconnu », pas « zéro ».** `updates: null` veut dire que la
  question n'a pas pu être posée (token sans `Sys.Modify`, ou première scrutation
  pas encore faite) ; `pendingUpdates: null` de même, par nœud. Un `0` affirme au
  contraire qu'il n'y a rien en attente. `quorum: null` désigne un nœud seul, sans
  cluster. `color: null` signifie qu'aucune couleur n'est configurée.
- **`status`** vaut `healthy`, `degraded` ou `unreachable`. Un cluster
  `unreachable` conserve son dernier instantané connu, daté par `fetchedAt` ; le
  frontend peut donc afficher des données vieillies plutôt qu'une carte vide.
- **Erreurs en anglais, avec un `kind` traduisible.** `error` vaut `null` ou
  `{ "kind": "timeout", "message": "cluster preproduction: GET /cluster/status: context deadline exceeded" }`.
  `kind` ∈ `auth`, `tls`, `timeout`, `network`, `protocol` : c'est lui que le
  frontend traduit ; `message` reste en anglais, destiné au diagnostic. Même
  principe pour `alerts[].kind` (`quorum_lost`, `node_offline`, `memory_high`,
  `updates_available`, `unreachable`) et pour les erreurs HTTP du serveur, de la
  forme `{ "error": "method not allowed" }`.
- **La maintenance n'est pas une alerte** : c'est un état choisi, porté par
  `nodes[].status = "maintenance"`.

### `GET /healthz`

Sonde de vivacité du démon lui-même, indépendante de l'état des clusters.

### Origine unique, pas de CORS

Le serveur de développement du frontend proxie `/api` vers `moxyd`. Il n'y a
**délibérément pas de CORS côté backend** : navigateur et API sont servis sous la
même origine, et n'ajouter aucun en-tête permissif évite d'ouvrir une surface
inutile sur un service qui détient des tokens d'hyperviseur.

## Confronté à un cluster réel

Les fixtures de test ont été écrites d'après le schéma documenté de PVE, aucun
cluster n'étant joignable depuis l'environnement de développement. Trois points
restaient à confirmer ; `scripts/probe-pve.sh` les sonde en lecture seule.

Résultats du **2026-09-12**, sur un cluster de qualification PVE 9 à 6 nœuds :

| Hypothèse | Verdict |
|---|---|
| Sérialisation `0/1` des booléens | **Confirmée.** `quorate`, `online`, `template` et `shared` arrivent en entiers `0`/`1`, pas en booléens JSON. Le schéma ment bien, les types `Flex*` sont nécessaires. |
| Emplacement de `node_status` | **Infirmée, puis corrigée.** Le plan supposait un champ à plat ; un vrai cluster l'imbrique dans `manager_status`. Le décodage tolérant absorbait déjà les deux formes, et un test dédié épingle désormais la forme réelle. |
| `pve-manager` dans `apt/update` | **Confirmée.** Le paquet figure dans la liste des mises à jour en attente des six nœuds, avec la version proposée. Le bandeau affichera donc un numéro de version, pas un simple décompte. |

Un détour instructif sur la troisième : au premier passage, les six nœuds ont
répondu **403**, le token n'ayant pas `Sys.Modify` sur `/nodes`. La dégradation
prévue a bien eu lieu — `updates` à `null`, aucune erreur, aucun plantage — ce qui
valide au passage le choix de traiter ce privilège comme facultatif. L'hypothèse
n'a pu être confirmée qu'après ajout du rôle.

Deux observations complémentaires :

- **Le dédoublonnage des stockages partagés n'est pas une précaution théorique.**
  Le cluster sondé expose 7 stockages partagés, chacun répété une fois par nœud,
  soit 42 entrées dans `/cluster/resources`. Sans dédoublonnage, sa capacité
  serait affichée multipliée par six.
- **Aucun champ numérique n'arrivait sérialisé en chaîne** sur ce cluster. Les
  types `Flex*` restent justifiés par les booléens et par les variations entre
  versions de PVE, mais cette forme-là n'y a pas été observée.

Reste ouvert, et un seul point : la valeur `maintenance` de `node_status` n'a pas pu être observée,
aucun nœud n'étant drainé. Elle ne s'observe pas passivement — il faut exécuter
`ha-manager crm-command node-maintenance enable <nœud>`, qui migre réellement les
ressources HA. Ce point devient incontournable à l'étape 4.

## Sécurité

moxy détient des tokens d'API vers un plan de contrôle d'hyperviseur, avec le
pouvoir de migrer des VM et de redémarrer des nœuds. Deux règles structurantes :

- **Les tokens ne quittent jamais le backend.** Le navigateur ne parle qu'à moxy,
  jamais directement à un nœud Proxmox.
- **Aucune dépendance externe côté backend.** Le code s'en tient à la bibliothèque
  standard Go ; les scripts posent `GOPROXY=off` pour que toute dépendance
  introduite par inadvertance fasse échouer la compilation.

S'y ajoutent, depuis l'étape 2 :

- **Aucun secret au repos dans la configuration** : le secret du token vit dans
  l'environnement, jamais dans un fichier committé ou sauvegardé.
- **Un token en lecture seule suffit** pour la vue d'ensemble (`PVEAuditor`).
- **`insecure` est un réglage par cluster**, journalisé, réservé au développement.
- **Pas d'authentification propre pour l'instant** : écoute loopback, exposition
  interdite, voir l'avertissement plus haut.
- **L'image de conteneur écoute sur `0.0.0.0`** par nécessité ; c'est la publication
  du port qui doit rester sur loopback ou un réseau privé, voir
  [Déploiement en conteneur](#déploiement-en-conteneur). L'image tourne sans shell,
  en utilisateur non privilégié, et ne contient ni secret ni fichier de
  configuration.

## Périmètre

Sont en place : le backend agrégateur avec `GET /api/overview` (étape 2), et le
frontend avec son layout, son thème, son arbre et **la vue d'ensemble des clusters**
— l'écran 4 (étape 3).

Restent à venir :

- **La vue nœud (écran 2) et la vue VM (écran 1)**, bloquées par le backend : elles
  demandent `/nodes/{node}/status` (kernel, load average, stockage local),
  `/nodes/{node}/rrddata` (sparklines) et `/cluster/tasks` (tableau des tâches),
  qu'aucune route n'expose aujourd'hui. Tant que ces données ne remontent pas,
  l'interface ne les échafaude pas.
- Le plan et l'exécution de la mise en maintenance, et la modal de l'écran 3
  (étape 4).
- Les tâches et le journal cluster en temps quasi réel, et les sparklines RRD
  (étape 5).
- L'authentification de moxy (étape dédiée).
