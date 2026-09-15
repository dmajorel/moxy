/**
 * Every string the interface shows, in both languages it speaks.
 *
 * WHY A CATALOGUE WRITTEN BY HAND, and not an i18n library. lib/format.ts
 * already explains why this codebase formats its numbers without `Intl`: ICU
 * output drifts between Node builds and these strings are asserted character by
 * character. The same reasoning applies to the lookup, which is a property
 * access, and to the interpolation, which is one regular expression. A library
 * would bring a loader, a plural engine and an async boundary for that.
 *
 * WHAT MAKES IT COMPLETE. `fr` is the source: it is declared `as const`, and
 * `MessageKey` is derived from it. `en` is then declared as
 * `Record<MessageKey, string>`, so a key added to French and forgotten in
 * English is a type error — `make check-web` fails at `typecheck`, with no test
 * to write and nothing to remember. That is the same mechanical discipline that
 * binds `aggregate/model.go` to `api/types.ts`.
 *
 * WHAT IS NOT HERE. Punctuation and number typography are code, not words: the
 * decimal separator, the narrow no-break space before a French `%` and the
 * grouping of thousands live in lib/format.ts, where the figures are built.
 * Language names are not here either — "Français" stays "Français" in an
 * English menu, which is what lets someone find their own language in an
 * interface they cannot read.
 *
 * STYLE. Sentence case in both languages, as section 2 of the handoff imposes,
 * and French keeps the space it puts before `:` `;` `?` `!`.
 *
 * Every French string here is the one its component used to hold, character for
 * character. That includes an inconsistency the codebase already had: the old
 * `lib/format.ts` and `screens/MaintenancePlanDialog.tsx` wrote a straight
 * apostrophe throughout, everything else a typographic one. Moving the strings
 * was not the moment to change what they say — settling on one apostrophe is
 * its own change, worth making deliberately rather than as a side effect of a
 * translation.
 */
import type { Locale } from "@/lib/lang";

/**
 * French, the source language.
 *
 * Keys are dotted and grouped by where they are read, not by what they mean: a
 * string is looked up from the component that shows it.
 */
