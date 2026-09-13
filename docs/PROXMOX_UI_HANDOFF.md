# Surcouche moderne pour Proxmox VE — document de passation

> Ce document résume un travail de conception réalisé en conversation avec Claude (claude.ai)
> les 10–12 septembre 2026. Il sert de point de départ pour l'implémentation dans Claude Code.
> Lis-le entièrement avant d'écrire du code. Les maquettes HTML en annexe sont la référence visuelle.

## 1. Contexte et objectif

L'interface native de Proxmox VE 9.2.x (ExtJS) est fonctionnelle mais datée :
icônes 16 px, gris partout, densité brutale, noms de VM tronqués à ~30 caractères,
graphe CPU auto-scalé qui transforme 0,6 % en pic. Elle est aussi mono-cluster :
chaque cluster est un endpoint API distinct, aucune vue agrégée.

**Objectif** : une interface web moderne (look 2026) qui :

1. agrège **plusieurs clusters** (qualification, préproduction, production…) dans une seule UI ;
2. reprend la structure mentale de Proxmox (arbre à gauche, contexte au centre, tâches en bas)
   pour ne pas dérouter les admins ;
3. met en avant le **statut** et les **métriques** plutôt que des tableaux clé/valeur ;
4. offre un bouton **« Mettre en maintenance »** sur les nœuds, avec un plan de migration
   explicite avant validation.

Environnement de référence : cluster `qualification`, nœud `prox-qual-2201-cit`,
VM 100/102/103 + template 101 (`template-rocky10`). Convention de nommage longue :
`sli-airflow-sep-exp-2601-qul.intranet.opt`. Tags Proxmox utilisés : `backup.none`,
`date.20260907`, `env.qualification`, `from.*`.

## 2. Décisions de design (à respecter)

| Sujet | Décision |
|---|---|
| Framework visuel | Surfaces plates, bordures fines (0,5 px), rayons 8–12 px, hiérarchie par la typo plutôt que par les bordures. Pas d'ombres portées. |
| Couleurs sémantiques | Vert `#1D9E75` / fond `#E1F5EE` / texte `#085041` = sain, running. Ambre `#EF9F27` / fond `#FAEEDA` / texte `#633806`–`#854F0B` = maintenance, dégradé, action à conséquence. Bleu `#378ADD` / fond `#E6F1FB` = données neutres (barres, graphes). Orange Proxmox `#D85A30` réservé au logo. |
| Arbre latéral | **3 niveaux** : cluster → nœud → VM. Chaque cluster affiche un compteur `nœuds en ligne / total` coloré (vert si complet, ambre sinon). État d'un nœud/VM = point de couleur 7 px, pas 4 icônes. Nœud en maintenance = point ambre + icône clé à molette. Noms d'invités affichés en entier, sans leur VMID — le débordement est coupé par la colonne et l'infobulle porte le nom complet (amendement de l'issue #22 ; la règle d'origine préfixait l'ID et ne gardait que le segment distinctif, `103 · airflow-sep-exp`). |
| Barre supérieure | Logo + version, sélecteur de cluster (« 3 clusters ▾ », permet de basculer ou tout voir), recherche globale centrée avec raccourci ⌘K, notifications, avatar utilisateur. |
| En-tête d'objet (VM ou nœud) | Nom en 18 px, puis sur la même ligne : tag d'état + uptime, tags Proxmox, et les **actions à droite** (Console/Shell, pause, maintenance, menu ⋯). Le statut ne doit jamais être enfoui dans une liste. |
| Onglets | Ligne horizontale sous l'en-tête (Résumé, Matériel/VM, Cloud-init/Disques, Snapshots/Réseau, Pare-feu, Options/Mises à jour). Remplace le menu vertical ExtJS. |
| Métriques | Cartes avec grande valeur (20 px) + unité en gris + barre de remplissage 3–4 px. CPU, Mémoire, Disque de boot pour une VM ; CPU, Mémoire, Stockage local, Load average pour un nœud. |
| Graphe CPU | Sparkline à **hauteur fixe** (70 px), area fill bleu clair + ligne bleue, moyenne affichée en libellé. Jamais d'axe Y auto-scalé sur des valeurs < 1 %. |
| Tâches | Tableau aéré : heure, description, **durée calculée**, état en tag. Pas de colonnes start/end à soustraire mentalement. |
| Bouton maintenance | Fond ambre, icône `tool`, libellé « Mettre en maintenance ». Sous la liste des VM du nœud, rappel « Migration à la maintenance : automatique (HA) ». *(amendement : le bouton ouvre le **plan**, en lecture seule, et non une exécution — voir l'amendement du §4 ; le rappel n'est pas affiché tel quel, le plan disant invité par invité ce que le CRM déplacerait de lui-même.)* |
| Confirmation maintenance | **Pas** de « Êtes-vous sûr ? » abstrait. Modal listant chaque VM → nœud de destination + RAM, un check de capacité **calculé avant le clic** (« 2202 passe à 34 % de RAM »), deux options : migrer aussi les VM hors HA, redémarrer le nœud une fois vide. Bouton de validation ambre foncé. Les templates restent sur place. *(amendement : les deux options et le bouton de validation sont des commandes d'exécution, et il n'y a pas d'exécution — voir l'amendement du §4. La modale rend le plan, signale les conteneurs qui seront redémarrés faute de migration à chaud, donne la commande `ha-manager` et les `qm migrate` / `pct migrate` des invités hors HA, puis se ferme. Les templates restent bien sur place.)* |
| Vue « tous clusters » | Une carte par cluster : point d'état + nom + tag Sain/Dégradé, barres CPU/Mémoire/Stockage (barre ambre si > 80 %), compteur de VM, liste des nœuds avec état, bandeau d'alerte en bas (quorum, mémoire, mise à jour disponible). Carte bordée en ambre si dégradée. En-tête : total nœuds, total VM, nombre d'alertes, bouton « Ajouter un cluster ». *(amendement demandé : la carte porte un **graphe d'utilisation sur la dernière heure** à la place des jauges CPU et mémoire — une barre ne dit que l'instant, la vue d'ensemble sert à repérer une dérive. Les valeurs instantanées restent en légende et virent à l'ambre au-delà du seuil ; le stockage garde sa barre, PVE n'exposant pas d'historique de capacité partagée. Le bandeau liste toutes les alertes, pas seulement la première. Pas de bouton « Ajouter un cluster » : un cluster se déclare côté serveur, son token ne passant pas par le navigateur.)* |
| Icônes | Tabler Icons (`ti ti-*`). Libellés en français, sentence case. |

