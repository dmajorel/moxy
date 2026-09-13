# ADR 0004 — Graphe d'utilisation sur les cartes, et jamais d'auto-échelle

- **Statut** : acceptée (dérogation assumée au §2, demandée explicitement)
- **Date** : 2026-09-12
- **Portée** : `apps/web/src/components/ui/Sparkline.tsx`,
  `apps/web/src/screens/ClusterCard.tsx`, `apps/web/src/lib/series.ts`,
  `apps/api/internal/detail/service.go` (`ClusterSeries`)

## Contexte

Deux constats se rejoignent ici.

Le §2 décrit la vue « tous clusters » avec des barres CPU / Mémoire / Stockage par
carte. Or une barre ne dit que l'instant, et la vue d'ensemble ne sert pas à lire une
valeur : elle sert à repérer une dérive. Un cluster à 71 % depuis un mois et un
cluster qui vient de passer de 30 % à 71 % donnent la même barre.

Et le défaut central de l'interface native est son graphe : il redimensionne son axe
vertical à la donnée, ce qui transforme un nœud qui flâne à 0,6 % en chaîne de
montagnes et fait paraître tous les nœuds également chargés. Le §2 le nomme
explicitement : « Jamais d'axe Y auto-scalé sur des valeurs < 1 % ».

## Décision

- La carte de cluster porte un **graphe d'utilisation sur la dernière heure** (deux
  courbes, CPU et mémoire) à la place des jauges CPU et mémoire. Les valeurs
  instantanées restent en légende et virent à l'ambre au-delà du seuil, qui vient du
  payload et non du composant.
- **Le stockage garde sa barre** : PVE n'expose pas d'historique de capacité partagée,
  donc il n'y a pas de courbe à tracer.
- La série vient de `/api/clusters/{cluster}/rrd`, repliée nœud par nœud parce que PVE
  n'a pas de RRD de cluster : moyenne CPU pondérée par les cœurs, mémoire sommée,
  points appariés sur l'horodatage et jamais sur l'indice — un nœud entré en cours
  d'heure a moins de points que ses voisins.
- `Sparkline` **fixe son axe à `[0, scaleMax]`**, défaut 1, hauteur fixe (70 px sur les
  écrans de détail, comme le §2). L'échelle n'est **jamais** dérivée des points.
- Un trou RRD coupe la courbe au lieu d'être tracé à zéro : `null` veut dire inconnu,
  pas zéro, et une lacune remplie inventerait une chute qui n'a pas eu lieu.
- Le backend sert des fractions brutes, jamais une image ni un pourcentage : l'échelle
  et la mise en forme sont au frontend.

## Conséquences

- Deux cartes côte à côte sont comparables, et une courbe plate près de zéro reste
  plate — c'est l'information.
- `Sparkline` prend des séries de ratios, pas des `Point` bruts : la traduction est le
  travail de `lib/series.ts`.
- Une carte dont la série manque ou échoue garde ses chiffres : le graphe est
  facultatif, il coûte la courbe et jamais la carte.
- Les cartes n'affichent pas d'axe temporel (48 px de haut) ; leur légende dit
  « Dernière heure ».
- Un nœud illisible perd sa part de courbe ; l'erreur n'est propagée que si aucun nœud
  n'a répondu.

## Alternatives écartées

- **Garder les jauges du §2** : lisibles, mais muettes sur la dérive, qui est la seule
  chose qu'une vue d'ensemble permet de voir.
- **Auto-échelonner la sparkline** pour « mieux voir les variations » : c'est
  exactement le défaut de l'interface native qu'on corrige.
- **Un graphe de stockage** sur la carte : PVE ne fournit pas l'historique.
- **Rendre le graphe côté backend** (image ou données pré-mises à l'échelle) : figerait
  l'échelle hors du composant qui connaît sa taille, et interdirait les thèmes.

## Vérification

`ui/Sparkline.tsx` (`scaleMax = 1`, `height = 70`, segments coupés sur `null`),
`screens/ClusterCard.tsx` (deux courbes, barre de stockage, seuil venu du payload),
`lib/series.ts` (`cpuRatios`, `memoryRatios`, `timeTicks`), `detail/service.go`
(`ClusterSeries`). Tests : `ui/Sparkline.test.tsx`, `screens/ClusterCard.test.tsx`.
