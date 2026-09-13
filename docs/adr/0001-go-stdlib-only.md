# ADR 0001 — Backend Go sur la bibliothèque standard seule

- **Statut** : acceptée
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
