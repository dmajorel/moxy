# ADR 0005 — Vue d'ensemble scrutée, détail à la demande derrière un cache court

- **Statut** : acceptée
- **Date** : 2026-09-12
- **Portée** : `apps/api/internal/detail/cache.go`, `apps/api/internal/detail/service.go`,
  `apps/api/internal/aggregate/poller.go`

## Contexte

La vue d'ensemble est le seul document affiché en permanence : un scrutateur
d'arrière-plan la rafraîchit pour tous les clusters (ADR 0006). Les routes par objet
— nœud, invité, séries RRD, journaux de tâches — sont dans une situation opposée :
un parc de six nœuds et cent cinquante invités par cluster représente des centaines
d'objets, et personne n'en regarde plus d'un à la fois.

Les scruter tous coûterait des centaines d'appels PVE toutes les cinq secondes pour
des pages que personne n'a ouvertes. Mais appeler PVE à chaque requête HTTP, sans
rien amortir, expose l'amont au nombre d'onglets : dix opérateurs sur le même nœud,
avec un rafraîchissement à cinq secondes, font dix appels identiques toutes les cinq
secondes.

## Décision

Les routes de détail appellent PVE **au moment de la requête**, derrière un cache
court à verrou anti-troupeau (`detail/cache.go`) :

- une entrée mémorise un résultat pour un TTL (`DefaultTTL`, 5 s — la cadence de
  rafraîchissement de l'UI) ;
- les requêtes concurrentes qui la manquent **attendent** l'appel amont en cours au
  lieu d'en lancer chacune un : dix onglets sur le même nœud ne déclenchent qu'un
  appel ;
- le verrou n'est jamais tenu pendant le chargement, donc une clé lente ne bloque pas
  les autres ;
- les échecs sont mémorisés plus brièvement (`errTTL`), sauf les refus établis — un
  403 sur `apt/update` est l'état documenté d'un token en lecture seule et garde le
  TTL complet ;
- les données qui changent rarement ont un cache long à part (configuration d'invité,
  paquets en attente) ;
- chaque cache est nommé par **famille** (`view`, `node`, `guest`, `rrd`, `tasks`,
  `guest_config`, `updates`, `ipv4`), jamais par objet : c'est ce qui tient les noms
  d'hôte hors de `/metrics` ;
- les lectures RRD par nœud passent par l'entrée de la vue nœud, si bien qu'une carte
  et un onglet ouvert sur le même nœud ne coûtent qu'un appel.

## Conséquences

- Un objet que personne ne consulte ne coûte rien.
- Le coût amont dépend du nombre d'objets **regardés**, pas de la taille du parc ni du
  nombre de clients.
- Un appel facultatif qui échoue laisse son champ à `nil` sans faire échouer la
  réponse ; seuls les appels essentiels propagent leur erreur.
- Les requêtes ont un budget borné (`fetchBudget` pour un appel amont, `requestBudget`
  pour la requête entière) : un cluster qui rampe ne retient pas un handler.
- **Ne pas « uniformiser »** en ajoutant ces objets au scrutateur : ce serait annuler
  la décision.
- Première ouverture d'un objet : latence d'un appel PVE, assumée.

## Alternatives écartées

- **Tout scruter**, pour n'avoir qu'un seul mécanisme : coût proportionnel au parc et
  non à l'usage, pour des pages fermées.
- **Ne rien cacher** : l'amont subit le nombre d'onglets ouverts, et un rafraîchissement
  à cinq secondes multiplie mécaniquement la charge.
- **Un cache long** (30 s, une minute) : diviserait encore les appels, mais l'UI
  rafraîchit toutes les cinq secondes et afficherait un état périmé sans le dire.
- **Un cache sans verrou** : un objet fraîchement ouvert dans plusieurs onglets
  déclencherait autant d'appels simultanés, c'est-à-dire précisément le moment où la
  protection compte.

## Vérification

`detail/cache.go` (TTL, `errTTL`, `sticky`, entrée en vol partagée), `detail/service.go`
(`DefaultTTL`, `fetchBudget`, `requestBudget`, les caches nommés), `aggregate/poller.go`
pour le contraste. Tests : `detail/cache_test.go` (horloge injectée, pas de `sleep`).