## 3. Écrans maquettés

1. **Vue VM** (`103 · sli-airflow-sep-exp-2601-qul`) — résumé, métriques, sparkline CPU, infos nœud/HA/IP, tâches récentes.
2. **Vue nœud** (`prox-qual-2201-cit`) — arbre multi-cluster déplié, bouton maintenance, métriques nœud, quorum/HA/kernel, tableau des VM hébergées.
3. **Modal de confirmation de maintenance** — plan de migration, check de capacité, options.
4. **Vue d'ensemble des clusters** — trois cartes (Qualification sain, Préproduction dégradé avec un nœud en maintenance, Production sain avec mise à jour disponible).

Le code HTML/CSS de ces quatre maquettes est en annexe A. Il utilise des variables CSS
(`--surface-0/1/2`, `--border`, `--text-primary/secondary/muted/accent/success`,
`--bg-accent`, `--fill-ghost-selected`, `--radius`, `--font-mono`) à définir dans le thème.

## 4. Contraintes techniques Proxmox

- **API REST** : `https://<node>:8006/api2/json/...`. Auth par token (`PVEAPIToken=user@realm!tokenid=uuid`) en header `Authorization`. Un token **par cluster** (les clusters ne partagent rien).
- **Pas de multi-cluster natif** : la surcouche doit agréger N endpoints. Prévoir un backend léger (proxy/agrégateur) qui :
  - stocke la config des clusters (nom, URL, token, couleur) ;
  - interroge `/cluster/resources` (nœuds, VM, stockage en un appel), `/cluster/status` (quorum), `/cluster/ha/status/current`, `/nodes/{node}/status`, `/nodes/{node}/rrddata`, `/cluster/tasks` ;
  - évite d'exposer les tokens au navigateur.
