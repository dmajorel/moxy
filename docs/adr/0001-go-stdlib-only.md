# ADR 0001 — Backend Go sur la bibliothèque standard seule

- **Statut** : acceptée, amendée le 2026-09-17 (voir « Amendement » ci-dessous)
- **Date** : 2026-09-12
- **Portée** : `apps/api`, `scripts/env.sh`, `Containerfile`, `.github/workflows/ci.yml`

## Contexte

`moxyd` détient un token d'API par cluster, termine du TLS vers des nœuds dont les
certificats sont souvent auto-signés, et analyse les certificats que ses pairs lui
présentent. Toute dépendance externe est du code tiers qui s'exécute dans le même
processus que ces secrets, et une chaîne de fourniture à surveiller pour la durée de
vie du produit.

Le §4 du document de passation décrit exactement le besoin : parler HTTPS à N
endpoints, décoder du JSON, servir du JSON et un bundle statique. `net/http`,
`crypto/tls` et `encoding/json` le couvrent intégralement.

## Décision

Le backend n'utilise **que** la bibliothèque standard : aucun `require` dans
`apps/api/go.mod`, pas de `go.sum`, pas de `vendor/`.

La règle est tenue mécaniquement plutôt que par la discipline :

- `scripts/env.sh` pose `GOPROXY=off` (une dépendance ne se télécharge pas),
  `CGO_ENABLED=0` (binaire statique) et `GOTOOLCHAIN=local` (la toolchain installée
  est celle qui compile, toujours).
- `go.mod` déclare `go 1.19`, le niveau de langage de la toolchain du mainteneur
  (1.19.8). Cette directive fixe **le langage, pas la bibliothèque standard livrée**.
- La matrice de `ci.yml` a deux jambes, `1.19` et la série Go supportée du moment
  (`1.27`), et l'étape `api` du `Containerfile` compile avec cette même série
  supportée. Les deux bougent ensemble.

## Amendement du 2026-09-17 — `golang.org/x/crypto/ssh`

La règle devient : **la bibliothèque standard seule, sauf `golang.org/x/crypto/ssh`,
vendoré.** L'[ADR 0010](0010-node-maintenance-over-ssh.md) fait exécuter la commande
CRM de mise en maintenance par SSH sur un nœud du cluster, et la bibliothèque standard
n'a pas de client SSH. Les alternatives qui auraient préservé la règle ont été pesées
là-bas : appeler `ssh(1)` impose de quitter `distroless/static` et d'installer un
exécuteur de commandes distantes dans l'image, et écrire le client soi-même est de la
cryptographie de transport à maintenir dans le seul endroit du produit où un défaut est
immédiatement exploitable. C'est le cas que le contexte ci-dessus ne couvrait pas : non
pas une commodité, mais un protocole que la stdlib n'implémente pas.

Ce qui change concrètement :

- `apps/api/go.mod` porte un `require`, `apps/api/go.sum` existe, et
  `apps/api/vendor/` entre dans le dépôt avec `x/crypto` et sa transitive `x/sys`.
  La compilation se fait en `-mod=vendor` : `GOPROXY=off` reste posé et reste
  satisfait, rien n'est téléchargé à la compilation. Une **deuxième** dépendance fait
  toujours échouer la compilation, et c'est toujours l'effet recherché.
- La directive `go` du module est **relevée** au niveau qu'exige la série courante de
  `x/crypto` : épingler une version ancienne du paquet cryptographique d'un démon qui
  ouvre des sessions privilégiées, pour garder un garde-fou de niveau de langage,
  serait un mauvais échange. La jambe `1.19` de la matrice de `ci.yml` disparaît avec
  la directive, et avec elle la vérification mécanique qui interdisait `log/slog` et
  `errors.Join`.
- `govulncheck`, déjà dans `scripts/analyze.sh`, surveille pour la première fois une
  dépendance réelle, et Dependabot se voit déclarer l'écosystème `gomod`. La chaîne de
  fourniture que le contexte ci-dessus voulait éviter existe désormais : elle est
  réduite à un module, et elle se surveille.

Le reste de l'ADR est inchangé : `CGO_ENABLED=0` et l'indisponibilité de
`go test -race`, `GOTOOLCHAIN=local`, et le refus des bibliothèques de confort —
routeur, logger structuré, assertions de test — pour lesquelles trois paquets standard
suffisent.

## Conséquences

- Une dépendance introduite par inadvertance fait échouer la compilation au lieu de
  s'installer — c'est l'effet recherché.
- `go test -race` est **indisponible** : le détecteur de courses exige CGO. Les tests
  de concurrence s'écrivent sans lui.
- Pas de `log/slog` (1.21), pas d'`errors.Join` (1.20), pas de paramètres de chemin
  dans `ServeMux` (1.22) : les routes de détail découpent leurs segments à la main et
  refont la validation à chaque ajout.
- Le binaire livré n'embarque pas une bibliothèque standard figée à septembre 2023,
  ce qui serait le cas si la livraison compilait en 1.19.
- `setup-go` tourne sans cache : sans `go.sum`, il ne saurait pas quoi mettre en
  cache.

## Alternatives écartées

- **Les bibliothèques usuelles** (routeur, logger structuré, assertions de test) :
  gain d'ergonomie réel, mais surface de code tiers à côté des tokens d'hyperviseur,
  et un `go.sum` à auditer pour un besoin qui tient dans trois paquets standard.
- **Compiler aussi la livraison en 1.19**, par cohérence avec `go.mod` : embarquerait
  une stdlib sans correctif de sécurité depuis septembre 2023 dans un démon qui
  termine du TLS.
- **Relever `go.mod` à la série courante** : ferait entrer silencieusement des API
  récentes et supprimerait la vérification mécanique du niveau de langage.

## Vérification

`apps/api/go.mod` (aucun `require`, aucun `go.sum` à côté), `scripts/env.sh`,
`.github/workflows/ci.yml` (matrice `['1.19', '1.27']`, commentaire d'intention),
`Containerfile` (étape `api` sur `golang:1.27`).
