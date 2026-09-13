# Déploiement sécurisé

moxy détient un token d'API par cluster Proxmox et rend, en une page, la
topologie complète d'un parc. Il **n'authentifie personne lui-même** : c'est
délibéré, il détient déjà des tokens d'hyperviseur et n'a pas à détenir des mots
de passe en plus. Ce qu'il sait faire, c'est refuser toute requête qui n'est pas
passée par le composant qui, lui, authentifie.

Ce document est la mise en œuvre de cette phrase. Il va du token en lecture seule
au proxy authentifiant, en passant par une unité systemd durcie. Les fichiers
prêts à l'emploi sont dans [`deploy/`](../deploy) ; leurs commentaires sont en
anglais comme le reste de l'outillage, le raisonnement est ici.

Pour la politique de signalement de vulnérabilité et le modèle de menace,
voir [`SECURITY.md`](../SECURITY.md).

## La topologie visée

```
                        :443 TLS
   navigateur ─────────────────────► reverse proxy ────────────► moxyd
   opérateur                         (Caddy / nginx)   127.0.0.1:8080
                                           │
                                           ▼                      │
                                    oauth2-proxy                  │  https, token
                                     / Authelia                   │  PVEAuditor
                                      / basic auth                ▼
                                                          nœuds Proxmox VE
```

Trois propriétés, et elles se tiennent :

1. **`moxyd` n'écoute que sur loopback.** Il ne sait pas terminer TLS et n'a pas
   à savoir : c'est le travail du proxy.
2. **Le proxy authentifie**, et pose un en-tête d'identité que moxy vérifie.
3. **Le token PVE ne sort jamais du démon.** Le navigateur ne parle qu'à moxy,
   jamais à un nœud Proxmox.

## 1. Le token PVE, en lecture seule

