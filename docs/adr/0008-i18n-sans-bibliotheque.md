# ADR 0008 — Interface bilingue : catalogue typé à la main, sans bibliothèque d'i18n

- **Statut** : acceptée
- **Date** : 2026-09-15
- **Portée** : `apps/web` (catalogue, formatage, sélecteur), `apps/web/public/boot.js`.
  Aucune route ni aucun champ d'API n'est touché.

## Contexte

L'interface était monolingue et en dur : les libellés, les explications d'erreur et
jusqu'à la typographie des nombres étaient du français écrit à même les composants —
vingt-huit fichiers hors tests. Un opérateur dont le navigateur est en anglais lisait
quand même « Mises à jour », « 1,2 TiB » et « Délai dépassé côté cluster », et
`<html lang="fr">` était affirmé pour tout le monde, ce qui fait prononcer « quorum »
et « Sys.Audit » avec la phonétique française à un lecteur d'écran anglophone.

Le contrat backend anticipait ce moment. `CLAUDE.md` l'a arrêté dès le départ : « le
backend renvoie ses erreurs en anglais (`method not allowed`) ; la traduction vers
l'utilisateur est au frontend, jamais à l'API ». Les alertes voyagent en `kind`
(`formatAlert`), les échecs aussi (`explainError`). Il n'y avait donc rien à changer
côté serveur — seulement une dette à payer côté navigateur.

La question ouverte était **comment** : `i18next` et ses satellites, ou un catalogue
maison.

## Décision

**Français et anglais, depuis un catalogue TypeScript écrit à la main.**

`src/i18n/messages.ts` porte deux objets. `fr` est la source, déclarée `as const` ;
`MessageKey` en dérive ; `en` est déclaré `Record<MessageKey, string>`. **La
complétude du catalogue anglais est donc une erreur de compilation** : une clé
ajoutée au français et oubliée en anglais fait échouer `make check-web` au
`typecheck`, sans test à écrire ni règle à retenir. C'est la discipline mécanique qui
lie déjà `aggregate/model.go` à `api/types.ts`, appliquée aux mots.

