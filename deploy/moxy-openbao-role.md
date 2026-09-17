# Le rôle OpenBao du mode `openbao`

Ce document décrit le moteur SSH et le rôle à créer dans OpenBao pour que moxy
puisse ouvrir ses sessions de maintenance avec un certificat plutôt qu'avec une clé
posée sur chaque nœud. Il accompagne `deploy/moxy-node-setup.sh --mode openbao` ; le
raisonnement est dans [ADR 0010](../docs/adr/0010-node-maintenance-over-ssh.md) et la
mise en place complète dans [`docs/DEPLOIEMENT.md`](../docs/DEPLOIEMENT.md).

Les commandes utilisent la CLI `bao` ; `vault` accepte exactement les mêmes, les deux
API étant identiques sur ce chemin.

## Ce que ce mode déplace

En mode `ssh-key`, les contraintes vivent dans le fichier `authorized_keys` de chaque
nœud : `restrict`, `command="…"`, `from="…"`. En mode `openbao`, elles ne disparaissent
pas, **elles changent de porteur** — et de propriétaire :

| Mode `ssh-key`, sur le nœud | Mode `openbao`, dans le rôle |
| --- | --- |
| `command="/usr/local/sbin/moxy-maintenance"` | `default_critical_options: {"force-command": "/usr/local/sbin/moxy-maintenance"}` |
| `restrict` | `default_extensions: {}` et `allowed_extensions: ""` |
| `from="10.0.0.5"` | `default_critical_options: {"source-address": "10.0.0.5"}` |
| le compte visé par la clé | `allowed_users: ["moxy"]`, principal `moxy` |
| — | `allowed_critical_options: ""` |

C'est le gain réel, et il vaut d'être dit explicitement : les contraintes ne sont plus
dans un fichier que le nœud héberge, donc plus réécrivables par qui obtient un pied sur
le nœud, et plus oubliables sur le nœud qu'on ajoute au cluster six mois plus tard.
Elles vivent à un seul endroit.

Le nœud, lui, ne porte plus qu'une ligne : `TrustedUserCAKeys /etc/ssh/moxy-ca.pub`.

## 1. Monter le moteur SSH et engendrer la CA

```sh
bao secrets enable -path=ssh-client-signer ssh
bao write ssh-client-signer/config/ca generate_signing_key=true
```

Le chemin `ssh-client-signer` est le défaut attendu par moxy (`maintenance.openbao.mountPath`) ;
un autre chemin se déclare dans la configuration. Le nom dit ce que le moteur fait :
il **signe des clés de client**, il ne stocke rien. Aucune clé privée de moxy n'entre
jamais ici.

`generate_signing_key=true` fait engendrer la paire de la CA par OpenBao : la clé
privée de signature ne sort donc jamais du coffre. L'alternative — importer une paire
existante avec `private_key=` et `public_key=` — n'a d'intérêt que pour reprendre une
CA déjà déployée sur le parc.

## 2. Créer le rôle

Le rôle est écrit depuis un fichier JSON plutôt qu'en arguments `clé=valeur` : deux des
paramètres sont des objets, et la CLI ne les accepte proprement que par ce chemin.

```json
{
  "key_type": "ca",
  "algorithm_signer": "ssh-ed25519",
  "allow_user_certificates": true,
  "allowed_users": "moxy",
  "allowed_users_template": false,
  "default_user": "moxy",
  "default_extensions": {},
  "allowed_extensions": "",
  "default_critical_options": {
    "force-command": "/usr/local/sbin/moxy-maintenance",
    "source-address": "10.0.0.5/32"
  },
  "allowed_critical_options": "",
  "ttl": "5m",
  "max_ttl": "15m"
}
```

```sh
bao write ssh-client-signer/roles/moxy-maintenance @moxy-maintenance-role.json
```

Le nom du rôle est ce que la configuration de moxy appelle `maintenance.openbao.sshRole`.

### Pourquoi chaque paramètre

- **`key_type: "ca"`** — le mode « autorité de certification ». Les autres modes du
  moteur SSH (`otp`, et le `dynamic` retiré depuis) distribuent un secret par session à
  un compte existant ; seul celui-ci signe une clé publique, et c'est ce qui permet de
  faire porter les contraintes par le certificat.
- **`algorithm_signer: "ssh-ed25519"`** — l'algorithme de la signature du certificat.
  À aligner sur le type de la clé de CA engendrée à l'étape 1 ; si la CA a été
  engendrée en RSA, c'est `rsa-sha2-256` ou `rsa-sha2-512`, jamais `ssh-rsa`, dont
  OpenSSH refuse les signatures depuis la 8.8.