- **Maintenance de nœud** : disponible depuis PVE 7.x via HA.
  CLI : `ha-manager crm-command node-maintenance enable|disable <node>`.
  API : `POST /cluster/ha/...` (vérifier l'endpoint exact dans la doc PVE 9 — `pvesh` peut aider : `pvesh ls /cluster/ha`). Le nœud reste dans le quorum, refuse les nouvelles VM, et le CRM migre les ressources HA. Les VM **hors HA** ne bougent pas seules : la modal doit proposer de les migrer (`POST /nodes/{node}/qemu/{vmid}/migrate` avec `online=1`).
  *(amendement, vérifié dans les sources le 2026-09-12 : **cet endpoint n'existe pas**. `node-maintenance-set` est enregistré dans `PVE/CLI/ha_manager.pm` et écrit une commande CRM dans le système de fichiers du cluster ; `/api2/json/cluster/ha` n'expose que `current`, `manager_status`, `disarm-ha` et `arm-ha`, et `PVE/API2/Nodes.pm` ne contient pas une occurrence de « maintenance ». moxy affiche donc la commande `ha-manager` à lancer, et n'aura pas de bouton d'exécution.)*
- **Check de capacité** avant maintenance : sommer la RAM des VM à migrer, la répartir sur les nœuds restants (même logique que le CRM : groupes HA, préférences), vérifier que chaque nœud reste sous un seuil (80 % par défaut, configurable).
  *(amendement : le plan ne reproduit pas la logique du CRM et ne le peut pas depuis `/cluster/resources` — ni groupes HA, ni `nofailback`, ni priorités, ni disques locaux, ni invités verrouillés n'y figurent. Il place à la mémoire seule, le plus gros d'abord, sous le seuil de la configuration. Limites détaillées dans le `README.md`.)*
- **Certificats** : les nœuds ont souvent des certs auto-signés ; le backend doit permettre de pinner un CA ou d'ignorer la vérif par cluster (avec avertissement).
- **Mise à jour** : `GET /nodes/{node}/apt/update` pour lister les paquets en attente (alimente le bandeau « 9.2.12 disponible »).

## 5. Deux voies d'implémentation (choix à faire)

**A. Extension navigateur** qui injecte CSS/JS dans l'UI ExtJS existante.
Faisable pour le thème (le DOM Proxmox est stable, bien classé), très difficile pour
restructurer les panneaux et impossible pour le multi-cluster. Non retenue sauf pour un
quick win visuel.

**B. Frontend maison + backend agrégateur** (recommandé).
Suggestion : backend Node (Fastify) ou Go, frontend React + Tailwind (ou Vue), Tabler Icons,
recharts/uPlot pour les sparklines. Auth de la surcouche elle-même à prévoir (SSO/OIDC
ou réutilisation d'un realm Proxmox).

## 6. Prochaines étapes suggérées pour Claude Code

1. Initialiser le repo (monorepo `apps/api` + `apps/web`), lint, CI minimale.
2. Backend : config multi-cluster, client Proxmox typé, endpoint agrégé `/api/overview`
   qui alimente l'écran 4. Mocks pour dev sans cluster.
3. Frontend : layout (barre, arbre 3 niveaux, zone centrale), thème (tokens du §2),
   écran 4 puis écran 2 puis écran 1.
4. Maintenance : endpoint `/api/clusters/{c}/nodes/{n}/maintenance/plan` (calcule le plan +
   check de capacité) puis `/execute`. Modal de l'écran 3.
   *(amendement : `/execute` **n'existera pas**. Proxmox n'expose aucune route REST
   pour basculer un nœud en maintenance — voir l'amendement du §4 — donc la modale
   donne la commande `ha-manager` et s'arrête là. `plan` est en lecture seule.)*
5. Tâches et journal cluster en temps quasi réel (polling 5 s ou SSE).

---

## Annexe A — Code HTML des maquettes

Les blocs ci-dessous ont été rendus dans un conteneur ~680 px de large. Ils sont
volontairement en HTML/CSS inline pour servir de référence de rendu, pas de code de prod.
Les variables CSS proviennent d'un design system de conversation ; à remplacer par les
tokens du projet.

### A.1 Vue VM (écran 1)

```html
<style>
.nav a{display:flex;align-items:center;gap:8px;padding:6px 10px;border-radius:var(--radius);font-size:13px;color:var(--text-secondary);text-decoration:none}
.nav a.on{background:var(--fill-ghost-selected);color:var(--text-primary);font-weight:500}
.tree div{display:flex;align-items:center;gap:6px;padding:4px 8px;font-size:12px;color:var(--text-secondary);border-radius:var(--radius)}
.tree .sel{background:var(--bg-accent);color:var(--text-accent);font-weight:500}
.kv{display:flex;justify-content:space-between;font-size:13px;padding:7px 0;border-bottom:0.5px solid var(--border)}
.kv span:first-child{color:var(--text-secondary)}
.tag{font-size:11px;padding:2px 8px;border-radius:999px;font-weight:500}
.tr{display:grid;grid-template-columns:70px 1fr 60px 90px;gap:8px;font-size:12px;padding:7px 0;border-top:0.5px solid var(--border);align-items:center}
</style>
<div style="border:0.5px solid var(--border);border-radius:12px;background:var(--surface-0);overflow:hidden">
<div style="display:flex;align-items:center;gap:12px;padding:10px 14px;background:var(--surface-2);border-bottom:0.5px solid var(--border)">
  <div style="width:22px;height:22px;border-radius:6px;background:#D85A30"></div>
  <span style="font-size:14px;font-weight:500">Proxmox VE</span>
  <span style="font-size:12px;color:var(--text-muted)">9.2.11</span>
  <div style="flex:1;margin:0 12px;display:flex;align-items:center;gap:8px;padding:6px 10px;border:0.5px solid var(--border);border-radius:var(--radius);background:var(--surface-1);font-size:12px;color:var(--text-muted)"><i class="ti ti-search"></i>Rechercher une VM, un nœud, une tâche…<span style="margin-left:auto;font-family:var(--font-mono);font-size:11px">⌘K</span></div>
  <i class="ti ti-bell" style="font-size:18px;color:var(--text-secondary)"></i>
  <div style="width:26px;height:26px;border-radius:50%;background:var(--bg-accent);color:var(--text-accent);font-size:11px;font-weight:500;display:flex;align-items:center;justify-content:center">ro</div>
</div>
<div style="display:grid;grid-template-columns:170px minmax(0,1fr)">
<div style="background:var(--surface-1);border-right:0.5px solid var(--border);padding:12px 10px">
  <p style="font-size:11px;color:var(--text-muted);margin:0 0 6px 8px">Datacenter · qualification</p>
  <div class="tree">
    <div><i class="ti ti-server"></i>Nœuds <span style="margin-left:auto;color:var(--text-muted)">1</span></div>
    <div><i class="ti ti-network"></i>Réseau</div>
    <div><i class="ti ti-database"></i>Stockage</div>
    <p style="font-size:11px;color:var(--text-muted);margin:12px 0 6px 8px">Machines virtuelles</p>
    <div><span style="width:7px;height:7px;border-radius:50%;background:#1D9E75"></span>100 · testproxmox</div>
    <div><span style="width:7px;height:7px;border-radius:50%;background:#1D9E75"></span>102 · testproxmox-2</div>
    <div class="sel"><span style="width:7px;height:7px;border-radius:50%;background:#1D9E75"></span>103 · airflow-sep-exp</div>
    <div><i class="ti ti-template"></i>101 · template-rocky10</div>
  </div>
</div>
<div style="padding:14px 16px">
  <p style="font-size:11px;color:var(--text-muted);margin:0 0 4px">prox-qual-2201-cit <i class="ti ti-chevron-right" style="font-size:11px"></i> VM 103</p>
  <div style="display:flex;align-items:center;gap:8px;flex-wrap:wrap;margin-bottom:12px">
    <span style="font-size:18px;font-weight:500">sli-airflow-sep-exp-2601-qul</span>
    <span class="tag" style="background:#E1F5EE;color:#085041"><i class="ti ti-point"></i> En cours · 2 j 22 h</span>
    <span class="tag" style="background:var(--surface-2);border:0.5px solid var(--border);color:var(--text-secondary)">env.qualification</span>
    <span class="tag" style="background:var(--surface-2);border:0.5px solid var(--border);color:var(--text-secondary)">backup.none</span>
    <span class="tag" style="background:var(--surface-2);border:0.5px solid var(--border);color:var(--text-secondary)">2026-09-07</span>
    <div style="margin-left:auto;display:flex;gap:6px">
      <button style="font-size:12px;padding:5px 10px"><i class="ti ti-terminal-2"></i> Console</button>
      <button style="font-size:12px;padding:5px 10px"><i class="ti ti-player-pause"></i></button>
      <button style="font-size:12px;padding:5px 10px"><i class="ti ti-dots"></i></button>
    </div>
  </div>
  <div class="nav" style="display:flex;gap:2px;border-bottom:0.5px solid var(--border);margin-bottom:14px;padding-bottom:6px;overflow:hidden">
    <a class="on" href="#"><i class="ti ti-layout-dashboard"></i>Résumé</a>
    <a href="#"><i class="ti ti-cpu"></i>Matériel</a>
    <a href="#"><i class="ti ti-cloud"></i>Cloud-init</a>
    <a href="#"><i class="ti ti-camera"></i>Snapshots</a>
    <a href="#"><i class="ti ti-shield"></i>Pare-feu</a>
    <a href="#"><i class="ti ti-settings"></i>Options</a>
  </div>
  <div style="display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:10px;margin-bottom:14px">
    <div style="background:var(--surface-2);border:0.5px solid var(--border);border-radius:var(--radius);padding:10px 12px">
      <p style="font-size:11px;color:var(--text-secondary);margin:0 0 4px">CPU</p>
      <p style="font-size:20px;font-weight:500;margin:0">0,25 %<span style="font-size:11px;color:var(--text-muted);font-weight:400"> · 6 vCPU</span></p>
      <div style="height:3px;background:var(--surface-0);border-radius:2px;margin-top:8px"><div style="width:2%;height:3px;background:#378ADD;border-radius:2px"></div></div>
    </div>
    <div style="background:var(--surface-2);border:0.5px solid var(--border);border-radius:var(--radius);padding:10px 12px">
      <p style="font-size:11px;color:var(--text-secondary);margin:0 0 4px">Mémoire</p>
      <p style="font-size:20px;font-weight:500;margin:0">1,25<span style="font-size:11px;color:var(--text-muted);font-weight:400"> / 8 GiB</span></p>
      <div style="height:3px;background:var(--surface-0);border-radius:2px;margin-top:8px"><div style="width:16%;height:3px;background:#378ADD;border-radius:2px"></div></div>
    </div>
    <div style="background:var(--surface-2);border:0.5px solid var(--border);border-radius:var(--radius);padding:10px 12px">
      <p style="font-size:11px;color:var(--text-secondary);margin:0 0 4px">Disque de boot</p>
      <p style="font-size:20px;font-weight:500;margin:0">28<span style="font-size:11px;color:var(--text-muted);font-weight:400"> GiB</span></p>
      <div style="height:3px;background:var(--surface-0);border-radius:2px;margin-top:8px"><div style="width:0%;height:3px;background:#378ADD;border-radius:2px"></div></div>
    </div>
  </div>
  <div style="display:grid;grid-template-columns:minmax(0,1.5fr) minmax(0,1fr);gap:10px;margin-bottom:14px">
    <div style="background:var(--surface-2);border:0.5px solid var(--border);border-radius:var(--radius);padding:10px 12px">
      <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:6px">
        <span style="font-size:12px;font-weight:500">Charge CPU</span>
        <span style="font-size:11px;color:var(--text-muted)">Dernière heure · moy. 0,42 %</span>
      </div>
      <svg viewBox="0 0 300 70" width="100%" height="70">
        <polygon points="0,70 0,40 20,42 40,37 60,44 80,36 100,40 120,45 140,10 160,42 180,40 200,32 220,42 240,38 260,40 280,36 300,38 300,70" fill="#E6F1FB"/>
        <polyline points="0,40 20,42 40,37 60,44 80,36 100,40 120,45 140,10 160,42 180,40 200,32 220,42 240,38 260,40 280,36 300,38" fill="none" stroke="#378ADD" stroke-width="1.5"/>
        <circle cx="300" cy="38" r="3" fill="#378ADD"/>
      </svg>
      <div style="display:flex;justify-content:space-between;font-size:10px;color:var(--text-muted)"><span>11:00</span><span>11:30</span><span>12:00</span></div>
    </div>
    <div style="background:var(--surface-2);border:0.5px solid var(--border);border-radius:var(--radius);padding:4px 12px">
      <div class="kv"><span>Nœud</span><span>prox-qual-2201-cit</span></div>
      <div class="kv"><span>HA</span><span style="color:var(--text-success)">started</span></div>
      <div class="kv"><span>Mémoire hôte</span><span>1,57 GiB</span></div>
      <div class="kv" style="border:0"><span>IPv4</span><span style="font-family:var(--font-mono);font-size:12px">10.18.160.4</span></div>
    </div>
  </div>
  <div style="background:var(--surface-2);border:0.5px solid var(--border);border-radius:var(--radius);padding:10px 12px">
    <div style="display:flex;gap:14px;align-items:center;margin-bottom:6px">
      <span style="font-size:12px;font-weight:500">Tâches récentes</span>
      <span style="font-size:12px;color:var(--text-muted)">Journal du cluster</span>
      <span style="margin-left:auto;font-size:11px;color:var(--text-muted)">Filtrer : ce nœud</span>
    </div>
    <div class="tr" style="border:0;color:var(--text-muted);font-size:11px"><span>Heure</span><span>Description</span><span>Durée</span><span>État</span></div>
    <div class="tr"><span>12:00:02</span><span>Realm INTRANET · sync</span><span>1 s</span><span class="tag" style="background:#E1F5EE;color:#085041;justify-self:start">OK</span></div>
    <div class="tr"><span>08:00:02</span><span>Realm INTRANET · sync</span><span>1 s</span><span class="tag" style="background:#E1F5EE;color:#085041;justify-self:start">OK</span></div>
    <div class="tr"><span>04:26:34</span><span>Mise à jour des paquets</span><span>4 s</span><span class="tag" style="background:#E1F5EE;color:#085041;justify-self:start">OK</span></div>
    <div class="tr"><span>04:00:02</span><span>Realm INTRANET · sync</span><span>4 s</span><span class="tag" style="background:#E1F5EE;color:#085041;justify-self:start">OK</span></div>
  </div>
</div>
</div>
</div>
```

### A.2 Vue nœud avec arbre multi-cluster et bouton maintenance (écran 2)

```html
<style>
.nav a{display:flex;align-items:center;gap:8px;padding:6px 10px;border-radius:var(--radius);font-size:13px;color:var(--text-secondary);text-decoration:none}
.nav a.on{background:var(--fill-ghost-selected);color:var(--text-primary);font-weight:500}
.tree div{display:flex;align-items:center;gap:6px;padding:4px 8px;font-size:12px;color:var(--text-secondary);border-radius:var(--radius)}
.tree .cl{color:var(--text-primary);font-weight:500;margin-top:6px}
.tree .n{padding-left:20px}
.tree .vm{padding-left:36px}
.tree .sel{background:var(--bg-accent);color:var(--text-accent);font-weight:500}
.kv{display:flex;justify-content:space-between;font-size:13px;padding:7px 0;border-bottom:0.5px solid var(--border)}
.kv span:first-child{color:var(--text-secondary)}
.tag{font-size:11px;padding:2px 8px;border-radius:999px;font-weight:500;white-space:nowrap}
.tr{display:grid;grid-template-columns:36px 1fr 70px 70px 80px;gap:8px;font-size:12px;padding:7px 0;border-top:0.5px solid var(--border);align-items:center}
.card{background:var(--surface-2);border:0.5px solid var(--border);border-radius:var(--radius);padding:10px 12px}
.dot{width:7px;height:7px;border-radius:50%;flex:none}
</style>
<div style="border:0.5px solid var(--border);border-radius:12px;background:var(--surface-0);overflow:hidden">
<div style="display:flex;align-items:center;gap:12px;padding:10px 14px;background:var(--surface-2);border-bottom:0.5px solid var(--border)">
  <div style="width:22px;height:22px;border-radius:6px;background:#D85A30"></div>
  <span style="font-size:14px;font-weight:500">Proxmox VE</span>
  <div style="display:flex;align-items:center;gap:6px;padding:4px 10px;border:0.5px solid var(--border);border-radius:var(--radius);font-size:12px"><span class="dot" style="background:#1D9E75"></span>3 clusters <i class="ti ti-chevron-down" style="font-size:12px;color:var(--text-muted)"></i></div>
  <div style="flex:1;margin:0 8px;display:flex;align-items:center;gap:8px;padding:6px 10px;border:0.5px solid var(--border);border-radius:var(--radius);background:var(--surface-1);font-size:12px;color:var(--text-muted)"><i class="ti ti-search"></i>Rechercher…<span style="margin-left:auto;font-family:var(--font-mono);font-size:11px">⌘K</span></div>
  <i class="ti ti-bell" style="font-size:18px;color:var(--text-secondary)"></i>
  <div style="width:26px;height:26px;border-radius:50%;background:var(--bg-accent);color:var(--text-accent);font-size:11px;font-weight:500;display:flex;align-items:center;justify-content:center">ro</div>
</div>
<div style="display:grid;grid-template-columns:190px minmax(0,1fr)">
<div style="background:var(--surface-1);border-right:0.5px solid var(--border);padding:10px 8px">
  <div class="tree">
    <div class="cl" style="margin-top:0"><i class="ti ti-chevron-down" style="font-size:12px"></i><i class="ti ti-topology-star-3"></i>Qualification<span class="tag" style="margin-left:auto;background:#E1F5EE;color:#085041">3/3</span></div>
    <div class="n sel"><span class="dot" style="background:#1D9E75"></span>prox-qual-2201-cit</div>
    <div class="vm"><span class="dot" style="background:#1D9E75"></span>100 · testproxmox</div>
    <div class="vm"><span class="dot" style="background:#1D9E75"></span>102 · testproxmox-2</div>
    <div class="vm"><span class="dot" style="background:#1D9E75"></span>103 · airflow-sep-exp</div>
    <div class="vm"><i class="ti ti-template" style="font-size:12px"></i>101 · template-rocky10</div>
    <div class="n"><span class="dot" style="background:#1D9E75"></span>prox-qual-2202-cit</div>
    <div class="n"><span class="dot" style="background:#1D9E75"></span>prox-qual-2203-cit</div>
    <div class="cl"><i class="ti ti-chevron-down" style="font-size:12px"></i><i class="ti ti-topology-star-3"></i>Préproduction<span class="tag" style="margin-left:auto;background:#FAEEDA;color:#633806">2/3</span></div>
    <div class="n"><span class="dot" style="background:#1D9E75"></span>prox-pprd-2301-cit</div>
    <div class="n"><span class="dot" style="background:#EF9F27"></span>prox-pprd-2302-cit<i class="ti ti-tool" style="margin-left:auto;font-size:12px;color:#BA7517"></i></div>
    <div class="n"><span class="dot" style="background:#1D9E75"></span>prox-pprd-2303-cit</div>
    <div class="cl"><i class="ti ti-chevron-right" style="font-size:12px"></i><i class="ti ti-topology-star-3"></i>Production<span class="tag" style="margin-left:auto;background:#E1F5EE;color:#085041">5/5</span></div>
    <div class="cl" style="color:var(--text-muted);font-weight:400"><i class="ti ti-plus" style="font-size:12px"></i>Ajouter un cluster</div>
  </div>
</div>
<div style="padding:14px 16px">
  <p style="font-size:11px;color:var(--text-muted);margin:0 0 4px">Qualification <i class="ti ti-chevron-right" style="font-size:11px"></i> Nœud</p>
  <div style="display:flex;align-items:center;gap:8px;flex-wrap:wrap;margin-bottom:12px">
    <span style="font-size:18px;font-weight:500">prox-qual-2201-cit</span>
    <span class="tag" style="background:#E1F5EE;color:#085041">En ligne · 41 j</span>
    <span class="tag" style="background:var(--surface-2);border:0.5px solid var(--border);color:var(--text-secondary)">PVE 9.2.11</span>
    <span class="tag" style="background:var(--surface-2);border:0.5px solid var(--border);color:var(--text-secondary)">4 VM · 1 template</span>
    <div style="margin-left:auto;display:flex;gap:6px">
      <button style="font-size:12px;padding:5px 10px"><i class="ti ti-terminal-2"></i> Shell</button>
      <button style="font-size:12px;padding:5px 10px;background:#FAEEDA;border-color:#EF9F27;color:#633806"><i class="ti ti-tool"></i> Mettre en maintenance</button>
      <button style="font-size:12px;padding:5px 10px"><i class="ti ti-dots"></i></button>
    </div>
  </div>
  <div class="nav" style="display:flex;gap:2px;border-bottom:0.5px solid var(--border);margin-bottom:14px;padding-bottom:6px;overflow:hidden">
    <a class="on" href="#"><i class="ti ti-layout-dashboard"></i>Résumé</a>
    <a href="#"><i class="ti ti-box"></i>VM</a>
    <a href="#"><i class="ti ti-database"></i>Disques</a>
    <a href="#"><i class="ti ti-network"></i>Réseau</a>
    <a href="#"><i class="ti ti-refresh"></i>Mises à jour</a>
    <a href="#"><i class="ti ti-shield"></i>Pare-feu</a>
  </div>
  <div style="display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:10px;margin-bottom:14px">
    <div class="card"><p style="font-size:11px;color:var(--text-secondary);margin:0 0 4px">CPU</p><p style="font-size:20px;font-weight:500;margin:0">3,1 %<span style="font-size:11px;color:var(--text-muted);font-weight:400"> · 32 c</span></p><div style="height:3px;background:var(--surface-0);border-radius:2px;margin-top:8px"><div style="width:3%;height:3px;background:#378ADD;border-radius:2px"></div></div></div>
    <div class="card"><p style="font-size:11px;color:var(--text-secondary);margin:0 0 4px">Mémoire</p><p style="font-size:20px;font-weight:500;margin:0">18<span style="font-size:11px;color:var(--text-muted);font-weight:400"> / 128 GiB</span></p><div style="height:3px;background:var(--surface-0);border-radius:2px;margin-top:8px"><div style="width:14%;height:3px;background:#378ADD;border-radius:2px"></div></div></div>
    <div class="card"><p style="font-size:11px;color:var(--text-secondary);margin:0 0 4px">Stockage local</p><p style="font-size:20px;font-weight:500;margin:0">412<span style="font-size:11px;color:var(--text-muted);font-weight:400"> / 1,8 TiB</span></p><div style="height:3px;background:var(--surface-0);border-radius:2px;margin-top:8px"><div style="width:23%;height:3px;background:#378ADD;border-radius:2px"></div></div></div>
    <div class="card"><p style="font-size:11px;color:var(--text-secondary);margin:0 0 4px">Load average</p><p style="font-size:20px;font-weight:500;margin:0">0,84<span style="font-size:11px;color:var(--text-muted);font-weight:400"> · 0,91 · 0,88</span></p><div style="height:3px;background:var(--surface-0);border-radius:2px;margin-top:8px"><div style="width:3%;height:3px;background:#378ADD;border-radius:2px"></div></div></div>
  </div>
  <div style="display:grid;grid-template-columns:minmax(0,1.5fr) minmax(0,1fr);gap:10px;margin-bottom:14px">
    <div class="card">
      <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:6px"><span style="font-size:12px;font-weight:500">Charge CPU du nœud</span><span style="font-size:11px;color:var(--text-muted)">Dernière heure</span></div>
      <svg viewBox="0 0 300 70" width="100%" height="70">
        <polygon points="0,70 0,48 30,50 60,44 90,52 120,40 150,46 180,30 210,44 240,42 270,46 300,44 300,70" fill="#E6F1FB"/>
        <polyline points="0,48 30,50 60,44 90,52 120,40 150,46 180,30 210,44 240,42 270,46 300,44" fill="none" stroke="#378ADD" stroke-width="1.5"/>
        <circle cx="300" cy="44" r="3" fill="#378ADD"/>
      </svg>
      <div style="display:flex;justify-content:space-between;font-size:10px;color:var(--text-muted)"><span>11:00</span><span>11:30</span><span>12:00</span></div>
    </div>
    <div class="card" style="padding:4px 12px">
      <div class="kv"><span>Cluster</span><span>Qualification</span></div>
      <div class="kv"><span>Quorum</span><span style="color:var(--text-success)">OK · 3/3 votes</span></div>
      <div class="kv"><span>HA</span><span style="color:var(--text-success)">actif</span></div>
      <div class="kv" style="border:0"><span>Kernel</span><span style="font-family:var(--font-mono);font-size:12px">6.14.8-2-pve</span></div>
    </div>
  </div>
  <div class="card">
    <div style="display:flex;gap:14px;align-items:center;margin-bottom:6px"><span style="font-size:12px;font-weight:500">Machines virtuelles sur ce nœud</span><span style="margin-left:auto;font-size:11px;color:var(--text-muted)">Migration à la maintenance : automatique (HA)</span></div>
    <div class="tr" style="border:0;color:var(--text-muted);font-size:11px"><span>ID</span><span>Nom</span><span>CPU</span><span>RAM</span><span>État</span></div>
    <div class="tr"><span>100</span><span>sli-testproxmox-qul</span><span>0,1 %</span><span>0,9 GiB</span><span class="tag" style="background:#E1F5EE;color:#085041;justify-self:start">running</span></div>
    <div class="tr"><span>102</span><span>sli-testproxmox-2-qul</span><span>0,2 %</span><span>1,1 GiB</span><span class="tag" style="background:#E1F5EE;color:#085041;justify-self:start">running</span></div>
    <div class="tr"><span>103</span><span>sli-airflow-sep-exp-2601-qul</span><span>0,25 %</span><span>1,25 GiB</span><span class="tag" style="background:#E1F5EE;color:#085041;justify-self:start">running</span></div>
    <div class="tr"><span>101</span><span>template-rocky10</span><span>—</span><span>—</span><span class="tag" style="background:var(--surface-1);color:var(--text-secondary);justify-self:start">template</span></div>
  </div>
</div>
</div>
</div>
```

### A.3 Modal de confirmation de maintenance (écran 3)

```html
<style>
.row{display:grid;grid-template-columns:36px 1fr 20px 1fr 60px;gap:8px;font-size:12px;padding:8px 0;border-top:0.5px solid var(--border);align-items:center}
.tag{font-size:11px;padding:2px 8px;border-radius:999px;font-weight:500;white-space:nowrap}
.opt{display:flex;gap:10px;align-items:flex-start;font-size:13px;padding:8px 0}
</style>
<div style="min-height:420px;background:rgba(0,0,0,0.45);display:flex;align-items:center;justify-content:center;border-radius:12px;padding:24px">
<div style="width:520px;max-width:100%;background:var(--surface-2);border-radius:12px;border:0.5px solid var(--border);padding:18px 20px">
  <div style="display:flex;align-items:center;gap:10px;margin-bottom:6px">
    <div style="width:32px;height:32px;border-radius:8px;background:#FAEEDA;color:#854F0B;display:flex;align-items:center;justify-content:center"><i class="ti ti-tool" style="font-size:18px"></i></div>
    <span style="font-size:16px;font-weight:500">Mettre prox-qual-2201-cit en maintenance</span>
  </div>
  <p style="font-size:13px;color:var(--text-secondary);margin:0 0 14px;line-height:1.5">Le nœud restera dans le quorum mais n'acceptera plus de nouvelles VM. Les VM gérées par HA seront migrées à chaud vers les nœuds ci-dessous.</p>
  <div style="display:grid;grid-template-columns:36px 1fr 20px 1fr 60px;gap:8px;font-size:11px;color:var(--text-muted)"><span>ID</span><span>VM</span><span></span><span>Destination</span><span>RAM</span></div>
  <div class="row"><span>100</span><span>sli-testproxmox-qul</span><i class="ti ti-arrow-right" style="color:var(--text-muted)"></i><span>prox-qual-2202-cit</span><span>0,9 GiB</span></div>
  <div class="row"><span>102</span><span>sli-testproxmox-2-qul</span><i class="ti ti-arrow-right" style="color:var(--text-muted)"></i><span>prox-qual-2203-cit</span><span>1,1 GiB</span></div>
  <div class="row"><span>103</span><span>sli-airflow-sep-exp-2601-qul</span><i class="ti ti-arrow-right" style="color:var(--text-muted)"></i><span>prox-qual-2202-cit</span><span>1,25 GiB</span></div>
  <div class="row" style="color:var(--text-muted)"><span>101</span><span>template-rocky10</span><span></span><span>reste sur place</span><span class="tag" style="background:var(--surface-1);color:var(--text-secondary);justify-self:start">template</span></div>
  <div style="display:flex;gap:8px;align-items:center;margin:14px 0 6px;padding:8px 10px;background:#E1F5EE;border-radius:var(--radius);font-size:12px;color:#085041"><i class="ti ti-check"></i>Capacité suffisante : 2202 passe à 34 % de RAM, 2203 à 29 %.</div>
  <div style="border-top:0.5px solid var(--border);margin-top:8px">
    <label class="opt"><input type="checkbox" checked style="margin-top:3px"> <span>Migrer aussi les VM hors HA <span style="color:var(--text-muted)">(aucune sur ce nœud)</span></span></label>
    <label class="opt" style="border-top:0.5px solid var(--border)"><input type="checkbox" style="margin-top:3px"> <span>Redémarrer le nœud une fois vide <span style="color:var(--text-muted)">(utile après une mise à jour du kernel)</span></span></label>
  </div>
  <div style="display:flex;gap:8px;justify-content:flex-end;margin-top:14px">
    <button style="font-size:13px;padding:7px 14px">Annuler</button>
    <button style="font-size:13px;padding:7px 14px;background:#854F0B;border-color:#854F0B;color:#FAEEDA"><i class="ti ti-tool"></i> Lancer la maintenance</button>
  </div>
</div>
</div>
```

### A.4 Vue d'ensemble des clusters (écran 4)

```html
<style>
.tag{font-size:11px;padding:2px 8px;border-radius:999px;font-weight:500;white-space:nowrap}
.card{background:var(--surface-2);border:0.5px solid var(--border);border-radius:12px;padding:14px 16px}
.m{display:flex;justify-content:space-between;font-size:12px;padding:5px 0}
.m span:first-child{color:var(--text-secondary)}
.bar{height:4px;background:var(--surface-0);border-radius:2px;margin:2px 0 8px}
.nd{display:flex;align-items:center;gap:6px;font-size:12px;padding:5px 0;border-top:0.5px solid var(--border)}
.dot{width:7px;height:7px;border-radius:50%;flex:none}
.al{display:flex;gap:8px;align-items:center;font-size:12px;padding:8px 10px;border-radius:var(--radius);margin-top:10px}
</style>
<div style="padding:4px 0">
<div style="display:flex;align-items:center;gap:10px;margin-bottom:14px">
  <span style="font-size:18px;font-weight:500">Clusters</span>
  <span class="tag" style="background:var(--surface-2);border:0.5px solid var(--border);color:var(--text-secondary)">11 nœuds</span>
  <span class="tag" style="background:var(--surface-2);border:0.5px solid var(--border);color:var(--text-secondary)">148 VM</span>
  <span class="tag" style="background:#FAEEDA;color:#633806"><i class="ti ti-alert-triangle" style="font-size:11px"></i> 2 alertes</span>
  <button style="margin-left:auto;font-size:12px;padding:5px 10px"><i class="ti ti-plus"></i> Ajouter un cluster</button>
</div>
<div style="display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:12px">
<div class="card">
  <div style="display:flex;align-items:center;gap:8px;margin-bottom:10px"><span class="dot" style="background:#1D9E75"></span><span style="font-size:15px;font-weight:500">Qualification</span><span class="tag" style="margin-left:auto;background:#E1F5EE;color:#085041">Sain</span></div>
  <div class="m"><span>CPU</span><span>4 %</span></div><div class="bar"><div style="width:4%;height:4px;background:#378ADD;border-radius:2px"></div></div>
  <div class="m"><span>Mémoire</span><span>61 / 384 GiB</span></div><div class="bar"><div style="width:16%;height:4px;background:#378ADD;border-radius:2px"></div></div>
  <div class="m"><span>Stockage</span><span>1,2 / 5,4 TiB</span></div><div class="bar"><div style="width:22%;height:4px;background:#378ADD;border-radius:2px"></div></div>
  <div class="m" style="margin-top:4px"><span>VM</span><span>12 en cours · 1 template</span></div>
  <div class="nd"><span class="dot" style="background:#1D9E75"></span>prox-qual-2201-cit</div>
  <div class="nd"><span class="dot" style="background:#1D9E75"></span>prox-qual-2202-cit</div>
  <div class="nd"><span class="dot" style="background:#1D9E75"></span>prox-qual-2203-cit</div>
  <div class="al" style="background:var(--surface-1);color:var(--text-secondary)"><i class="ti ti-check"></i>Quorum 3/3 · aucune alerte</div>
</div>
<div class="card" style="border:2px solid #EF9F27">
  <div style="display:flex;align-items:center;gap:8px;margin-bottom:10px"><span class="dot" style="background:#EF9F27"></span><span style="font-size:15px;font-weight:500">Préproduction</span><span class="tag" style="margin-left:auto;background:#FAEEDA;color:#633806">Dégradé</span></div>
  <div class="m"><span>CPU</span><span>31 %</span></div><div class="bar"><div style="width:31%;height:4px;background:#378ADD;border-radius:2px"></div></div>
  <div class="m"><span>Mémoire</span><span>212 / 256 GiB</span></div><div class="bar"><div style="width:83%;height:4px;background:#EF9F27;border-radius:2px"></div></div>
  <div class="m"><span>Stockage</span><span>3,9 / 8 TiB</span></div><div class="bar"><div style="width:49%;height:4px;background:#378ADD;border-radius:2px"></div></div>
  <div class="m" style="margin-top:4px"><span>VM</span><span>44 en cours · 2 arrêtées</span></div>
  <div class="nd"><span class="dot" style="background:#1D9E75"></span>prox-pprd-2301-cit</div>
  <div class="nd"><span class="dot" style="background:#EF9F27"></span>prox-pprd-2302-cit<span class="tag" style="margin-left:auto;background:#FAEEDA;color:#633806">maintenance</span></div>
  <div class="nd"><span class="dot" style="background:#1D9E75"></span>prox-pprd-2303-cit</div>
  <div class="al" style="background:#FAEEDA;color:#633806"><i class="ti ti-alert-triangle"></i>Mémoire à 83 % sur 2 nœuds pendant la maintenance</div>
</div>
<div class="card">
  <div style="display:flex;align-items:center;gap:8px;margin-bottom:10px"><span class="dot" style="background:#1D9E75"></span><span style="font-size:15px;font-weight:500">Production</span><span class="tag" style="margin-left:auto;background:#E1F5EE;color:#085041">Sain</span></div>
  <div class="m"><span>CPU</span><span>22 %</span></div><div class="bar"><div style="width:22%;height:4px;background:#378ADD;border-radius:2px"></div></div>
  <div class="m"><span>Mémoire</span><span>418 / 1 024 GiB</span></div><div class="bar"><div style="width:41%;height:4px;background:#378ADD;border-radius:2px"></div></div>
  <div class="m"><span>Stockage</span><span>14 / 32 TiB</span></div><div class="bar"><div style="width:44%;height:4px;background:#378ADD;border-radius:2px"></div></div>
  <div class="m" style="margin-top:4px"><span>VM</span><span>89 en cours · 3 templates</span></div>
  <div class="nd"><span class="dot" style="background:#1D9E75"></span>prox-prod-2401-cit</div>
  <div class="nd"><span class="dot" style="background:#1D9E75"></span>prox-prod-2402-cit</div>
  <div class="nd" style="color:var(--text-muted)"><i class="ti ti-dots" style="font-size:12px"></i>3 autres nœuds</div>
  <div class="al" style="background:#FAEEDA;color:#633806"><i class="ti ti-refresh"></i>Mise à jour 9.2.12 disponible sur 5 nœuds</div>
</div>
</div>
</div>
```

## Annexe B — Prompt de démarrage suggéré pour Claude Code

```
Lis docs/PROXMOX_UI_HANDOFF.md en entier. C'est la spec d'une surcouche web
multi-cluster pour Proxmox VE, conçue en amont. Respecte les décisions de design
du §2 et les contraintes du §4. Commence par le §6 étape 1 (init du repo) puis
propose-moi un plan détaillé pour l'étape 2 (backend agrégateur) avant de coder.
Utilise des données mockées tant qu'aucun cluster n'est configuré.
```
