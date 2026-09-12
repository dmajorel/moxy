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
| `apps/web` | Frontend (React + Tailwind) — échafaudé à l'étape 3 |
| `docs` | Document de passation et spécifications |
| `scripts` | Build et vérifications |

## Développement

Prérequis : Go ≥ 1.19.

```sh
./scripts/check.sh   # gofmt, go vet, go test
./scripts/build.sh   # compile bin/moxyd
./bin/moxyd          # écoute sur 127.0.0.1:8080 par défaut
```

L'adresse d'écoute se règle via `-addr` ou la variable `MOXY_ADDR`.

## Sécurité

moxy détient des tokens d'API vers un plan de contrôle d'hyperviseur, avec le
pouvoir de migrer des VM et de redémarrer des nœuds. Deux règles structurantes :

- **Les tokens ne quittent jamais le backend.** Le navigateur ne parle qu'à moxy,
  jamais directement à un nœud Proxmox.
- **Aucune dépendance externe côté backend.** Le code s'en tient à la bibliothèque
  standard Go ; les scripts posent `GOPROXY=off` pour que toute dépendance
  introduite par inadvertance fasse échouer la compilation.
