# ADR 0006 — Scrutation plutôt que flux poussé

- **Statut** : acceptée
- **Date** : 2026-09-12
- **Portée** : `apps/api/internal/aggregate/poller.go`,
  `apps/web/src/api/usePolledResource.ts`, `apps/web/src/api/useOverview.ts`

## Contexte

L'interface doit paraître vivante : un nœud qui tombe, une sauvegarde qui démarre, un
quorum qui se perd doivent apparaître sans rechargement de page.

PVE n'offre aucun flux d'événements exploitable : `/cluster/resources`,
`/cluster/status`, `/nodes/{node}/tasks` et les RRD sont des lectures ponctuelles.
Quel que soit le transport choisi vers le navigateur, **il faudra de toute façon
scruter PVE** pour produire les événements.

## Décision

La scrutation à tous les étages, aucun flux poussé — ni SSE, ni WebSocket, ni
long-polling.

- Backend : `aggregate/poller.go` rafraîchit la vue d'ensemble de tous les clusters
  toutes les 5 s (`pollInterval`), avec une passe lente et séparée pour les paquets en
  attente (`updatesInterval`, 10 min, parce qu'ils ne changent pas plus vite et que
  l'appel est coûteux). Un instantané plus vieux que `staleAfter` (60 s) est **signalé
  périmé mais reste servi** : le dernier état connu vaut mieux qu'une page vide.
- Frontend : `usePolledResource` interroge toutes les 5 s (`POLL_INTERVAL_MS`), les
  séries toutes les 60 s (`SERIES_POLL_INTERVAL_MS`), et applique un repli exponentiel
  plafonné à 60 s après un échec. Il **ne vide jamais ses données** : il conserve la
  dernière lecture réussie et lève `isStale`.
- Le journal du cluster relit `.../tasks` toutes les 5 s, ce que le cache court du
  service de détail absorbe (ADR 0005).

## Conséquences

- L'état affiché a jusqu'à cinq secondes de retard, ce qui convient à des tâches qui
  durent de quelques secondes à plusieurs minutes.
- Aucune connexion longue à maintenir à travers un reverse proxy, aucun chemin de
  reconnexion à écrire ni à tester, aucun état de session côté serveur.
- La charge amont est bornée et prévisible : elle ne dépend pas du nombre de clients
  connectés.
- Un cluster injoignable ne fait pas disparaître la vue, il la marque périmée.
- Le rythme de l'UI et celui du cache backend sont volontairement les mêmes : scruter
  plus vite que le backend ne rafraîchit n'achète rien.

## Alternatives écartées

- **SSE ou WebSocket** : il faudrait quand même scruter PVE pour alimenter le flux, on
  ajouterait une connexion longue par onglet, sa reconnexion, sa traversée de proxy et
  son état côté serveur — pour un gain de latence de quelques secondes sur des
  événements qui durent des minutes.
- **Long-polling** : même complexité de transport, mêmes soucis de proxy, sans le
  bénéfice de latence.
- **Écouter PVE directement** : PVE n'expose pas de flux d'événements hors console.
- **Scruter plus vite** (1 s) : multiplierait par cinq les appels au parc pour un gain
  imperceptible sur les objets observés.

## Vérification

`aggregate/poller.go` (`pollInterval`, `updatesInterval`, `staleAfter`),
`apps/web/src/api/usePolledResource.ts` (intervalles, repli, conservation de la
dernière lecture), `useOverview.ts`. Aucune occurrence d'`EventSource`, de
`WebSocket` ni de `text/event-stream` dans `apps/web/src` ou `apps/api/internal`
(vérifié le 2026-09-13).
