---
description: Traite une issue GitHub de bout en bout (analyse, branche, implémentation, tests, PR)
argument-hint: [numéro d'issue | mot-clé | vide pour choisir]
allowed-tools: Bash(gh:*), Bash(git:*), Read, Edit, Write, Glob, Grep
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

Pars de `main` à jour, jamais de commit direct dessus :

```
git switch main && git pull --ff-only
git switch -c issue-<numéro>-<slug-court>
```

## 4. Implémenter

- Corrige la cause, pas le symptôme.
- Reste dans le périmètre de l'issue : pas de refactor opportuniste, pas de
  reformatage de code non concerné.
- Respecte les conventions du fichier que tu modifies.

## 5. Vérifier

- Lance les tests et le linter du projet ; ajoute un test qui échouait avant le
  correctif quand c'est pertinent.
- S'il n'existe aucun harnais de test, vérifie le comportement manuellement et
  dis explicitement comment tu l'as vérifié.
- Rapporte les échecs tels quels — ne les masque pas.

## 6. Commit et PR

```
git commit -m "<résumé impératif court>

Closes #<numéro>"
git push -u origin HEAD
gh pr create --fill --body "Closes #<numéro>

<ce qui a changé et comment ça a été vérifié>"
```

Puis donne à l'utilisateur l'URL de la PR.

## Garde-fous

- Ne ferme jamais une issue à la main : laisse `Closes #<numéro>` le faire à la fusion.
- Ne pousse pas et n'ouvre pas de PR si les tests échouent — signale-le et arrête-toi.
- Une seule issue par branche et par PR.