La recherche est un accès de propriété, l'interpolation une expression régulière sur
`{nom}`. Il n'y a ni pluralisation dans le gabarit, ni branchement : `plural(count,
noun)` choisit une clé (`plural.node.one` / `.other`) plutôt que d'en analyser une.

**La langue suit le navigateur par défaut**, résolue depuis `navigator.languages` lu
dans l'ordre déclaré, chaque étiquette coupée à son premier sous-tag ; repli sur le
français, qui est la langue source. Un sélecteur de la barre supérieure permet d'en
décider autrement, et le choix est mémorisé **exactement comme celui du thème** :
`lib/lang.ts` est le miroir de `lib/theme.ts`, clé `moxy.lang` dans `localStorage`,
l'absence d'entrée valant « suis le navigateur ».

**La typographie n'est pas dans le catalogue.** Le séparateur décimal, l'espace fine
insécable devant un `%` français et le groupement des milliers sont du code : ils
vivent dans la table `TYPOGRAPHY` de `lib/format.ts`, à côté de ce qui les applique.

## Conséquences

- `lib/format.ts` se lit en deux moitiés. Ce qui ne dépend pas de la langue reste un
  export de module — `formatTime`, `formatVlan`, `formatGuestRef`, `splitTag`,
  `formatVolumeName` — et le reste pend à `createFormat(locale)`, que les composants
  obtiennent par `useFormat()`. Les sites d'appel n'ont pas changé de forme : ils
  destructurent au lieu d'importer.
- La langue voyage par un **contexte React**, seul endroit où ce projet s'écarte de sa
  règle « l'état vit à la racine et descend en props ». Le thème est lu par un
  composant, la langue par tous : la faire descendre en props ferait de chaque
  signature le porteur de quelque chose qu'aucun composant ne décide.
- **Le défaut du contexte est le français.** Ce n'est pas un repli que personne
  n'exerce : c'est ce qu'obtient tout composant rendu hors `LocaleProvider`,
  c'est-à-dire la quasi-totalité des tests unitaires — et la raison pour laquelle
  ajouter une langue n'a pas demandé de retoucher deux cents assertions.
- La suite de tests **fixe la langue du navigateur** (`src/test/setup.ts`), faute de
  quoi elle dépendrait de la machine : jsdom annonce `en-US`, et un test écrit contre
  les maquettes françaises du §2 échouerait sur une application qui fonctionne. Les
  cas qui veulent l'anglais le disent eux-mêmes.
- L'attribut `lang` de `<html>` est **toujours** posé, contrairement à `data-theme`
  qui est retiré en mode système. Le thème a un repli CSS ; la langue n'en a aucun.
  `public/theme-boot.js` devient donc `public/boot.js` et pose les deux avant la
  première peinture.
- Les deux noms de langue ne se traduisent pas : « Français » reste « Français » dans
  un menu anglais.
- Une troisième langue coûte un objet de plus dans `messages.ts`, une entrée dans
  `LOCALES`, une ligne dans `TYPOGRAPHY` — et rien d'autre. C'est le moment où cet ADR
  mériterait d'être relu : à trois langues, l'argument « une bibliothèque coûte plus
  qu'elle ne rapporte » s'affaiblit.

## Alternatives écartées

- **`i18next` / `react-i18next`.** La voie évidente, et elle apporte de vraies choses
  qu'on n'a pas : les règles de pluriel CLDR, le chargement paresseux des catalogues,
  un écosystème d'outils de traduction. Écartée pour deux raisons. La première est
  qu'aucune n'est utile ici : le français et l'anglais partagent la règle
  « singulier à un, pluriel partout ailleurs », il y a deux catalogues de quelques
  kilo-octets qu'il serait absurde de charger à la demande, et le traducteur est
  l'auteur. La seconde est que la complétude serait alors vérifiée par un outil
  externe ou pas du tout, là où le typage la rend impossible à oublier.
- **`Intl` et `Intl.PluralRules` pour la mise en forme.** Déjà écartés en tête de
  `lib/format.ts`, et pour la même raison qu'alors : la sortie d'ICU varie d'un build
  de Node à l'autre — notamment l'espace posée devant un `%` — et ces chaînes sont
  asserties caractère par caractère. Adopter ICU, ce serait rendre la suite de tests
  dépendante de la version de Node de la machine.
- **Négocier la langue côté serveur via `Accept-Language`.** Il n'y a pas de rendu
  serveur : `moxyd` sert un bundle statique, la langue se décide dans le navigateur, et
  passer par l'en-tête obligerait à servir un `index.html` variable — donc à le sortir
  du cache statique — pour un gain nul.
- **Un seul fichier de catalogue par langue, chargé en JSON.** Rend la complétude
  invérifiable par le compilateur, ce qui était l'intérêt principal.
- **Traduire aussi les messages du backend.** Contraire au contrat cité plus haut, et
  sans objet : l'API sert des `kind`, pas des phrases.

## Vérification

La complétude se contrôle en ajoutant une clé au seul objet `fr` de
`src/i18n/messages.ts` : `npm run typecheck` échoue aussitôt sur `en`. Le test
`src/i18n/messages.test.ts` ajoute ce que le typage ne voit pas — qu'une traduction
n'a pas *perdu* un `{nom}` en route, ce qui compilerait et rendrait une phrase
complète amputée de ce qu'elle nommait.

La résolution depuis le navigateur est couverte par `src/lib/lang.test.ts` (ordre des
étiquettes, coupe au premier sous-tag, repli), la mémorisation par
`src/lib/useLang.test.tsx`, le contrôle par `src/components/LangToggle.test.tsx`, et
le tout bout à bout par le bloc « App, in the browser's language » de
`src/App.test.tsx` : un navigateur anglais obtient une interface anglaise, un choix
explicite survit au rechargement et l'emporte sur le navigateur, et les chiffres
adoptent la typographie de la langue affichée.

`src/lib/lang.test.ts` vérifie enfin que `public/boot.js` redit bien la même clé, le
même attribut et les mêmes locales que le module, puisqu'il ne peut pas l'importer —
comme `theme.test.ts` le fait déjà pour la moitié « thème » du même fichier.
