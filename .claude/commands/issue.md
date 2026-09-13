---
description: Traite une issue GitHub de bout en bout (analyse, branche, implémentation, tests, PR)
argument-hint: [numéro d'issue | mot-clé | vide pour choisir]
allowed-tools: Bash(gh:*), Bash(git:*), Bash(make:*), Bash(./scripts/*), Read, Edit, Write, Glob, Grep
---

# Traitement d'une issue

Cible demandée : `$ARGUMENTS`

## 1. Identifier l'issue

- Si `$ARGUMENTS` est un numéro, c'est l'issue à traiter.
- Si `$ARGUMENTS` est du texte, cherche l'issue correspondante :
  `gh issue list --state open --search "$ARGUMENTS"`
- Si `$ARGUMENTS` est vide, liste les issues ouvertes et demande laquelle traiter :
  `gh issue list --state open --limit 30`
  N'en choisis pas une toi-même : attends la réponse.

Récupère ensuite le détail complet, commentaires inclus :
`gh issue view <numéro> --comments`

## 2. Analyser avant de coder

- Reformule en une phrase le problème réel et le critère d'acceptation.
- Explore le code concerné pour localiser précisément la cause.
- Si l'issue est ambiguë, si elle recouvre plusieurs changements indépendants,
  ou si le correctif toucherait bien plus que ce qui est décrit : pose la question
  avant d'implémenter, ne devine pas.

## 3. Branche

Pars de `main` à jour, jamais de commit direct dessus. Le dépôt travaille en
worktrees — la copie principale occupe déjà `main` — alors isole l'issue dans le
sien :

```
git fetch origin
git worktree add .claude/worktrees/issue-<numéro>-<slug-court> -b issue-<numéro>-<slug-court> origin/main
cd .claude/worktrees/issue-<numéro>-<slug-court>
```

Un worktree neuf n'a pas `apps/web/node_modules` : relie-le à celui de la copie
principale (`ln -s`) ou réinstalle-le (`npm ci`) avant de vérifier le frontend.

## 4. Implémenter

- Corrige la cause, pas le symptôme.
- Reste dans le périmètre de l'issue : pas de refactor opportuniste, pas de
  reformatage de code non concerné.
- Respecte les conventions du fichier que tu modifies.

## 5. Vérifier

- Lance `make check` (backend et frontend) ; `make check-api` et `make check-web`
  en isolent une moitié. Ajoute un test qui échouait avant le correctif quand
  c'est pertinent.
- S'il n'existe aucun harnais de test, vérifie le comportement manuellement et
  dis explicitement comment tu l'as vérifié.
- Rapporte les échecs tels quels — ne les masque pas.

## 6. Commit et PR

Le message de commit est **en anglais**, comme le code. **Le titre et le corps de
la PR aussi** (cf. `CLAUDE.md`, « Langue ») : GitHub recopie le titre dans le
commit de fusion, où il se lit dans `git log` à côté des autres sujets. Titre à
l'impératif, en minuscules, sans point final — la forme d'un sujet de commit.

```
git commit -m "<short imperative summary>

<what changed and why>

Closes #<numéro>"
git push -u origin HEAD
gh pr create \
  --title "<short imperative summary, lowercase>" \
  --body "Closes #<numéro>

<what changed and how it was verified>"
```

`--title` explicite plutôt que `--fill` : `--fill` reprend le sujet du dernier
commit, qui ne résume plus la PR dès que la branche en porte plusieurs.

Puis donne à l'utilisateur l'URL de la PR.

## Garde-fous

- Ne ferme jamais une issue à la main : laisse `Closes #<numéro>` le faire à la fusion.
- Ne pousse pas et n'ouvre pas de PR si les tests échouent — signale-le et arrête-toi.
- Une seule issue par branche et par PR.
- Le titre et le corps de la PR sont en anglais : ils finissent dans `git log`.
- Une fois la PR fusionnée, retire le worktree :
  `git worktree remove .claude/worktrees/issue-<numéro>-<slug-court>`.