const fr = {
  // ---------------------------------------------------------------- units
  // The base unit only. KiB…PiB are IEC prefixes and are not translated.
  "unit.bytes": "o",
  "unit.cores": "c",
  "unit.vcpus": "vCPU",
  "unit.day": "j",
  "unit.hour": "h",
  "unit.minute": "min",
  "unit.second": "s",

  // ----------------------------------------------------------------- list
  // Joins the last item of an enumeration: `9.2.9 et 9.2.12`.
  "list.and": "et",

  // ----------------------------------------------------------------- time
  "time.justNow": "à l'instant",
  "time.ago": "il y a {duration}",
  "time.dateAt": "{date} à {time}",

  // -------------------------------------------------------------- plurals
  // Read by `plural(count, noun)`; the count is prepended by the formatter.
  "plural.cluster.one": "cluster",
  "plural.cluster.other": "clusters",
  "plural.node.one": "nœud",
  "plural.node.other": "nœuds",
  // Invariable in French: only the count changes.
  "plural.vm.one": "VM",
  "plural.vm.other": "VM",
  "plural.alert.one": "alerte",
  "plural.alert.other": "alertes",
  "plural.guest.one": "invité",
  "plural.guest.other": "invités",
  "plural.template.one": "modèle",
  "plural.template.other": "modèles",
  // Agrees with "machine", which is what the cluster card counts.
  "plural.stopped.one": "arrêtée",
  "plural.stopped.other": "arrêtées",
  "plural.package.one": "paquet",
  "plural.package.other": "paquets",
  "plural.disk.one": "disque",
  "plural.disk.other": "disques",
  "plural.net.one": "interface",
  "plural.net.other": "interfaces",
  "plural.detachedVolume.one": "volume détaché",
  "plural.detachedVolume.other": "volumes détachés",
  "plural.result.one": "résultat",
  "plural.result.other": "résultats",

  // --------------------------------------------------------------- status
  "status.node.online": "En ligne",
  "status.node.offline": "Hors ligne",
  "status.node.maintenance": "Maintenance",
  "status.node.unknown": "Inconnu",
  "status.guest.running": "En cours",
  "status.guest.stopped": "Arrêtée",
  "status.guest.template": "Modèle",
  "status.cluster.healthy": "Sain",
  "status.cluster.degraded": "Dégradé",
  "status.cluster.unreachable": "Injoignable",
  // The dot has a vocabulary of its own: it labels nodes, guests and clusters
  // with one table, so "En maintenance" reads as a sentence next to a name.
  "status.dot.healthy": "Sain",
  "status.dot.online": "En ligne",
  "status.dot.running": "En cours",
  "status.dot.degraded": "Dégradé",
  "status.dot.maintenance": "En maintenance",
  "status.dot.unreachable": "Injoignable",
  "status.dot.offline": "Hors ligne",
  "status.dot.stopped": "Arrêté",
  "status.dot.unknown": "État inconnu",

  // ----------------------------------------------------------- guest kind
  "guestKind.qemu": "Machine virtuelle",
  "guestKind.lxc": "Conteneur LXC",
  "guestKind.unknown": "Invité",

  // --------------------------------------------------------------- quorum
  "quorum.standalone": "Nœud seul",
  "quorum.ok": "OK",
  "quorum.lost": "Perdu",
  "quorum.votes": "{verdict} · {online}/{nodes} votes",

  // ------------------------------------------------------------- HA state
  // The CRM's own vocabulary, for a resource and for a node.
  "ha.started": "Démarré",
  "ha.stopped": "Arrêté",
  "ha.disabled": "Désactivé",
  "ha.ignored": "Ignoré",
  "ha.error": "Erreur",
  "ha.fence": "Isolation",
  "ha.freeze": "Gelé",
  "ha.migrate": "Migration",
  "ha.relocate": "Relocalisation",
  "ha.online": "Actif",
  "ha.maintenance": "En maintenance",
  "ha.unknown": "Inconnu",
  "ha.gone": "Disparu",

  // ------------------------------------------ why a cluster cannot be read
  "errorKind.tokenRefused": "jeton refusé",
  "errorKind.insufficientRights": "droits insuffisants",
  "errorKind.authRefused": "authentification refusée",
  "errorKind.tls": "certificat non vérifiable",
  "errorKind.timeout": "délai dépassé",
  "errorKind.network": "réseau injoignable",
  "errorKind.protocol": "réponse inattendue",

  // --------------------------------------------------------------- alerts
  "alert.generic": "Alerte",
  "alert.quorumLost": "Quorum perdu",
  "alert.unreachable": "Cluster injoignable",
  "alert.on": " sur {count}",
  "alert.nodeOffline": "{count} hors ligne",
  "alert.nodeOfflineOne": "Nœud hors ligne",
  "alert.nodeUnknown": "{count} dans un état inconnu",
  "alert.nodeUnknownOne": "Nœud dans un état inconnu",
  "alert.memoryHigh": "Mémoire élevée{on}",
  "alert.memoryAtMax": "Mémoire à {ratio}{on} (max.)",
  "alert.memoryAt": "Mémoire à {ratio}",
  "alert.updatesAvailable": "Mise à jour{version} disponible{on}",
  "alert.updatesUnevenBounded":
    "Mises à jour inégales : de {min} à {max} paquets en attente selon les nœuds",
  "alert.updatesUneven": "Mises à jour inégales entre les nœuds",
  "alert.versionsUneven": "Versions Proxmox inégales entre les nœuds",
  "alert.versionsUnevenList": "Versions Proxmox inégales : {versions}",
  "alert.versionsUnevenSpread": "{count} versions, de {first} à {last}",
  "alert.statsUnavailable": "Mesures CPU et mémoire indisponibles{on}",

  // ---------------------------------------------------------------- tasks
  "task.outcome.running": "En cours",
  "task.outcome.ok": "OK",
  "task.outcome.warnings": "Avertissements",
  "task.outcome.failed": "Échec",
  "task.outcome.unknown": "Alerte",
  "task.type.vzdump": "Sauvegarde",
  "task.type.qmstart": "Démarrage",
  "task.type.qmstop": "Arrêt",
  "task.type.qmshutdown": "Extinction",
  "task.type.qmreboot": "Redémarrage",
  "task.type.qmigrate": "Migration",
  "task.type.qmclone": "Clonage",
  "task.type.qmcreate": "Création",
  "task.type.qmdestroy": "Suppression",
  "task.type.qmsnapshot": "Instantané",
  "task.type.vzstart": "Démarrage",
  "task.type.vzstop": "Arrêt",
  "task.type.vzshutdown": "Extinction",
  "task.type.vzmigrate": "Migration",
  "task.type.vzcreate": "Création",
  "task.type.vzdestroy": "Suppression",
  "task.type.aptupdate": "Mise à jour des paquets",
  "task.type.srvstart": "Démarrage du service",
  "task.type.srvstop": "Arrêt du service",
  "task.type.srvreload": "Rechargement du service",
  "task.type.srvrestart": "Redémarrage du service",
  "task.type.imgcopy": "Copie d'image",
  "task.type.imgdel": "Suppression d'image",
  "task.type.download": "Téléchargement",
  "task.type.hamigrate": "Migration HA",
  "task.type.harelocate": "Relocalisation HA",
  "task.type.auth_realm_sync": "Synchronisation d'annuaire",
  "task.type.auth-realm-sync": "Synchronisation d'annuaire",
  "task.type.startall": "Démarrage groupé",
  "task.type.stopall": "Arrêt groupé",
  "task.type.migrateall": "Migration groupée",
  "task.type.spiceproxy": "Console SPICE",
  "task.type.vncproxy": "Console",
  "task.type.termproxy": "Terminal",
  "task.type.qmresume": "Reprise",
  "task.type.qmsuspend": "Suspension",
  "task.type.qmpause": "Mise en pause",
  "task.type.qmtemplate": "Conversion en modèle",
  "task.type.qmrestore": "Restauration",
  "task.type.qmsnapshotdelete": "Suppression d'instantané",
  "task.type.qmdelsnapshot": "Suppression d'instantané",
  "task.type.qmrollback": "Retour à un instantané",
  "task.type.qmmove": "Déplacement de disque",
  "task.type.qmconfig": "Modification de configuration",
  "task.type.qmreset": "Réinitialisation",
  "task.type.vzrestore": "Restauration",
  "task.type.vzsnapshot": "Instantané",
  "task.type.vzdelsnapshot": "Suppression d'instantané",
  "task.type.vzrollback": "Retour à un instantané",
  "task.type.vzclone": "Clonage",
  "task.type.vzreboot": "Redémarrage",
  "task.type.vzsuspend": "Suspension",
  "task.type.vzresume": "Reprise",
  "task.type.vztemplate": "Conversion en modèle",
  "task.type.vzmount": "Montage",
  "task.type.vzumount": "Démontage",
  "task.type.hastart": "Démarrage HA",
  "task.type.hastop": "Arrêt HA",
  "task.type.hashutdown": "Extinction HA",
  "task.type.resize": "Redimensionnement",
  "task.type.move_volume": "Déplacement de volume",
  "task.type.move_disk": "Déplacement de disque",
  "task.type.imgdelete": "Suppression d'image",
  "task.type.unknownimgdel": "Suppression d'image orpheline",
  "task.type.wipedisk": "Effacement de disque",
  "task.type.acmenewcert": "Nouveau certificat ACME",
  "task.type.acmerenew": "Renouvellement ACME",
  "task.type.acmerevoke": "Révocation ACME",
  "task.type.cephcreateosd": "Création d'OSD Ceph",
  "task.type.cephdestroyosd": "Suppression d'OSD Ceph",
  "task.type.cephcreatepool": "Création de pool Ceph",
  "task.type.cephdestroypool": "Suppression de pool Ceph",
  "task.type.cephcreatemon": "Création de moniteur Ceph",
  "task.type.cephdestroymon": "Suppression de moniteur Ceph",
  "task.type.cephcreatemds": "Création de MDS Ceph",
  "task.type.cephdestroymds": "Suppression de MDS Ceph",
  "task.type.cephfscreate": "Création de CephFS",
  "task.type.clusterjoin": "Adhésion au cluster",
  "task.type.clustercreate": "Création du cluster",
  "task.type.reboot": "Redémarrage du nœud",
  "task.type.shutdown": "Extinction du nœud",
  "task.type.pull_file": "Copie de fichier",
  "task.type.push_file": "Copie de fichier",
  "task.type.dircreate": "Création de répertoire",
  "task.type.diskinit": "Initialisation de disque",
  "task.type.lvmcreate": "Création de volume LVM",
  "task.type.lvmthincreate": "Création de pool LVM-thin",
  "task.type.zfscreate": "Création de pool ZFS",
  "task.type.unknown": "Tâche",

  // ----------------------------------------------------------- timeframes
  "timeframe.hour": "Dernière heure",
  "timeframe.day": "Dernières 24 h",
  "timeframe.week": "7 derniers jours",
  "timeframe.month": "30 derniers jours",
  "timeframe.year": "Dernière année",
  "timeframe.short.hour": "1 h",
  "timeframe.short.day": "24 h",
  "timeframe.short.week": "7 j",
  "timeframe.short.month": "30 j",
  "timeframe.short.year": "1 an",

  // ----------------------------------------------------------- allocation
  "allocation.allocated": "· alloué",
  "allocation.atLeast": "· au moins",

  // -------------------------------------------------------- node updates
  "nodeUpdates.upToDate": "À jour",
  "nodeUpdates.pending": "{count} en attente",

  // --------------------------------------------------------------- search
  "search.noResult": "Aucun résultat",

  // ---------------------------------------------------------------- shell
  "shell.skipToContent": "Aller au contenu",
  "shell.clusters": "Clusters",
  "shell.sidebarWidth": "Largeur du panneau de navigation",

  // --------------------------------------------------------------- topBar
  "topBar.searchPlaceholder": "Rechercher une VM ou un nœud…",
  "topBar.searchLabel": "Recherche globale",

  // --------------------------------------------------------------- alerts
  "alerts.notifications": "Notifications",
  "alerts.buttonLabel": "Notifications · {alerts}",
  "alerts.menuLabel": "Alertes",
  "alerts.empty": "Aucune alerte",

  // ------------------------------------------------------ cluster switcher
  "clusterSwitcher.all": "Tous les clusters",
  "clusterSwitcher.menuLabel": "Clusters",

  // ----------------------------------------------------------------- tree
  "tree.label": "Arborescence des clusters",
  "tree.empty": "Aucun cluster configuré",
  "tree.maintenanceIcon": "Maintenance planifiée",

  // ---------------------------------------------------------------- theme
  "theme.label": "Thème",
  "theme.light": "Clair",
  "theme.dark": "Sombre",
  "theme.system": "Système",

  // ------------------------------------------------------------- language
  "lang.label": "Langue",
  "lang.system": "Langue du navigateur",

  // ----------------------------------------------------------- state views
  "state.loading": "Chargement…",
  "state.retry": "Réessayer",
  "state.backToOverview": "Retour à la vue d’ensemble",
  "state.technicalDetail": "Détail technique",
  "state.stale": "Données précédentes · connexion perdue",
  "state.staleAt": "Données du {stamp} · connexion perdue",

  // --------------------------------------------------------------- errors
  "error.unreachable.title": "moxy est injoignable",
  "error.unreachable.body":
    "Le service moxy n’a pas répondu. Vérifiez qu’il est démarré et que les " +
    "clusters sont joignables, puis réessayez.",
  "error.unauthorized.title": "Authentification requise",
  "error.unauthorized.body":
    "moxy a refusé la requête faute d’authentification. Saisissez le jeton " +
    "d’accès, ou vérifiez que le proxy d’authentification est bien en place.",
  "error.notFound.title": "Objet introuvable",
  "error.notFound.body":
    "Ce nœud ou cette machine n’existe plus dans le cluster : supprimé, " +
    "renommé, ou migré ailleurs ? La vue d’ensemble dit ce qui s’y trouve " +
    "encore.",
  "error.forbidden.title": "Droits insuffisants sur ce nœud",
  "error.forbidden.body":
    "Proxmox a refusé la requête. Le token a besoin de Sys.Audit sur /nodes, " +
    "et un rôle posé sur /nodes remplace celui hérité de / au lieu de s’y " +
    "ajouter : un rôle ne portant que Sys.Modify efface Sys.Audit. " +
    "Voir « Privilèges PVE requis » dans le README.",
  "error.upstream.title": "Cluster injoignable",
  "error.upstream.body":
    "moxy répond, mais le cluster PVE ne répond pas. Le journal du serveur " +
    "dit pourquoi ; la vue d’ensemble continue d’afficher son dernier état " +
    "connu.",
  "error.timeout.title": "Délai dépassé côté cluster",
  "error.timeout.body":
    "Le cluster PVE n’a pas répondu dans le temps imparti. Il est peut-être " +
    "surchargé ; réessayez dans un instant.",
  "error.unsupported.title": "Indisponible sans connexion au cluster",
  "error.unsupported.body":
    "moxy tourne sans connexion à ce cluster : cet écran a besoin d’un " +
    "cluster PVE configuré pour dire quoi que ce soit.",
  "error.invalid.title": "Requête invalide",
  "error.invalid.body":
    "moxy a refusé cette requête : un identifiant, une période ou une limite " +
    "n’est pas acceptable. Le détail technique ci-dessous nomme le paramètre " +
    "en cause.",
  "error.internal.title": "Erreur interne de moxy",
  "error.internal.body":
    "Le service a échoué en traitant la requête. Le journal du serveur en " +
    "dit plus ; le détail technique ci-dessous donne le code.",
  "error.unreadable.title": "Réponse inattendue",
  "error.unreadable.body":
    "La réponse reçue n’est pas celle de moxy : un proxy renvoie-t-il une " +
    "page HTML à sa place ? Le détail technique ci-dessous dit quelle " +
    "requête l’a reçue.",
  "error.unknown.title": "Impossible de charger les données",
  "error.unknown.body":
    "Une erreur inattendue s’est produite. Le détail technique ci-dessous en " +
    "dit plus ; réessayez.",

  // ---------------------------------------------------------------- login
  "login.title": "Authentification requise",
  "login.intro":
    "Saisissez le jeton d’accès configuré sur ce serveur pour consulter les " +
    "clusters.",
  "login.field": "Jeton d’accès",
  "login.submit": "Se connecter",
  "login.submitting": "Connexion…",
  "login.shared":
    "Ce jeton est partagé : il autorise l’accès, il n’identifie personne. " +
    "Les actions faites depuis moxy ne sont donc attribuées à aucun compte.",
  "login.refused":
    "Jeton refusé. Vérifiez la valeur transmise par l’administrateur de ce " +
    "serveur.",

  // ------------------------------------------------------------- overview
  "overview.title": "Clusters",

  // --------------------------------------------------------- cluster card
  "card.open": "Ouvrir {name}",
  "card.cpu": "CPU",
  "card.memory": "Mémoire",
  "card.storage": "Stockage",
  "card.vms": "VM",
  "card.noVm": "Aucune VM",
  "card.running": "{count} en cours",
  "card.lastHour": "Dernière heure",
  "card.chartLabel":
    "Utilisation de {name} sur la dernière heure : CPU {cpu}, mémoire {memory}",
  "card.nodes": "Nœuds",
  "card.nodesCaption": "Nœuds de {name}",
  "card.noNode": "Aucun nœud à afficher.",
  "card.quietNoAlert": "Aucune alerte",
  "card.quietQuorum": "Quorum {online}/{nodes} · aucune alerte",
  "card.noReading": "Aucune lecture disponible{suffix}",
  "card.staleReading": "Lecture ancienne · {relative}{suffix}",
  "card.column.node": "Nœud",
  "card.column.cpu": "CPU",
  "card.column.memory": "Mémoire",
  // The version each node is RUNNING. "PVE" and not "Version": the same card
  // carries the version apt OFFERS in its update banner, and a bare "Version"
  // over a column of installed numbers would read as that one.
  "card.column.pveVersion": "PVE",
  "card.column.uptime": "En service",

  // ----------------------------------------------------------- node detail
  "node.breadcrumb": "Nœud",
  "node.planMaintenance": "Plan de maintenance",
  "node.metric.cpu": "CPU",
  "node.metric.memory": "Mémoire",
  "node.metric.localStorage": "Stockage local",
  "node.metric.loadAverage": "Load average",
  "node.loadAverageDetail": "· 1, 5, 15 min",
  "node.chartTitle": "Charge CPU du nœud",
  "node.chartLabel": "Charge CPU de {name}",
  "node.kv.cluster": "Cluster",
  "node.kv.quorum": "Quorum",
  "node.kv.ha": "HA",
  "node.kv.kernel": "Noyau",
  "node.kv.updates": "Mises à jour",
  "node.guests.title": "Invités sur ce nœud",
  "node.guests.drained":
    "Nœud en maintenance : les invités gérés par HA ont été migrés.",
  "node.guests.caption": "Invités hébergés par ce nœud",
  "node.guests.emptyDrained": "Ce nœud a été vidé par la mise en maintenance.",
  "node.guests.empty": "Aucun invité sur ce nœud.",
  "node.guests.open": "Ouvrir {name}",
  "node.column.vmid": "ID",
  "node.column.name": "Nom",
  "node.column.cpu": "CPU",
  "node.column.memory": "RAM",
  "node.column.status": "État",
  "node.updates.title": "Mises à jour en attente",
  "node.updates.group": "Paquets en attente",
  "node.updates.caption": "Paquets en attente de mise à jour",
  "node.updates.empty": "Aucun paquet en attente.",
  "node.updates.column.package": "Paquet",
  "node.updates.column.version": "Version",
  "node.updates.column.title": "Description",

  // ---------------------------------------------------------- guest detail
  "guest.metric.cpu": "CPU",
  "guest.metric.memory": "Mémoire",
  "guest.metric.bootDisk": "Disque de boot",
  "guest.metric.volumes": "Volumétrie",
  "guest.chartTitle": "Charge CPU",
  "guest.chartLabel": "Charge CPU de {name}",
  "guest.kv.node": "Nœud",
  "guest.kv.ha": "HA",
  "guest.kv.bootDisk": "Disque de boot",
  "guest.kv.hostMemory": "Mémoire hôte",
  "guest.kv.ipv4": "IPv4",
  "guest.disks.title": "Disques",
  "guest.nets.title": "Réseaux",
  "guest.detachedNote": "{volumes} · hors total",
  "guest.tags.title": "Étiquettes",
  "guest.tasks.title": "Tâches récentes",
  "guest.tasks.source": "Tâches de cette machine sur {node}",
  "guest.tasks.empty": "Aucune tâche récente pour cette machine.",

  // ----------------------------------------------------------- disks table
  "disks.column.slot": "Emplacement",
  "disks.column.storage": "Stockage",
  "disks.column.volume": "Volume",
  "disks.column.size": "Taille",
  "disks.caption": "Volumes déclarés par cet invité",
  "disks.empty": "Ce système ne déclare aucun disque.",
  "disks.detached": "Détaché",

  // ------------------------------------------------------------ nets table
  "nets.column.slot": "Interface",
  "nets.column.network": "Réseau",
  "nets.column.mac": "Adresse MAC",
  "nets.caption": "Réseaux auxquels cet invité est raccordé",
  "nets.empty": "Ce système ne déclare aucune interface.",

  // ----------------------------------------------------------- tasks table
  "tasks.column.time": "Heure",
  "tasks.column.label": "Description",
  "tasks.column.duration": "Durée",
  "tasks.column.outcome": "État",
  "tasks.caption": "Tâches récentes, de la plus récente à la plus ancienne",
  "tasks.empty": "Aucune tâche récente.",

  // --------------------------------------------------------------- journal
  "journal.title": "Journal du cluster",
  "journal.disconnected": "Connexion perdue",
  "journal.updated": "Mis à jour {relative}",
  "journal.unavailable": "Journal indisponible pour le moment.",

  // ----------------------------------------------------------------- chart
  "chart.windowLabel": "Fenêtre du graphe",
  "chart.average": "{timeframe} · moy. {value}",
  "chart.noData": "Aucune donnée",
  "chart.noDataLabel": "{label} — aucune donnée",

  // ------------------------------------------------------- maintenance plan
  "plan.title": "Mettre {node} en maintenance",
  "plan.close": "Fermer",
  "plan.intro":
    "Le nœud resterait dans le quorum mais n'accepterait plus de nouvelles " +
    "machines. Voici ce que deviendraient celles qu'il héberge, dans le " +
    "cluster {cluster} tel qu'il est en ce moment.",
  "plan.restartOne":
    "Un conteneur sera arrêté puis redémarré pendant sa migration : Proxmox " +
    "ne sait pas déplacer un conteneur à chaud.",
  "plan.restartMany":
    "{count} conteneurs seront arrêtés puis redémarrés pendant leur " +
    "migration : Proxmox ne sait pas déplacer un conteneur à chaud.",
  "plan.caption": "Invités à déplacer et leur destination",
  "plan.empty":
    "Ce nœud n'héberge aucune machine : il peut être drainé sans migration.",
  "plan.column.vmid": "ID",
  "plan.column.name": "Machine",
  "plan.column.target": "Destination",
  "plan.column.memory": "RAM",
  "plan.column.method": "Migration",
  "plan.noTarget": "Aucune destination",
  "plan.stayPut": "reste sur place",
  "plan.automatic": "Automatique",
  "plan.manual": "À la main",
  "plan.restart": "Redémarrage",
  "plan.offline": "Hors ligne",
  "plan.saturated": "déjà saturé",
  "plan.noMigration": "Aucune migration nécessaire.",
  "plan.capacityOk": "Capacité suffisante : {targets}.",
  "plan.targetAfter": "{name} passe à {ratio} de RAM",
  "plan.cannotDrain": "Ce nœud ne peut pas être drainé en l'état.",
  "plan.shortfallOne":
    "Une machine ne trouve aucune destination sous {threshold} de mémoire.",
  "plan.shortfallMany":
    "{count} machines ne trouvent aucune destination sous {threshold} de " +
    "mémoire.",
  "plan.handOverTitle": "À lancer sur un nœud du cluster",
  "plan.handOverBody":
    "moxy ne peut pas déclencher la maintenance lui-même : Proxmox n'expose " +
    "cette commande que par sa ligne de commande, jamais par son API REST. Le " +
    "plan ci-dessus décrit ce qui se passera une fois la commande lancée.",
  "plan.noManager":
    "Ce cluster n'a pas de gestionnaire HA : la commande ci-dessus ne " +
    "déplacera rien. Toutes les machines listées sont à migrer à la main.",
  "plan.manualOne":
    "Une machine n'est pas gérée par HA : le CRM ne la déplacera pas. À " +
    "migrer à la main, avant ou après.",
  "plan.manualMany":
    "{count} machines ne sont pas gérées par HA : le CRM ne les déplacera " +
    "pas. À migrer à la main, avant ou après.",
  "plan.blocker.noTarget":
    "Aucun autre nœud disponible pour recevoir les machines",
  "plan.blocker.sourceOffline":
    "Ce nœud est hors ligne : ses machines n'y tournent pas",
  "plan.blocker.targetStatsUnavailable":
    "La mémoire des nœuds de destination est inconnue : le token n'a pas " +
    "Sys.Audit sur /nodes, donc aucun placement ne peut être justifié",

  // ------------------------------------------------------------------- app
  "app.notFound.title": "Objet introuvable",
  "app.notFound.hint":
    "Cette adresse ne désigne ni un cluster, ni un nœud, ni une machine. " +
    "Revenez à la vue d’ensemble pour retrouver ce que moxy connaît.",
  "app.noCluster.title": "Aucun cluster à afficher",
  "app.noCluster.hint": "Ajoutez un cluster dans la configuration de moxyd.",
  "app.title.clusters": "Clusters",
  "app.title.notFound": "Objet introuvable",
} as const;

