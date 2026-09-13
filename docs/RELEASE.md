# Publier une version

Ce document décrit comment on publie une version de moxy, et pourquoi la
procédure est celle-là. Il complète la section
[Déploiement en conteneur](../README.md#déploiement-en-conteneur) du README, qui
décrit ce qu'on déploie ; ici, on décrit ce qui le produit.

## Le tag *est* la version

Il n'y a pas de fichier `VERSION` dans le dépôt, et c'est volontaire. La chaîne
que rapportent `moxyd -version`, `GET /healthz`, la première ligne du journal,
le label `org.opencontainers.image.version` et les tags ghcr vient d'un seul
endroit : `git describe --tags`, lu par `scripts/build.sh` au moment de la
compilation et injecté dans le binaire à l'édition de liens
(`-X …/internal/server.Version`). Un deuxième endroit où écrire le numéro serait
un deuxième endroit où l'oublier.

Conséquence directe : **poser le tag, c'est faire la version**. Tout ce qui doit
être vrai d'une version doit donc être vrai *avant* que le tag existe — d'où la
répétition décrite plus bas. Et un tag ne se déplace jamais : la CI signe l'image
par digest précisément parce qu'un tag est mutable, et une version déjà tirée par
quelqu'un ne peut plus changer de contenu.

Corollaire moins évident : **tout tag présent dans le dépôt devient une version**.
`git describe --tags` sans `--match 'v*'` prend le tag le plus récent quel qu'il
soit, si bien qu'un `doc-freeze` posé un jour de rangement se retrouverait dans
`/healthz` et sur ghcr. `scripts/release.sh` avertit lorsque de tels tags
existent et vérifie, après avoir posé le sien, que `git describe` répond bien
`vX.Y.Z` — à défaut il retire le tag qu'il vient de créer. Le filtre `--match`
dans les scripts de build est suivi à part (issue #100).

## Numérotation

[SemVer](https://semver.org/lang/fr/), avec un `v` en tête du tag (`v0.1.0`) :
c'est ce que `image.yml` filtre (`tags: ['v*']`) et ce qui distingue une version
de n'importe quel autre tag.

Tant que la majeure vaut `0`, une mineure a le droit de casser la compatibilité :
le format de configuration, celui de `/api/overview` et les routes de détail ne
sont pas figés. Chaque rupture est annoncée dans `CHANGELOG.md` — c'est la
contrepartie du droit de casser.

Une pré-version se tague `v0.2.0-rc.1`. Elle publie ses propres tags d'image
mais **ne déplace pas `latest`** : `docker/metadata-action` réserve `latest` aux
versions stables.

## Avant de taguer

À vérifier une fois, à la main, parce qu'aucun script ne peut en juger :

- [ ] les correctifs de sécurité de plus haute priorité sont fusionnés — ce sont
      eux qui justifient qu'on publie maintenant, et ils sont listés dans les
      notes de version ;
- [ ] `CHANGELOG.md` décrit la version du point de vue d'un exploitant, et sa
      section porte la date du jour (`## v0.1.0 — 2026-09-13`) ;
- [ ] les ruptures de compatibilité y sont dites, avec ce qu'il faut faire ;
- [ ] la licence est arrêtée et `LICENSE` est à la racine (voir plus bas) ;
- [ ] `main` est à jour et vert : la CI ne rejoue pas sur un tag, elle tourne sur
      les branches et les PR. Le commit tagué doit donc être un commit de `main`
      déjà vérifié — c'est aussi ce que contrôle `release.sh`.

## Répéter

```sh
make release VERSION=v0.1.0        # ou ./scripts/release.sh v0.1.0
```

Sans `RELEASE_APPLY=1`, le script n'écrit rien dans le dépôt. Il vérifie, dans
l'ordre : le format de la version, l'absence du tag, la propreté de l'arbre de
travail, que `HEAD` est bien `origin/main`, la présence de `LICENSE`, la section
datée du `CHANGELOG.md`, puis il lance `scripts/check.sh` et compile le binaire
**avec la version demandée** pour confronter `moxyd -version` à ce qu'on attend.
C'est cette dernière étape qui répond à « la version du binaire et celle de
l'image concordent-elles ? » : les deux lisent la même variable, on vérifie ici
qu'elle vaut bien `v0.1.0`.

Il écrit au passage `bin/release-notes-v0.1.0.md`, qui est la section du
`CHANGELOG.md` de cette version, et affiche les commandes à lancer ensuite.

En répétition, les points qui n'ont de sens que pour une vraie publication —
être sur `main`, avoir daté la section — ne sont que des avertissements. En
`RELEASE_APPLY=1`, ce sont des échecs.

## Poser le tag et le pousser

```sh
RELEASE_APPLY=1 ./scripts/release.sh v0.1.0   # tag annoté, localement
git push origin v0.1.0                        # ← c'est la publication
```

Le script s'arrête au tag local : **le push reste une décision humaine**, parce
que c'est lui qui déclenche la construction et la publication de l'image. Un tag
local se supprime (`git tag -d`), un tag poussé beaucoup moins facilement.

Le tag est **annoté** et jamais léger : il porte un auteur, une date et un
message, et `git describe` le préfère.

## Les notes de version

```sh
gh release create v0.1.0 \
  --title "moxy v0.1.0" \
  --notes-file bin/release-notes-v0.1.0.md \
  --generate-notes
```

Les notes ont deux moitiés, et c'est délibéré :

- le **résumé en français**, écrit à la main, celui du `CHANGELOG.md` : ce qui
  change pour un exploitant, ce qu'il doit faire avant de mettre à jour ;
- la **liste des PR fusionnées**, produite par « Generate release notes » de
  GitHub et ajoutée dessous. Elle est en anglais puisque les titres de PR le
  sont, comme le code qu'ils décrivent. `.github/release.yml` en fixe les
  catégories (sécurité d'abord, correctifs ensuite, etc.) : une PR tombe dans la
  première catégorie dont elle porte un label, et une PR sans label reste listée
  sous « Other changes » plutôt que de disparaître.

Cette liste ne vaut que ce que valent les titres de PR — raison de plus pour
qu'ils soient des phrases à l'impératif, en anglais, et non `fix stuff`.

## Ce que la CI publie

Le push du tag déclenche `.github/workflows/image.yml`, qui construit l'image
pour `linux/amd64` et `linux/arm64` et pousse sur `ghcr.io/dmajorel/moxy` :

| Tag ghcr | Vient de |
|---|---|
| `0.1.0` | `type=semver,pattern={{version}}` |
| `0.1` | `type=semver,pattern={{major}}.{{minor}}` — suit les correctifs de la mineure |
| `latest` | déplacé par toute version stable |
| `sha-<commit>` | le commit tagué |

L'image est signée par cosign sans clé (l'identité est celle du workflow,
attestée par l'OIDC de GitHub), avec provenance complète et SBOM. La rotation
des images ne touche jamais une image portant un tag de version, quel que soit
son âge : c'est ce qu'épingle un déploiement.

## Vérifier après coup

```sh
git describe --tags --match 'v*'          # v0.1.0

cosign verify ghcr.io/dmajorel/moxy:0.1.0 \
  --certificate-identity-regexp '^https://github.com/dmajorel/moxy/' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com

podman run --rm ghcr.io/dmajorel/moxy:0.1.0 -version   # moxyd v0.1.0
podman image inspect --format '{{json .Labels}}' ghcr.io/dmajorel/moxy:0.1.0
```

Et sur une instance déjà déployée, une fois l'image tirée :

```sh
curl -s http://127.0.0.1:8080/healthz     # {"status":"ok","version":"v0.1.0"}
```

Les trois doivent dire la même chose. Si `/healthz` répond un identifiant de
commit (`0862b0c`) plutôt qu'une version, c'est que l'image tournant là n'a pas
été construite depuis un tag : soit elle vient de `edge`, soit le tag manquait à
la construction (`fetch-depth: 0` dans le workflow, faute de quoi `git describe`
n'a pas les tags).

## Se rattraper

**On ne déplace pas un tag.** Une version fautive se corrige par la suivante :
`v0.1.1` publie un correctif, et `0.1` comme `latest` suivent. Déplacer un tag
laisserait des déploiements épinglés sur `0.1.0` avec deux contenus différents
selon la date de leur `pull`, et invaliderait une signature qui, elle, porte sur
un digest.

Si le tag n'a pas encore été poussé, il se retire : `git tag -d v0.1.0`.

## La licence

Le dépôt est sous **Apache-2.0** : `LICENSE` à la racine porte le texte canonique
non modifié, et le `Containerfile` en déclare l'identifiant SPDX dans
`org.opencontainers.image.licenses` — c'est ce qui dit à qui tire l'image ce
qu'il a le droit d'en faire. Pour l'image publiée, `docker/metadata-action` pose
le même label depuis la licence que GitHub détecte sur le dépôt, d'où l'intérêt
que le fichier soit le texte exact.

Pourquoi celle-là pour un outil d'administration auto-hébergé : elle est
permissive, donc elle n'impose rien à l'exploitant qui déploie l'image ni à qui
l'intégrerait à une plateforme interne ; elle ajoute à MIT une **concession de
brevet explicite** et sa clause de représailles, ce qui compte pour un outil
d'exploitation susceptible d'être déployé en entreprise ; et elle demande de
préserver l'attribution et de signaler les modifications, ce que MIT ne fait
qu'à moitié. C'est aussi la licence attendue dans l'écosystème des outils
d'infrastructure, donc celle qui pose le moins de questions à une revue
juridique.

À noter : moxy ne parle à Proxmox VE que par son API REST, par le réseau, sans
en lier une ligne de code. Le choix de licence de moxy est donc indépendant de
l'AGPL-3.0 de Proxmox VE.

Changer d'avis reste peu coûteux **tant que rien n'est publié** : le fichier
`LICENSE`, le label du `Containerfile` et la ligne du `CHANGELOG.md` sont les
trois seuls endroits à reprendre. Après la première release publique, il faut
l'accord de tous les contributeurs.

## La première version

`v0.1.0` est la première version taguée. Deux choses à savoir avant de la poser :

- **Le dépôt est privé aujourd'hui.** Rendre le dépôt ou le paquet ghcr public
  est une décision distincte de celle de taguer ; la licence doit être arrêtée
  avant, pas après.
- **`git tag` est vide.** Aucun tag ancien ne vient donc brouiller
  `git describe` — c'est le bon moment pour que le tout premier tag du dépôt soit
  une version.
