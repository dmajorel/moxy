# Journal des modifications

Le format s'inspire de [Keep a Changelog](https://keepachangelog.com/fr/1.1.0/)
et la numérotation suit [SemVer](https://semver.org/lang/fr/).

Ce fichier est **rédigé à la main, en français**, et il ne remplace pas les notes
de version : la liste exhaustive des PR fusionnées est produite par « Generate
release notes » de GitHub et vient s'ajouter sous la section correspondante au
moment de la publication (voir [`docs/RELEASE.md`](docs/RELEASE.md)). Ce que
l'on trouve ici est ce qui intéresse un exploitant — ce qui change pour lui,
ce qu'il doit faire avant de mettre à jour — et non l'historique du dépôt, qui
est dans `git log`.

Chaque version publiée porte la date de son tag. `## Non publié` recueille ce
qui a été fusionné depuis ; la procédure de release consiste, entre autres, à
renommer cette section en `## vX.Y.Z — AAAA-MM-JJ`.

## Non publié

## v0.1.0 — à paraître

Première version taguée. Tant que la version majeure est `0`, une version
mineure peut casser la compatibilité : les formats de configuration et de l'API
ne sont pas encore figés, et chaque rupture est annoncée ici.

### Ajouté

- **Vue d'ensemble multi-cluster** : `GET /api/overview` agrège N clusters
  Proxmox VE 9 en un seul document — nœuds, invités, stockages, alertes et
  quorum — servi depuis le dernier état connu quand un cluster est injoignable,
  jamais une page vide.
- **API de détail à la demande** : nœud, invité, séries RRD, tâches du cluster
  et d'un invité, et plan de mise en maintenance (strictement en lecture
  seule). Les routes de détail interrogent PVE au moment de la requête, avec un
  cache court et un verrou anti-troupeau.
- **Frontend** (React 19, Tailwind 4) : vue d'ensemble des clusters, vue nœud,
  vue VM, journal du cluster, arbre de navigation accessible au clavier, thème
  clair et sombre. La sélection vit dans l'URL, donc un lien collé s'ouvre sur
  le même objet chez son destinataire. Les sparklines sont à échelle fixe : un
  nœud à 0,6 % ne ressemble pas à une chaîne de montagnes.
- **Image de conteneur unique** : `moxyd -web` sert l'API et le bundle sous la
  même origine, dans une image `distroless/static` sans shell, en utilisateur
  non privilégié. Publiée sur `ghcr.io/dmajorel/moxy` pour `linux/amd64` et
  `linux/arm64`, signée avec cosign (sans clé), avec provenance et SBOM.
- **Exploitation** : `GET /healthz` (vivacité, et la version du binaire),
  `GET /readyz` (disponibilité), `GET /metrics` (exposition Prometheus
  authentifiée, sans étiquette de cardinalité libre), `moxyd -version`,
  `moxyd -healthcheck`, et une première ligne de journal qui nomme la version
  de moxy, la toolchain Go et la plateforme.
- **Mode mock** (`moxyd -mock`) : l'ensemble des routes répond sur des données
  d'exemple, sans contacter le moindre cluster.

### Sécurité

- **Les tokens ne quittent jamais le backend** : le navigateur ne parle qu'à
  moxy. Un secret se rédige en `***` dans tout journal, toute erreur et toute
  sérialisation.
- **Aucune dépendance externe côté backend** : bibliothèque standard Go
  uniquement, `GOPROXY=off` dans les scripts pour que ce soit mécanique. La
  livraison est compilée avec une série Go supportée, pendant que `go.mod`
  garde `go 1.19` comme niveau de langage.
- **moxy n'authentifie personne, il vérifie qui l'a fait** : le mode
  `proxy-header` refuse toute requête qui n'arrive pas d'un proxy déclaré, et
  le démarrage avertit lorsque l'écoute dépasse la boucle locale sans bloc
  `auth`.
- **L'en-tête `Host` est vérifié**, ce qui ferme le rebinding DNS.
- **La vérification TLS ne s'assouplit que par cluster**, jamais globalement, et
  toujours avec un avertissement explicite.

### Licence

- Le dépôt est publié sous **Apache-2.0** ([`LICENSE`](LICENSE)).
