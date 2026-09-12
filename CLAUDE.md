# moxy

Surcouche web multi-cluster pour Proxmox VE. La spécification de référence est
`docs/PROXMOX_UI_HANDOFF.md` : lis-la avant toute tâche de fond. Les décisions de
design du §2 et les contraintes du §4 sont arrêtées, ne les rediscute pas sans
raison explicite.

## Langue

- **Le code source est en anglais**, sans exception : identifiants, commentaires,
  messages de log, chaînes d'erreur, noms de tests, scripts shell et workflows CI.
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

## Frontend (`apps/web`)

React + Tailwind + Tabler Icons (`ti ti-*`). Thème construit sur les tokens CSS du
§2 du document de passation. Libellés en français, sentence case.

## Vérifications

```sh
./scripts/check.sh   # gofmt, go vet, go test
./scripts/build.sh   # compile bin/moxyd
```

`make` n'est pas disponible dans l'environnement de développement ; tout passe par
`scripts/`.

## Règles de sécurité

- Un token d'API Proxmox ne doit jamais atteindre le navigateur, ni un log, ni un
  message d'erreur renvoyé au client.
- La vérification TLS peut être assouplie **par cluster** (certificats auto-signés),
  jamais globalement, et toujours avec un avertissement explicite.
- Les actions destructrices (maintenance, migration, redémarrage) exigent une
  autorisation vérifiée côté backend, jamais seulement masquée côté UI.