/** Every key the interface can ask for. Derived from French, the source. */
export type MessageKey = keyof typeof fr;

/**
 * English.
 *
 * Typed as `Record<MessageKey, string>` rather than inferred: that annotation
 * is the completeness check. A key added to `fr` and missing here fails the
 * typecheck, and a key here that `fr` does not have fails it too.
 */
const en: Record<MessageKey, string> = {
  // ---------------------------------------------------------------- units
  "unit.bytes": "B",
  "unit.cores": "c",
  "unit.vcpus": "vCPU",
  "unit.day": "d",
  "unit.hour": "h",
  "unit.minute": "min",
  "unit.second": "s",

  // ----------------------------------------------------------------- list
  "list.and": "and",

  // ----------------------------------------------------------------- time
  "time.justNow": "just now",
  "time.ago": "{duration} ago",
  "time.dateAt": "{date} at {time}",

  // -------------------------------------------------------------- plurals
  "plural.cluster.one": "cluster",
  "plural.cluster.other": "clusters",
  "plural.node.one": "node",
  "plural.node.other": "nodes",
  "plural.vm.one": "VM",
  "plural.vm.other": "VMs",
  "plural.alert.one": "alert",
  "plural.alert.other": "alerts",
  "plural.guest.one": "guest",
  "plural.guest.other": "guests",
  "plural.template.one": "template",
  "plural.template.other": "templates",
  "plural.stopped.one": "stopped",
  "plural.stopped.other": "stopped",
  "plural.package.one": "package",
  "plural.package.other": "packages",
  "plural.disk.one": "disk",
  "plural.disk.other": "disks",
  "plural.net.one": "interface",
  "plural.net.other": "interfaces",
  "plural.detachedVolume.one": "detached volume",
  "plural.detachedVolume.other": "detached volumes",
  "plural.result.one": "result",
  "plural.result.other": "results",

  // --------------------------------------------------------------- status
  "status.node.online": "Online",
  "status.node.offline": "Offline",
  "status.node.maintenance": "Maintenance",
  "status.node.unknown": "Unknown",
  "status.guest.running": "Running",
  "status.guest.stopped": "Stopped",
  "status.guest.template": "Template",
  "status.cluster.healthy": "Healthy",
  "status.cluster.degraded": "Degraded",
  "status.cluster.unreachable": "Unreachable",
  "status.dot.healthy": "Healthy",
  "status.dot.online": "Online",
  "status.dot.running": "Running",
  "status.dot.degraded": "Degraded",
  "status.dot.maintenance": "In maintenance",
  "status.dot.unreachable": "Unreachable",
  "status.dot.offline": "Offline",
  "status.dot.stopped": "Stopped",
  "status.dot.unknown": "Unknown state",

  // ----------------------------------------------------------- guest kind
  "guestKind.qemu": "Virtual machine",
  "guestKind.lxc": "LXC container",
  "guestKind.unknown": "Guest",

  // --------------------------------------------------------------- quorum
  "quorum.standalone": "Standalone node",
  "quorum.ok": "OK",
  "quorum.lost": "Lost",
  "quorum.votes": "{verdict} · {online}/{nodes} votes",

  // ------------------------------------------------------------- HA state
  "ha.started": "Started",
  "ha.stopped": "Stopped",
  "ha.disabled": "Disabled",
  "ha.ignored": "Ignored",
  "ha.error": "Error",
  "ha.fence": "Fencing",
  "ha.freeze": "Frozen",
  "ha.migrate": "Migrating",
  "ha.relocate": "Relocating",
  "ha.online": "Active",
  "ha.maintenance": "In maintenance",
  "ha.unknown": "Unknown",
  "ha.gone": "Gone",

  // ------------------------------------------ why a cluster cannot be read
  "errorKind.tokenRefused": "token refused",
  "errorKind.insufficientRights": "insufficient privileges",
  "errorKind.authRefused": "authentication refused",
  "errorKind.tls": "certificate cannot be verified",
  "errorKind.timeout": "timed out",
  "errorKind.network": "network unreachable",
  "errorKind.protocol": "unexpected response",

  // --------------------------------------------------------------- alerts
  "alert.generic": "Alert",
  "alert.quorumLost": "Quorum lost",
  "alert.unreachable": "Cluster unreachable",
  "alert.on": " on {count}",
  "alert.nodeOffline": "{count} offline",
  "alert.nodeOfflineOne": "Node offline",
  "alert.nodeUnknown": "{count} in an unknown state",
  "alert.nodeUnknownOne": "Node in an unknown state",
  "alert.memoryHigh": "High memory{on}",
  "alert.memoryAtMax": "Memory at {ratio}{on} (max.)",
  "alert.memoryAt": "Memory at {ratio}",
  "alert.updatesAvailable": "Update{version} available{on}",
  "alert.updatesUnevenBounded":
    "Uneven updates: {min} to {max} packages pending depending on the node",
  "alert.updatesUneven": "Uneven updates across the nodes",
  "alert.versionsUneven": "Uneven Proxmox versions across the nodes",
  "alert.versionsUnevenList": "Uneven Proxmox versions: {versions}",
  "alert.versionsUnevenSpread": "{count} versions, from {first} to {last}",
  "alert.statsUnavailable": "CPU and memory readings unavailable{on}",

  // ---------------------------------------------------------------- tasks
  "task.outcome.running": "Running",
  "task.outcome.ok": "OK",
  "task.outcome.warnings": "Warnings",
  "task.outcome.failed": "Failed",
  "task.outcome.unknown": "Alert",
  "task.type.vzdump": "Backup",
  "task.type.qmstart": "Start",
  "task.type.qmstop": "Stop",
  "task.type.qmshutdown": "Shutdown",
  "task.type.qmreboot": "Reboot",
  "task.type.qmigrate": "Migration",
  "task.type.qmclone": "Clone",
  "task.type.qmcreate": "Creation",
  "task.type.qmdestroy": "Deletion",
  "task.type.qmsnapshot": "Snapshot",
  "task.type.vzstart": "Start",
  "task.type.vzstop": "Stop",
  "task.type.vzshutdown": "Shutdown",
  "task.type.vzmigrate": "Migration",
  "task.type.vzcreate": "Creation",
  "task.type.vzdestroy": "Deletion",
  "task.type.aptupdate": "Package update",
  "task.type.srvstart": "Service start",
  "task.type.srvstop": "Service stop",
  "task.type.srvreload": "Service reload",
  "task.type.srvrestart": "Service restart",
  "task.type.imgcopy": "Image copy",
  "task.type.imgdel": "Image deletion",
  "task.type.download": "Download",
  "task.type.hamigrate": "HA migration",
  "task.type.harelocate": "HA relocation",
  "task.type.auth_realm_sync": "Realm sync",
  "task.type.auth-realm-sync": "Realm sync",
  "task.type.startall": "Bulk start",
  "task.type.stopall": "Bulk stop",
  "task.type.migrateall": "Bulk migration",
  "task.type.spiceproxy": "SPICE console",
  "task.type.vncproxy": "Console",
  "task.type.termproxy": "Terminal",
  "task.type.qmresume": "Resume",
  "task.type.qmsuspend": "Suspend",
  "task.type.qmpause": "Pause",
  "task.type.qmtemplate": "Conversion to template",
  "task.type.qmrestore": "Restore",
  "task.type.qmsnapshotdelete": "Snapshot deletion",
  "task.type.qmdelsnapshot": "Snapshot deletion",
  "task.type.qmrollback": "Snapshot rollback",
  "task.type.qmmove": "Disk move",
  "task.type.qmconfig": "Configuration change",
  "task.type.qmreset": "Reset",
  "task.type.vzrestore": "Restore",
  "task.type.vzsnapshot": "Snapshot",
  "task.type.vzdelsnapshot": "Snapshot deletion",
  "task.type.vzrollback": "Snapshot rollback",
  "task.type.vzclone": "Clone",
  "task.type.vzreboot": "Reboot",
  "task.type.vzsuspend": "Suspend",
  "task.type.vzresume": "Resume",
  "task.type.vztemplate": "Conversion to template",
  "task.type.vzmount": "Mount",
  "task.type.vzumount": "Unmount",
  "task.type.hastart": "HA start",
  "task.type.hastop": "HA stop",
  "task.type.hashutdown": "HA shutdown",
  "task.type.resize": "Resize",
  "task.type.move_volume": "Volume move",
  "task.type.move_disk": "Disk move",
  "task.type.imgdelete": "Image deletion",
  "task.type.unknownimgdel": "Orphan image deletion",
  "task.type.wipedisk": "Disk wipe",
  "task.type.acmenewcert": "New ACME certificate",
  "task.type.acmerenew": "ACME renewal",
  "task.type.acmerevoke": "ACME revocation",
  "task.type.cephcreateosd": "Ceph OSD creation",
  "task.type.cephdestroyosd": "Ceph OSD deletion",
  "task.type.cephcreatepool": "Ceph pool creation",
  "task.type.cephdestroypool": "Ceph pool deletion",
  "task.type.cephcreatemon": "Ceph monitor creation",
  "task.type.cephdestroymon": "Ceph monitor deletion",
  "task.type.cephcreatemds": "Ceph MDS creation",
  "task.type.cephdestroymds": "Ceph MDS deletion",
  "task.type.cephfscreate": "CephFS creation",
  "task.type.clusterjoin": "Cluster join",
  "task.type.clustercreate": "Cluster creation",
  "task.type.reboot": "Node reboot",
  "task.type.shutdown": "Node shutdown",
  "task.type.pull_file": "File copy",
  "task.type.push_file": "File copy",
  "task.type.dircreate": "Directory creation",
  "task.type.diskinit": "Disk initialisation",
  "task.type.lvmcreate": "LVM volume creation",
  "task.type.lvmthincreate": "LVM-thin pool creation",
  "task.type.zfscreate": "ZFS pool creation",
  "task.type.unknown": "Task",

  // ----------------------------------------------------------- timeframes
  "timeframe.hour": "Last hour",
  "timeframe.day": "Last 24 h",
  "timeframe.week": "Last 7 days",
  "timeframe.month": "Last 30 days",
  "timeframe.year": "Last year",
  "timeframe.short.hour": "1 h",
  "timeframe.short.day": "24 h",
  "timeframe.short.week": "7 d",
  "timeframe.short.month": "30 d",
  "timeframe.short.year": "1 y",

  // ----------------------------------------------------------- allocation
  "allocation.allocated": "· allocated",
  "allocation.atLeast": "· at least",

  // --------------------------------------------------------- node updates
  "nodeUpdates.upToDate": "Up to date",
  "nodeUpdates.pending": "{count} pending",

  // --------------------------------------------------------------- search
  "search.noResult": "No result",

  // ---------------------------------------------------------------- shell
  "shell.skipToContent": "Skip to content",
  "shell.clusters": "Clusters",
  "shell.sidebarWidth": "Navigation panel width",

  // --------------------------------------------------------------- topBar
  "topBar.searchPlaceholder": "Search for a VM or a node…",
  "topBar.searchLabel": "Global search",

  // --------------------------------------------------------------- alerts
  "alerts.notifications": "Notifications",
  "alerts.buttonLabel": "Notifications · {alerts}",
  "alerts.menuLabel": "Alerts",
  "alerts.empty": "No alert",

  // ------------------------------------------------------ cluster switcher
  "clusterSwitcher.all": "All clusters",
  "clusterSwitcher.menuLabel": "Clusters",

  // ----------------------------------------------------------------- tree
  "tree.label": "Cluster tree",
  "tree.empty": "No cluster configured",
  "tree.maintenanceIcon": "Planned maintenance",

  // ---------------------------------------------------------------- theme
  "theme.label": "Theme",
  "theme.light": "Light",
  "theme.dark": "Dark",
  "theme.system": "System",

  // ------------------------------------------------------------- language
  "lang.label": "Language",
  "lang.system": "Browser language",

  // ----------------------------------------------------------- state views
  "state.loading": "Loading…",
  "state.retry": "Retry",
  "state.backToOverview": "Back to the overview",
  "state.technicalDetail": "Technical detail",
  "state.stale": "Previous data · connection lost",
  "state.staleAt": "Data from {stamp} · connection lost",

  // --------------------------------------------------------------- errors
  "error.unreachable.title": "moxy is unreachable",
  "error.unreachable.body":
    "The moxy service did not answer. Check that it is running and that the " +
    "clusters are reachable, then try again.",
  "error.unauthorized.title": "Authentication required",
  "error.unauthorized.body":
    "moxy refused the request for lack of authentication. Enter the access " +
    "token, or check that the authenticating proxy is in place.",
  "error.notFound.title": "Object not found",
  "error.notFound.body":
    "This node or machine is no longer in the cluster: deleted, renamed, or " +
    "migrated elsewhere? The overview says what is still there.",
  "error.forbidden.title": "Insufficient privileges on this node",
  "error.forbidden.body":
    "Proxmox refused the request. The token needs Sys.Audit on /nodes, and a " +
    "role set on /nodes replaces the one inherited from / instead of adding " +
    "to it: a role carrying only Sys.Modify erases Sys.Audit. See “Required " +
    "PVE privileges” in the README.",
  "error.upstream.title": "Cluster unreachable",
  "error.upstream.body":
    "moxy answers, but the PVE cluster does not. The server log says why; the " +
    "overview keeps showing its last known state.",
  "error.timeout.title": "Cluster timed out",
  "error.timeout.body":
    "The PVE cluster did not answer in time. It may be overloaded; try again " +
    "in a moment.",
  "error.unsupported.title": "Unavailable without a cluster connection",
  "error.unsupported.body":
    "moxy runs without a connection to this cluster: this screen needs a " +
    "configured PVE cluster to say anything at all.",
  "error.invalid.title": "Invalid request",
  "error.invalid.body":
    "moxy refused this request: an identifier, a window or a limit is not " +
    "acceptable. The technical detail below names the parameter at fault.",
  "error.internal.title": "Internal moxy error",
  "error.internal.body":
    "The service failed while handling the request. The server log says more; " +
    "the technical detail below gives the code.",
  "error.unreadable.title": "Unexpected response",
  "error.unreadable.body":
    "The response received is not moxy’s: is a proxy returning an HTML page " +
    "in its place? The technical detail below says which request got it.",
  "error.unknown.title": "Could not load the data",
  "error.unknown.body":
    "An unexpected error occurred. The technical detail below says more; try " +
    "again.",

  // ---------------------------------------------------------------- login
  "login.title": "Authentication required",
  "login.intro":
    "Enter the access token configured on this server to consult the clusters.",
  "login.field": "Access token",
  "login.submit": "Sign in",
  "login.submitting": "Signing in…",
  "login.shared":
    "This token is shared: it authorizes access, it identifies nobody. " +
    "Actions taken from moxy are therefore attributed to no account.",
  "login.refused":
    "Token refused. Check the value given to you by the administrator of this " +
    "server.",

  // ------------------------------------------------------------- overview
  "overview.title": "Clusters",

  // --------------------------------------------------------- cluster card
  "card.open": "Open {name}",
  "card.cpu": "CPU",
  "card.memory": "Memory",
  "card.storage": "Storage",
  "card.vms": "VMs",
  "card.noVm": "No VM",
  "card.running": "{count} running",
  "card.lastHour": "Last hour",
  "card.chartLabel":
    "Usage of {name} over the last hour: CPU {cpu}, memory {memory}",
  "card.nodes": "Nodes",
  "card.nodesCaption": "Nodes of {name}",
  "card.noNode": "No node to show.",
  "card.quietNoAlert": "No alert",
  "card.quietQuorum": "Quorum {online}/{nodes} · no alert",
  "card.noReading": "No reading available{suffix}",
  "card.staleReading": "Old reading · {relative}{suffix}",
  "card.column.node": "Node",
  "card.column.cpu": "CPU",
  "card.column.memory": "Memory",
  "card.column.pveVersion": "PVE",
  "card.column.uptime": "Uptime",

  // ----------------------------------------------------------- node detail
  "node.breadcrumb": "Node",
  "node.planMaintenance": "Maintenance plan",
  "node.metric.cpu": "CPU",
  "node.metric.memory": "Memory",
  "node.metric.localStorage": "Local storage",
  "node.metric.loadAverage": "Load average",
  "node.loadAverageDetail": "· 1, 5, 15 min",
  "node.chartTitle": "Node CPU load",
  "node.chartLabel": "CPU load of {name}",
  "node.kv.cluster": "Cluster",
  "node.kv.quorum": "Quorum",
  "node.kv.ha": "HA",
  "node.kv.kernel": "Kernel",
  "node.kv.updates": "Updates",
  "node.guests.title": "Guests on this node",
  "node.guests.drained":
    "Node in maintenance: the guests managed by HA have been migrated.",
  "node.guests.caption": "Guests hosted by this node",
  "node.guests.emptyDrained": "This node was drained by the maintenance mode.",
  "node.guests.empty": "No guest on this node.",
  "node.guests.open": "Open {name}",
  "node.column.vmid": "ID",
  "node.column.name": "Name",
  "node.column.cpu": "CPU",
  "node.column.memory": "RAM",
  "node.column.status": "State",
  "node.updates.title": "Pending updates",
  "node.updates.group": "Pending packages",
  "node.updates.caption": "Packages pending an update",
  "node.updates.empty": "No package pending.",
  "node.updates.column.package": "Package",
  "node.updates.column.version": "Version",
  "node.updates.column.title": "Description",

  // ---------------------------------------------------------- guest detail
  "guest.metric.cpu": "CPU",
  "guest.metric.memory": "Memory",
  "guest.metric.bootDisk": "Boot disk",
  "guest.metric.volumes": "Allocated storage",
  "guest.chartTitle": "CPU load",
  "guest.chartLabel": "CPU load of {name}",
  "guest.kv.node": "Node",
  "guest.kv.ha": "HA",
  "guest.kv.bootDisk": "Boot disk",
  "guest.kv.hostMemory": "Host memory",
  "guest.kv.ipv4": "IPv4",
  "guest.disks.title": "Disks",
  "guest.nets.title": "Networks",
  "guest.detachedNote": "{volumes} · not counted",
  "guest.tags.title": "Tags",
  "guest.tasks.title": "Recent tasks",
  "guest.tasks.source": "Tasks of this machine on {node}",
  "guest.tasks.empty": "No recent task for this machine.",

  // ----------------------------------------------------------- disks table
  "disks.column.slot": "Slot",
  "disks.column.storage": "Storage",
  "disks.column.volume": "Volume",
  "disks.column.size": "Size",
  "disks.caption": "Volumes declared by this guest",
  "disks.empty": "This system declares no disk.",
  "disks.detached": "Detached",

  // ------------------------------------------------------------ nets table
  "nets.column.slot": "Interface",
  "nets.column.network": "Network",
  "nets.column.mac": "MAC address",
  "nets.caption": "Networks this guest is wired to",
  "nets.empty": "This system declares no interface.",

  // ----------------------------------------------------------- tasks table
  "tasks.column.time": "Time",
  "tasks.column.label": "Description",
  "tasks.column.duration": "Duration",
  "tasks.column.outcome": "State",
  "tasks.caption": "Recent tasks, newest first",
  "tasks.empty": "No recent task.",

  // --------------------------------------------------------------- journal
  "journal.title": "Cluster journal",
  "journal.disconnected": "Connection lost",
  "journal.updated": "Updated {relative}",
  "journal.unavailable": "Journal unavailable for now.",

  // ----------------------------------------------------------------- chart
  "chart.windowLabel": "Chart window",
  "chart.average": "{timeframe} · avg. {value}",
  "chart.noData": "No data",
  "chart.noDataLabel": "{label} — no data",

  // ------------------------------------------------------- maintenance plan
  "plan.title": "Put {node} into maintenance",
  "plan.close": "Close",
  "plan.intro":
    "The node would stay in the quorum but would accept no new machine. Here " +
    "is what would become of those it hosts, in cluster {cluster} as it " +
    "stands right now.",
  "plan.restartOne":
    "One container will be stopped then restarted during its migration: " +
    "Proxmox cannot move a container live.",
  "plan.restartMany":
    "{count} containers will be stopped then restarted during their " +
    "migration: Proxmox cannot move a container live.",
  "plan.caption": "Guests to move and their destination",
  "plan.empty":
    "This node hosts no machine: it can be drained without any migration.",
  "plan.column.vmid": "ID",
  "plan.column.name": "Machine",
  "plan.column.target": "Destination",
  "plan.column.memory": "RAM",
  "plan.column.method": "Migration",
  "plan.noTarget": "No destination",
  "plan.stayPut": "stays put",
  "plan.automatic": "Automatic",
  "plan.manual": "By hand",
  "plan.restart": "Restart",
  "plan.offline": "Offline",
  "plan.saturated": "already saturated",
  "plan.noMigration": "No migration needed.",
  "plan.capacityOk": "Enough capacity: {targets}.",
  "plan.targetAfter": "{name} goes to {ratio} of RAM",
  "plan.cannotDrain": "This node cannot be drained as things stand.",
  "plan.shortfallOne":
    "One machine finds no destination under {threshold} of memory.",
  "plan.shortfallMany":
    "{count} machines find no destination under {threshold} of memory.",
  "plan.handOverTitle": "To run on a node of the cluster",
  "plan.handOverBody":
    "moxy cannot trigger the maintenance itself: Proxmox exposes this command " +
    "through its command line only, never through its REST API. The plan " +
    "above describes what will happen once the command is run.",
  "plan.noManager":
    "This cluster has no HA manager: the command above will move nothing. " +
    "Every machine listed is to be migrated by hand.",
  "plan.manualOne":
    "One machine is not managed by HA: the CRM will not move it. To be " +
    "migrated by hand, before or after.",
  "plan.manualMany":
    "{count} machines are not managed by HA: the CRM will not move them. To " +
    "be migrated by hand, before or after.",
  "plan.blocker.noTarget": "No other node available to take the machines",
  "plan.blocker.sourceOffline":
    "This node is offline: its machines are not running on it",
  "plan.blocker.targetStatsUnavailable":
    "The memory of the destination nodes is unknown: the token does not have " +
    "Sys.Audit on /nodes, so no placement can be justified",

  // ------------------------------------------------------------------- app
  "app.notFound.title": "Object not found",
  "app.notFound.hint":
    "This address designates neither a cluster, nor a node, nor a machine. " +
    "Go back to the overview to find what moxy knows about.",
  "app.noCluster.title": "No cluster to show",
  "app.noCluster.hint": "Add a cluster to the moxyd configuration.",
  "app.title.clusters": "Clusters",
  "app.title.notFound": "Object not found",
};

