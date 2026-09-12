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