- **`allow_user_certificates: true`** — un certificat de *client*, pas d'hôte. Le rôle
  ne doit pas pouvoir signer des clés d'hôte : c'est justement ce que le `known_hosts`
  de moxy vérifie hors bande, et un rôle qui pourrait signer les deux permettrait de
  fabriquer le nœud auquel moxy croit parler.
- **`allowed_users: "moxy"`** — la liste close des comptes que ce rôle peut viser. Une
  demande pour `root` est refusée par OpenBao, sans que le nœud ait à en juger. C'est
  la moitié qui se décide dans le coffre ; `sshd` vérifie l'autre, en exigeant que le
  principal du certificat couvre le compte demandé.
- **`allowed_users_template: false`** — interdit qu'un gabarit (`{{identity.entity.name}}`,
  par exemple) recalcule le compte visé à partir de l'appelant. La liste doit rester
  littérale : un gabarit rend la valeur dépendante de l'identité AppRole, donc
  d'un endroit de plus.
- **`default_user: "moxy"`** — le principal posé quand la demande n'en nomme aucun, ce
  qui est le cas de moxy : le client ne demande rien, il signe et présente. Un champ
  de configuration en moins côté moxy, et une valeur de moins à pouvoir diverger.
- **`default_extensions: {}`** et **`allowed_extensions: ""`** — l'équivalent de
  `restrict`. Les extensions sont ce qui *autorise* : `permit-pty`, `permit-agent-forwarding`,
  `permit-port-forwarding`, `permit-X11-forwarding`, `permit-user-rc`. Aucune par
  défaut, et aucune demandable : le certificat n'ouvre ni terminal, ni agent, ni
  redirection de port. Attention à la valeur vide de `allowed_extensions`, qui est bien
  « rien n'est autorisé » : c'est `"*"` qui veut dire « tout », et c'est le défaut de
  certaines versions — l'écrire explicitement n'est pas une redondance.
- **`default_critical_options`** — les options *critiques*, celles qu'un `sshd` qui ne
  les comprend pas doit refuser plutôt qu'ignorer. C'est ce qui rend ce champ utilisable
  comme barrière :
  - `force-command` remplace `command="…"` de l'`authorized_keys`. Le certificat impose
    donc le validateur, et ce que moxy demande n'arrive que dans
    `$SSH_ORIGINAL_COMMAND`, en donnée.
  - `source-address` remplace `from="…"`. À poser quand le déploiement rend stable
    l'adresse d'où moxy sort ; la valeur est une liste de CIDR séparés par des virgules.
    À omettre sinon — une valeur fausse se paie d'une maintenance impossible, pas d'une
    alerte.
- **`allowed_critical_options: ""`** — le pendant indispensable du précédent. Sans lui,
  un client qui obtient une signature peut **demander** ses propres options critiques,
  donc réécrire `force-command`. Vide, le rôle impose et le client ne propose rien.
  C'est la ligne dont l'absence ruine silencieusement tout le reste du tableau.
- **`ttl` / `max_ttl`** — la durée de vie du certificat. Elle est fixée **ici et nulle
  part ailleurs** : moxy ne demande aucun TTL, parce que la durée de vie d'un
  identifiant est une décision de l'autorité qui le délivre, pas du client qui s'en
  sert. Quelques minutes suffisent largement : le certificat est signé au moment de
  l'exécution et meurt avec elle. Le compter en heures ne gagne rien et allonge la
  fenêtre pendant laquelle un certificat intercepté reste utilisable.

Ce qui n'est **pas** dans le rôle mérite aussi d'être dit :

- **pas de `valid_principals` côté demande** : `default_user` le pose, `allowed_users`
  le borne. moxy n'envoie que sa clé publique.
- **pas d'`AuthorizedPrincipalsFile` côté nœud** : `sshd` vérifie déjà que le principal
  du certificat couvre le compte visé. C'est un durcissement possible, à ne poser que si
  quelqu'un le demande — il rétablirait un fichier par nœud, c'est-à-dire précisément ce
  que ce mode supprime.

## 3. Récupérer la clé publique de la CA

C'est le seul fichier à déposer sur les nœuds. Il est **public** : rien n'est à
protéger ici, et il peut voyager par n'importe quel canal.

