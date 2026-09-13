# Politique de sécurité

moxy détient des tokens d'API vers un plan de contrôle d'hyperviseur. Une faille
qui les expose, ou qui expose la topologie d'un parc, ne se traite pas comme un
défaut d'affichage : ce document dit par où la signaler, ce qui entre dans le
périmètre, et contre quoi moxy prétend protéger.

## Signaler une vulnérabilité

**N'ouvrez pas d'issue publique et ne décrivez pas la faille dans une pull
request.** Le dépôt est privé aujourd'hui, mais son historique et ses issues ont
vocation à être lus par tous ses collaborateurs : un rapport détaillé y est déjà
une diffusion.

Deux canaux, dans cet ordre :

1. **Signalement privé GitHub** — onglet *Security* du dépôt, *Report a
   vulnerability*. C'est le canal à privilégier : le fil reste invisible des
   autres collaborateurs et devient un avis publiable une fois le correctif
   livré.
2. **À défaut**, une issue portant le label `security` et **rien de plus qu'un
   « j'ai quelque chose à signaler, comment vous joindre ? »**. Le détail passe
   ensuite par le canal privé que la réponse indiquera.

Un bon rapport contient : la version touchée (`moxyd -version`, ou le tag et le
digest de l'image), le mode de déploiement (conteneur ou unité systemd, derrière
quel proxy, `auth.mode` en vigueur), les étapes de reproduction, l'impact
constaté, et la configuration minimale qui le reproduit — **sans aucun secret de
token, ni capture qui en contienne un**.

### Ce à quoi vous attendre

Le projet est maintenu sur du temps limité ; les délais ci-dessous sont des
engagements de bonne foi, pas un contrat de service.

| Étape | Délai indicatif |
|---|---|
| Accusé de réception | 5 jours ouvrés |
| Première évaluation (périmètre, gravité, reproduction) | 10 jours ouvrés |
| Correctif livré sur `main`, ou refus motivé | 30 jours pour une faille sérieuse |
| Publication de l'avis | après le correctif, en coordination avec vous |

Divulgation coordonnée : le crédit vous revient dans l'avis, sauf si vous
préférez l'anonymat. Il n'y a **pas de prime** ; c'est un projet sans budget.

## Versions supportées

| Version | Supportée |
|---|---|
| `main` / image `edge` | oui |
| tout le reste | non |

Aucune version n'est taguée à ce jour : il n'existe donc **aucune branche de
maintenance**, et un correctif de sécurité se livre sur `main`, d'où l'image
`edge` est reconstruite. Les images `sha-<commit>` ne sont pas maintenues et la
rotation n'en garde que les cinq dernières — voir
[Déploiement en conteneur](README.md#déploiement-en-conteneur). Quand des tags
`vX.Y.Z` existeront, ce tableau dira laquelle reçoit les correctifs.

Les images publiées sont signées (cosign, sans clé) et portent provenance et
SBOM : avant de déployer un correctif, **vérifiez la signature** plutôt que le
tag, la commande est dans le README.

## Périmètre

### Dans le périmètre

- le démon `moxyd` : routes HTTP, agrégation, API de détail, client Proxmox,
  exposition `/metrics`, gestion des secrets et des erreurs ;
- le bundle frontend servi par `moxyd -web`, et les en-têtes qui l'accompagnent ;
- l'image OCI et le `Containerfile` : contenu, utilisateur, permissions ;
- les exemples de déploiement de [`deploy/`](deploy/) — unité systemd, Caddy,
  nginx — et la documentation qui les accompagne. **Une recommandation fausse
  est une vulnérabilité** : c'est elle qui sera appliquée telle quelle ;
- la chaîne de construction : workflows CI, scripts de `scripts/`, publication
  et signature des images.

### Hors périmètre

- **Proxmox VE lui-même.** Une faille de PVE se signale à Proxmox ; moxy n'en
  est que le client. Ce qui reste dans le périmètre, c'est la façon dont moxy
  parle à PVE (vérification TLS, transport du token, données rendues).
- **Le composant qui authentifie** — oauth2-proxy, Authelia, nginx, Caddy,
  l'IdP. Les exemples de `deploy/` sont dans le périmètre, les produits qu'ils
  configurent ne le sont pas.
- **Un déploiement que la documentation déconseille explicitement.** Publier le
  port sans proxy authentifiant, ou laisser `auth.mode` à `none` sur une écoute
  qui dépasse loopback, expose le parc en lecture : `moxyd` l'écrit dans son
  journal au démarrage, ce n'est pas une faille mais une configuration.
- **`tls.mode: insecure` utilisé volontairement.** C'est un réglage par cluster,
  journalisé à chaque démarrage, réservé au développement. Qu'il désactive la
  vérification du certificat est son objet, pas un défaut. En revanche, tout
  chemin par lequel il s'appliquerait à un **autre** cluster que celui qui le
  déclare est une vulnérabilité, et sérieuse.
- **Le déni de service par un appelant déjà authentifié**, et la charge que moxy
  impose à un cluster : ce sont des sujets de dimensionnement (voir les délais
  et le budget de tour dans le README).
- Un rapport produit par un scanner sans démonstration d'impact : en-tête absent
  sur une route d'API, version divulguée par `/healthz` (c'est son rôle),
  absence de protection CSRF sur une API strictement en lecture.

