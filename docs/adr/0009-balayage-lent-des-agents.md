# ADR 0009 — Le drapeau d'agent invité se lit par invité, dans le cycle lent

- **Statut** : acceptée
- **Date** : 2026-09-15
- **Portée** : `apps/api/internal/aggregate/poller.go`,
  `apps/api/internal/aggregate/derive.go`, `apps/api/internal/aggregate/model.go`

## Contexte

L'arbre du panneau de gauche colore désormais chaque invité, et l'une des quatre
couleurs — le bleu — dit qu'une VM n'a pas d'agent QEMU configuré (issue #226).

Cette information n'est pas dans `/cluster/resources`, la liste unique que la
scrutation lit pour peupler la vue d'ensemble. Elle vit dans le champ `agent` de
`/nodes/{node}/{kind}/{vmid}/status/current` : **un appel par invité**. C'est
exactement la forme d'appel que l'ADR 0005 tient hors du scrutateur, et pour la
bonne raison — des centaines d'objets toutes les cinq secondes.

Mais elle n'a pas non plus la forme d'un appel à la demande : l'arbre est affiché
en permanence et montre *tous* les invités de *tous* les clusters à la fois, si
bien qu'une lecture « quand on regarde » reviendrait à tout lire tout le temps.

Le fait lui-même est d'une stabilité inhabituelle : le drapeau ne bouge que
lorsqu'un administrateur édite la configuration d'une VM.

## Décision

Le drapeau est collecté par un **troisième membre du cycle lent**
(`pollAgents`), aux côtés des paquets en attente et de la version de PVE :

- même cadence qu'eux, `updatesInterval` (10 min) : un drapeau qui ne bouge qu'à
  l'édition d'une VM n'a rien à faire dans un tour de cinq secondes ;
- **budget propre** (`agentsBudget`, 2 min), parce que le balayage n'a pas la
  forme des deux autres : ils posent une question par *nœud*, celui-ci une par
  *VM* ;
- **concurrence plafonnée** (`agentsConcurrency`, 8) par un groupe d'exécutants
  lisant une file. Les balayages par nœud n'ont aucun plafond et n'en ont pas
  besoin — un cluster a une poignée de nœuds ; le même patron sur trois cents VM
  ouvrirait trois cents connexions d'un coup sur l'API qui sert aussi les
  navigateurs des opérateurs ;
- **on n'interroge que ce qui peut répondre** : QEMU seulement — un conteneur n'a
  pas d'arbre `/agent` — et jamais un modèle, qui ne tourne pas ;
- le nœud est relu de la carte à chaque balayage, jamais mémorisé : un invité qui
  a migré est ailleurs, et son ancien nœud répondrait une erreur ;
- un balayage entièrement muet **garde la réponse précédente**, comme
  `pollVersions` : un token sans `VM.Audit` ou un cluster qui vacille ne doit pas
  faire clignoter le panneau.

Le champ `agent` du contrat est donc un booléen **nullable**, et `null` y est
l'état ordinaire : jamais balayé, pas balayable, ou PVE trop ancien pour publier
le champ.

## Conséquences

- Coût amont : un appel par VM toutes les dix minutes — trois cents VM ≈ 0,5
  appel par seconde —, entièrement hors du tour de cinq secondes, qui n'accueille
  pas un appel de plus. L'ADR 0005 tient.
- Le drapeau peut avoir jusqu'à dix minutes de retard. C'est acceptable pour ce
  qu'il dit, et ce ne le serait pour presque aucun autre champ de la vue.
- Au démarrage du démon, tous les invités ont `agent: null` jusqu'au premier
  cycle lent : l'arbre les peint alors sur leur seul état d'exécution.
- **`null` n'est pas « pas d'agent »**, et l'interface ne doit jamais le rendre
  ainsi. C'est aussi ce qui distingue une VM qui a répondu « non » (`false`) de
  celle à qui on n'a pas demandé.
- La vue nœud, qui liste les mêmes invités, laisse `agent` à `null` : elle
  n'interroge pas, et le mode mock fait de même sur cette route pour ne pas
  laisser croire que le champ y est servi.
- `agent` dit **configuré**, pas **installé et répondant** : PVE ne sait que si
  la case est cochée. Le libellé de l'interface dit donc « sans agent QEMU
  configuré » et rien de plus fort.

## Alternatives écartées

- **Sur le tour de cinq secondes** : N appels par tour, c'est-à-dire ce que
  l'ADR 0005 interdit, pour un fait qui bouge une fois par trimestre.
- **À la demande, comme le détail** : l'arbre affiche tous les invités de tous
  les clusters en permanence ; « à la demande » y signifie « en continu, pour
  tout le parc ».
- **Renoncer au bleu dans l'arbre** et ne le montrer que sur la page d'une VM, où
  la configuration est déjà lue : coût nul, mais l'information n'est alors plus
  jamais vue au moment où elle sert, qui est le balayage du parc.
- **Déduire l'agent de l'absence d'IPv4** : une VM sans agent et une VM dont
  l'agent ne répond pas donneraient le même verdict, et l'appel coûte le même
  prix. Deviner plus cher, c'est perdre deux fois.
- **Une cadence à part, plus rapide que dix minutes** : un troisième minuteur à
  régler pour un fait plus stable que les deux qui partagent déjà celui-là.

## Vérification

`aggregate/poller.go` (`pollAgents`, `knownAgentGuests`, `agentsBudget`,
`agentsConcurrency`, `pollSlow`), `aggregate/derive.go` (`agentOf`,
`ClusterData.Agents`), `aggregate/model.go` (`Guest.Agent`). Tests :
`poller_test.go` — `TestPollAgentsAsksTheVMsAndNobodyElse` (conteneurs et
modèles jamais interrogés, `false` distinct de `null`),
`TestPollAgentsKeepsTheLastKnownOnTotalFailure`,
`TestPollAgentsIgnoresAnAbsentField` ; `derive_test.go` —
`TestDeriveGuestAgentIsUnknownUntilSwept`.

Le champ `agent` de `status/current` et son absence sur les PVE plus anciens sont
documentés dans `proxmox/types.go` (`GuestStatus.Agent`, `AgentConfigured`), qui
le traitait déjà comme un inconnu et non comme un refus.