```sh
# Sans authentification : le point d'accès est non authentifié par construction.
curl -sf https://bao.example.net:8200/v1/ssh-client-signer/public_key -o moxy-ca.pub

# Ou depuis la CLI, en lisant la configuration du moteur :
bao read -field=public_key ssh-client-signer/config/ca >moxy-ca.pub
```

À vérifier avant de la diffuser, faute de quoi une erreur de chemin se découvrira à la
première session :

```sh
ssh-keygen -lf moxy-ca.pub
```

Puis, sur chaque nœud :

```sh
./moxy-node-setup.sh --mode openbao --ca moxy-ca.pub
```

Le script pose `/etc/ssh/moxy-ca.pub` et `/etc/ssh/sshd_config.d/10-moxy.conf`, vérifie
auprès de `sshd -T` que la directive est réellement lue, puis recharge le service. Il ne
retire **jamais** l'`authorized_keys` du mode `ssh-key` : c'est ce qui rend la bascule
possible sans coupure, et le retrait est une passe séparée
(`--remove-authorized-key`), à jouer seulement après avoir vérifié une mise en
maintenance de bout en bout par le certificat.

## 4. La politique et l'AppRole dont moxy se sert

moxy s'authentifie en AppRole — c'est la seule méthode, donc la configuration n'a pas de
champ pour en nommer une autre — et n'a besoin que de faire signer.

```sh
bao policy write moxy-maintenance - <<'EOF'
path "ssh-client-signer/sign/moxy-maintenance" {
  capabilities = ["update"]
}
EOF

bao auth enable approle
bao write auth/approle/role/moxy-maintenance \
    token_policies=moxy-maintenance \
    token_ttl=5m \
    token_max_ttl=15m \
    secret_id_num_uses=0 \
    secret_id_ttl=0

bao read -field=role_id auth/approle/role/moxy-maintenance/role-id
```

La politique ne donne **que** `update` sur le chemin de signature de ce rôle : ni
lecture du rôle, ni lecture de la configuration de la CA, ni aucun autre chemin. Le
`token_ttl` court est sans conséquence : moxy se connecte, fait signer, et ne conserve
rien entre deux exécutions — pas de jeton gardé, pas de renouvellement à écrire.

Le `role_id` va dans `maintenance.openbao.roleId`. Le `secret_id`, lui, va dans un
fichier que `maintenance.openbao.secretIdFile` désigne, et il y a deux façons de le
produire :

```sh
# Recommandé : jeton de response wrapping, à usage unique, déwrappé au démarrage.
bao write -wrap-ttl=60s -field=wrapping_token \
    -f auth/approle/role/moxy-maintenance/secret-id >/run/moxy/openbao-secret-id
# La configuration porte alors "wrapped": true.

# Plus simple à exploiter, moins bon : un secret_id à TTL long, monté en fichier.
bao write -field=secret_id -f auth/approle/role/moxy-maintenance/secret-id \
    >/etc/moxy/openbao-secret-id
# La configuration porte alors "wrapped": false, ou rien.
```

La contrainte d'exploitation du premier est à énoncer et non à découvrir : le jeton est
à usage unique, donc le fichier doit vivre sur un `tmpfs` et **être régénéré à chaque
redémarrage de moxy**. Le choix appartient à l'opérateur.

## 5. Vérifier avant de basculer

Le rôle se teste sans moxy, et c'est le moment le moins cher pour découvrir qu'une
option critique manque :

```sh
ssh-keygen -t ed25519 -N '' -f /tmp/moxy-test
bao write -field=signed_key ssh-client-signer/sign/moxy-maintenance \
    public_key=@/tmp/moxy-test.pub >/tmp/moxy-test-cert.pub

# Ce que le certificat porte vraiment : principal moxy, force-command, aucune
# extension, et le TTL du rôle.
ssh-keygen -Lf /tmp/moxy-test-cert.pub

# Et de bout en bout, sur un nœud déjà préparé :
SSH_ORIGINAL_COMMAND= ssh -i /tmp/moxy-test -o CertificateFile=/tmp/moxy-test-cert.pub \
    moxy@10.0.0.11 'node-maintenance disable prox-qual-2201-cit'
rm -f /tmp/moxy-test /tmp/moxy-test.pub /tmp/moxy-test-cert.pub
```

`ssh-keygen -Lf` doit montrer `Critical Options: force-command /usr/local/sbin/moxy-maintenance`
et une section `Extensions:` **vide**. Une ligne `permit-pty` qui traîne veut dire que
`default_extensions` n'a pas été appliqué, et que le certificat ouvre un terminal.
