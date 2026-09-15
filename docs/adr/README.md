# Décisions d'architecture (ADR)

Un ADR par décision structurante : ce qu'on a décidé, pourquoi, ce qu'on a écarté,
et ce que la décision coûte. Le fichier est court par construction — si une
décision demande trois pages, c'est qu'elle en cache plusieurs.

Le partage des rôles avec `CLAUDE.md` est délibéré :

- **`CLAUDE.md` porte la règle applicable**, en une ligne, avec un renvoi à l'ADR.
  C'est le fichier lu à chaque session : il doit rester une liste d'instructions,
  pas un exposé.
- **L'ADR porte le raisonnement** : le contexte du moment, l'alternative écartée et
  ce qu'on a vérifié pour trancher. On le lit quand on veut *remettre en cause* une
  règle, pas quand on veut l'appliquer.

Une issue ou une revue cite « ADR 0003 » plutôt que de recopier le paragraphe.

## Index

| # | Décision | Statut |
|---|---|---|
| [0001](0001-go-stdlib-only.md) | Backend Go sur la bibliothèque standard seule | Acceptée |
| [0002](0002-ceph-counted-once.md) | La capacité partagée se compte par backend, pas par ligne | Acceptée |
| [0003](0003-no-maintenance-execute.md) | `maintenance/plan` existe, `maintenance/execute` n'existera pas | Acceptée |
| [0004](0004-usage-chart-instead-of-gauges.md) | Graphe d'utilisation sur les cartes, jamais auto-échelonné | Acceptée |
| [0005](0005-detail-on-demand.md) | Vue d'ensemble scrutée, détail à la demande derrière un cache court | Acceptée |
| [0006](0006-polling-not-push.md) | Scrutation plutôt que flux poussé | Acceptée |
| [0007](0007-react-spa-not-htmx.md) | SPA React sur API JSON, plutôt que HTML rendu par Go (HTMX) | Acceptée |
| [0008](0008-i18n-sans-bibliotheque.md) | Interface bilingue : catalogue typé à la main, sans bibliothèque d'i18n | Acceptée |

## Format

Un ADR tient en cinq rubriques : **Contexte**, **Décision**, **Conséquences**,
**Alternatives écartées**, **Vérification**. L'en-tête porte le statut, la date de
la décision et la portée dans le dépôt. La rubrique « Vérification » est celle qui
manque le plus souvent ailleurs : elle nomme les fichiers et les observations qui
soutiennent la décision, pour qu'on puisse la contrôler plutôt que la croire, et
elle accueille les relevés faits sur un vrai cluster.

Le numéro ne se réutilise jamais. Une décision annulée devient « Remplacée par ADR
000N » et reste en place : l'historique est la moitié de l'intérêt du dossier.

Les ADR sont de la documentation : ils s'écrivent **en français**, comme `README.md`
et `docs/`, alors que le code et les messages de commit sont en anglais.
