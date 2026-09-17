# ADR 0003 — `maintenance/plan` existe, `maintenance/execute` n'existera pas

- **Statut** : remplacée par [ADR 0010](0010-node-maintenance-over-ssh.md)
- **Date** : 2026-09-12
- **Portée** : `apps/api/internal/detail/plan.go`, `apps/api/internal/server/detail.go`,
  `apps/web/src/screens/MaintenancePlanDialog.tsx`

## Contexte

Le §4 du document de passation suppose une route REST de mise en maintenance
(« `POST /cluster/ha/...` — vérifier l'endpoint exact dans la doc PVE 9 »), et
l'écran 3 dessine une modal de confirmation avec un bouton de validation.

La vérification a été faite dans les sources de PVE le 2026-09-12 :

- `node-maintenance-set` est enregistré dans `PVE/CLI/ha_manager.pm` : c'est une
  sous-commande de CLI, qui écrit une commande CRM dans le système de fichiers du
  cluster ;
- l'API2 HA n'expose que `current`, `manager_status`, `disarm-ha` et `arm-ha` ;
- `PVE/API2/Nodes.pm` ne contient pas une occurrence de « maintenance ».

**Il n'y a donc aucune route REST à appeler.** Le §4 décrivait une intention, pas une
API existante.

## Décision

moxy s'arrête à la moitié qui a de la valeur et qui est possible :

- `GET /api/clusters/{cluster}/nodes/{node}/maintenance/plan` calcule ce que le drain
  déplacerait — quel invité, vers quel nœud, avec quelle méthode (`online`, `restart`,
  `offline`), et si chaque cible reste sous le seuil de mémoire. Ce calcul est
  **strictement en lecture seule**.
- L'interface affiche la commande à lancer,
  `ha-manager crm-command node-maintenance enable <nœud>`, et s'arrête là.
- **Aucune route d'exécution, aucun bouton d'exécution, pas même désactivé.**

## Conséquences

- Le plan reste le vrai apport : c'est précisément ce que l'interface native ne donne
  pas avant le clic, et ce que le §2 exigeait à la place d'un « Êtes-vous sûr ? ».
- Le geste final se fait en SSH sur un nœud, hors de moxy.
- Un bouton désactivé assorti d'une infobulle promettrait une fonctionnalité qui
  n'arrivera pas : il est exclu.
- Le token de cluster peut rester en lecture seule pour cette fonctionnalité.
- Si PVE expose un jour la route, cet ADR sera **remplacé** par un autre, pas modifié.

## Alternatives écartées

- **Écrire la commande CRM nous-mêmes**, en SSH vers un nœud : moxy deviendrait un
  exécuteur de commandes distantes avec des identifiants shell sur chaque nœud, très
  au-delà d'un token d'API, et pour un service qui détient déjà les tokens de tout le
  parc.
- **Simuler le drain** en migrant les invités un par un via
  `POST /nodes/{node}/qemu/{vmid}/migrate` : cela ne met pas le nœud en maintenance —
  le CRM peut y renvoyer des ressources aussitôt —, exige un token en écriture, et
  donnerait l'illusion d'une mise en maintenance qui n'en est pas une.
- **Un bouton désactivé** avec explication : documente ce qui n'existe pas, ce que le
  projet s'interdit.

## Vérification

Sources PVE lues le 2026-09-12 (`PVE/CLI/ha_manager.pm`, `PVE/API2/HA/*`,
`PVE/API2/Nodes.pm`). Côté moxy : `detail/plan.go` (aucune écriture), l'interface
`DetailSource` de `server/detail.go` qui n'a pas de méthode d'exécution, et
`MaintenancePlanDialog.tsx`, qui rend la commande `ha-manager` comme texte à copier.
