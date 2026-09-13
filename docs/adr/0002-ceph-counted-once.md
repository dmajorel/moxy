# ADR 0002 — La capacité partagée se compte par backend, pas par ligne

- **Statut** : acceptée
- **Date** : 2026-09-12
- **Portée** : `apps/api/internal/aggregate/derive.go`, `apps/api/internal/proxmox/types.go`

## Contexte

La carte de cluster répond à une question simple : combien de place reste-t-il pour
héberger des invités. La source est `/cluster/resources`, qui énumère les stockages
**une ligne par nœud et par stockage**, et où plusieurs lignes peuvent désigner la
même capacité physique :

- un stockage `shared` est répété autant de fois qu'il y a de nœuds ;
- tous les pools RBD et tous les CephFS taillés dans un même Ceph rapportent
  l'espace libre de ce Ceph, pas le leur ;
- un stockage partagé peut ne rapporter aucune taille (`maxdisk: 0`).

Sommer les lignes donnait, sur un cluster à 6 nœuds le 2026-09-12, **262 TiB annoncés
pour 37 TiB réels** : 7 stockages Ceph additionnés.

## Décision

`deriveStorage` groupe les lignes en **backends** et somme les backends.

- Clé d'un stockage non-Ceph : `Resource.StorageKey()`, soit `storage` s'il est
  partagé, `node/storage` sinon.
- Clé d'un stockage Ceph (`rbd`, `cephfs`) : `ceph/<espace libre>`, et **non** la
  seule constante `ceph`. L'espace libre fait partie de la clé parce que c'est la
  seule chose que `/cluster/resources` nous donne pour distinguer deux Ceph : un pool
  adossé à un second Ceph (Ceph mutualisé) rapporte un libre différent et compte à
  part, sans quoi sa capacité disparaissait derrière celle du premier.
- À l'intérieur d'un backend, deux lignes au même couple (utilisé, total) ne sont
  comptées qu'une fois : plusieurs CephFS montés sur le même système de fichiers
  rapportent les mêmes chiffres.
- Un backend ne compte que s'il peut porter des disques d'invité (`images` ou
  `rootdir`) **et** rapporte une taille. Un partagé à `maxdisk: 0` — une cible iSCSI
  exposée directement — n'est pas un backend.
- Sans backend partagé utilisable, repli sur les stockages locaux des nœuds, pour
  qu'un nœud seul affiche quand même sa capacité.

## Conséquences

- Les chiffres de la carte ne sont plus proportionnels au nombre de nœuds.
- Une cible iSCSI sans taille ne masque plus les stockages locaux derrière un
  « 0 o / 0 o ».
- Deux Ceph distincts comptent deux fois, ce qui est le comportement voulu.
- Contrepartie assumée : deux backends réellement distincts dont l'espace libre est
  identique **à l'octet près** fusionneraient. C'est improbable et l'erreur va dans le
  sens prudent — sous-estimer la place disponible.
- La règle vit dans `aggregate`, donc la vue d'ensemble et le détail la partagent.

## Alternatives écartées

- **Sommer toutes les lignes** : faux d'un facteur égal au nombre de nœuds, c'est le
  bug d'origine.
- **Grouper les Ceph sur la seule constante `ceph`** : correct pour un cluster à un
  seul Ceph, mais fait disparaître la capacité d'un second Ceph derrière celle du
  premier.
- **Interroger `/nodes/{node}/storage/{storage}/status`** pour chaque stockage :
  N appels supplémentaires à chaque tour de scrutation pour une information déjà
  présente dans l'appel unique `/cluster/resources`.

## Vérification

`aggregate/derive.go` (`storageBackend`, `deriveStorage`, `backendAccum.add`),
`proxmox/types.go` (`StorageKey`, `IsCephBacked`, `HoldsGuestDisks`), fixture
`proxmox/testdata/cluster_resources_ceph.json`, tests de `aggregate/derive_test.go`.
Relevé de terrain : cluster à 6 nœuds, 2026-09-12, 7 stockages Ceph, 262 TiB contre
37 TiB.