## Modèle de menace, en résumé

**Ce qui a de la valeur** : les secrets de token PVE (un par cluster), et la
topologie du parc — noms de nœuds et d'invités, adresses, capacités, ce que
`/api/overview` et `/metrics` décrivent.

**Les frontières de confiance** : le navigateur et `moxyd` (même origine, pas de
CORS) ; `moxyd` et chaque cluster PVE (TLS, en-tête `Authorization`) ; la
configuration et l'environnement du démon (fichier + `EnvironmentFile`).

Les adversaires effectivement considérés, et ce qui leur répond :

| Adversaire | Réponse dans le code |
|---|---|
| Une page hostile visitée par l'opérateur (rebinding DNS) | Vérification de l'en-tête `Host` (`421`), aucun en-tête CORS permissif, `frame-ancestors 'none'`. |
| Quelqu'un qui atteint le port d'écoute | `auth.mode: proxy-header` : **adresse du pair TCP** listée dans `trustedProxies` **et** en-tête d'identité, les deux. Un avertissement au démarrage quand l'écoute dépasse loopback sans authentification. |
| Un intermédiaire réseau entre moxy et PVE | Vérification TLS `system` ou `pinned` par cluster ; les variables `HTTPS_PROXY` de l'environnement sont **ignorées** pour les appels PVE, précisément pour qu'aucun intermédiaire ne s'insère sans qu'on l'ait écrit. |
| Un lecteur de journaux, de tickets ou de sauvegardes | Aucun secret dans le fichier de configuration ; le secret est un type qui se rédige en `***` pour tout formatage ; interdiction de journaliser une requête HTTP sortante ; les erreurs de transport servies par l'API ne nomment ni hôte, ni adresse, ni certificat, et `/metrics` n'a aucune étiquette de cardinalité libre — donc aucun nom de nœud. |
| Un utilisateur local de la machine | Le secret arrive par `EnvironmentFile` en `0640`, jamais en ligne de commande visible dans `ps(1)` ; l'unité de `deploy/moxyd.service` tourne sous un utilisateur dédié, sans capacité, en système de fichiers en lecture seule. |

Ce qui **n'est pas** couvert : un opérateur légitime hostile — l'API est en
lecture seule, mais elle montre tout à qui a le droit d'entrer ; la compromission
d'un nœud Proxmox, qui rend la question de moxy secondaire ; et la compromission
de l'IdP ou du proxy, qui est exactement l'autorité qu'on leur a déléguée.

Deux propriétés structurantes tiennent le reste :

- **Aucune dépendance externe côté backend.** La bibliothèque standard Go est la
  seule dépendance, donc la seule surface à suivre ; `GOPROXY=off` fait échouer
  la compilation si l'une s'introduit. Corollaire : le niveau de correctif de la
  stdlib *est* la posture du binaire, et la livraison est compilée avec une série
  Go supportée, pas avec le `go 1.19` de `go.mod`, qui n'est qu'un niveau de
  langage.
- **L'API est en lecture seule.** moxy ne migre pas, ne redémarre pas, n'exécute
  aucune mise en maintenance : il en calcule le *plan*. Un token `PVEAuditor`
  suffit, et c'est celui qu'il faut utiliser.

## Avant de déployer

La lecture qui évite l'essentiel des erreurs :
[docs/DEPLOIEMENT.md](docs/DEPLOIEMENT.md), qui va du token PVE en lecture seule
à l'unité systemd durcie et au proxy authentifiant.
