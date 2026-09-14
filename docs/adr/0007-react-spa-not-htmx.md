# ADR 0007 — SPA React sur API JSON, plutôt que HTML rendu par Go (HTMX)

- **Statut** : acceptée
- **Date** : 2026-09-14
- **Portée** : `apps/web`, `apps/api/internal/server` (sert du JSON et un bundle, jamais du HTML)

## Contexte

Le §5 du document de passation posait deux voies — une extension navigateur, ou un
frontend maison devant un backend agrégateur — et recommandait la seconde, « React +
Tailwind (ou Vue), Tabler Icons ». C'est ce qui a été fait. Mais le §5 ne nommait pas
HTMX : la voie « tout en Go, HTML rendu côté serveur » n'a donc jamais été écartée par
écrit, elle n'a pas été examinée. La question revient (issue #199), et elle est
légitime.

Elle l'est d'autant plus que l'ADR 0001 interdit toute dépendance au backend, et que
l'argument de HTMX est précisément d'étendre cette discipline à la pile entière :
`html/template` est dans la bibliothèque standard, l'arbre npm disparaît, il n'y a
plus qu'un langage, plus de bundle, et plus de contrat entre deux représentations du
même objet.

L'inventaire, mesuré plutôt que supposé : `npm ci` installe **355 paquets**, mais la
fermeture qui part réellement au navigateur en compte **cinq** — `react`, `react-dom`,
`scheduler`, `@tabler/icons-react`, `@tabler/icons`. Le bundle livré pèse **352 Kio**,
dont 214 Kio pour React, isolé dans son propre fichier. Les 350 autres paquets sont de
l'outillage de construction et de test : ils s'exécutent sur les machines de
développement et en CI, jamais chez l'utilisateur.

## Décision

Le frontend est une application React + TypeScript servie comme un bundle statique,
qui parle au backend en JSON. Le backend ne rend **jamais** de HTML.

Trois raisons, dans l'ordre de leur poids.

**1. L'écran porte un état que le serveur n'a pas.** `usePolledResource` applique la
règle « une lecture périmée vaut mieux qu'un écran blanc » : un sondage qui échoue ne
vide pas la dernière valeur bonne, il la marque `isStale`, et l'intervalle grimpe
jusqu'à 60 s tant que rien ne répond. Avec le modèle d'échange de fragments de HTMX,
une réponse en erreur remplace la lecture valide par un message d'erreur, ou ne fait
rien du tout — dans les deux cas, « voici l'état d'il y a 40 secondes, la dernière
tentative a échoué, la prochaine est dans une minute » reste à tenir côté client. On
l'écrirait à la main, et c'est là que HTMX cesse de payer.

**2. Les écrans du §2 sont des widgets clavier, pas des documents.** L'arbre est un
vrai `role="tree"` à `tabindex` glissant, les menus rendent le focus à `Échap`
(`useMenu`), la modale piège le focus (`useFocusTrap`), la recherche répond à ⌘K.
HTMX ne traite aucun de ces sujets : il transporte du HTML, il ne gère pas le focus.
Ces composants s'écriraient en JavaScript à la main, ou avec une seconde bibliothèque
(Alpine) — la dépendance revient, sans les garanties du framework ni son outillage de
test.

**3. La sparkline doit refuser de s'auto-échelonner (ADR 0004) et suivre le sélecteur
de fenêtre sans aller-retour.** Elle est calculée à partir des points RRD bruts, que
le backend sert sans les mettre en forme, parce que l'échelle se décide là où l'on
connaît la taille du tracé et le thème.

## Conséquences

- Un arbre npm existe, et c'est le prix. Il est borné par ce qui est livré : cinq
  paquets au navigateur, contre zéro dépendance au backend. La distinction entre ce
  qui s'exécute chez l'utilisateur et ce qui s'exécute au build est réelle, mais elle
  ne supprime pas la chaîne de fourniture de construction — `npm audit` est en CI pour
  cette raison, l'ADR 0001 ne couvrant que `apps/api`.
- Le même objet existe deux fois, en Go et en TypeScript. Le contrat est tenu
  mécaniquement et non par la discipline : les fixtures du frontend sont générées
  depuis le démon mock par `TestMockMatchesWebFixtures`, et `types.contract.test.ts`
  échoue tant que `types.ts` n'a pas suivi `model.go`.
- Le backend renvoie ses erreurs en anglais avec un `kind` traduisible : la traduction
  est au frontend, ce qui n'aurait pas de sens si le serveur rendait les écrans.
- L'image porte un étage `web` (Node + Vite) qui ne sert qu'à produire le bundle ;
  l'image finale ne contient que le binaire et des fichiers statiques.

## Alternatives écartées

- **Go + HTMX.** La voie la plus séduisante sur le papier, et elle aurait tenu ses
  promesses sur une partie de l'interface : le journal des tâches, les tableaux
  d'invités et les pages de détail sont des documents que `html/template` rendrait
  très bien, sans bundle ni contrat à maintenir. Elle est écartée sur les trois points
  ci-dessus, qui ne sont pas des détails d'implémentation mais le cœur de ce que
  l'outil corrige : le comportement en panne, l'accessibilité clavier et le graphe non
  auto-échelonné. Le choix est défendable, pas évident — si le produit se réduisait un
  jour à des tableaux et perdait la scrutation, cet ADR mériterait d'être rouvert.
- **Extension navigateur injectant du CSS dans l'UI ExtJS** (§5.A) : déjà écartée par
  le document de passation, et à raison — le multi-cluster y est impossible, or c'est
  l'objectif n° 1 du §1.
- **Vue ou Svelte** à la place de React : même classe de solution, mêmes conséquences,
  aucun argument qui justifie de rouvrir le sujet.
- **Rendre le HTML en Go et n'hydrater que les widgets** : cumule les deux chaînes de
  construction et les deux modèles de rendu pour économiser une partie du bundle.

## Vérification

Les chiffres se recomptent : `npm ls --omit=dev --all` dans `apps/web` donne la
fermeture livrée (cinq paquets), `du -sh apps/web/dist` le poids du bundle après
`make build-web` (relevé le 2026-09-14 : 352 Kio, dont un fichier React de 214 Kio).

Le comportement en panne est dans `api/usePolledResource.ts` (`isStale`,
`MAX_POLL_INTERVAL_MS = 60_000`), couvert par `api/usePolledResource.test.tsx` et
`api/useOverview.test.tsx`. Les widgets clavier sont dans `components/ClusterTree.tsx`
(`role="tree"`, `tabIndex` glissant), `lib/useMenu.ts` et `lib/useFocusTrap.ts`,
chacun avec son test, et `App.a11y.test.tsx` passe axe sur l'application assemblée. Le
contrat est dans `api/types.contract.test.ts` et
`apps/api/internal/server/fixtures_test.go`.