const CATALOGUES: Record<Locale, Record<MessageKey, string>> = { fr, en };

/** Values a placeholder can take. Numbers are stringified as they come. */
export type MessageParams = Record<string, string | number>;

/**
 * `{name}` and nothing else.
 *
 * Deliberately not a template language: a catalogue that can branch is a
 * catalogue a translator has to debug. Plurals go through `plural` in
 * lib/format.ts, which picks a key rather than parsing one.
 */
const PLACEHOLDER = /\{(\w+)\}/g;

/**
 * Looks a message up and fills its placeholders.
 *
 * A placeholder with no matching parameter is left as it is written, rather
 * than blanked: `{name}` on screen is a bug anyone can see and search for,
 * while an empty space is a sentence that quietly lost a word.
 */
export function translate(
  locale: Locale,
  key: MessageKey,
  params?: MessageParams,
): string {
  const message = CATALOGUES[locale][key];
  if (params === undefined) {
    return message;
  }
  return message.replace(PLACEHOLDER, (whole, name: string) => {
    const value = params[name];
    return value === undefined ? whole : String(value);
  });
}

/**
 * Whether a string built at runtime names a message.
 *
 * Needed for the two vocabularies PVE owns rather than moxy: a task type and an
 * HA state arrive as raw words from the cluster, and a newer PVE may send one
 * this catalogue has never heard of. The caller then shows the raw word, which
 * an operator can at least search for — better than a generic label that hides
 * which task it was.
 */
export function isMessageKey(value: string): value is MessageKey {
  return Object.hasOwn(fr, value);
}

/** A `translate` with its locale already bound, which is what components hold. */
export type Translator = (key: MessageKey, params?: MessageParams) => string;

export function translator(locale: Locale): Translator {
  return (key, params) => translate(locale, key, params);
}

/** Exposed for the catalogue test alone: nothing else should read a catalogue. */
export const CATALOGUES_FOR_TEST: Record<Locale, Record<string, string>> = CATALOGUES;
