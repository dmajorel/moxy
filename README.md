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
| `apps/web` | Frontend (React 19 + Tailwind 4) — vue d'ensemble, vues nœud et VM, plan de maintenance et journal des tâches, voir [`apps/web/README.md`](apps/web/README.md) |
| `deploy` | Unité systemd durcie et reverse proxy authentifiant, voir [Déploiement sécurisé](docs/DEPLOIEMENT.md) |
| `docs` | Document de passation et spécifications |
| `scripts` | Build et vérifications |
| `Containerfile` | Image OCI unique (`moxyd` + bundle du frontend), voir [Déploiement en conteneur](#déploiement-en-conteneur) |

## Compatibilité

| | Version | D'où elle vient |
|---|---|---|
| Proxmox VE | **9.2.x**, vérifié le 2026-09-12 sur un cluster de six nœuds | Les endpoints employés existent depuis PVE 7.x — c'est la version qui a introduit la maintenance de nœud via HA — mais rien n'a été vérifié en deçà de 9, et les pièges consignés plus bas l'ont été sur un cluster 9. |
| Go, **niveau de langage** | 1.19 | La directive `go` d'`apps/api/go.mod`. Elle borne les API utilisables : pas de `errors.Join` (1.20), pas de `log/slog` (1.21), pas de paramètres de chemin dans `ServeMux` (1.22). |
| Go, **compilation livrée** | 1.27 | L'étage `api` du `Containerfile`. La directive `go` ne décide pas de la bibliothèque standard réellement liée, et c'est elle qui fait la posture de sécurité d'un binaire sans dépendance — voir [Sécurité](#sécurité). La CI compile avec les deux (`ci.yml`, matrice `go: ['1.19', '1.27']`), ce qui garde la contrainte mécanique. |
| Node | 22 pour les vérifications et la CI (`apps/web/.nvmrc`), 26 pour l'étage `web` du `Containerfile` | Le bundle est produit par Vite dans les deux cas. |
| Navigateur | cible `ES2022` | `apps/web/tsconfig.app.json`. |

## Développement

Prérequis : Go ≥ 1.19, et Node ≥ 22 pour le frontend.

```sh
make help      # liste les cibles
make check     # vérifie tout : backend et frontend
make fmt       # reformate le backend (gofmt -w), ce que check exige
make build     # compile bin/moxyd
make mock      # compile puis lance moxyd sur les données d'exemple
make dev       # le démon mock et le serveur Vite ensemble, un seul terminal
```

Le Makefile est une commodité : il enveloppe les scripts de `scripts/`, qui restent
la référence et que la CI appelle directement. Ils s'utilisent aussi seuls :

```sh
./scripts/check.sh   # gofmt, go vet, go test, puis les vérifications frontend
./scripts/build.sh   # compile bin/moxyd
./bin/moxyd          # écoute sur 127.0.0.1:8080 par défaut
```

Une seule commande sort de ce cadre, et elle **écrit** au lieu de vérifier :

```sh
cd apps/api && go test ./internal/server -update   # régénère les fixtures du frontend
```

Les payloads d'exemple que le frontend teste (`apps/web/src/test/fixtures/`)
sont produits par `TestMockMatchesWebFixtures` à partir du démon mock, sur une
horloge figée. Ils ne se recopient pas à la main. Voir
[Le contrat Go ↔ TypeScript](#le-contrat-go--typescript).

### Analyse statique et vulnérabilités

`check.sh` reste exécutable avec le seul Go local : c'est ce qui permet de vérifier
le dépôt hors ligne, sans rien installer. Les analyseurs qui demandent autre chose
vivent donc dans un script à part, `scripts/analyze.sh`, exposé par `make analyze` :

```sh
make analyze   # shellcheck, staticcheck, govulncheck, npm audit
```

Le script n'installe rien : il exécute ce qu'il trouve sur le `PATH` et **saute en
le disant** ce qui manque, pour rester utile sur un poste qui n'a que `shellcheck`.
`MOXY_ANALYZE_REQUIRE=1` transforme chaque saut en échec ; c'est ce que pose la CI,
sans quoi un outil qui cesserait d'être installé rendrait le job vert sans rien
vérifier.

Les quatre outils, et pourquoi ils sont appelés ainsi :

| Outil | Appel | Raison |
| --- | --- | --- |
| `shellcheck` | `-x -s sh scripts/*.sh` | `-x` suit le `. env.sh` de `build.sh`, `check.sh` et `analyze.sh` ; sans lui les trois signalent `SC1091`. |
| `staticcheck` | `./...` dans `apps/api` | Ce que `go vet` ne couvre pas. |
| `govulncheck` | `-mode=binary bin/moxyd` | moxy n'a aucune dépendance : la seule vulnérabilité possible vient de la bibliothèque standard **liée dans le binaire livré**, et le mode source ne dit rien de celle-là. Construire d'abord (`./scripts/build.sh`). |
| `npm audit` | `--omit=dev --audit-level=high` | Le bundle est livré dans l'image et s'exécute dans le navigateur de l'opérateur. Seul endroit qui interroge le registre : `check-web.sh` et `build-web.sh` gardent `--no-audit` pour rester installables hors ligne. |

`staticcheck` et `govulncheck` sont des **outils de CI, pas des dépendances du
service**. La CI les installe avec `go install …@version` depuis un répertoire
**hors du module** : `go install pkg@version` se résout alors dans un module
jetable, n'écrit pas `apps/api/go.mod`, ne crée pas de `go.sum`, et ne met rien
dans `bin/moxyd`. `GOPROXY` n'est réactivé que pour ce step — `scripts/env.sh` le
laisse à `off` partout ailleurs, ce qui est précisément ce qui fait échouer la
compilation si une dépendance s'introduit dans le backend. Les deux outils
refusent de se compiler en 1.19 : le job d'analyse utilise la série Go de
livraison, le module reste `go 1.19` et compile inchangé.

### Couverture

Elle n'est mesurée qu'en CI, et publiée en artefact (`coverage-api`,
`coverage-web`). Côté frontend, `check-web.sh` lance déjà `vitest --coverage` avec
un plancher dans `vitest.config.ts`. Côté backend, `MOXY_COVER` désigne un profil à
écrire, résolu depuis `apps/api` :

```sh
MOXY_CHECK_WEB=0 MOXY_COVER=cover.out ./scripts/check.sh
```

Vide par défaut : un profil écrit à chaque exécution locale laisserait un fichier
derrière lui. `-covermode=atomic`, parce que c'est le mode qu'un profil fusionné
entre paquets doit avoir ; `-race`, qui l'accompagne d'ordinaire, n'est pas
utilisable ici — le détecteur de courses exige CGO.

L'adresse d'écoute se règle via `-addr` ou la variable `MOXY_ADDR`. Les noms
d'hôte supplémentaires que `moxyd` accepte dans l'en-tête `Host` se déclarent
via `-allowed-hosts` ou `MOXY_ALLOWED_HOSTS` — voir
[Vérification de l'en-tête `Host`](#vérification-de-len-tête-host).

Deux modes répondent sans lire de configuration : `moxyd -version` imprime la
version et sort, `moxyd -healthcheck` sonde `/healthz` sur l'adresse locale et
sort 0 ou 1 — c'est ce que le `HEALTHCHECK` de l'image appelle, faute de shell.
`SIGHUP` est **ignoré** : son action par défaut tuerait le démon, et recharger
la configuration en place demanderait de reconstruire clients et scrutateur de
façon atomique, ce qui n'est pas fait.

Par défaut `moxyd` ne sert que l'API : en développement, c'est le serveur Vite qui
sert le frontend (voir plus bas). Le drapeau `-web` (ou la variable `MOXY_WEB`)
désigne un répertoire contenant le bundle produit par `./scripts/build-web.sh` ;
`moxyd` le sert alors lui-même sous la même origine que l'API, ce qui est le mode
de l'image de conteneur. Le démarrage échoue si le répertoire n'a pas d'`index.html`.

> **Avertissement — écoute loopback par défaut.**
> `moxyd` écoute sur `127.0.0.1` et, **sans bloc `auth`**, sert la vue d'ensemble
> de tous les clusters configurés à quiconque atteint le port. Une écoute plus
> large sans `auth` déclenche un avertissement explicite au démarrage.
> Voir [Authentification](#authentification) : moxy n'authentifie personne
> lui-même, il vérifie que le composant qui authentifie est bien en place.

## Frontend

Le frontend vit dans [`apps/web`](apps/web/README.md), qui est son document de
référence : démarrage, thème, organisation du code et conventions. La CI
construit avec Node 22.

```sh
make dev   # démon mock sur 127.0.0.1:8080 + UI sur 127.0.0.1:5173
```

`make dev` lance `scripts/dev.sh`, qui démarre `moxyd -mock` en arrière-plan et
l'arrête quand Vite se termine (y compris au `Ctrl-C`). Les deux moitiés se
lancent toujours séparément si besoin :

```sh
./bin/moxyd -mock                          # terminal 1 — API sur 127.0.0.1:8080
cd apps/web && npm install && npm run dev  # terminal 2 — UI sur 127.0.0.1:5173
```

Le serveur de développement proxie `/api` vers `http://127.0.0.1:8080`
(surchargeable par `MOXY_API`), parce qu'il n'y a pas de CORS côté backend.
Vérifications : `./scripts/check-web.sh` et `./scripts/build-web.sh`.

**La sélection vit dans l'URL** : `/clusters/{id}`,
`/clusters/{id}/nodes/{node}`, `/clusters/{id}/guests/{vmid}`. Un rafraîchissement
garde l'objet ouvert, un lien collé dans un canal d'astreinte l'ouvre chez le
destinataire, et le bouton précédent fonctionne. `moxyd -web` sert `index.html`
pour tout chemin sans extension, ce qui est exactement ce qu'il faut : rien à
configurer. Détails dans [`apps/web/README.md`](apps/web/README.md).

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
| `auth.mode` | non | `none` | `none`, `proxy-header` ou `token` — voir [Authentification](#authentification). |
| `auth.header` | non | `X-Forwarded-User` | En-tête portant l'identité, en mode `proxy-header` uniquement. |
| `auth.trustedProxies` | si `proxy-header` | — | Blocs CIDR depuis lesquels l'en-tête est cru. Au moins un ; sans cela l'en-tête ne prouverait rien. |
| `auth.tokenEnv` | si `token` | — | Nom de la variable d'environnement portant le jeton partagé, en mode `token` uniquement. Le jeton lui-même n'est **jamais** dans le fichier ; la variable est effacée après lecture, comme un secret de cluster. |
| `maintenance` | non | — | Bloc de mise en maintenance des nœuds : le mode de fourniture de la clé SSH et les réglages de session, pour **tout le parc**. Absent — le cas normal — signifie que moxy n'exécute rien nulle part. Champ par champ dans [Mise en maintenance d'un nœud](#mise-en-maintenance-dun-nœud). |
| `thresholds.memory` | non | `0.80` | Seuil du ratio mémoire au-delà duquel une alerte `memory_high` est levée. Fraction dans `]0,1]`. |
| `thresholds.cpu` | non | `0.80` | Seuil du ratio CPU au-delà duquel la valeur affichée passe à l'ambre. Aucune alerte n'est levée sur le CPU : un nœud à 95 % pendant une seconde fait son travail. Fraction dans `]0,1]`. |
| `thresholds.storage` | non | `0.80` | Seuil du ratio de stockage au-delà duquel la barre de capacité passe à l'ambre. Fraction dans `]0,1]`. |
| `clusters` | oui | — | Au moins un cluster. |
| `clusters[].id` | oui | — | Identifiant stable, unique, de la forme `[a-z0-9-]+`. Sert de clé dans l'API et dans les logs. |
| `clusters[].name` | oui | — | Libellé affiché dans l'UI. |
| `clusters[].color` | non | `null` | Couleur d'accent du cluster, au format `#rrggbb`, passée telle quelle au frontend (§4 du document de passation). |
| `clusters[].urls` | oui | — | Liste d'URL de nœuds du cluster, au moins une, toutes en `https`, sans chemin (le client ajoute `/api2/json`), sans identifiants, sans requête ni fragment. Une même URL ne peut apparaître deux fois, ni dans un cluster, ni dans deux. moxy bascule d'une URL à l'autre en cas de panne d'un nœud. |
| `clusters[].tokenId` | oui | — | Identifiant du token PVE, forme `user@realm!tokenid` (ex. `moxy@pve!ro`). **Non sensible** — voir [Secrets](#secrets). |
| `clusters[].secretEnv` | oui | — | Nom de la variable d'environnement qui porte le secret du token, de la forme `[A-Za-z_][A-Za-z0-9_]*`. La variable doit être présente et non vide au démarrage, sinon échec franc ; elle est **effacée de l'environnement** une fois lue. |
| `clusters[].tls.mode` | non | `system` | `system`, `pinned` ou `insecure` — voir [TLS](#tls). |
| `clusters[].tls.caFile` | si `pinned` | — | Chemin d'un fichier PEM lisible contenant le CA du cluster. **Interdit** dans les autres modes. Un chemin relatif est résolu depuis le dossier du fichier de configuration, pas depuis le répertoire courant. |
| `clusters[].timeout` | non | `4s` | Délai pour obtenir une **réponse**, par appel PVE, au format `time.Duration` (`4s`, `1500ms`…). Refusé au-delà de `60s`. |
| `clusters[].connectTimeout` | non | `2s` | Délai pour **établir la connexion** (TCP puis TLS). Un nœud éteint, ou derrière un pare-feu qui jette au lieu de refuser, coûte ce délai-là et non le précédent. Doit rester inférieur ou égal à `timeout`. |
| `clusters[].proxy` | non | — | Proxy HTTP par lequel joindre ce cluster, URL `http`, `https` ou `socks5` sans chemin ni identifiants. Absent — le cas normal — signifie **connexion directe** : voir [Proxy](#proxy). |
| `clusters[].maintenance` | non | — | Ce que ce cluster dit de la maintenance : s'il y participe, qui y est autorisé, et à quelle adresse joindre chacun de ses nœuds. Il ne porte **aucun mode** — celui-ci est décidé une fois, pour le processus. Voir [Mise en maintenance d'un nœud](#mise-en-maintenance-dun-nœud). |

La configuration est validée au démarrage : identifiants uniques et bien formés,
URL en `https` sans chemin et sans doublon, `tokenId` conforme, `secretEnv`
renseignée, `caFile` lisible et PEM valide, `color` en `#rrggbb`, `proxy` de
schéma connu et sans identifiants, seuil et délais dans leurs bornes.

### Authentification

moxy **n'authentifie personne lui-même**, et c'est délibéré : il détient déjà
des tokens d'hyperviseur, il n'a pas à détenir des mots de passe en plus. Ce
qu'il peut faire — et ce qu'il ne faisait pas — c'est **refuser de servir une
requête qui n'est pas passée par le composant qui, lui, authentifie**.

| Mode | Effet |
|---|---|
| `none` (défaut) | Aucune vérification. Sûr sur loopback uniquement. |
| `proxy-header` | La requête doit venir d'un proxy listé **et** porter l'en-tête d'identité. |
| `token` | La requête doit présenter le jeton partagé, en cookie ou en `Authorization: Bearer`. Pour un poste isolé, sans proxy devant. |

```json
{
  "auth": {
    "mode": "proxy-header",
    "header": "X-Forwarded-User",
    "trustedProxies": ["127.0.0.1/32", "10.0.0.0/8"]
  }
}
```

**Les deux conditions sont nécessaires, et aucune ne suffit.** L'en-tête seul ne
prouve rien : quiconque atteint le port le pose lui-même — et le croire serait
pire que de ne rien vérifier, puisque le journal nommerait alors la personne
qu'il prétend être. L'adresse seule ne prouve rien non plus : le proxy relaie
pour tout le monde. C'est la combinaison qui dit « cette requête a traversé le
composant qui authentifie », et `trustedProxies` est obligatoire pour cette
raison — une configuration sans lui est refusée au démarrage.

Le composant qui authentifie, lui, est à mettre en place : deux exemples
complets et équivalents, `forward_auth` vers oauth2-proxy ou Authelia, avec une
variante d'authentification basique pour un poste isolé, sont livrés dans
[`deploy/Caddyfile`](deploy/Caddyfile) et [`deploy/nginx.conf`](deploy/nginx.conf).
Voir [Déploiement sécurisé](docs/DEPLOIEMENT.md).

L'adresse comparée est celle du pair TCP, qu'aucun en-tête ne peut changer.
`header` vaut `X-Forwarded-User` par défaut ; sa **valeur** n'est jamais
journalisée ni renvoyée, c'est un nom d'utilisateur affirmé par quelqu'un qui
n'a peut-être pas qualité pour l'affirmer.

`/healthz` et `/readyz` restent servies sans identité : un orchestrateur n'en a
pas à présenter, et une sonde de vivacité qui échoue sur l'authentification
redémarre un démon qui fonctionne. Elles ne portent qu'un état et un
identifiant de build.

Le bundle du frontend est protégé comme l'API : c'est la topologie du parc
rendue en page. Le mode `token` fait exception, et une seule — voir ci-dessous.

Restent à venir, et le bloc `auth` est fait pour les accueillir : le mTLS et
l'OIDC annoncé.

#### Mode `token` : un jeton partagé pour un poste isolé

`proxy-header` suppose une brique en amont. Un opérateur qui fait tourner moxy
sur un poste d'administration n'en a pas, et n'avait donc que `none` : la vue
d'ensemble de tout le parc servie à quiconque atteint le port. Le mode `token`
est la réponse à ce cas-là, **et à aucun autre**.

```json
{
  "auth": {
    "mode": "token",
    "tokenEnv": "MOXY_UI_TOKEN"
  }
}
```

```sh
# le jeton ne s'écrit pas dans le fichier de configuration, jamais
export MOXY_UI_TOKEN="$(openssl rand -hex 16)"
./bin/moxyd -config config.local.json -web apps/web/dist
```

Le jeton suit exactement le chemin d'un secret de cluster : lu dans
l'environnement au démarrage, enveloppé dans le type qui se rédige en `***`
partout, puis la variable est **effacée de l'environnement**. Il n'apparaît ni
dans un journal, ni dans un message d'erreur, ni dans une réponse. Le
chargement refuse un jeton de moins de 32 caractères — rien ne limite les
tentatives, c'est la longueur qui rend la recherche vaine — ainsi qu'un jeton
portant un caractère qu'un cookie ne peut pas transporter (espace, `;`, `,`,
`\`, `"`).

À l'usage :

1. le navigateur ouvre moxy, l'API répond `401`, l'interface affiche un écran
   de saisie et rien d'autre ;
2. `POST /api/login` avec `{"token": "…"}` en `application/json` ; un corps de
   formulaire est refusé (`415`), ce qu'une page tierce ne peut de toute façon
   pas envoyer sans préflight ;
3. le serveur répond `204` et pose un cookie `HttpOnly`, `SameSite=Strict`,
   `Path=/`, `Secure` **quand la requête est en TLS**, sans date d'expiration —
   il disparaît avec la session du navigateur ;
4. un jeton faux répond `401`, sans cookie et sans indice. La comparaison est à
   temps constant, sur des empreintes SHA-256 : ni la longueur du jeton
   configuré ni la longueur d'un préfixe juste ne se mesurent.

Pour tout ce qui n'est pas un navigateur — un scrutateur Prometheus sur
`/metrics`, une commande dans un runbook — le jeton se présente en en-tête :

```sh
curl -fsS -H "Authorization: Bearer $MOXY_UI_TOKEN" http://127.0.0.1:8080/metrics
```

**Il n'y a pas de session côté serveur.** Le cookie porte le jeton, rien de
plus : une session serait un état à stocker, à expirer et à invalider, et un
démon qui refuse de détenir des mots de passe n'a pas à détenir une table de
sessions. La conséquence se dit franchement : pour révoquer, il faut changer le
jeton et redémarrer.

**Le bundle du frontend est servi sans jeton dans ce mode**, et lui seul : sans
cela le navigateur recevrait un `401` sans aucun moyen de demander le jeton. Ce
qui est servi est du JavaScript et du CSS identiques dans tous les
déploiements — aucun nom de cluster, aucun nœud, aucune mesure. Tout ce qui
porte le parc, `/api` et `/metrics`, reste derrière le jeton.

**Ce mode est explicitement inférieur à `proxy-header`, et ne doit pas devenir
le défaut de confort.** Un jeton partagé **autorise, il n'identifie personne** :
tous ceux qui le détiennent sont le même appelant, et aucune ligne de journal ne
pourra jamais dire qui a demandé quoi. Le démarrage le rappelle à chaque
lancement :

```
warning: auth mode "token" authorizes with one shared secret and identifies
nobody; prefer "proxy-header" wherever an authenticating proxy can be put in
front (see README)
```

Partout où une brique authentifiante peut être posée devant moxy, c'est
`proxy-header` qu'il faut choisir.

### Délais et bascule d'URL

Quatre durées, et elles se combinent :

| Durée | Ce qu'elle borne |
|---|---|
| `connectTimeout` | l'établissement d'une connexion vers **un** nœud |
| `timeout` | l'obtention d'une réponse depuis **un** nœud, connexion comprise |
| le budget d'un tour | l'ensemble des tentatives d'un tour de scrutation |
| le budget d'une requête de détail | l'ensemble des appels que sert une route par objet |

**`timeout` est un délai par tentative, pas un total**, et c'est le piège de
dimensionnement de ce réglage : trois `urls` avec `timeout: 4s` peuvent coûter
douze secondes avant que moxy renonce, pas quatre. Le régler comme s'il bornait
l'ensemble des tentatives donne N fois ce qu'on croyait demander. Ce qui borne
l'ensemble, c'est le budget de l'appelant, et il n'est pas configurable : celui
d'un tour de scrutation est décrit juste en dessous ; sur les routes de détail,
c'est **15 s par appel amont et 20 s pour la requête entière**
(`internal/detail/service.go`), une requête de détail en enchaînant plusieurs et
en lançant une partie de front.

Le budget d'un tour n'est pas un réglage : il est **dérivé** du cluster, à
`2 × timeout + 2s`, de quoi essayer deux URL. C'était auparavant une constante
de six secondes, ce qui désactivait en silence la bascule que la liste `urls`
promet : avec `timeout: 4s` et un premier nœud figé — accessible mais muet — ce
nœud consommait quatre secondes, le deuxième héritait des deux restantes et le
troisième n'était jamais essayé ; avec `timeout: 6s`, la première tentative
consommait le tour entier. Chaque tick échouait, et le cluster passait
« injoignable » au bout d'une minute alors que deux de ses trois nœuds
répondaient.

Séparer la connexion de la réponse règle l'autre moitié du problème : un nœud
**éteint** ne coûte plus que `connectTimeout`, pas `timeout`.

Le démarrage journalise la combinaison retenue pour chaque cluster — c'est la
première chose à regarder quand un cluster clignote :

```
cluster "qualification": 2s to connect, 4s per call, 10s per poll round
```

Un budget de tour supérieur à la minute au bout de laquelle une lecture est
déclarée ancienne fait l'objet d'un avertissement explicite : le cluster se
déclarerait injoignable alors qu'il répond.

**Tout champ inconnu fait échouer le démarrage**, en le nommant. Une faute de
frappe qui se décode en silence est un réglage que l'opérateur croit appliqué :
`"memroy": 0.9` laissait le seuil mémoire à son défaut sans un mot. JSON n'a pas
de commentaires, donc une clé `_comment` est refusée elle aussi — c'est déjà une
faute de frappe, et elle en cacherait d'autres. À noter que Go apparie les noms
de champs **sans tenir compte de la casse** : `tokenID` et `cafile` atteignent
bien `tokenId` et `caFile`, ce sont les mots réellement différents qui sont
attrapés.

### Proxy

**moxy joint les nœuds en direct et n'utilise aucun proxy par défaut.** Les
variables d'environnement `HTTPS_PROXY`, `https_proxy`, `ALL_PROXY` et
`all_proxy` du processus sont **ignorées** pour les appels PVE ; si l'une d'elles
est posée, le démarrage le signale dans le journal plutôt que de laisser croire
qu'elle s'applique.

C'est délibéré. Le modèle « une liste d'URL de nœuds » suppose une connexion
directe, et un proxy hérité de l'environnement produit deux effets fâcheux : des
échecs `proxyconnect tcp: …` incompréhensibles sur un cluster parfaitement
joignable, et surtout — pour un cluster en `tls.mode: insecure` — une
interception acceptée sans broncher, le proxy présentant son propre certificat et
lisant l'en-tête `Authorization`, donc le token. La règle « TLS assoupli par
cluster, jamais globalement » ne tient que si aucun intermédiaire ne peut
s'insérer par l'environnement.

Un opérateur qui a réellement besoin d'un proxy le déclare **par cluster** :

```json
{
  "id": "production",
  "urls": ["https://prox-prod-2401-cit:8006"],
  "proxy": "http://proxy-sortant.example:3128"
}
```

Deux conséquences à connaître :

- le proxy reste soumis à la politique TLS de son cluster. Un proxy interceptant
  échouera en mode `system` ou `pinned`, et c'est le comportement voulu : la
  vérification du certificat ne se contourne pas en passant par un intermédiaire ;
- l'URL ne porte ni chemin, ni identifiants. Un mot de passe de proxy serait un
  secret au repos dans un fichier qui se copie et se joint à un ticket, ce que la
  configuration de moxy ne contient nulle part — voir [Secrets](#secrets).

### Variables d'environnement

Elles sont rassemblées ici parce qu'elles étaient dispersées de section en
section, et que deux d'entre elles n'apparaissaient nulle part. **Un drapeau
l'emporte toujours sur la variable correspondante** : la variable ne fait que
fournir le défaut du drapeau.

| Variable | Équivalent | Défaut | Qui la lit |
|---|---|---|---|
| `MOXY_ADDR` | `-addr` | `127.0.0.1:8080` (`0.0.0.0:8080` dans l'image) | `moxyd`. Voir l'avertissement d'écoute de [Développement](#développement). |
| `MOXY_CONFIG` | `-config` | `config.local.json` dans le répertoire courant | `moxyd`. Ignorée avec `-mock`, qui ne lit aucune configuration. |
| `MOXY_WEB` | `-web` | vide, donc API seule | `moxyd`. Répertoire du bundle ; le démarrage échoue s'il n'a pas d'`index.html`. |
| `MOXY_ALLOWED_HOSTS` | `-allowed-hosts` | vide | `moxyd`. Liste séparée par des virgules, voir [Vérification de l'en-tête `Host`](#vérification-de-len-tête-host). |
| *le nom donné par `clusters[].secretEnv`* | — | — | `moxyd`, au démarrage. C'est le secret du token, il n'a pas de nom imposé ; la convention du dépôt est `MOXY_SECRET_<CLUSTER>`. La variable est **effacée de l'environnement** une fois lue, voir [Secrets](#secrets). |
| `MOXY_API` | — | `http://127.0.0.1:8080` | Le serveur de développement Vite (`apps/web/vite.config.ts`) : la cible vers laquelle `/api` est proxié. |
| `MOXY_CHECK_WEB` | — | `1` | `scripts/check.sh`. `0` saute les vérifications frontend ; c'est ce que fait `make check-api`, et le job backend de la CI. |
| `MOXY_CONTAINER_ENGINE` | — | `podman`, sinon `docker` | `scripts/build-image.sh`. Force le moteur quand les deux sont installés. |
| `MOXY_SECRET` | — | — | `scripts/probe-pve.sh` seulement : le secret du token que la sonde présente. Sans rapport avec `secretEnv`. |

Les variables `HTTPS_PROXY`, `https_proxy`, `ALL_PROXY` et `all_proxy` sont
**ignorées** pour les appels PVE, et leur présence est signalée au démarrage :
voir [Proxy](#proxy). Les scripts de construction lisent par ailleurs `VERSION`,
`IMAGE` et `TAG` (`scripts/build.sh`, `scripts/build-image.sh`), et la rotation
d'images `OWNER`, `PACKAGE`, `KEEP`, `REGISTRY`, `GHCR_TOKEN` et `PRUNE_APPLY`
(`scripts/prune-images.sh`).

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

En production, le secret arrive par un `EnvironmentFile` que seul `root` peut
lire — **jamais par la ligne de commande**, que `ps(1)` expose à tous les
utilisateurs de la machine :

```sh
# /etc/moxy/secrets.env — chmod 0600, propriétaire root:root
MOXY_SECRET_QUALIFICATION=...
MOXY_SECRET_PREPRODUCTION=...
MOXY_SECRET_PRODUCTION=...
```

systemd lit ce fichier lui-même, en tant que `root`, avant de déposer les
privilèges : l'utilisateur de service n'a donc pas besoin d'y accéder. L'unité
correspondante est livrée durcie dans
[`deploy/moxyd.service`](deploy/moxyd.service) — utilisateur dédié, aucune
capacité, système de fichiers en lecture seule, filtre d'appels système ;
`systemd-analyze security --offline=true deploy/moxyd.service` la note **1.2**.
La marche à suivre complète, du token en lecture seule au reverse proxy qui
authentifie, est dans [Déploiement sécurisé](docs/DEPLOIEMENT.md).

Une fois la configuration chargée, `moxyd` **efface** de son propre
environnement les variables nommées par `secretEnv` : elles ne sont plus dans
`os.Environ()`, donc plus dans ce que le processus pourrait transmettre.

### Rotation d'un token

Le secret n'est lu qu'**au démarrage**, et `SIGHUP` est ignoré : il n'y a pas de
rechargement à chaud. Une rotation est donc un redémarrage, et la seule question
est de savoir combien de temps le cluster reste sans être lu. La voie qui ne
coûte rien crée le nouveau token avant de retirer l'ancien :

```sh
# 1. sur un nœud du cluster : un second token, à côté de celui qui sert
pveum user token add moxy@pve ro2 --privsep 1
pveum acl modify / --tokens 'moxy@pve!ro2' --roles PVEAuditor
#    la commande affiche le secret UNE SEULE FOIS

# 2. sur l'hôte moxy : le nouveau tokenId dans la configuration, le nouveau
#    secret dans le fichier d'environnement, puis
systemctl restart moxyd

# 3. une fois la carte du cluster revenue au vert, retirer l'ancien
pveum user token remove moxy@pve ro
```

Faire l'inverse — retirer d'abord — laisse le cluster en `unreachable` avec une
erreur `auth` jusqu'au redémarrage : moxy continue de servir son dernier
instantané, daté, mais il ne lit plus rien. C'est visible immédiatement sur
`moxy_pve_requests_total{outcome="auth"}` et sur
`moxy_poll_last_success_timestamp_seconds`, qui cesse d'avancer — voir
[Observabilité](#observabilité).

Le même geste vaut pour un secret qu'on croit divulgué, à ceci près qu'il n'y a
alors rien à ménager : retirer le token d'abord, la coupure de lecture étant
préférable à un secret vivant.

## Privilèges PVE requis

Pour la vue d'ensemble, le rôle **`PVEAuditor` sur `/`** suffit. Il couvre les trois
appels du chemin critique :

| Endpoint | Usage | Privilège |
|---|---|---|
| `/cluster/resources` | nœuds, VM/CT, stockage | `Sys.Audit` (`PVEAuditor`) |
| `/cluster/status` | quorum, nœuds en ligne | `Sys.Audit` (`PVEAuditor`) |
| `/cluster/ha/status/manager_status` | nœuds en maintenance | `Sys.Audit` (`PVEAuditor`) |
| `/nodes/{node}/apt/update` | paquets en attente, version `pve-manager` proposée | **`Sys.Modify` sur `/nodes`** |
| `/nodes/{node}/status` | version `pve-manager` installée | `Sys.Audit` sur `/nodes/{node}` |

Le bandeau « mise à jour disponible » repose sur `apt/update`, qui exige en plus
`Sys.Modify` sur `/nodes`. Sans ce droit, **moxy n'échoue pas** : l'appel renvoie
403, le cluster reste servi normalement, `updates` vaut `null` et le bandeau
n'apparaît pas. Donner ce privilège est donc un choix, pas une obligation.

Les deux appels par nœud — `apt/update` et `status` — sont faits sur le **tempo
lent** (10 min), et non à chaque scrutation : une version ne bouge qu'à une mise à
jour suivie d'un redémarrage. Ils échouent séparément, puisqu'ils ne demandent pas
le même privilège : un `apt/update` refusé ne coûte pas la version installée, et
inversement. Un nœud qui échoue laisse son champ à `null` sans faire échouer la
scrutation, et la dernière valeur connue est conservée tant qu'aucun nœud ne
répond.

> **`Sys.Audit` doit atteindre chaque nœud.** `/cluster/resources` ne refuse
> jamais une ligne `node` : sans `Sys.Audit` sur `/nodes/{node}`, PVE la renvoie
> **sans** `cpu`, `maxcpu`, `mem`, `maxmem` ni `uptime`. Les cartes affichent
> alors CPU et mémoire comme inconnus (`—`) avec l'alerte « Mesures CPU et
> mémoire indisponibles », et le cluster reste « sain » : c'est le token qui est
> en cause, pas le cluster. Un `PVEAuditor` posé sur `/` **avec propagation**
> (le défaut de `pveum acl modify`) suffit ; vérifier avec
> `pveum user token permissions moxy@pve ro --path /nodes/<nœud>`, qui doit
> lister `Sys.Audit`. Le diagnostic est aussi rendu par `scripts/probe-pve.sh`.

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

Pour activer en plus le bandeau de mises à jour, il faut `Sys.Modify` sur
`/nodes`. **Attention au piège** : les ACL de Proxmox ne s'additionnent pas d'un
niveau à l'autre. Un rôle posé sur `/nodes` **remplace** celui hérité de `/`
(`AccessControl.pm` : `$roles = $new; # overwrite previous settings`). Un rôle
ne portant que `Sys.Modify` effacerait donc `Sys.Audit` sur tout `/nodes`, et
les vues nœud et VM répondraient 403 alors que la vue d'ensemble continuerait de
fonctionner. Le rôle doit porter **les deux** privilèges :

```sh
pveum role add MoxyAptCheck --privs "Sys.Audit,Sys.Modify"
pveum acl modify /nodes --tokens 'moxy@pve!ro' --roles MoxyAptCheck
```

Si le rôle existe déjà sans `Sys.Audit` :

```sh
pveum role modify MoxyAptCheck --privs "Sys.Audit,Sys.Modify"
```

Les rôles posés **au même chemin**, eux, s'additionnent bien :
`--roles PVEAuditor,MoxyAptCheck` est une alternative valable.

À répéter sur chaque cluster : Proxmox n'a pas de notion de multi-cluster, chaque
cluster a son propre utilisateur, son propre token et son propre secret.

La mise en maintenance ne demande **aucun privilège PVE de plus**, et c'est
délibéré : elle ne passe pas par l'API, mais par un second canal SSH, et le token
reste en lecture seule. Ce qu'elle demande est ailleurs, sur les nœuds — voir
[Mise en maintenance d'un nœud](#mise-en-maintenance-dun-nœud).

## Mise en maintenance d'un nœud

> **Le canal n'est pas encore opérationnel.** La décision est prise et écrite
> ([ADR 0010](docs/adr/0010-node-maintenance-over-ssh.md)), le bloc `maintenance` est
> chargé et validé au démarrage, et `deploy/` porte de quoi préparer un nœud. Le
> **transport SSH lui-même** — connexion, vérification de la clé d'hôte, exécution —
> **n'est pas dans cette révision** : l'environnement de développement courant n'a pas
> d'accès réseau et `golang.org/x/crypto/ssh` n'y est pas vendorable. Un moxy
> configuré comme ci-dessous démarre et refuse une configuration fautive, mais
> **aucune session ne peut encore s'ouvrir** : la fonctionnalité n'est pas
> opérationnelle en production, et rien dans l'interface ne doit la promettre. Ce qui
> suit dit ce qu'il faut avoir posé le jour où le transport arrive.

PVE n'expose aucune route REST de mise en maintenance : `node-maintenance-set` est une
sous-commande de CLI qui écrit une commande CRM dans le système de fichiers du cluster
(vérifié dans les sources le 2026-09-12). Le
[plan](#plan-de-mise-en-maintenance) reste donc strictement en lecture seule, et
l'exécution passe par un **second canal**, SSH, ouvert vers un **autre** nœud du
cluster que celui qu'on draine :

```sh
ha-manager crm-command node-maintenance enable <nœud>
```

Le token PVE, lui, ne bouge pas : il reste en lecture seule, et le SSH ne sert pas à
élargir ses droits.

Ce que ce canal ouvre est délibérément minuscule — un compte de service sans shell
atteignable, une commande imposée par `sshd`, deux verbes, un `sudo` borné à un seul
verbe. C'est cette clôture, et non la confiance dans le démon, qui rend le canal
acceptable ; le raisonnement et le modèle de menace — ce qu'un attaquant obtient s'il
prend la clé, le compte `moxy` sur un nœud, ou le processus `moxyd` — sont dans
l'[ADR 0010](docs/adr/0010-node-maintenance-over-ssh.md).

### Deux modes, et on commence par le premier

Le mode dit **d'où vient la clé qui ouvre les sessions**. C'est une propriété du
processus : un seul mode pour tout le parc, aucun cluster et aucun nœud n'y déroge, et
il n'y a pas de repli automatique de l'un vers l'autre.

| Mode | La clé | Ce que chaque nœud héberge | Quand |
|---|---|---|---|
| `ssh-key` | une paire ed25519 dédiée, sans passphrase, lue une fois au démarrage | la clé publique dans `authorized_keys`, avec `restrict`, `command=` et éventuellement `from=` | le mode par lequel on commence, et il suffit aux cas simples |
| `openbao` | une paire éphémère engendrée **à chaque exécution**, dont la publique est signée par OpenBao | une ligne `TrustedUserCAKeys`, et rien à faire vivre par nœud | le mode vers lequel on bascule quand le durcissement le justifie |

Le gain du second tient en une phrase : les contraintes ne sont plus dans un fichier
que le nœud héberge — réécrivable par qui obtient un pied sur le nœud, oubliable sur le
nœud ajouté six mois plus tard — mais dans un rôle, à un seul endroit.

#### Commencer en mode `ssh-key`

```json
{
  "auth": { "mode": "proxy-header", "trustedProxies": ["10.0.0.0/24"] },
  "maintenance": {
    "mode": "ssh-key",
    "sshKey": { "keyFile": "/etc/moxy/ssh/id_ed25519" },
    "ssh": {
      "user": "moxy",
      "port": 22,
      "knownHostsFile": "/etc/moxy/ssh/known_hosts",
      "connectTimeout": "2s",
      "timeout": "20s"
    }
  },
  "clusters": [
    {
      "id": "qualification",
      "maintenance": {
        "enabled": true,
        "allowedUsers": ["alice", "bob"],
        "hosts": { "prox-qual-2201-cit": "10.0.0.11", "prox-qual-2202-cit": "10.0.0.12" }
      }
    }
  ]
}
```

La paire est **dédiée à ce déploiement de moxy**, sans passphrase — un démon ne peut
pas se voir demander une passphrase, et la ranger dans la variable d'environnement d'à
côté ne protégerait de rien —, jamais partagée avec un humain et jamais réutilisée pour
autre chose :

```sh
ssh-keygen -t ed25519 -N '' -C 'moxy@<déploiement>' -f /etc/moxy/ssh/id_ed25519
```

La clé privée vit dans un **fichier**, et non dans une variable d'environnement comme
le secret d'un token : c'est une donnée multi-lignes, que les orchestrateurs montent en
fichier, et `tls.caFile` a déjà posé le précédent. Elle est lue une fois au démarrage
et enveloppée dans le même type que les autres secrets, qui se rédige en `***` partout.

#### Basculer en mode `openbao`

Seul le bloc de la clé change ; le bloc `ssh` et tout ce qui est par cluster sont
identiques :

```json
  "maintenance": {
    "mode": "openbao",
    "openbao": {
      "address": "https://bao.example.net:8200",
      "tls": { "mode": "pinned", "caFile": "/etc/moxy/ca/openbao.pem" },
      "roleId": "db02de05-fa39-4855-059b-67221c5c2f63",
      "secretIdFile": "/run/moxy/openbao-secret-id",
      "wrapped": true,
      "mountPath": "ssh-client-signer",
      "sshRole": "moxy-maintenance",
      "timeout": "5s"
    },
    "ssh": { "…": "inchangé" }
  }
```

moxy ne parle à OpenBao qu'en HTTP et en JSON, sans SDK : un login AppRole puis une
signature, à chaque exécution, et rien n'est gardé entre deux. Le **TTL du certificat
n'est pas un réglage de moxy** — le rôle le fixe, moxy ne demande rien, et c'est un
champ de configuration en moins. Le moteur SSH, le rôle, la politique et l'AppRole à
créer sont décrits pas à pas dans
[`deploy/moxy-openbao-role.md`](deploy/moxy-openbao-role.md).

`wrapped: true` est le motif recommandé : le fichier porte alors un jeton de *response
wrapping* à usage unique, déwrappé au démarrage. La contrainte d'exploitation est à
connaître **avant** de choisir — le fichier doit vivre sur un `tmpfs` et être
**régénéré à chaque redémarrage de moxy**. `wrapped: false` monte un `secret_id` à TTL
long : plus simple à exploiter, moins bon.

Deux conséquences de ce mode, à avoir en tête avant de basculer :

- **Le démarrage ne dépend pas de la joignabilité d'OpenBao**, et `/readyz` non plus.
  La forme est validée et les fichiers sont lus au chargement, la connectivité ne l'est
  pas : sortir du service un démon de supervision parce qu'un tiers est scellé, c'est
  perdre la vue d'ensemble — qui, elle, fonctionne — en même temps que la maintenance.
- **La source de la clé devient une dépendance de disponibilité sur le chemin d'un
  geste d'urgence.** On draine un nœud surtout quand quelque chose va mal, et c'est le
  pire moment pour découvrir qu'OpenBao est scellé. Il n'y a **pas** de repli
  automatique vers `ssh-key` : ce serait le parc mixte que la configuration rend
  impossible. Le recours est ailleurs et il est déjà là — la commande `ha-manager` que
  la modale affiche. Revenir en `ssh-key` reste possible, mais c'est un changement de
  configuration délibéré, écrit et redémarré.

#### Ce que le démarrage refuse

- **`mode` est obligatoire dès que le bloc existe, et n'a aucun défaut.** `tls.mode`
  peut se permettre un défaut parce que `system` est le choix sûr ; ici les deux modes
  ne sont pas comparables, et celui qui décide où vit un secret l'écrit de sa main.
- **Le bloc du mode non choisi doit être absent, pas seulement ignoré** :
  `maintenance: openbao is only used in "openbao" mode`, sur le patron de
  `auth: tokenEnv is only used in "token" mode`. Un réglage qui ne fait rien dans le
  mode choisi fait lire au fichier autre chose que ce qu'il applique.
- **`clusters[].maintenance` ne porte aucun champ de mode** : le parc mixte n'est pas
  interdit par une validation, il est impossible à écrire.
- **`auth.mode: "none"` interdit la maintenance.** Un bloc `maintenance` sans
  authentification fait échouer le démarrage, sinon quiconque atteint le port draine la
  production.
- **`knownHostsFile` est obligatoire dans les deux modes**, et le fichier doit exister
  au démarrage. Il n'y a pas de mode permissif pour les clés d'hôte et il n'y en aura
  pas : `tls.mode: "insecure"` assouplit une *lecture* de mesures, tandis qu'une clé
  d'hôte non vérifiée fait exécuter une commande privilégiée sur une machine usurpée.

### Les clés de configuration

| Champ | Obligatoire | Défaut | Description |
|---|---|---|---|
| `maintenance.mode` | oui, si le bloc existe | — (aucun) | `ssh-key` ou `openbao`. Sans défaut, et le bloc de l'autre mode doit être absent. |
| `maintenance.sshKey.keyFile` | si `ssh-key` | — | Clé privée ed25519 au format PEM, **non chiffrée**, en `0600` et appartenant à l'uid du processus. Lue une fois au démarrage. Chemin relatif résolu depuis le dossier du fichier de configuration, comme `caFile`. **Interdit** en mode `openbao`. |
| `maintenance.openbao.address` | si `openbao` | — | URL absolue en `https`, sans chemin, sans identifiants, sans requête ni fragment : le client y ajoute `/v1/…`. |
| `maintenance.openbao.tls.mode` | non | `system` | `system` ou `pinned`. **Pas d'`insecure` ici** : ce qui revient de cette adresse n'est pas une mesure, c'est une signature. |
| `maintenance.openbao.tls.caFile` | si `pinned` | — | Fichier PEM du CA d'OpenBao. Interdit dans les autres modes. |
| `maintenance.openbao.roleId` | si `openbao` | — | La moitié non sensible des identifiants AppRole. Vit en clair dans la configuration. |
| `maintenance.openbao.secretIdFile` | si `openbao` | — | Fichier portant la moitié sensible. Lu au démarrage, espaces de fin retirés ; un fichier vide fait échouer le démarrage. |
| `maintenance.openbao.wrapped` | non | `false` | `true` quand le fichier porte un jeton de *response wrapping* à usage unique, à déwrapper au démarrage — et donc à régénérer à chaque redémarrage. |
| `maintenance.openbao.mountPath` | non | `ssh-client-signer` | Montage du moteur SSH. Un ou plusieurs segments `[A-Za-z0-9._-]`, sans échappement ni segment `..` : le chemin est interpolé dans l'URL de la requête de signature. |
| `maintenance.openbao.sshRole` | si `openbao` | — | Le rôle qui signe, et qui porte **toutes** les contraintes du certificat. Un seul segment. |
| `maintenance.openbao.timeout` | non | `5s` | Budget d'un appel à OpenBao, au format `time.Duration`. Deux appels par exécution. Refusé au-delà de `60s`. |
| `maintenance.ssh.user` | non | `moxy` | Compte de service sur les nœuds, conforme à `^[a-z_][a-z0-9_-]{0,31}$`. |
| `maintenance.ssh.port` | non | `22` | Port `sshd` des nœuds, dans `1..65535`. |
| `maintenance.ssh.knownHostsFile` | **oui** | — | Clés d'hôte relevées hors bande, au format habituel. Obligatoire dans les deux modes ; le fichier doit exister et être régulier au démarrage. Aucun réglage ne permet de désactiver la vérification. |
| `maintenance.ssh.connectTimeout` | non | `2s` | Délai pour établir la connexion vers **un** nœud. Doit rester inférieur ou égal à `timeout`. |
| `maintenance.ssh.timeout` | non | `20s` | Budget de la session entière, connexion comprise. Refusé au-delà de `60s`. |
| `clusters[].maintenance.enabled` | non | `false` | Ouvre la maintenance sur ce cluster. Exige le bloc `maintenance` du processus, faute de quoi le démarrage échoue. Un cluster qui ne dit rien ne participe pas, et la route lui répondra `404`. |
| `clusters[].maintenance.allowedUsers` | non | — | Identités autorisées à drainer un nœud de ce cluster. Qui peut drainer la production n'est pas qui peut drainer la qualification, d'où le par-cluster. Un nom vide fait échouer le démarrage. |
| `clusters[].maintenance.hosts` | non | — | Nom de nœud PVE → adresse à laquelle la session est ouverte. **Table de surcharge** : un nœud absent est joint à son propre nom. Elle existe parce qu'`urls` est une liste sans étiquette — rien ne garantit que la première étiquette d'un FQDN soit le nom du nœud derrière. |

Le bloc `maintenance` du processus et son sous-bloc `ssh` sont **à portée processus** :
un seul `known_hosts` est fait pour porter tout un parc, et un fichier par cluster
multiplierait les endroits où le nœud ajouté le mois dernier manque. Ce qui reste par
cluster est ce qui diffère réellement d'un cluster à l'autre : la participation, les
autorisations et les adresses.

### Préparer les nœuds

[`deploy/moxy-node-setup.sh`](deploy/moxy-node-setup.sh) se joue **en `root` sur chaque
nœud** du cluster — à la main, depuis Ansible, ou par un essaimage `pvesh`. Il est
idempotent : le rejouer sur un nœud déjà préparé ne change rien et le dit.

```sh
# mode ssh-key : une clé publique, contrainte dans authorized_keys
./moxy-node-setup.sh --mode ssh-key --pubkey moxy_ed25519.pub --from 10.0.0.5

# mode openbao : la CA de confiance, aucun fichier par nœud à faire vivre
./moxy-node-setup.sh --mode openbao --ca moxy-ca.pub

# dernière étape de la bascule, et une passe à part
./moxy-node-setup.sh --remove-authorized-key
```

Ce qu'il pose est **le même dans les deux modes**, et seule la façon dont `sshd` décide
de faire confiance à moxy diffère :

| Fichier | Mode | Rôle |
|---|---|---|
| le compte `moxy` (`/usr/sbin/nologin`, `/var/lib/moxy`) | les deux | un compte de service sans interpréteur atteignable |
| [`/usr/local/sbin/moxy-maintenance`](deploy/moxy-maintenance) — `0755`, `root:root` | les deux | le validateur de la commande imposée : trois mots, deux verbes, un nom de nœud vérifié **contre les nœuds réels** (`/etc/pve/nodes/`). Codes de sortie distincts, `64` pour la grammaire et `65` pour un nœud inconnu, que le backend traduit en `kind` différents. Il appartient à `root` : le compte qu'il contraint ne peut pas le réécrire. |
| [`/etc/sudoers.d/moxy-maintenance`](deploy/moxy-maintenance.sudoers) — `0440` | les deux | le `sudo` borné au seul verbe `ha-manager crm-command node-maintenance`. Contrôlé par `visudo -c -f` **avant** d'être posé : un fichier `sudoers` qui ne s'analyse pas n'est pas un fichier inerte, c'est un `sudo` cassé pour tous les utilisateurs du nœud. |
| `/var/lib/moxy/.ssh/authorized_keys` — `0600` | `ssh-key` | la clé publique, précédée de `restrict`, de `command="/usr/local/sbin/moxy-maintenance"` et, si l'adresse de sortie de moxy est stable, de `from=`. |
| `/etc/ssh/sshd_config.d/10-moxy.conf` + `/etc/ssh/moxy-ca.pub` | `openbao` | `TrustedUserCAKeys` : ce sont les certificats qui portent les contraintes. Le script vérifie que `sshd` lit réellement le fichier déposé, puis recharge le service. |

**Le `*` de `sudoers` n'est pas la barrière**, et le lire comme telle est l'erreur que
le commentaire du fichier existe pour empêcher : le joker de `sudo` accepte les espaces,
donc il ne borne pas le dernier argument. La vraie barrière est le validateur, seul
chemin par lequel le compte `moxy` peut atteindre `sudo` — pas de shell, commande
imposée. Ce que `sudoers` apporte est l'autre moitié : il ferme le verbe, pour que la
permission ne soit jamais « tout `ha-manager` ».

#### Relever les clés d'hôte

Les clés d'hôte se relèvent **hors bande**, et se comparent à ce que la console du nœud
affiche — un `known_hosts` rempli par une première connexion confiante ne vérifie rien :

```sh
ssh-keyscan -t ed25519 prox-qual-2201-cit >> /etc/moxy/ssh/known_hosts
# à comparer à l'empreinte lue sur la console du nœud :
ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub
```

C'est la seule étape que le script ne peut pas faire à votre place, et il le rappelle en
dernière ligne.

### Basculer d'un mode à l'autre sans coupure

Les deux variantes **coexistent sur un nœud** : `sshd` accepte une clé listée dans
`authorized_keys` *ou* un certificat signé par une CA de confiance. C'est ce qui rend la
trajectoire — commencer simple, durcir plus tard — praticable sans fenêtre
d'indisponibilité, à condition de tenir l'ordre :

1. **déposer la CA sur tous les nœuds**, en gardant les `authorized_keys` :
   `./moxy-node-setup.sh --mode openbao --ca moxy-ca.pub` ;
2. **basculer** `maintenance.mode` de `ssh-key` à `openbao` dans la configuration, puis
   redémarrer moxy — il n'y a pas de rechargement à chaud, `SIGHUP` est ignoré ;
3. **vérifier** une mise en maintenance de bout en bout, sur un nœud sans conséquence ;
4. **alors seulement** retirer les clés et détruire la paire :
   `./moxy-node-setup.sh --remove-authorized-key`.

L'ordre importe, et c'est tout ce qu'il y a à retenir : retirer la clé avant l'étape 3,
c'est découvrir un rôle OpenBao mal cadré sans plus aucun moyen d'entrer. Le script
refuse pour cette raison de combiner `--mode` et `--remove-authorized-key` — ajouter la
CA ne retire jamais la clé. Le rôle se teste d'ailleurs sans moxy avant l'étape 2,
`ssh-keygen -Lf` sur un certificat d'essai devant montrer `force-command` et une section
`Extensions:` vide ; la marche à suivre est au §5 de
[`deploy/moxy-openbao-role.md`](deploy/moxy-openbao-role.md).

Le retour en arrière emprunte le même chemin dans l'autre sens : reposer la clé
(`--mode ssh-key`), rebasculer le mode, vérifier, et retirer la CA ensuite.

### Monter la clé ou le `secret_id` dans le conteneur

Le processus tourne en **uid 65532** (`nonroot`) dans l'image, et deux vérifications du
démarrage portent exactement là-dessus : la clé privée doit être en `0600` — un
`mode & 0o077` non nul est un échec franc et nommé — et **appartenir à l'uid du
processus**, pas à `root`. C'est le seul moment où voir le problème est bon marché ;
l'alternative est une clé lisible par le groupe depuis six semaines quand quelqu'un
regarde enfin.

```sh
# sur l'hôte, avant de démarrer le conteneur
install -o 65532 -g 65532 -m 0600 id_ed25519  /etc/moxy/ssh/id_ed25519
install -o 65532 -g 65532 -m 0644 known_hosts /etc/moxy/ssh/known_hosts

podman run --rm --read-only \
  -p 127.0.0.1:8080:8080 \
  -v /etc/moxy:/etc/moxy:ro \
  --env-file /etc/moxy/secrets.env \
  ghcr.io/dmajorel/moxy:edge
```

En mode `openbao`, il n'y a **aucune clé durable à monter** : la paire naît et meurt
avec l'exécution. Ce qui se monte est le `secret_id`, aux mêmes conditions de lisibilité
par l'uid 65532, et — en `wrapped: true` — sur un `tmpfs` de l'hôte dont le contenu est
régénéré avant chaque démarrage du conteneur :

```sh
install -d -o 65532 -g 65532 -m 0700 /run/moxy
bao write -wrap-ttl=60s -field=wrapping_token \
    -f auth/approle/role/moxy-maintenance/secret-id >/run/moxy/openbao-secret-id
chown 65532:65532 /run/moxy/openbao-secret-id
chmod 0600 /run/moxy/openbao-secret-id

podman run --rm --read-only \
  -p 127.0.0.1:8080:8080 \
  -v /etc/moxy:/etc/moxy:ro \
  -v /run/moxy:/run/moxy:ro \
  --env-file /etc/moxy/secrets.env \
  ghcr.io/dmajorel/moxy:edge
```

Le jeton étant à usage unique, un redémarrage sans régénération fait échouer la première
maintenance, pas le démarrage : la joignabilité de la source de la clé n'est pas
vérifiée au chargement. C'est la contrepartie assumée du point précédent.

Ni la clé ni le `secret_id` n'apparaissent jamais dans un journal, un message d'erreur
ou une réponse : ils suivent le chemin des autres secrets du démon, enveloppés dans le
type qui se rédige en `***`. Voir [Secrets](#secrets).

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
supprime le risque. C'est aussi pourquoi aucun proxy n'est hérité de
l'environnement : un intermédiaire imposé au processus suffirait à annuler le
« par cluster » de cette page (voir [Proxy](#proxy)).

## Mode mock

```sh
./bin/moxyd -mock
```

`-mock` sert un jeu de données figé (quatre clusters, quatorze nœuds, 154 VM,
sept alertes) **sans lire de configuration ni ouvrir la moindre connexion
réseau**. C'est le mode prévu pour développer le frontend sans cluster
joignable, et pour les tests de bout en bout du serveur.

Les trois clusters de l'écran 4 du document de passation sont là tels quels :
`qualification` sain, `preproduction` dégradé par un nœud en maintenance,
`production` sain sous son bandeau de mise à jour. Un quatrième,
`lab`, existe pour les états que les trois autres n'atteignent jamais :
**injoignable** avec son dernier instantané connu et une erreur `tls`, un nœud
**hors ligne**, un nœud en ligne **sans mesures** — ce que PVE renvoie quand le
token n'a pas `Sys.Audit` sur `/nodes/{node}` —, un **quorum perdu**, et un nœud
parfaitement **à jour** (`pendingUpdates: 0`, qui se lit « à jour » et non
« inconnu »). Chacun de ces cas a un rendu dans l'interface ; un jeu d'exemple
où tout va bien laisserait passer une interface incapable de montrer autre
chose.

Les **routes de détail répondent aussi** en mode mock, et leurs réponses sont
dérivées de cette même vue d'ensemble : un nœud ouvert depuis l'arbre porte
exactement les chiffres qu'affichait sa carte. Les séries RRD comportent
délibérément des trous, et la tâche la plus récente est laissée en cours — un
mock sans ces cas laisserait passer une interface incapable de les afficher.

## Déploiement en conteneur

Le produit se livre sous forme d'une **image OCI unique** : `moxyd` y sert l'API et
le bundle du frontend (`-web`), sous la même origine. L'image est publiée par la CI
sur `ghcr.io/dmajorel/moxy` avec les tags `edge` (dernier `main`), `X.Y.Z` / `X.Y` /
`latest` (tags `vX.Y.Z`) et `sha-<commit>`, pour `linux/amd64` et `linux/arm64`.

Les images `sha-<commit>` s'accumulent sans fin : la rotation n'en garde que les
**cinq dernières**, `edge` compris puisqu'il désigne la plus récente. La
[variante de débogage](#variante-de-débogage) roule dans une fenêtre de cinq qui
lui est propre, ses tags de version compris — ce n'est pas ce qu'épingle un
déploiement : la compter avec l'image livrée diviserait par deux la rétention
promise, puisqu'une poussée publie désormais deux images. Une image
portant un tag de version (`X.Y.Z`, `X.Y`, `latest`) n'est jamais supprimée, quel
que soit son âge — c'est ce qu'épingle un déploiement. Elle s'exécute après
chaque publication, et une fois par semaine pour les semaines sans fusion
(`scripts/prune-images.sh`, workflow `Image retention` ; sans `PRUNE_APPLY=1` il
se contente de lister ce qu'il supprimerait).

Supprimer une version non taguée pour son seul âge casserait l'image qui la
référence : une publication multi-arch en produit quatre — les deux manifestes
de plateforme et les deux attestations — et elles ne sont pas des orphelines.
Le script part donc des images à garder et conserve tout ce dont elles sont
faites.

Construction locale, avec `podman` ou `docker` :

```sh
./scripts/build-image.sh                 # ghcr.io/dmajorel/moxy:dev
IMAGE=moxy TAG=test ./scripts/build-image.sh
TARGET=debug ./scripts/build-image.sh    # …:dev-debug, voir plus bas
```

Le [`Containerfile`](Containerfile) construit le bundle (Node 26), compile `moxyd`
(Go 1.27, `CGO_ENABLED=0`, `GOPROXY=off`, donc sans accès réseau) et assemble une
image `distroless/static` : pas de shell, pas de client HTTP, pas de gestionnaire
de paquets, utilisateur `nonroot` (uid 65532), bundle de CA système présent (le
mode `tls.mode: system` fonctionne). Le
[`.dockerignore`](.dockerignore) tient les artefacts locaux et les `*.local.json`
hors du contexte de build.

### Variante de débogage

Cette absence se paie au diagnostic : `podman exec -it moxy sh` n'a rien à
lancer, et rien ne permet d'éprouver depuis le conteneur ce que `moxyd` voit
réellement du réseau — un nœud PVE joignable ou non, un certificat épinglé, un
DNS muet plutôt qu'un pare-feu qui jette, un montage lisible par l'uid 65532.
D'où une **variante de débogage**, construite depuis `debian:12-slim` et portant
`bash`, `curl` et `ca-certificates` autour du même binaire et du même bundle :

```sh
make image-debug                              # ghcr.io/dmajorel/moxy:dev-debug
TARGET=debug IMAGE=moxy TAG=test ./scripts/build-image.sh
podman build -f Containerfile --target debug -t moxy:debug .
```

> **Ce n'est pas l'image à déployer.** `curl` dans un processus qui détient des
> tokens d'hyperviseur est une primitive d'exfiltration toute prête, et un shell
> rend exploitable ce qui n'était qu'une lecture de fichier. La variante élargit
> délibérément la surface d'attaque — un shell, un client HTTP, un gestionnaire
> de paquets et quelques dizaines de paquets Debian — le temps d'un diagnostic,
> et rien de plus. Elle ne porte jamais le tag principal : la CI la publie sous
> `edge-debug`, `X.Y.Z-debug` et `X.Y-debug`, **jamais sous `latest`**, et un
> `build` sans `--target` produit toujours l'image sans shell — l'étage
> `distroless` est le dernier du `Containerfile`.

Elle tourne avec le **même uid (65532), les mêmes variables d'environnement et le
même point d'entrée** que l'image livrée : une variante qui ne reproduirait pas
le runtime qu'elle sert à expliquer ne prouverait rien. Ce qu'elle permet :

```sh
podman run --rm -d --name moxy -p 127.0.0.1:8080:8080 \
  -v /etc/moxy:/etc/moxy:ro --env-file /etc/moxy/secrets.env \
  ghcr.io/dmajorel/moxy:edge-debug
podman exec -it moxy bash
curl -sS localhost:8080/healthz               # depuis le conteneur
```

`/metrics` reste authentifiée dans les deux images : `curl` sous la main ne
change rien au fait que l'exposition nomme le parc.

La variante n'est publiée que pour **`linux/amd64`** : son étage installe ses
paquets avec `apt`, qui s'exécute sur l'architecture cible, et la construire pour
une autre demanderait de l'émulation. Ailleurs — et le plus souvent, car c'est la
voie qui ne coûte aucune image —, un **conteneur éphémère** partageant les
espaces de noms de celui qui tourne donne `bash` et `curl` dans le même réseau
sans rien ajouter à l'image livrée :

```sh
podman run --rm -it --pid=container:moxy --network=container:moxy \
  docker.io/library/debian:12-slim bash
kubectl debug -it moxy-0 --image=docker.io/library/debian:12-slim --target=moxy
```

### Vérifier une image publiée

Les images publiées par la CI sont **signées** (cosign, sans clé : l'identité est
celle du workflow, attestée par l'OIDC de GitHub) et portent une **provenance
complète** et un **SBOM**. Avant de déployer, plutôt que de faire confiance au tag :

```sh
cosign verify ghcr.io/dmajorel/moxy:edge \
  --certificate-identity-regexp '^https://github.com/dmajorel/moxy/' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

La signature couvre l'index multi-architecture, donc chacun des manifestes qu'il
référence. Pour lire ce qui a servi à la construire, et ce qu'elle contient :

```sh
docker buildx imagetools inspect ghcr.io/dmajorel/moxy:edge --format '{{json .Provenance}}'
docker buildx imagetools inspect ghcr.io/dmajorel/moxy:edge --format '{{json .SBOM}}'
```

Les trois `FROM` du `Containerfile` sont épinglés par digest, y compris la couche
finale `distroless/static` : un tag déplacé en amont ne peut pas changer le
contenu d'une image publiée ensuite sans que le digest change avec.

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
- **La vérification du `Host` est inactive par défaut dans l'image**, puisque
  l'écoute y est générique : poser `MOXY_ALLOWED_HOSTS` au nom public par lequel
  moxy est atteint la réactive. Voir
  [Vérification de l'en-tête `Host`](#vérification-de-len-tête-host).
- **Sondes** : `GET /healthz` pour la vivacité, `GET /readyz` pour la
  disponibilité. L'image déclare un `HEALTHCHECK` qui appelle
  `moxyd -healthcheck` : sans shell ni client HTTP, le binaire est le seul
  exécutable disponible, et il sonde `/healthz` sur son adresse locale.
  `/healthz` est exemptée de la vérification du `Host`, pour que la sonde de
  l'orchestrateur n'ait rien à savoir de ce réglage. C'est bien `/readyz` qui
  doit conditionner l'envoi de trafic : un cluster lent n'est pas une raison de
  redémarrer le démon. Sur la [variante de débogage](#variante-de-débogage), les
  deux sondes se rejouent aussi à la main (`curl -sS localhost:8080/readyz`).
- **Identité du binaire** : la première ligne du journal nomme la version de moxy,
  la toolchain Go qui a compilé le binaire et la plateforme cible.

  ```
  moxyd v0.3.1 starting (go1.27.0, linux/amd64)
  ```

  Elle est émise avant toute validation de configuration, donc elle est là même
  quand le démarrage échoue ensuite. C'est la réponse à « avec quelle stdlib cette
  instance a-t-elle été construite ? » quand un avis de sécurité Go touche
  `crypto/tls`, `crypto/x509` ou `net/http` : `go.mod` fixe le niveau de langage,
  pas la bibliothèque standard réellement liée, qui vient de l'image de base du
  `Containerfile`. L'information reste dans le journal, lisible par l'exploitant ;
  `GET /healthz` ne sert que la version de moxy.
- **Système de fichiers en lecture seule** : `moxyd` n'écrit rien sur disque,
  `--read-only` fonctionne sans volume temporaire.
- L'unité systemd durcie de [`deploy/moxyd.service`](deploy/moxyd.service) reste
  la voie de déploiement sans conteneur ; les options `--cap-drop=ALL`,
  `--security-opt no-new-privileges` et `--read-only` en sont l'équivalent ici.
  Voir [Déploiement sécurisé](docs/DEPLOIEMENT.md).

## Publier une version

Il n'y a pas de fichier `VERSION` : la version **est** le tag. `scripts/build.sh`
lit `git describe --tags` et injecte le résultat dans le binaire à l'édition de
liens, si bien que `moxyd -version`, `GET /healthz`, le label
`org.opencontainers.image.version` et les tags ghcr disent tous la même chose.
Poser un tag `vX.Y.Z`, c'est donc faire la version — et tout ce qui doit être
vrai d'une version doit l'être avant.

```sh
make release VERSION=v0.1.0                    # répétition : ne tague rien
RELEASE_APPLY=1 ./scripts/release.sh v0.1.0    # tag annoté, localement
git push origin v0.1.0                         # ← c'est la publication
gh release create v0.1.0 --title "moxy v0.1.0" \
  --notes-file bin/release-notes-v0.1.0.md --generate-notes
```

La répétition vérifie l'arbre de travail, la branche, la présence de `LICENSE`,
la section datée du [`CHANGELOG.md`](CHANGELOG.md), lance `scripts/check.sh`,
puis compile avec la version demandée et confronte `moxyd -version` à ce qu'on
attend. Le push du tag déclenche la publication des images `X.Y.Z`, `X.Y` et
`latest` — et un tag ne se déplace jamais, une version fautive se corrige par la
suivante.

La procédure complète, ce que la CI publie, comment le vérifier et pourquoi la
licence est celle-là : [`docs/RELEASE.md`](docs/RELEASE.md).

## API

### `GET /api/overview`

La route de la vue d'ensemble. Elle renvoie, en un seul document, de quoi
peindre l'écran 4 (vue d'ensemble) intégralement : totaux inter-clusters, carte de
chaque cluster, détail par nœud et alertes. Le backend scrute les clusters en
arrière-plan, la route se contente de servir le dernier état connu — elle ne bloque
donc pas sur un cluster injoignable. Réponse en `Cache-Control: no-store`.

Elle répond `200`, `405` sur une méthode autre que `GET` ou `HEAD` (avec
`Allow: GET, HEAD`), et **`503 {"error":"overview unavailable"}`** quand
l'agrégateur lui-même ne peut rien rendre — un cas que la scrutation
d'arrière-plan rend rare, puisqu'un cluster injoignable est servi avec son
dernier instantané plutôt qu'en erreur. S'y ajoutent le `401` de
l'[authentification](#authentification) et le `421` de la
[vérification du `Host`](#vérification-de-len-tête-host), qui s'appliquent
avant le handler.

**Le schéma est figé** : le frontend de l'étape 3 est construit dessus. Sa
définition de référence est `apps/api/internal/aggregate/model.go`.

Extrait abrégé :

```json
{
  "generatedAt": "2026-09-12T10:00:00Z",
  "thresholds": { "memory": 0.8, "cpu": 0.8, "storage": 0.8 },
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
          "pendingUpdates": 0,
          "pveVersion": "9.2.11"
        }
      ],
      "updates": null,
      "alerts": [
        { "kind": "memory_high", "nodes": ["prox-pprd-2301-cit"], "ratio": 0.892 }
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
  pas encore faite) ; `pendingUpdates: null` de même, par nœud, et
  `pveVersion: null` pour un nœud hors ligne ou qu'aucun `Sys.Audit` ne permet
  d'interroger. Un `0` affirme au
  contraire qu'il n'y a rien en attente. `quorum: null` désigne un nœud seul, sans
  cluster. `color: null` signifie qu'aucune couleur n'est configurée. `cpu: null`
  et `memory: null`, sur un cluster comme sur un nœud, signifient que PVE a listé
  les nœuds **sans leurs mesures** — ce qu'il fait quand le token n'a pas
  `Sys.Audit` sur `/nodes/{node}` — et s'accompagnent d'une alerte
  `node_stats_unavailable` (voir [Privilèges PVE requis](#privilèges-pve-requis)).
  Trois champs suivent la même règle, pour la même raison : `uptime: null` sur
  un nœud (vue d'ensemble et vue nœud) ou un invité signifie qu'il n'y a **pas
  de durée à rapporter** — nœud hors ligne, invité arrêté, ou nœud dont PVE a
  amputé la ligne faute de `Sys.Audit` ; `cpuAverage: null` signifie qu'aucun
  point de la fenêtre RRD n'a été mesuré ; `disk.used: null`, sur un invité,
  signifie qu'aucun agent invité n'a rapporté ce qu'il consomme, la taille
  déclarée (`disk.total`) restant connue. Un `0` à ces trois endroits
  affirmerait respectivement un redémarrage à l'instant, une machine au repos
  et un volume vide.
- **`storage` est la capacité partagée utilisable pour des disques de VM**, pas
  la somme de tout ce que PVE liste. Seuls les stockages `shared` dont le contenu
  admet `images` ou `rootdir` comptent ; les stockages locaux des nœuds (`local`,
  `local-lvm`…) relèvent de la vue nœud et n'y figurent pas, sauf si le cluster
  n'a aucun stockage partagé, auquel cas ils servent de repli. **Les stockages
  adossés à Ceph (`rbd`, `cephfs`) comptent pour un seul backend par cluster
  Ceph**, reconnu à l'espace libre que ses pools rapportent tous à l'identique :
  le total est cet espace plus ce que chaque pool a réellement stocké. Sans cette
  règle, trois pools RBD et quatre montages CephFS sur un Ceph de 37 TiB
  affichaient 262 TiB ; un pool RBD adossé à un **second** Ceph, lui, garde sa
  propre capacité au lieu de disparaître derrière celle du premier. Un stockage
  partagé qui ne rapporte aucune taille (`maxdisk: 0`, cas d'une cible iSCSI
  exposée directement) n'est pas une capacité : il est ignoré, et le repli sur
  les stockages locaux reste possible.
- **`status`** vaut `healthy`, `degraded` ou `unreachable`. Un cluster
  `unreachable` conserve son dernier instantané connu, daté par `fetchedAt` ; le
  frontend peut donc afficher des données vieillies plutôt qu'une carte vide.
- **Erreurs en anglais, avec un `kind` traduisible.** `error` vaut `null` ou
  `{ "kind": "network", "message": "cluster preproduction: /cluster/status: dial: connection refused" }`.
  `kind` ∈ `auth`, `tls`, `timeout`, `network`, `protocol`, accompagné de
  `status` — le code HTTP de la réponse, `null` quand il n'y en a pas eu. C'est
  `status` qui sépare un jeton révoqué (401) d'un privilège manquant (403), que
  `auth` seul ne dit pas et qui appellent deux gestes différents. C'est lui que le
  frontend traduit ; `message` reste en anglais, destiné au diagnostic. Il ne
  nomme **jamais un hôte, une adresse, un port ni un résolveur** — pas plus ici
  que sur les routes de détail : `dial: connection refused` et non
  `dial tcp 10.0.0.3:8006: connect: connection refused`, `dns: no such host` et
  non `lookup pve-03.internal on 169.254.1.1:53`. Le service n'a pas
  d'authentification, ce document est donc à considérer comme public ; la cause
  complète part dans le journal du serveur, qui est le seul endroit où elle a sa
  place. Même
  principe pour `alerts[].kind` (`quorum_lost`, `node_offline`, `node_unknown`,
  `memory_high`, `updates_available`, `updates_uneven`, `versions_uneven`,
  `unreachable`, `node_stats_unavailable`)
  et pour les erreurs HTTP du serveur, de la forme
  `{ "error": "method not allowed" }`. `node_stats_unavailable` et
  `updates_available` sont informatives : elles ne dégradent pas le cluster,
  l'une parle du token de moxy, l'autre d'une nouvelle.
- **`memory_high` porte le ratio des nœuds qu'elle nomme, pas celui du
  cluster.** Avec `nodes` non vide, `ratio` est le **maximum** des ratios de ces
  nœuds ; il n'est le ratio du cluster que lorsque `nodes` est vide — cas d'un
  cluster globalement plein sans qu'aucun nœud pris isolément ne dépasse le
  seuil. Un cluster à 55 % hébergeant un nœud à 92 % annonçait « Mémoire à 55 %
  sur 1 nœud », phrase fausse à propos du seul nœud qu'elle désigne.
- **`node_offline` et `node_unknown` sont deux faits distincts.** Le premier
  désigne les nœuds que `/cluster/status` déclare hors ligne. Le second désigne
  ceux qui n'apparaissent que dans `/cluster/resources` (statut `unknown`) :
  un nœud qui vient de rejoindre le cluster et n'a pas encore été vu par une
  source faisant autorité, ou une ligne résiduelle d'un nœud qui n'existe plus.
  Ni l'un ni l'autre n'est une panne, et les confondre envoyait chercher une
  coupure inexistante. Les deux dégradent le cluster.
- **`updates_uneven` signale des nœuds qui ne sont pas au même niveau de
  paquets**, avec l'amplitude observée dans `pendingMin` et `pendingMax`. Seuls
  les nœuds allumés dont le compte est connu sont comparés — un `pendingUpdates`
  à `null` est écarté, jamais lu comme un zéro — et il en faut au moins deux.
  L'alerte précède `updates_available` dans la liste : la carte les rend
  toutes, dans cet ordre, et un écart se lit donc avant une nouvelle.
- **`versions_uneven` signale des nœuds qui ne tournent pas la même version de
  PVE**, avec la liste des versions distinctes observées dans `versions`, de la
  plus basse à la plus haute. C'est le pendant *installé* de `updates_uneven`, et
  il répond à une question que celle-ci ne voit pas : un nœud mis à jour mais
  jamais redémarré n'annonce plus rien en attente tout en exécutant encore
  l'ancienne version. Même règle de comparaison — seuls les nœuds allumés dont la
  version est connue comptent, un `pveVersion` à `null` est écarté et n'est jamais
  une version de plus, et il en faut au moins deux. Une liste plutôt qu'un couple
  min/max : un nombre de paquets est une quantité, qu'un intervalle décrit ; une
  version ne l'est pas, et ce qu'il faut savoir avant de migrer un invité est
  *combien* de niveaux coexistent. L'alerte ouvre les trois bandeaux de mise à
  jour : ce que les nœuds exécutent se lit avant ce qu'ils ont en attente, qui se
  lit avant la nouvelle qu'une mise à jour existe.
- **La maintenance n'est pas une alerte** : c'est un état choisi, porté par
  `nodes[].status = "maintenance"`.

### Routes de détail

Huit routes **en lecture** servent les écrans d'objet — la vue nœud (écran 2), la
vue VM (écran 1) et le plan de maintenance (écran 3) —, les journaux de tâches,
celui du cluster et celui d'une machine, et l'historique que dessine la carte
d'un cluster. Toutes sont sous `/api/clusters/{cluster}/` :

| Route | Alimente | Rôle |
|---|---|---|
| `GET /api/clusters/{cluster}/rrd?timeframe=hour` | Écran 4, graphe de la carte de cluster | La série temporelle du cluster, **repliée nœud par nœud** : PVE n'a pas de RRD de cluster. |
| `GET /api/clusters/{cluster}/nodes/{node}` | Écran 2, en-tête et cartes de métriques | Un nœud : état, uptime, CPU, mémoire, swap, système de fichiers racine, load average, quorum, état HA, version PVE et kernel, mises à jour en attente, et la liste des invités qu'il héberge. |
| `GET /api/clusters/{cluster}/nodes/{node}/rrd?timeframe=hour` | Écran 2, sparkline CPU | La série temporelle du nœud : un point par échantillon RRD, plus la moyenne CPU de la fenêtre. |
| `GET /api/clusters/{cluster}/guests/{vmid}` | Écran 1, en-tête, cartes de métriques et tableau « Disques » | Un invité (VM ou conteneur) : nœud hôte, état, uptime, CPU, mémoire, volumétrie allouée et liste de ses volumes, disque de boot, mémoire côté hyperviseur, tags, état HA, adresse IPv4. |
| `GET /api/clusters/{cluster}/guests/{vmid}/rrd?timeframe=hour` | Écran 1, sparkline CPU | La même série temporelle, pour un invité. |
| `GET /api/clusters/{cluster}/guests/{vmid}/tasks?limit=50` | Écran 1, tableau « Tâches récentes » | Les dernières tâches **de cet invité**, lues sur le nœud qui l'héberge. |
| `GET /api/clusters/{cluster}/tasks?limit=50` | Journal du cluster, tableau « Tâches récentes » | Les dernières tâches du cluster, avec leur **durée déjà calculée**. |
| `GET /api/clusters/{cluster}/nodes/{node}/maintenance/plan` | Écran 3, modale de confirmation | Ce que drainer ce nœud impliquerait : quelle machine irait où, et si les nœuds restants ont la place. **Strictement en lecture seule.** |

Les deux journaux ne sont pas le même document lu deux fois. `/cluster/tasks`
n'accepte aucun paramètre côté PVE : filtrer sur une machine reviendrait à
demander la queue du journal du cluster puis à en écarter ce qui ne la concerne
pas — et sur un cluster qui sauvegarde quatre-vingt-dix invités la nuit, les
lignes d'une machine donnée sont sorties de cette queue depuis longtemps. La
route par nœud, elle, accepte `vmid` : elle est interrogée pour exactement ce
qui est affiché, au prix d'un appel au nœud hôte, que la vue cluster nomme déjà.

**Le compromis, à connaître :** un invité qui a migré laisse ses tâches
anciennes sur le nœud qu'il a quitté, que cette route ne lit pas — son
historique commence là où il est arrivé. Un historique incomplet vaut mieux
qu'une liste vide, qui était le comportement précédent. L'interface nomme le
nœud d'où vient le journal, pour que cette coupure soit lisible.

S'y ajoute la seule route en **écriture** du service : `POST` sur
`/api/clusters/{cluster}/nodes/{node}/maintenance`, qui exécute le drain au lieu
de le décrire, par SSH et non par l'API PVE
([ADR 0010](docs/adr/0010-node-maintenance-over-ssh.md)). Elle répond `404` sur un
cluster sans bloc `maintenance` — jamais un bouton désactivé — et reste sans effet
tant que le transport SSH n'est pas livré : voir
[Mise en maintenance d'un nœud](#mise-en-maintenance-dun-nœud).

Leur définition de référence est `apps/api/internal/detail/model.go`, commenté
champ par champ, miroir de `apps/web/src/api/types.ts`.

#### Paramètres

| Paramètre | Où | Valeurs acceptées |
|---|---|---|
| `cluster` | chemin | L'`id` d'un cluster de la configuration (`[a-z0-9-]+`). Inconnu → 404. |
| `node` | chemin | Le nom d'un nœud du cluster, tel que la vue d'ensemble le nomme. Inconnu → 404. |
| `vmid` | chemin | L'identifiant numérique de l'invité, dans la plage que PVE s'autorise : de `100` à `999999999`. L'écriture doit être **canonique** — `+101` et `0101` sont refusés, faute de quoi un même invité aurait plusieurs URL, donc plusieurs entrées de cache et plusieurs formes dans le journal. Hors plage ou mal écrit → 400 ; absent du cluster → 404. |
| `timeframe` | requête | `hour`, `day`, `week`, `month` ou `year`. Absent → `hour`. Toute autre valeur → 400. |
| `limit` | requête | Entier strictement positif, nombre maximal de tâches renvoyées. Absent → `50`. Valeur non numérique, nulle ou négative → 400. **Au-delà de `500`, la valeur est ramenée à `500`** plutôt que refusée : une demande large mais bien formée continue de fonctionner, ce qu'un 400 ne ferait pas. |

#### La vue d'ensemble est scrutée, le détail est à la demande

C'est la distinction structurante entre `/api/overview` et ces routes, et
elle explique tout le reste.

Un scrutateur d'arrière-plan rafraîchit la vue d'ensemble toutes les 5 s, pour
tous les clusters : c'est le seul document que l'interface affiche en
permanence. Interroger sur le même rythme six nœuds et cent cinquante invités
coûterait bien plus que ça ne vaut, et personne ne regarde plus d'un objet à la
fois. Les routes de détail interrogent donc PVE **à la demande**, au moment où
quelqu'un ouvre l'écran.

À la demande ne veut pas dire à chaque requête : un cache court absorbe les
répétitions qu'un rafraîchissement d'UI toutes les 5 s produit, et un verrou
anti-troupeau les regroupe — dix onglets ouverts sur le même nœud ne déclenchent
qu'un seul appel amont. Chaque réponse porte son `fetchedAt`, qui date
l'instantané servi.

#### Détail d'un nœud ou d'un invité

Extrait abrégé, pour un nœud :

```json
{
  "cluster": "qualification",
  "name": "prox-qual-2201-cit",
  "status": "online",
  "uptime": 3542400,
  "fetchedAt": "2026-09-12T10:00:00Z",
  "pveVersion": "9.2.11",
  "kernelVersion": "6.14.11-4-pve",
  "cpu": { "ratio": 0.42, "cores": 32 },
  "memory": { "used": 76000000000, "total": 91625968981, "ratio": 0.83 },
  "swap": { "used": 0, "total": 8589934592, "ratio": 0 },
  "rootfs": { "used": 12884901888, "total": 100000000000, "ratio": 0.129 },
  "loadAverage": [0.84, 0.91, 1.02],
  "quorum": { "quorate": true, "nodes": 3, "online": 3 },
  "haState": "online",
  "pendingUpdates": 2,
  "updates": [
    {
      "package": "pve-manager",
      "title": "Proxmox Virtual Environment Management Tools",
      "oldVersion": "9.2.11",
      "version": "9.2.12"
    },
    {
      "package": "systemd",
      "title": "system and service manager",
      "oldVersion": "257.3-1",
      "version": "257.4-1"
    }
  ],
  "guests": [
    {
      "vmid": 103,
      "name": "airflow-sep-exp",
      "kind": "qemu",
      "status": "running",
      "cpu": { "ratio": 0.06, "cores": 4 },
      "memory": { "used": 6871947674, "total": 8589934592, "ratio": 0.8 },
      "tags": ["prod"]
    }
  ]
}
```

Le payload d'un invité suit les mêmes conventions, avec ce qui lui est propre :
`node` (le nœud qui l'héberge aujourd'hui, et qui change à la migration),
`kind` (`qemu` ou `lxc`), `disk` (le disque de boot **seul**), `disks` et
`allocated` (tout ce qu'il alloue, voir plus bas), `hostMemory` (ce que
l'hyperviseur dépense pour lui, supérieur à ce que l'invité voit lui-même),
`tags`, `haState` et `ipv4`.

`disks` liste un volume par ligne, tel que la configuration de l'invité le
déclare — `scsi0`, `rootfs`, `mp0`, ou `unused0` pour un volume détaché — avec
son stockage, son identifiant et sa taille en octets. `maxdisk`, que PVE remonte
et que `disk` reprend, n'est **pas** la volumétrie d'un invité : c'est le disque
de boot d'une VM, le `rootfs` d'un conteneur, et rien d'autre. Une VM portant un
disque système de 32 Gio et un disque de données de 2 Tio y apparaît à 32 Gio.
`allocated` donne le total : `bytes` somme les volumes **attachés** dont la
taille est connue, `partial` signale qu'au moins l'un d'eux n'en déclare aucune
— le total est alors un plancher —, et `detached` compte ce qu'un détachement a
laissé derrière lui, qui occupe toujours son stockage sans appartenir à
l'invité.

Les deux champs valent `null` ensemble quand la configuration n'a pas pu être
lue : sans `VM.Audit` sur l'invité, PVE répond 403. C'est un appel facultatif —
la liste manque, jamais la page.

Les conventions du payload de la vue d'ensemble s'appliquent telles quelles :
tailles en octets, ratios en fractions `0..1`, statuts repris du même
vocabulaire (`online`, `offline`, `maintenance`, `unknown` pour un nœud ;
`running`, `stopped`, `template` pour un invité). Les deux vues ne doivent
jamais diverger sur l'état d'un même objet.

`updates` liste les paquets en attente que `pendingUpdates` compte : les deux
champs sortent de la même réponse et disent la même chose, `null` ensemble quand
le token n'a pas le droit de poser la question, et un tableau vide quand le nœud
est à jour. `oldVersion` est `null` pour un paquet qu'apt installerait pour la
première fois. Un compte seul ne dit pas s'il faut poser une fenêtre de
maintenance : un noyau et une page de manuel valent tous les deux « 1 en attente ».

Deux valeurs demandent une lecture prudente : `disk.used` d'un invité est
souvent `null`, parce que Proxmox ne sait ce qu'un invité consomme réellement
que si l'agent le lui dit — la taille déclarée (`disk.total`) reste connue, et
`disk.ratio` est `null` avec `used`, un taux de remplissage calculé sur un
inconnu n'étant qu'une supposition tracée en barre ; et `loadAverage` porte les
chiffres à 1, 5 et 15 minutes, dans cet ordre.

#### Séries RRD

Un graphe de supervision se dessine côté frontend. Le backend livre des
**points, pas une image** :

```json
{
  "cluster": "qualification",
  "timeframe": "hour",
  "fetchedAt": "2026-09-12T10:00:00Z",
  "cpuAverage": 0.061,
  "points": [
    { "time": "2026-09-12T09:00:00Z", "cpu": 0.058, "memUsed": 6871947674, "memTotal": 8589934592, "netIn": 148234, "netOut": 91002 },
    { "time": "2026-09-12T09:01:00Z", "cpu": null,  "memUsed": null,       "memTotal": null,       "netIn": null,   "netOut": null }
  ]
}
```

- **Les fractions sont brutes, l'échelle appartient au frontend.** Le §2 du
  document de passation est explicite : la sparkline CPU est à hauteur fixe
  (70 px) et n'est **jamais** auto-échelonnée, parce que l'auto-échelle
  transforme un 0,6 % en pic spectaculaire. Le backend ne décide donc pas de
  l'échelle ; il sert `cpu` en fraction `0..1`, comme partout ailleurs, et
  `cpuAverage` pour le libellé de moyenne affiché à côté du graphe —
  `null` quand aucun point de la fenêtre n'a été mesuré, la moyenne d'une
  fenêtre vide n'étant pas zéro mais rien.
- **Un trou vaut `null`, pas `0`.** RRD renvoie des lacunes — un nœud redémarré,
  une consolidation pas encore faite. Dessiner une lacune comme un zéro
  inventerait une chute qui n'a jamais eu lieu. Le point existe, ses valeurs sont
  nulles, et le frontend interrompt la courbe.
- `timeframe` est renvoyé dans la réponse, pour qu'un rendu tardif sache quelle
  fenêtre il tient.
- **La série d'un cluster est repliée, pas lue.** PVE n'a pas de RRD de cluster :
  `GET /api/clusters/{cluster}/rrd` lit celui de chaque nœud en ligne et les
  additionne. Le CPU est une moyenne **pondérée par les cœurs** — la règle de la
  vue d'ensemble —, la mémoire une somme, et les points sont appariés **sur leur
  horodatage**, jamais sur leur indice : un nœud entré en cours d'heure a moins
  de points que ses voisins. Un nœud qui n'a pas répondu perd sa part de courbe
  sans faire échouer la requête ; l'erreur n'est propagée que si aucun nœud n'a
  répondu. Les lectures par nœud passent par la même entrée de cache que la vue
  nœud : une carte et un onglet ouverts sur le même nœud ne coûtent qu'un appel.
  `netIn` et `netOut` restent `null` sur cette série, le trafic d'un cluster
  n'étant pas la somme de celui de ses nœuds.

#### Tâches

```json
{
  "cluster": "qualification",
  "fetchedAt": "2026-09-12T10:00:00Z",
  "entries": [
    {
      "upid": "UPID:prox-qual-2201-cit:0011A2B3:0F4E12:68C3A1D0:qmigrate:103:moxy@pve!ro:",
      "node": "prox-qual-2201-cit",
      "type": "qmigrate",
      "id": "103",
      "user": "moxy@pve!ro",
      "start": "2026-09-12T09:41:12Z",
      "end": "2026-09-12T09:42:04Z",
      "duration": 52,
      "status": "OK",
      "outcome": "ok",
      "warnings": null
    },
    {
      "upid": "UPID:prox-qual-2202-cit:0011A2C4:0F4E20:68C3A2E0:vzdump:105:root@pam:",
      "node": "prox-qual-2202-cit",
      "type": "vzdump",
      "id": "105",
      "user": "root@pam",
      "start": "2026-09-12T09:10:00Z",
      "end": "2026-09-12T09:11:00Z",
      "duration": 60,
      "status": "WARNINGS: 2",
      "outcome": "warnings",
      "warnings": 2
    }
  ]
}
```

- **La durée est calculée côté backend**, en secondes. C'est le défaut exact de
  l'interface native que le §2 corrige : elle affiche un début et une fin, et
  laisse l'opérateur soustraire deux horodatages de tête. Le tableau des tâches
  affiche une durée, il ne la fabrique pas.
- **`outcome` porte le verdict, et il y en a quatre** : `running`, `ok`,
  `warnings`, `failed`. Un booléen n'en portait que deux, et PVE termine une
  tâche qui a émis des avertissements — un `vzdump` typiquement — avec le
  statut `WARNINGS: <n>` (pve-common, `RESTEnvironment::fork_worker`), que son
  interface native rend en ambre et non en rouge. Tout traiter comme un échec
  dès que le statut n'était pas `OK` transformait chaque sauvegarde nocturne
  ayant averti sur un invité en ligne rouge : une fausse alerte quotidienne,
  précisément sur la tâche la plus surveillée. `warnings` porte le nombre
  extrait, `null` quand il n'est pas lisible — l'`outcome` ne dépend pas de
  lui.
- Une tâche en cours a `end` et `duration` à `null`, `status` à `running` et
  `outcome` à `running`. Une tâche terminée garde dans `status` la chaîne brute
  de PVE — matière à infobulle, jamais à décision : c'est `outcome` qui décide.
  Une tâche terminée **sans statut du tout** vaut `failed` avec
  `status: "unknown"` : un verdict que personne n'a énoncé n'est pas un succès
  silencieux.

#### Plan de mise en maintenance

`GET /api/clusters/{cluster}/nodes/{node}/maintenance/plan` répond à la question
que la modale de l'écran 3 doit poser **avant** le clic : qu'est-ce qui bouge, où,
et les nœuds restants ont-ils la place ? Elle ne prend aucun paramètre et
**ne change rien** : calculer un plan est une lecture.

```json
{
  "cluster": "preproduction",
  "node": "prox-pprd-2301-cit",
  "fetchedAt": "2026-09-12T10:00:00Z",
  "threshold": 0.8,
  "feasible": true,
  "moves": [
    {
      "vmid": 103, "name": "airflow-sep-exp", "kind": "qemu", "status": "running",
      "memory": 8589934592, "method": "online", "ha": true,
      "target": "prox-pprd-2302-cit", "placed": true
    }
  ],
  "staying": [ { "vmid": 900, "name": "debian-13-tmpl", "reason": "template" } ],
  "targets": [
    {
      "name": "prox-pprd-2302-cit", "measured": true,
      "before": { "used": 46000000000, "total": 91625968981, "ratio": 0.502 },
      "after":  { "used": 54589934592, "total": 91625968981, "ratio": 0.596 },
      "incoming": 1, "exceeds": false
    }
  ],
  "blockers": []
}
```

- **`threshold` est le seuil de la configuration**, celui-là même sur lequel la
  vue d'ensemble lève `memory_high`. Deux réponses à « qu'est-ce qui est plein »
  feraient avertir les cartes à un chiffre et refuser les placements à un autre.
- **`method` annonce l'interruption** : `online` pour une VM QEMU en marche, qui
  migre à chaud ; `restart` pour un conteneur en marche, que PVE arrête, déplace
  et redémarre — Proxmox ne sait pas migrer un LXC à chaud, et la modale doit le
  dire avant le clic ; `offline` pour un invité à l'arrêt.
- **`ha` sépare ce qui se fera tout seul de ce qu'il faudra faire à la main.**
  `true` quand le CRM déplacera l'invité de lui-même au drainage, `false` quand
  il le connaît mais l'a désactivé ou ignoré, `null` quand le cluster ne fait
  tourner aucun gestionnaire HA — auquel cas rien ne bouge de soi-même.
- **`memory` est le chiffre qu'a utilisé le contrôle de capacité** : le maximum
  configuré de l'invité tant qu'il tourne, et zéro une fois arrêté, un invité à
  l'arrêt ne réservant rien sur sa destination avant d'être démarré.
- **`exceeds` est informatif et ne décide pas de `feasible`.** Le placement
  refuse déjà d'envoyer un invité sur un nœud qui passerait le seuil ; un nœud
  marqué ici est un nœud déjà plein avant ce plan, et qui ne reçoit rien. Le
  compter comme un empêchement refuserait de drainer un nœud qui n'a rien à
  déplacer.
- **`blockers` porte des clés stables, pas des phrases** : `source_offline` (le
  nœud est déjà hors ligne, ses machines n'y tournent pas), `no_target` (aucun
  autre nœud en ligne pour recevoir) et `target_stats_unavailable` (les nœuds de
  destination sont listés sans leurs mesures, faute de `Sys.Audit` sur
  `/nodes` : sans cette clé, le plan ressemblait exactement à un cluster plein).
  La traduction est au frontend. `feasible` est vrai quand tout invité a trouvé
  une place **et** que `blockers` est vide.

**Limites connues, et elles comptent.** Le plan place à la **mémoire seule**, le
plus gros d'abord, sur le nœud qui resterait le moins chargé sous le seuil. Le
§4 du document de passation demande « la même logique que le CRM » ; ce n'est
pas ce qui est fait, et ce n'est pas faisable depuis `/cluster/resources`, qui
ne porte rien de tout cela :

| Non pris en compte | Conséquence |
|---|---|
| Groupes HA et contraintes de nœud | Le plan peut proposer une destination que le CRM n'aurait pas choisie. |
| `nofailback` et priorités | L'ordre réel de replacement peut différer. |
| Disques locaux, périphériques passés | Un invité qui ne peut pas migrer est compté comme déplaçable. |
| Invités verrouillés (`lock`), sauvegarde en cours | Idem : rien ne les distingue dans la liste. |
| CPU, stockage, réseau | Seule la mémoire entre dans le contrôle de capacité. |

C'est un **plan, pas une garantie** : il dit ce qu'il faudrait de place et ce
qui bougerait, pas ce que le CRM fera exactement. Cette route reste en lecture
seule ; l'exécution est une route distincte, passant par SSH et non par l'API
PVE, décidée par l'[ADR 0010](docs/adr/0010-node-maintenance-over-ssh.md) et pas
encore livrée — voir [Mise en maintenance d'un nœud](#mise-en-maintenance-dun-nœud).

#### Champs facultatifs et dégradation

**Un champ facultatif vaut `null` quand l'information est indisponible, et cela
ne fait jamais échouer la réponse.** `ipv4` est `null` sans agent invité,
`haState` `null` sur un cluster sans gestionnaire HA, `pendingUpdates` `null`
quand le token n'a pas `Sys.Modify`, `quorum` `null` pour un nœud seul,
`loadAverage` `null` quand le nœud ne le rapporte pas. Le reste de la réponse
est servi normalement.

C'est la philosophie de la vue d'ensemble, appliquée à l'objet : mieux vaut
servir le dernier état connu, ou un état partiel honnêtement troué, qu'une page
vide. Comme ailleurs dans moxy, **`null` signifie « inconnu », pas « zéro »** —
l'UI rend alors le tiret cadratin `—`.

#### Codes d'erreur

| Code | Quand |
|---|---|
| `400` | Paramètre invalide : `vmid` non numérique, `timeframe` hors de la liste, `limit` non entier ou nul. Le service de détail répond de même (`ErrInvalidArgument`) pour un appelant qui l'atteindrait sans passer par la validation de la couche HTTP : une fenêtre inconnue n'est pas un objet manquant. |
| `401` | Aucune identité, ou une identité venue d'ailleurs que d'un proxy de confiance — voir [Authentification](#authentification). |
| `403` | PVE a refusé la requête : il manque un privilège au token. La cause exacte est dans le journal. |
| `404` | Cluster, nœud ou invité inconnu. Aussi : un chemin qui n'a la forme d'aucune route, et un chemin d'`/api/` que `ServeMux` réécrirait (`.`, `..`, double barre), refusé plutôt que redirigé — une API n'a pas à rediriger, et une règle d'autorisation future ne doit jamais s'évaluer sur un chemin différent de celui qui a été envoyé. |
| `405` | Méthode autre que `GET` ou `HEAD`. La réponse porte `Allow: GET, HEAD`. |
| `421` | La requête n'est pas adressée à moxy : en-tête `Host` non reconnu. Le contrôle s'applique à toutes les routes sauf `/healthz`. |
| `502` | PVE injoignable : erreur réseau ou TLS, réponse amont illisible. |
| `504` | PVE a mis trop de temps à répondre. Distinct du `502` : le cluster est là, il est lent. |

Les messages gardent la forme `{ "error": "invalid timeframe" }` du reste de
l'API : **en anglais, et volontairement laconiques**. Le détail — hôte contacté,
chemin PVE, cause exacte — part dans le journal du serveur, jamais dans la
réponse : il peut nommer des hôtes internes, et le client n'en a pas l'usage.
C'est la même règle que pour le `message` de `/api/overview`, et elle vaut pour
les deux familles de routes. La traduction vers l'utilisateur reste la
responsabilité du frontend.

### `GET /healthz`

Sonde de vivacité du démon lui-même, indépendante de l'état des clusters :

```sh
curl -s http://127.0.0.1:8080/healthz
# {"status":"ok","version":"v0.3.1"}
```

`version` est l'identifiant de build, posé au lien
(`-X …/internal/server.Version`, alimenté par `scripts/build.sh`) et valant
`dev` à défaut. C'est tout ce que la sonde divulgue : aucun nom de cluster,
aucun nom d'hôte. Elle
répond à `GET` comme à `HEAD` — c'est la méthode qu'emploient plusieurs
équilibreurs de charge, `httpchk` d'HAProxy en tête — et jamais depuis un cache
(`Cache-Control: no-store`). `/healthz/`, avec la barre en trop, est un 404 JSON
explicite : sans cela une sonde mal écrite recevrait la page du frontend, et
donc un 200, tant que le bundle reste lisible.

### `GET /readyz`

Sonde de **disponibilité**, à ne pas confondre avec la précédente. Elle répond
`503 {"error":"warming up"}` tant que le scrutateur n'a pas achevé un premier
tour sur chaque cluster, puis `200 {"status":"ready","version":"v0.3.1"}` —
même document que `/healthz`, à l'état près. En mode mock, il n'y a
rien à réchauffer : elle répond 200 d'emblée. Mêmes méthodes et même
`Cache-Control` que `/healthz`, et le même 404 explicite sur `/readyz/`.

La distinction est ce qui évite le pire scénario d'exploitation : une sonde de
vivacité branchée sur l'état des clusters redémarre un démon parfaitement sain
parce qu'un cluster répond lentement, et le redémarrage relance l'attente.
`/healthz` dit que le processus répond, `/readyz` dit qu'il a quelque chose à
servir ; c'est la seconde qui doit conditionner l'envoi de trafic.

Le port s'ouvre **avant** le premier tour de scrutation. `GET /api/overview`
attend malgré tout ce premier tour : une requête arrivée pendant l'échauffement
patiente et reçoit de vraies données, jamais un document sans cluster.

### `GET /metrics`

Exposition Prometheus, **soumise à l'authentification** comme le reste de
l'API et contrairement aux deux sondes ci-dessus : elle nomme chaque cluster
configuré. Voir [Observabilité](#observabilité).

### `POST /api/login`

**N'existe qu'en mode `auth.mode: "token"`** ; ailleurs, la route n'est pas une
route. Elle prend `{"token": "…"}` en `application/json` et répond `204` avec le
cookie qui portera le jeton ensuite, ou `401` sans rien dire de plus. Un corps
de formulaire vaut `415`, une autre méthode `405`, un corps illisible ou
au-delà de 4 Kio `400`. Voir
[Mode `token`](#mode-token--un-jeton-partagé-pour-un-poste-isolé).

### Interroger l'API en ligne de commande

Tout est en `GET`, tout est du JSON, et rien ne demande d'en-tête particulier
tant que `auth.mode` vaut `none`. De quoi vérifier une instance sans ouvrir un
navigateur :

```sh
BASE=http://127.0.0.1:8080

curl -s $BASE/healthz                      # le démon répond-il ?
curl -s $BASE/readyz                       # a-t-il quelque chose à servir ?
curl -s $BASE/api/overview | python3 -m json.tool | head -40

# le verdict de chaque cluster, en une ligne chacun
curl -s $BASE/api/overview \
  | python3 -c 'import json,sys; [print(c["id"], c["status"], c["error"]) for c in json.load(sys.stdin)["clusters"]]'

# un nœud, sa dernière heure, et le journal du cluster
curl -s $BASE/api/clusters/qualification/nodes/prox-qual-2201-cit
curl -s "$BASE/api/clusters/qualification/nodes/prox-qual-2201-cit/rrd?timeframe=day"
curl -s "$BASE/api/clusters/qualification/tasks?limit=10"

# ce que drainer ce nœud impliquerait — lecture seule
curl -s $BASE/api/clusters/qualification/nodes/prox-qual-2201-cit/maintenance/plan

curl -s $BASE/metrics | grep -v '^#' | head    # soumis à l'authentification
```

En mode `proxy-header`, ajouter l'en-tête d'identité et appeler depuis une
adresse listée dans `trustedProxies` :

```sh
curl -s -H 'X-Forwarded-User: alice' $BASE/api/overview
```

Un nom de nœud qui contient un caractère à échapper se passe encodé
(`%2F` pour une barre oblique) : chaque segment est décodé séparément, de sorte
qu'il reste un seul segment.

### Origine unique, pas de CORS

Le serveur de développement du frontend proxie `/api` vers `moxyd`. Il n'y a
**délibérément pas de CORS côté backend** : navigateur et API sont servis sous la
même origine, et n'ajouter aucun en-tête permissif évite d'ouvrir une surface
inutile sur un service qui détient des tokens d'hyperviseur.

### Vérification de l'en-tête `Host`

L'absence de CORS protège les *autres* origines de moxy ; elle ne protège pas
moxy d'une origine étrangère. Le scénario est le **rebinding DNS** : une page
malveillante que l'opérateur visite fait pointer `attacker.example` vers
`127.0.0.1` après son premier chargement, puis lit `/api/overview` depuis sa
propre origine. Le navigateur émet alors la requête avec
`Host: attacker.example`, et il n'y a ni CORS à franchir ni identifiant à
produire, puisqu'il n'y en a pas.

`moxyd` refuse donc toute requête dont l'en-tête `Host` ne le désigne pas, avec
un `421 Misdirected Request` :

```sh
curl -s -H 'Host: attacker.example' http://127.0.0.1:8080/api/overview
# {"error":"misdirected request"}
```

Sont acceptés `localhost`, `127.0.0.1`, `::1`, l'hôte de `-addr` quand il n'est
pas générique, et tout ce que déclare `-allowed-hosts` (ou `MOXY_ALLOWED_HOSTS`),
liste séparée par des virgules. La casse, le port et un point final sont
ignorés.

**Ce n'est pas une authentification** : ce contrôle n'identifie personne. Il
empêche seulement une origine étrangère de parler à moxy *à travers le
navigateur de l'opérateur*. C'est la seule mitigation disponible tant que moxy
n'authentifie pas ses appelants.

**Derrière un reverse proxy.** Un proxy qui réécrit `Host` en `127.0.0.1:8080`
passe sans configuration. Un proxy qui préserve le `Host` public — c'est le cas
de l'exemple de déploiement — exige que ce nom soit déclaré :

```sh
moxyd -addr 127.0.0.1:8080 -allowed-hosts moxy.interne.example
```

**Écoute générique.** Avec `-addr 0.0.0.0:8080` — ce que fait l'image de
conteneur — et sans liste, moxy ne peut pas connaître le nom par lequel on
l'atteint. Le contrôle est alors **inactif**, et une ligne le dit au démarrage :

```
warning: listening on 0.0.0.0:8080 with no -allowed-hosts, so the Host header is
not checked; set -allowed-hosts (or MOXY_ALLOWED_HOSTS) to the name moxy is
reached by, or listen on a fixed address
```

**`/healthz` est exempté**, délibérément. Une sonde de vivacité est le seul
appelant dont l'opérateur ne maîtrise pas le `Host` — kubelet envoie l'IP du
pod, `httpchk` de HAProxy ce qu'on lui a configuré, certaines n'en envoient
aucun — et un `421` y transformerait un démon en bonne santé en démon en échec.
Ce qu'on y concède est mince : `/healthz` rend un état et un identifiant de
build, rien d'un cluster. Toutes les routes qui décrivent l'infrastructure sont
contrôlées, `/healthz/` compris.

### En-têtes de sécurité

Toute réponse JSON porte `X-Content-Type-Options: nosniff` : une seule règle
pour tout le serveur se tient mieux qu'une règle avec une exception.

Avec `-web`, la page elle-même est servie avec une politique complète, puisqu'il
s'agit d'une interface d'administration **sans authentification** :

| En-tête | Valeur |
|---|---|
| `Content-Security-Policy` | `default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'` |
| `Referrer-Policy` | `no-referrer` |
| `X-Frame-Options` | `DENY` |
| `Permissions-Policy` | `camera=(), microphone=(), geolocation=()` |

`frame-ancestors 'none'` est celui qui compte le plus : sans lui, l'interface
s'embarque dans l'iframe d'un tiers et un clic atterrit où cette page l'a décidé.
`'unsafe-inline'` n'apparaît que pour `style-src`, parce que le bundle pose des
attributs `style` calculés (`Sparkline`, `UsageBar`) ; il n'a pas d'équivalent
côté scripts, le script d'amorçage — celui qui pose le thème et la langue avant
la première peinture — ayant été sorti d'`index.html` vers `public/boot.js` pour
cette raison exacte. Le mode API seule ne sert aucune
page et ne pose donc aucun de ces en-têtes, `nosniff` excepté.

## Journalisation

`moxyd` écrit sur **la sortie d'erreur**, avec le paquet `log` de la
bibliothèque standard et ses réglages par défaut : une ligne par événement,
préfixée de la date et de l'heure locales. **Il n'y a ni niveaux, ni format
structuré, ni fichier de journal** — c'est `journald`, le moteur de conteneurs
ou le superviseur qui horodate, range et fait tourner. `log/slog` demanderait Go
1.21, au-dessus du niveau de langage que `go.mod` fixe.

```
2026/09/12 10:00:00 moxyd v0.3.1 starting (go1.27.0, linux/amd64)
2026/09/12 10:00:00 cluster "qualification": 2s to connect, 4s per call, 10s per poll round
2026/09/12 10:00:00 warning: cluster "lab" runs with TLS verification disabled
2026/09/12 10:00:00 moxyd listening on 127.0.0.1:8080
2026/09/12 10:03:17 poll failed (auth): cluster preproduction: /cluster/status: 401 authentication failure
```

Ce qu'il faut en savoir :

- **La première ligne est l'identité du binaire** — version de moxy, toolchain
  Go, plateforme — et elle est émise avant toute validation, donc elle est là
  même quand le démarrage échoue ensuite.
- **Le mot `warning:` est la seule convention de sévérité.** Il préfixe ce qui
  mérite un regard sans empêcher de servir : écoute au-delà de loopback sans
  `auth`, `Host` non vérifié, TLS désactivé sur un cluster, budget de tour plus
  long que le délai de péremption, variable de proxy posée mais ignorée. Tout le
  reste est informatif, et un échec fatal passe par `log.Fatalf`, qui sort en 1.
- **Une panne de cluster n'est journalisée qu'au changement.** Le scrutateur
  compare la cause à la précédente et ne réécrit la ligne que si elle diffère :
  sans cela un cluster éteint produirait une ligne toutes les cinq secondes,
  soit dix-sept mille par jour, et noierait tout le reste.
- **Le journal est le seul endroit qui porte la cause complète.** Les réponses
  HTTP restent laconiques et ne nomment ni hôte, ni adresse, ni port ; la ligne
  de journal, elle, garde la version non expurgée de l'erreur. C'est délibéré :
  le document servi est à considérer comme public, le journal non.
- **Un secret n'y apparaît jamais.** Les en-têtes de requête ne sont pas
  journalisés, un `Secret` se rend en `***`, et le `tokenId` — qui est un nom,
  pas une clé — est la seule moitié du token qui puisse apparaître.

## Observabilité

`GET /metrics` sert l'exposition texte de Prometheus. Le démon qui rend les
clusters observables n'avait aucun chiffre le concernant : « pourquoi la carte
de préproduction est-elle passée injoignable à 3 h 12 ? » et « combien d'appels
moxy envoie-t-il à mon cluster ? » n'avaient de réponse que dans le journal, et
seulement si quelqu'un le lisait à ce moment-là.

Le format est écrit à la main, en bibliothèque standard : il tient en une
fonction, et un démon qui détient des tokens d'hyperviseur ne prend pas une
dépendance pour une métrique.

| Métrique | Type | Étiquettes | Ce qu'elle dit |
|---|---|---|---|
| `moxy_pve_requests_total` | compteur | `cluster`, `path_kind`, `outcome` | Les appels envoyés à un cluster, par famille d'endpoint et par issue. |
| `moxy_pve_request_seconds` | histogramme | `cluster`, `path_kind` | Leur durée. Les seaux encadrent le délai par appel : ce qu'on veut savoir, c'est si un cluster répond en millisecondes ou s'il rampe vers son budget. |
| `moxy_poll_last_success_timestamp_seconds` | jauge | `cluster` | Quand un cluster a répondu à un tour de scrutation complet pour la dernière fois. **C'est la métrique du « à 3 h 12 »** : elle cesse d'avancer à l'instant où le cluster a cessé de répondre. |
| `moxy_cluster_status` | jauge | `cluster`, `status` | Le verdict de la carte, une série par statut valant 0 ou 1. Trois séries plutôt qu'un statut encodé en nombre : « combien de clusters sont dégradés » est une somme sur une étiquette, pas une comparaison. |
| `moxy_detail_cache_events_total` | compteur | `cache`, `event` | Ce que les caches à la demande ont fait d'une requête : `hit`, `miss`, ou `join` — un appelant qui a attendu un appel déjà en vol. Le verrou anti-troupeau y est visible, et nulle part ailleurs. |
| `moxy_maintenance_commands_total` | compteur | `cluster`, `action`, `outcome` | Les mises en maintenance exécutées par SSH, par verbe et par issue. C'est la seule trace d'une action qui ne laisse aucune tâche PVE derrière elle. **Jamais le nom du nœud** : il est dans le journal d'audit, pas dans un document scruté. |
| `moxy_maintenance_command_seconds` | histogramme | `cluster`, `action`, `outcome` | Leur durée, de bout en bout, obtention de la clé comprise. Les seaux encadrent les budgets SSH de la configuration. L'issue est une étiquette ici, contrairement à l'histogramme PVE : un dépassement de délai tombe dans le dernier seau et une commande refusée dans le premier, et les moyenner masquerait les deux. |
| `moxy_maintenance_keysource_total` | compteur | `mode`, `outcome` | Les obtentions de la clé qui ouvre une session : un fichier lu, ou un certificat signé par OpenBao. Elle sépare « la source de la clé est en panne » de « le nœud est en panne » — une seule panne vue du siège de l'opérateur, deux à réparer. Pas d'étiquette `cluster` : la source est à portée processus. **L'adresse d'OpenBao n'apparaît nulle part**, pas même dans le texte d'aide. |
| `moxy_build_info` | jauge | `version` | Toujours 1 ; sert à annoter un déploiement sur un tableau de bord. |

`outcome` reprend le vocabulaire des erreurs de l'API — `ok`, `auth`, `tls`,
`timeout`, `network`, `protocol` — pour qu'un opérateur qui lit `/metrics` et
un opérateur qui lit le journal regardent les mêmes mots. Sur les trois séries
de maintenance, c'est l'autre ensemble fermé qui sert, celui des `kind` renvoyés
par l'API — `ok`, `no_quorum`, `ssh_unreachable`, `command_failed`… —, pour la
même raison.

**Un échantillon par appel logique, décodage compris.** La mesure est prise au
point de passage unique d'un appel, qui englobe la bascule d'une URL vers la
suivante *et* l'analyse de la réponse : un cluster qui répond `200` avec un
corps qui ne s'analyse pas — un portail captif, un proxy inverse mal configuré
devant `pveproxy` — compte donc `outcome="protocol"`, jamais `outcome="ok"`.
C'est le seul échec que le transport seul ne peut pas voir.

**Aucune cardinalité libre.** Chaque valeur d'étiquette vient d'un ensemble
fermé : un identifiant de cluster venu de la configuration, l'une des onze
familles d'endpoint, l'une des six issues — et, côté maintenance, l'un des deux
verbes, l'un des deux modes de fourniture de clé, l'un des `kind` d'erreur.
**Jamais un nom de nœud, jamais un
`vmid`, jamais une URL.** Ce n'est pas seulement un choix de cardinalité — une
étiquette prenant un nom de nœud ferait croître une série temporelle par objet
de chaque cluster, ce qui met un Prometheus à genoux — c'est aussi la règle qui
tient les noms d'hôte hors d'un document qui sort du processus, la même que
pour les messages d'erreur.

**`/metrics` est soumis à l'authentification**, contrairement à `/healthz` et
`/readyz`. L'exposition nomme chaque cluster configuré et dit quand chacun a
répondu pour la dernière fois : c'est du détail d'exploitation sur un parc. Un
collecteur se configure avec des identifiants comme n'importe quel autre
client.

Le mode mock expose les mêmes séries, dérivées des données d'exemple — cluster
injoignable compris, qui est justement celui qui mérite un panneau.

```yaml
scrape_configs:
  - job_name: moxy
    scrape_interval: 15s
    metrics_path: /metrics
    scheme: https
    static_configs:
      - targets: ['moxy.example.net']
    # Le reverse proxy qui authentifie moxy authentifie aussi le collecteur.
    basic_auth:
      username: prometheus
      password_file: /etc/prometheus/moxy.password
```

Deux alertes qui se déduisent directement du tableau ci-dessus :

```yaml
groups:
  - name: moxy
    rules:
      - alert: MoxyClusterUnreachable
        expr: time() - moxy_poll_last_success_timestamp_seconds > 60
        for: 5m
        annotations:
          summary: "moxy n'a pas lu {{ $labels.cluster }} depuis plus d'une minute"

      - alert: MoxyTokenRefused
        expr: rate(moxy_pve_requests_total{outcome="auth"}[15m]) > 0
        for: 15m
        annotations:
          summary: "PVE refuse le token de moxy sur {{ $labels.cluster }}"
```

## Le contrat Go ↔ TypeScript

`apps/api/internal/aggregate/model.go`, `detail/model.go` et `plan.go` d'un
côté, `apps/web/src/api/types.ts` de l'autre, décrivent le même document. Rien
dans les deux langages ne relie ces fichiers : un champ renommé d'un côté et
oublié de l'autre produit du JSON parfaitement valide que le frontend lit comme
`undefined`. C'est la rupture silencieuse la plus coûteuse du dépôt, et elle est
désormais tenue par trois mécanismes plutôt que par la discipline.

1. **Les fixtures du frontend sont générées.** `TestMockMatchesWebFixtures`
   (`apps/api/internal/server/fixtures_test.go`) sert chaque route du démon mock
   sur une horloge figée et compare l'octet près aux fichiers de
   `apps/web/src/test/fixtures/`. Un champ renommé dans le modèle Go fait donc
   échouer `make check-api` tant que les fixtures ne sont pas régénérées
   (`go test ./internal/server -update`). Elles étaient auparavant capturées à
   la main, ce qui voulait dire « quand quelqu'un y repense » : elles avaient
   dérivé d'un cluster entier.
2. **Le frontend vérifie la forme dans les deux sens.**
   `apps/web/src/api/types.contract.test.ts` affecte chaque fixture à son
   interface — ce qui échoue à la compilation si `types.ts` déclare un champ que
   le payload ne porte pas — puis compare les jeux de clés, ce qui échoue à
   l'exécution si le payload porte un champ que `types.ts` ne déclare pas. La
   régénération sans mise à jour de `types.ts` fait donc échouer `make
   check-web`.
3. **La CI vérifie que l'arbre de travail est propre** après les vérifications,
   dans les deux jobs, pour attraper une régénération faite en local et non
   commise.

Une route ajoutée au backend doit être déclarée dans `webFixtures` : c'est la
seule façon qu'elle soit surveillée.

## Confronté à un cluster réel

Les fixtures de test ont été écrites d'après le schéma documenté de PVE, aucun
cluster n'étant joignable depuis l'environnement de développement. Trois points
restaient à confirmer ; `scripts/probe-pve.sh` les sonde en lecture seule.

```sh
MOXY_SECRET='<uuid>' ./scripts/probe-pve.sh https://node:8006 'moxy@pve!ro' [--insecure]
MOXY_SECRET='<uuid>' make probe URL=https://node:8006 TOKEN='moxy@pve!ro' [INSECURE=1]
```

La sonde demande `curl` et `python3` ; `--insecure` se place où l'on veut. Sa
sortie est en anglais, comme le reste du code — c'est ici, dans le README, que
les conclusions se consignent en français. **Le secret ne passe jamais par la
ligne de commande de `curl`** : il est écrit dans un fichier de configuration
temporaire en 0600, lu avec `-K`, de sorte qu'un `ps -ef` lancé pendant la sonde
depuis un bastion partagé ne le montre pas.

Elle interroge aussi `/nodes/{node}/status` et croise son code avec celui
d'`apt/update` : un `403` d'un côté et un `200` de l'autre désigne exactement le
piège d'ACL décrit plus haut — un rôle posé sur `/nodes` qui porte `Sys.Modify`
sans `Sys.Audit`, donc qui efface l'audit hérité de `/` — et la sonde imprime
alors la commande `pveum` qui le corrige.

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

Deux constats supplémentaires sont venus du premier affichage réel des cartes
(issue #10), le même jour :

- **Un token peut lister les nœuds sans pouvoir les mesurer.** Sans `Sys.Audit`
  sur `/nodes/{node}`, PVE renvoie les lignes `node` de `/cluster/resources`
  amputées de `cpu`, `maxcpu`, `mem`, `maxmem` et `uptime`, sans erreur. Les
  cartes affichaient alors « CPU 0 % » et une mémoire absente sur un cluster
  « sain ». Depuis, `cpu` et `memory` valent `null` dans ce cas et une alerte
  `node_stats_unavailable` désigne les nœuds concernés.
- **Les stockages Ceph rapportent tous le même espace libre.** Trois pools RBD et
  quatre montages CephFS, adossés au même Ceph d'environ 37 TiB utilisables,
  affichaient 262 TiB une fois additionnés aux `local-lvm` et `local` des six
  nœuds. La capacité d'un cluster est désormais celle de ses stockages partagés
  utilisables pour des VM, Ceph compté une seule fois ; la fixture
  `cluster_resources_ceph.json`, capturée sur ce cluster, épingle le calcul.

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
  introduite par inadvertance fasse échouer la compilation. Une exception, et une
  seule, est décidée sans être encore livrée : `golang.org/x/crypto/ssh`, vendoré,
  pour le canal de maintenance — la stdlib n'a pas de client SSH
  ([ADR 0001](docs/adr/0001-go-stdlib-only.md#amendement-du-2026-09-17--golangorgxcryptossh),
  amendée par l'[ADR 0010](docs/adr/0010-node-maintenance-over-ssh.md)). Une
  **deuxième** dépendance fera toujours échouer la compilation.
- **La bibliothèque standard est donc la seule dépendance, et elle se tient à
  jour.** Sans dépendance externe, le niveau de correctif de la stdlib *est* la
  posture de sécurité du binaire : `moxyd` termine du HTTP et analyse des
  certificats que ses pairs contrôlent. La livraison est compilée avec une série
  Go supportée (Go 1.27, voir le `Containerfile`), pendant que `apps/api/go.mod`
  garde `go 1.19` comme **niveau de langage** — la directive `go` ne décide pas
  de la stdlib embarquée, seulement des API que le code a le droit d'employer.

S'y ajoutent, depuis l'étape 2 :

- **Aucun secret au repos dans la configuration** : le secret du token vit dans
  l'environnement, jamais dans un fichier committé ou sauvegardé.
- **Un token en lecture seule suffit** pour la vue d'ensemble (`PVEAuditor`).
- **`insecure` est un réglage par cluster**, journalisé, réservé au développement.
- **moxy n'authentifie personne, il vérifie qui l'a fait.** Le mode
  `proxy-header` refuse toute requête qui n'arrive pas d'un proxy listé avec une
  identité, et le démarrage avertit quand l'écoute dépasse loopback sans `auth`.
  Voir [Authentification](#authentification).
- **Le mode `token` couvre le poste isolé, et rien d'autre.** Un jeton partagé,
  lu dans l'environnement comme un secret de cluster, comparé à temps constant,
  porté par un cookie `HttpOnly; SameSite=Strict`. Il **autorise sans identifier
  personne** : c'est la limite, elle est rappelée à chaque démarrage et sur
  l'écran de saisie, et `proxy-header` reste préférable partout où une brique
  authentifiante peut être posée devant.
- **L'en-tête `Host` est vérifié**, ce qui ferme le rebinding DNS — la seule
  attaque côté navigateur contre laquelle un service loopback sans
  authentification peut se défendre. Voir
  [Vérification de l'en-tête `Host`](#vérification-de-len-tête-host).
- **L'image de conteneur écoute sur `0.0.0.0`** par nécessité ; c'est la publication
  du port qui doit rester sur loopback ou un réseau privé, voir
  [Déploiement en conteneur](#déploiement-en-conteneur). L'image tourne sans shell
  ni client HTTP, en utilisateur non privilégié, et ne contient ni secret ni
  fichier de configuration. La [variante de débogage](#variante-de-débogage), qui
  porte `bash` et `curl`, est une image distincte, taguée `-debug`, et n'est pas
  destinée à la production.
- **Le canal de maintenance n'ouvre pas un shell.** Compte de service sans
  interpréteur atteignable, commande imposée par `sshd`, deux verbes sur un nom de
  nœud que le cluster a réellement, `sudo` borné à un seul verbe, clé d'hôte
  vérifiée sans aucun réglage permissif, et `auth.mode: "none"` refusé. C'est cette
  clôture qui rend le second canal acceptable : voir
  [Mise en maintenance d'un nœud](#mise-en-maintenance-dun-nœud) et le modèle de
  menace de l'[ADR 0010](docs/adr/0010-node-maintenance-over-ssh.md).

**Déployer sans se tromper** : [docs/DEPLOIEMENT.md](docs/DEPLOIEMENT.md) donne
la marche à suivre complète — utilisateur dédié et permissions, unité systemd
durcie ([`deploy/moxyd.service`](deploy/moxyd.service)), reverse proxy qui
authentifie ([`deploy/Caddyfile`](deploy/Caddyfile),
[`deploy/nginx.conf`](deploy/nginx.conf)), et la vérification d'après
déploiement.

**Signaler une vulnérabilité** : voir [SECURITY.md](SECURITY.md), qui donne le
canal privé, les versions supportées, le périmètre et le modèle de menace.

## Périmètre

Sont en place :

- le backend agrégateur avec `GET /api/overview` (étape 2) ;
- le frontend avec son layout, son thème, son arbre et **la vue d'ensemble des
  clusters** — l'écran 4 (étape 3) ;
- **l'API de détail** : nœud, invité, séries RRD et tâches, décrites plus haut ;
- **la vue nœud (écran 2) et la vue VM (écran 1)**, qui la consomment. Elles
  n'ont pas de barre d'onglets : le §2 en dessine six, mais une seule a du
  contenu à ce stade, et cinq onglets morts promettraient ce qui n'existe pas ;
- **le plan de mise en maintenance** (écran 3) : `GET /api/clusters/{cluster}/nodes/{node}/maintenance/plan`
  dit quelle machine irait où, et si les nœuds restants ont la place, **avant**
  toute action. Strictement en lecture seule ;
- **le journal du cluster**, rafraîchi toutes les 5 s comme le reste.

Restent à venir :

- **L'exécution** de la mise en maintenance. Proxmox n'expose toujours aucune
  route REST pour basculer un nœud en maintenance — `node-maintenance-set` vit
  dans `PVE/CLI/ha_manager.pm` et écrit directement une commande CRM dans le
  système de fichiers du cluster —, mais la voie est désormais tranchée : un
  second canal SSH, sous une grammaire fermée
  ([ADR 0010](docs/adr/0010-node-maintenance-over-ssh.md)). Le bloc
  `maintenance` et la préparation des nœuds sont livrés et documentés dans
  [Mise en maintenance d'un nœud](#mise-en-maintenance-dun-nœud) ; **le
  transport SSH ne l'est pas**, et sans lui rien ne s'exécute — moxy continue
  d'ici là d'afficher la commande `ha-manager` à lancer.
- Le temps quasi réel : les tâches et le journal cluster se lisent aujourd'hui
  par scrutation de `.../tasks`, pas par un flux poussé (étape 5).
- Les modes d'authentification restants : le mTLS et l'OIDC annoncé. Le refus
  d'une requête non authentifiée est en place — `proxy-header` pour un
  déploiement derrière une brique authentifiante, `token` pour un poste isolé,
  voir [Authentification](#authentification).

## Licence

moxy est publié sous licence **Apache-2.0** ; le texte est dans
[`LICENSE`](LICENSE) et les images portent l'identifiant SPDX correspondant dans
`org.opencontainers.image.licenses`. Le raisonnement derrière ce choix — et le
fait que l'AGPL-3.0 de Proxmox VE ne s'y communique pas, moxy ne parlant à PVE
que par son API REST — est dans [`docs/RELEASE.md`](docs/RELEASE.md#la-licence).