La vue d'ensemble, les vues de détail et le plan de maintenance se contentent du
rôle **`PVEAuditor` sur `/`**. Un token privilégié n'apporte rien et transforme
une lecture indiscrète en prise de contrôle. La création du token et le détail
des privilèges par endpoint sont dans le README, section
[Privilèges PVE requis](../README.md#privilèges-pve-requis).

Deux champs facultatifs y gagnent : `Sys.Audit` sur `/nodes/{node}` (sans lui, un
nœud en ligne remonte « inconnu » plutôt que mesuré) et `Sys.Modify` pour la
lecture des mises à jour en attente. Les deux dégradent un champ, jamais la
réponse.

## 2. L'utilisateur et les fichiers

Un utilisateur système dédié, sans interpréteur de commandes ni répertoire
personnel :

```sh
useradd --system --no-create-home --shell /usr/sbin/nologin moxy
install -d -o root -g moxy -m 0750 /etc/moxy
```

Trois fichiers, trois natures :

| Chemin | Contenu | Propriétaire | Mode |
|---|---|---|---|
| `/etc/moxy/config.json` | clusters, URL, `tokenId`, politique TLS. **Aucun secret** | `root:moxy` | `0640` |
| `/etc/moxy/secrets.env` | `MOXY_SECRET_<CLUSTER>=…`, un par cluster | `root:root` | `0600` |
| `/etc/moxy/ca/*.pem` | CA des clusters en `tls.mode: pinned` | `root:moxy` | `0644` |

L'utilisateur `moxy` n'a **pas** besoin de lire `secrets.env` : systemd le lit
lui-même, en tant que root, avant de déposer les privilèges, et n'en transmet au
démon que les variables. Le fichier peut donc rester `root:root 0600`, ce qui est
plus étroit que ce dont le démon a l'air d'avoir besoin.

Le partage du fichier de configuration est ce qui justifie la séparation : il se
sauvegarde, se copie dans un dépôt, se joint à un ticket. **Le secret du token
n'y figure pas** ; il vit uniquement dans la variable d'environnement que
`secretEnv` nomme, et `moxyd` efface cette variable de son propre environnement
une fois la configuration chargée.

Le secret ne passe **jamais** en ligne de commande : `ps(1)` est lisible par tous
les utilisateurs de la machine.

```sh
install -o root -g moxy -m 0640 config.example.json /etc/moxy/config.json
# puis l'éditer : identifiants, URL des nœuds, tokenId, mode TLS
printf 'MOXY_SECRET_PRODUCTION=%s\n' "$SECRET" | install -o root -g root -m 0600 /dev/stdin /etc/moxy/secrets.env
```

## 3. Le binaire et le bundle

```sh
./scripts/build.sh && install -o root -g root -m 0755 bin/moxyd /usr/local/bin/moxyd
./scripts/build-web.sh && install -d /usr/share/moxy && cp -r apps/web/dist /usr/share/moxy/web
```

Sans `-web`, `moxyd` sert l'API seule — c'est le mode de développement, où Vite
sert le frontend. En production, un seul processus sert les deux sous la même
origine : il n'y a alors ni CORS à ouvrir, ni seconde origine à protéger.

## 4. L'unité systemd

[`deploy/moxyd.service`](../deploy/moxyd.service) s'installe tel quel :

```sh
install -o root -g root -m 0644 deploy/moxyd.service /etc/systemd/system/moxyd.service
systemctl daemon-reload
systemctl enable --now moxyd
```

Deux lignes sont à adapter avant : le nom public passé à `-allowed-hosts`, et le
chemin du bundle si vous ne servez pas le frontend.

### Ce que l'unité fait, et pourquoi

`moxyd` est un binaire statique (`CGO_ENABLED=0`) qui ouvre une socket d'écoute,
appelle les nœuds PVE, et lit deux fichiers. **Il n'écrit rien sur disque, ne
lance aucun processus, n'a besoin d'aucune capacité.** Le bac à sable de l'unité
n'est que cette phrase écrite en directives :

| Directive | Ce qu'elle ferme |
|---|---|
| `Wants=` + `After=network-online.target` | `After=` seul n'attend rien : la cible n'est tirée que par un `Wants=`, sans quoi elle est atteinte immédiatement et le démon démarre avant que la moindre adresse soit configurée. |
| `CapabilityBoundingSet=` (vide) | Aucune capacité, pas même `CAP_NET_BIND_SERVICE` : le port 8080 est au-dessus de la plage privilégiée. |
| `NoNewPrivileges=yes` | Aucun `setuid` ne peut regagner de privilège. |
| `ProtectSystem=strict`, sans `ReadWritePaths=` | Toute la hiérarchie en lecture seule. `/etc/moxy` n'a besoin que d'être lisible. Si le démon cesse de démarrer après cette ligne, c'est qu'il s'est mis à écrire quelque part. |
| `ProtectHome=yes`, `PrivateTmp=yes`, `PrivateDevices=yes`, `PrivateMounts=yes` | Ni répertoires personnels, ni `/tmp` partagé, ni périphériques. |
| `ProtectProc=invisible`, `ProcSubset=pid` | La liste des processus de la machine n'est pas l'affaire de moxy. |
| `ProtectKernelTunables/Modules/Logs`, `ProtectControlGroups`, `ProtectClock`, `ProtectHostname` | Le noyau et l'état de la machine sont hors d'atteinte. |
| `RestrictAddressFamilies=AF_INET AF_INET6` | TCP et UDP sur IP, rien d'autre. `AF_UNIX` est **absent**, et le binaire statique est ce qui le permet : avec CGO désactivé, le résolveur Go lit `/etc/resolv.conf` et parle DNS sur IP, il n'y a aucune socket NSS ou `nscd` à joindre. Le journal est un descripteur hérité, pas une socket ouverte par le processus. |
| `RestrictNamespaces`, `RestrictRealtime`, `RestrictSUIDSGID`, `RemoveIPC`, `LockPersonality` | Les mécanismes dont un démon HTTP n'a aucun usage. |
| `MemoryDenyWriteExecute=yes` | Go n'émet pas de code à l'exécution : rien de légitime n'a besoin d'une page à la fois inscriptible et exécutable. |
| `SystemCallFilter=@system-service` puis `~@privileged @resources @obsolete` | Le jeu d'appels système d'un service ordinaire, moins ce qui touche aux privilèges et aux limites. |
| `UMask=0077` | Ce que moxy pourrait créer — il ne crée rien — serait privé. |
| `EnvironmentFile=` | Le secret arrive par un fichier à droits restreints, pas par la ligne de commande. |

`IPAddressDeny=any` avec un `IPAddressAllow=` listant les nœuds est commenté dans
l'unité : c'est le durcissement qui borne le mieux ce qu'un démon compromis peut
joindre, mais il dépend des sous-réseaux de chaque parc et exige un noyau avec le
support BPF des cgroups.

**Il n'y a pas d'`ExecReload`.** `moxyd` ignore `SIGHUP` volontairement et ne
relit rien : modifier `config.json` ou `secrets.env` veut dire
`systemctl restart moxyd`. Le démon draine ses connexions pendant 5 s au plus sur
`SIGTERM`, d'où le `TimeoutStopSec=20s`.

### Mesurer le durcissement

```sh
systemd-analyze security moxyd
# hors machine cible, sur le fichier :
systemd-analyze security --offline=true deploy/moxyd.service
```

L'unité livrée obtient **1.2 — OK** (systemd 252). Ce qui reste exposé est
inhérent à un démon réseau : accès à la pile IP, absence de `PrivateNetwork` et
de liste d'adresses autorisées, racine du système hôte. Tout ajout à l'unité
devrait maintenir ce score sous 3.

## 5. Le proxy authentifiant

C'est la brique que le README exige sans la montrer, et elle est **la seule
protection réelle** tant que moxy n'a que le mode `proxy-header`. Deux exemples
équivalents, complets, à choisir selon ce qui est déjà en place :

- [`deploy/Caddyfile`](../deploy/Caddyfile) — certificat obtenu par ACME,
  `forward_auth` vers oauth2-proxy ou Authelia ;
- [`deploy/nginx.conf`](../deploy/nginx.conf) — `auth_request` vers le même
  composant.

Les deux se terminent par une variante `basic_auth` / `auth_basic`, commentée :
sur un poste isolé, avec un seul opérateur et aucun fournisseur d'identité à
joindre, c'est une authentification réelle et elle suffit.

Quatre points les gouvernent, et chacun a sa raison :

**a. L'en-tête d'identité entrant est détruit.** Caddy le fait par
`request_header -X-Forwarded-User`, nginx par le `proxy_set_header` qui le
réécrit. Sans cela, n'importe quel appelant affirme n'importe quelle identité —
et la croire serait pire que de ne rien vérifier, puisque le journal nommerait
alors la personne qu'il prétend être.

**b. `/healthz` et `/readyz` ne sont pas authentifiés.** C'est le miroir exact de
ce que fait `moxyd`, qui exempte ces deux chemins : un orchestrateur n'a pas
d'identité à présenter, et une sonde de vivacité qui échoue sur
l'authentification redémarre un démon qui fonctionne. Elles ne rendent qu'un état
et un identifiant de build. « Non authentifié » ne veut pas dire « public » pour
autant : les deux exemples les restreignent aux hôtes qui sondent réellement.

**c. `/metrics` est authentifié, lui.** L'exposition Prometheus nomme chaque
cluster configuré et dit quand chacun a été joignable pour la dernière fois :
c'est un document qui décrit le parc. Il traverse donc la même authentification
que l'API, et un collecteur reçoit des identifiants comme n'importe quel client.
Le bundle du frontend est protégé pour la même raison : c'est la topologie du
parc rendue en page.

**d. Le `Host` public est transmis, donc déclaré.** Les deux exemples préservent
le nom public, ce qui exige
`-allowed-hosts moxy.interne.example` côté `moxyd` — sans quoi toute requête
reçoit `421 Misdirected Request`. La solution inverse — réécrire `Host` en
`127.0.0.1:8080` — ne demande aucun réglage mais fait disparaître le nom public
des journaux de moxy. Ce contrôle ferme le rebinding DNS ; il n'identifie
personne, voir
[Vérification de l'en-tête `Host`](../README.md#vérification-de-len-tête-host).

Enfin, n'ajoutez **pas** de seconde politique d'en-têtes : `moxyd` pose déjà
`Content-Security-Policy`, `Referrer-Policy`, `X-Frame-Options`,
`Permissions-Policy` et `nosniff` sur la page. Le proxy n'ajoute que HSTS, parce
que c'est lui qui termine TLS.

## 6. Le bloc `auth` de moxy

Le proxy authentifie ; ce bloc fait que moxy **refuse de servir qui ne serait pas
passé par lui** :

```json
{
  "auth": {
    "mode": "proxy-header",
    "header": "X-Forwarded-User",
    "trustedProxies": ["127.0.0.1/32"]
  }
}
```

Les deux conditions sont nécessaires et aucune ne suffit : l'adresse comparée est
celle du **pair TCP** — qu'aucun en-tête ne peut changer — et l'en-tête doit être
présent. Une configuration `proxy-header` sans `trustedProxies` est refusée au
démarrage, pour cette raison exacte.

La limite à connaître : `trustedProxies: ["127.0.0.1/32"]` fait confiance à
*toute* la machine, pas au seul processus du proxy. Quiconque peut ouvrir une
connexion locale vers le port 8080 peut poser l'en-tête et sera servi. C'est
acceptable sur une machine dédiée au couple proxy + moxy ; ça ne l'est pas sur un
serveur partagé avec des utilisateurs non privilégiés. Dans ce cas, faites
tourner le proxy sur une autre machine et listez son adresse, ou isolez le réseau
du couple.

Sans le bloc `auth`, le démon le dit au démarrage dès que l'écoute dépasse
loopback :

```
warning: listening on 0.0.0.0:8080 and serving every cluster to anyone who
reaches the port; publish it on loopback, or configure auth and put an
authenticating proxy in front (see README)
```

## 7. La variante conteneur

Même architecture, mêmes règles. L'image écoute sur `0.0.0.0:8080` par nécessité
— sinon le port publié n'atteindrait pas le processus — donc **c'est la
publication du port qui doit rester privée** :

```sh
podman run --rm --read-only \
  --cap-drop=ALL --security-opt no-new-privileges \
  -p 127.0.0.1:8080:8080 \
  -v /etc/moxy:/etc/moxy:ro \
  --env-file /etc/moxy/secrets.env \
  -e MOXY_ALLOWED_HOSTS=moxy.interne.example \
  ghcr.io/dmajorel/moxy:edge
```

`-p 127.0.0.1:8080:8080`, et non `-p 8080:8080` : c'est l'erreur la plus probable
de tout ce document. Le proxy tourne sur l'hôte, ou dans un réseau conteneur
privé dont seul le proxy est publié.

`--cap-drop=ALL`, `--security-opt no-new-privileges` et `--read-only` sont les
équivalents des directives de l'unité systemd ; l'image est déjà `distroless`,
sans interpréteur de commandes, et tourne en `nonroot` (uid 65532), qui doit donc
pouvoir lire `/etc/moxy`. Vérifiez la signature de l'image avant de la déployer,
la commande `cosign` est dans le README, section
[Vérifier une image publiée](../README.md#vérifier-une-image-publiée).

## 8. Vérifier le déploiement

```sh
# 1. la sonde passe sans authentification
curl -sS -o /dev/null -w '%{http_code}\n' https://moxy.interne.example/healthz
# 200

# 2. l'API redirige vers le fournisseur d'identité (ou répond 401)
curl -sS -o /dev/null -w '%{http_code}\n' https://moxy.interne.example/api/overview
# 302

# 3. /metrics est traité comme l'API, pas comme une sonde
curl -sS -o /dev/null -w '%{http_code}\n' https://moxy.interne.example/metrics
# 302

# 4. le port de moxyd n'est pas joignable depuis le réseau
curl -sS --max-time 3 http://<ip-publique-de-l-hôte>:8080/healthz
# connexion refusée

# 5. le rebinding DNS est fermé
curl -sS -H 'Host: attacker.example' http://127.0.0.1:8080/api/overview
# {"error":"misdirected request"}

# 6. une identité forgée depuis l'extérieur ne passe pas
curl -sS -H 'X-Forwarded-User: root@pam' https://moxy.interne.example/api/overview
# 302 : l'en-tête a été détruit par le proxy
```

Puis le journal, qui doit être **muet** sur les avertissements :

```sh
journalctl -u moxyd | grep warning
```

Trois lignes y sont attendues seulement si vous les avez voulues : écoute
au-delà de loopback sans `auth`, vérification du `Host` inactive, et
`cluster "x" runs with TLS verification disabled`. La dernière rappelle que
`tls.mode: insecure` s'assouplit **par cluster**, jamais globalement, et qu'il
est réservé au développement : un cluster en `insecure` accepte n'importe quel
certificat, donc n'importe quel intermédiaire, avec le token dans l'en-tête
`Authorization`. En production, `system` ou `pinned`.

## À ne pas faire

- Publier le port de `moxyd` sans proxy authentifiant devant, « le temps de
  tester ». L'API rend le parc entier en un appel.
- Mettre le secret du token dans `config.json`, dans `ExecStart=`, ou dans une
  variable d'environnement posée par un script que tout le monde peut lire.
- Exempter `/metrics` de l'authentification « puisque c'est de la supervision ».
- Ajouter une seconde `Content-Security-Policy` au niveau du proxy.
- Mettre `tls.mode: insecure` sur un cluster de production pour faire taire une
  erreur de certificat. C'est un CA à épingler (`pinned`), pas une vérification
  à désactiver.
