import { IconAlertTriangle, IconArrowRight, IconCheck, IconX } from "@tabler/icons-react";
import { useEffect, useId, useRef } from "react";

import type { MaintenancePlan, PlannedMove, StayingGuest } from "@/api/types";
import { useMaintenancePlan } from "@/api/useDetail";
import type { DataTableColumn } from "@/components/ui";
import { AlertBanner, DataTable, Tag } from "@/components/ui";
import { ErrorView, LoadingView } from "@/components/StateViews";
import {
  FALLBACK,
  formatBytes,
  formatGuestName,
  formatRatio,
  formatStayReason,
} from "@/lib/format";
import { useFocusTrap } from "@/lib/useFocusTrap";

/**
 * Screen 3 of the mockups — the migration plan of a drain.
 *
 * The handoff is blunt about what this replaces: a dialog asking "are you
 * sure?" tells an operator nothing. This one names every guest, its
 * destination, and what the destination looks like afterwards, before anything
 * happens.
 *
 * It stops at showing the plan. moxy cannot perform the drain: PVE registers
 * node-maintenance in its CLI, not under /api2, so there is no route to call.
 * The dialog therefore hands over the exact command instead of offering a
 * button that could not work.
 */
export interface MaintenancePlanDialogProps {
  cluster: string;
  clusterName: string;
  node: string;
  onClose: () => void;
}

export function MaintenancePlanDialog({
  cluster,
  clusterName,
  node,
  onClose,
}: MaintenancePlanDialogProps) {
  const { data, error, isLoading, refresh } = useMaintenancePlan(cluster, node);
  const dialogRef = useRef<HTMLDivElement>(null);
  // The dialog is named by its own visible heading rather than by an
  // aria-label repeating it: two strings for one title is one string too many.
  const titleId = useId();

  // Keeps the keyboard inside while it is open, and hands it back to whatever
  // opened it on close. Tab used to walk out into the tree underneath, and
  // closing dropped the focus on <body>.
  useFocusTrap(dialogRef);

  useEffect(() => {
    function onKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") {
        onClose();
      }
    }
    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("keydown", onKeyDown);
    };
  }, [onClose]);

  return (
    // Clicking the veil closes the dialog: a mouse convenience, and one the
    // keyboard already has through Escape. Nothing is reachable here without
    // a pointer, so there is no keyboard path to add.
    // eslint-disable-next-line jsx-a11y/no-static-element-interactions, jsx-a11y/click-events-have-key-events -- mouse-only convenience, Escape is the keyboard path
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-scrim p-6"
      onClick={(event) => {
        // Only a click on the veil ITSELF closes. Comparing the target with
        // the current target does what a stopPropagation on the dialog did,
        // without hanging a click handler on the dialog — which is a
        // non-interactive element, and was reported as one.
        if (event.target === event.currentTarget) {
          onClose();
        }
      }}
    >
      <div
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        className="max-h-full w-[560px] max-w-full overflow-y-auto rounded-panel border-[0.5px] border-border bg-surface-2 px-5 py-4"
      >
        <div className="mb-1.5 flex items-center gap-2.5">
          <span className="flex size-8 items-center justify-center rounded-card bg-bg-warning text-text-warning-strong">
            <IconAlertTriangle size={18} aria-hidden />
          </span>
          <h2 id={titleId} className="text-[16px] font-medium text-text-primary">
            Mettre {node} en maintenance
          </h2>
          <button
            type="button"
            onClick={onClose}
            aria-label="Fermer"
            className="ml-auto rounded-card p-1 text-text-muted hover:bg-fill-ghost-selected"
          >
            <IconX size={16} aria-hidden />
          </button>
        </div>

        {isLoading ? (
          <LoadingView />
        ) : data === null ? (
          <ErrorView error={error ?? new Error("plan unavailable")} onRetry={refresh} />
        ) : (
          <PlanBody plan={data} clusterName={clusterName} />
        )}
      </div>
    </div>
  );
}

function PlanBody({ plan, clusterName }: { plan: MaintenancePlan; clusterName: string }) {
  const unplaced = plan.moves.filter((move) => !move.placed);
  // An interruption has to be announced before the click, not discovered after.
  const restarts = plan.moves.filter((move) => move.method === "restart").length;

  return (
    <>
      <p className="mb-3.5 text-[13px] leading-relaxed text-text-secondary">
        Le nœud resterait dans le quorum mais n'accepterait plus de nouvelles machines.
        Voici ce que deviendraient celles qu'il héberge, dans le cluster{" "}
        {clusterName} tel qu'il est en ce moment.
      </p>

      {plan.blockers.length > 0 && (
        <AlertBanner variant="warning" className="mb-3">
          {plan.blockers.map(blockerLabel).join(" · ")}
        </AlertBanner>
      )}

      {restarts > 0 && (
        <AlertBanner variant="warning" className="mb-3">
          {restarts === 1
            ? "Un conteneur sera arrêté puis redémarré pendant sa migration : Proxmox ne sait pas déplacer un conteneur à chaud."
            : `${String(restarts)} conteneurs seront arrêtés puis redémarrés pendant leur migration : Proxmox ne sait pas déplacer un conteneur à chaud.`}
        </AlertBanner>
      )}
      {plan.moves.length === 0 && plan.staying.length === 0 ? (
        <p className="mb-3 text-[12px] text-text-muted">
          Ce nœud n'héberge aucune machine : il peut être drainé sans migration.
        </p>
      ) : (
        <DataTable
          caption="Invités à déplacer et leur destination"
          columns={PLAN_COLUMNS}
          rows={[
            ...plan.moves.map((move): PlanRow => ({ kind: "move", move })),
            ...plan.staying.map((guest): PlanRow => ({ kind: "staying", guest })),
          ]}
          rowKey={(row) => (row.kind === "move" ? row.move.vmid : row.guest.vmid)}
          rowClassName={(row) =>
            row.kind === "move" ? "text-text-secondary" : "text-text-muted"
          }
          scrollable={false}
          className="mb-3"
        />
      )}

      <AlertBanner
        variant={plan.feasible ? "neutral" : "warning"}
        icon={plan.feasible ? "check" : "alert"}
        className="mb-3"
      >
        {plan.feasible ? capacityVerdict(plan) : shortfall(plan, unplaced.length)}
      </AlertBanner>

      {plan.targets.length > 0 && (
        <ul className="mb-3.5 grid gap-1 text-[12px]">
          {plan.targets.map((target) => (
            <li
              key={target.name}
              className="flex items-baseline justify-between gap-3 border-t-[0.5px] border-border py-1.5"
            >
              <span className="text-text-secondary">{target.name}</span>
              <span className="tabular-nums text-text-primary">
                {target.measured
                  ? `${formatRatio(target.before.ratio)} → ${formatRatio(target.after.ratio)}`
                  : FALLBACK}
                {target.incoming > 0 && (
                  <span className="ml-1 text-text-muted">
                    (+{target.incoming})
                  </span>
                )}
                {target.exceeds && (
                  <span className="ml-1.5 text-text-warning-strong">déjà saturé</span>
                )}
              </span>
            </li>
          ))}
        </ul>
      )}

      <HandOver plan={plan} />
    </>
  );
}

/**
 * One line of the plan: a guest that moves, or one that stays.
 *
 * Both kinds share the table because they answer the same question about the
 * same node. They used to be two `map`s emitting rows of six and of five
 * cells, which tells a screen reader that a column has shifted; going through
 * one column list makes that impossible.
 */
type PlanRow =
  | { kind: "move"; move: PlannedMove }
  | { kind: "staying"; guest: StayingGuest };

const PLAN_COLUMNS: DataTableColumn<PlanRow>[] = [
  {
    header: "ID",
    cellClassName: "tabular-nums",
    render: (row) => (row.kind === "move" ? row.move.vmid : row.guest.vmid),
  },
  {
    header: "Machine",
    render: (row) =>
      row.kind === "move" ? (
        <span className="text-text-primary">
          {formatGuestName(row.move.vmid, row.move.name)}
        </span>
      ) : (
        formatGuestName(row.guest.vmid, row.guest.name)
      ),
  },
  {
    header: "",
    cellClassName: "text-text-muted",
    render: (row) =>
      row.kind === "move" ? <IconArrowRight size={14} aria-hidden /> : null,
  },
  {
    header: "Destination",
    render: (row) => {
      if (row.kind === "staying") {
        return "reste sur place";
      }
      return row.move.placed ? (
        <span className="text-text-primary">{row.move.target}</span>
      ) : (
        <Tag variant="warning">Aucune destination</Tag>
      );
    },
  },
  {
    header: "RAM",
    cellClassName: "tabular-nums",
    render: (row) => {
      if (row.kind === "staying") {
        // A stable key from the backend, translated by format.ts: it must read
        // the same word as everywhere else in the interface.
        return <Tag>{formatStayReason(row.guest.reason)}</Tag>;
      }
      return row.move.memory === 0 ? FALLBACK : formatBytes(row.move.memory);
    },
  },
  {
    header: "Migration",
    render: (row) =>
      row.kind === "move" ? <MigrationKind move={row.move} /> : null,
  },
];

/**
 * What moxy cannot do, and what to run instead.
 *
 * Offering a disabled "Lancer la maintenance" button would suggest the feature
 * is merely switched off. It is not available at all through the API, and the
 * honest thing is to say so and give the command.
 */
function HandOver({ plan }: { plan: MaintenancePlan }) {
  const command = `ha-manager crm-command node-maintenance enable ${plan.node}`;
  // What the CRM will not do for you. A guest it does not manage -- or manages
  // but has disabled -- stays on the drained node until somebody moves it.
  const manual = plan.moves.filter((move) => move.placed && move.ha === false);
  const noManager = plan.moves.length > 0 && plan.moves.every((move) => move.ha === null);

  return (
    <div className="rounded-card border-[0.5px] border-border bg-surface-1 px-3 py-2.5">
      <p className="mb-1.5 flex items-center gap-1.5 text-[12px] font-medium text-text-primary">
        <IconCheck size={14} aria-hidden />À lancer sur un nœud du cluster
      </p>
      <p className="mb-2 text-[12px] leading-relaxed text-text-secondary">
        moxy ne peut pas déclencher la maintenance lui-même : Proxmox
        n'expose cette commande que par sa ligne de commande, jamais par son API
        REST. Le plan ci-dessus décrit ce qui se passera une fois la commande
        lancée.
      </p>
      <code className="block overflow-x-auto rounded-card bg-surface-0 px-2.5 py-1.5 font-mono text-[11px] text-text-primary">
        {command}
      </code>

      {noManager && (
        <p className="mt-2.5 text-[12px] leading-relaxed text-text-warning-strong">
          Ce cluster n'a pas de gestionnaire HA : la commande ci-dessus ne
          déplacera rien. Toutes les machines listées sont à migrer à la main.
        </p>
      )}

      {manual.length > 0 && (
        <>
          <p className="mt-2.5 mb-1.5 text-[12px] leading-relaxed text-text-secondary">
            {manual.length === 1
              ? "Une machine n'est pas gérée par HA : le CRM ne la déplacera pas. À migrer à la main, avant ou après."
              : `${String(manual.length)} machines ne sont pas gérées par HA : le CRM ne les déplacera pas. À migrer à la main, avant ou après.`}
          </p>
          <code className="block overflow-x-auto rounded-card bg-surface-0 px-2.5 py-1.5 font-mono text-[11px] text-text-primary">
            {manual.map((move) => (
              <span key={move.vmid} className="block whitespace-nowrap">
                {migrateCommand(move)}
              </span>
            ))}
          </code>
        </>
      )}
    </div>
  );
}

/**
 * Whether the CRM will move this guest on its own. Three answers, and the
 * difference matters: a line the CRM handles happens by itself, a line it does
 * not is work somebody has to do.
 */
function MigrationKind({ move }: { move: PlannedMove }) {
  const who =
    move.ha === null ? (
      <span className="text-text-muted">{FALLBACK}</span>
    ) : move.ha ? (
      <Tag variant="success">Automatique</Tag>
    ) : (
      <Tag variant="warning">À la main</Tag>
    );

  return (
    <span className="flex flex-wrap items-center gap-1">
      {who}
      {move.method === "restart" && <Tag variant="warning">Redémarrage</Tag>}
      {move.method === "offline" && <Tag>Hors ligne</Tag>}
    </span>
  );
}

/** The command that moves a guest the CRM will not move. */
function migrateCommand(move: PlannedMove): string {
  // A running container cannot migrate live: PVE stops it, moves it and starts
  // it again, so the flag is --restart and the guest goes down for a moment.
  if (move.kind === "lxc") {
    return `pct migrate ${String(move.vmid)} ${move.target}${move.status === "running" ? " --restart" : ""}`;
  }
  return `qm migrate ${String(move.vmid)} ${move.target}${move.status === "running" ? " --online" : ""}`;
}

function capacityVerdict(plan: MaintenancePlan): string {
  const receiving = plan.targets.filter((target) => target.incoming > 0);
  if (receiving.length === 0) {
    return "Aucune migration nécessaire.";
  }
  const parts = receiving.map(
    (target) => `${target.name} passe à ${formatRatio(target.after.ratio)} de RAM`,
  );
  return `Capacité suffisante : ${parts.join(", ")}.`;
}

function shortfall(plan: MaintenancePlan, unplaced: number): string {
  if (unplaced === 0) {
    return "Ce nœud ne peut pas être drainé en l'état.";
  }
  const seuil = formatRatio(plan.threshold);
  return unplaced === 1
    ? `Une machine ne trouve aucune destination sous ${seuil} de mémoire.`
    : `${String(unplaced)} machines ne trouvent aucune destination sous ${seuil} de mémoire.`;
}

function blockerLabel(blocker: string): string {
  switch (blocker) {
    case "no_target":
      return "Aucun autre nœud disponible pour recevoir les machines";
    case "source_offline":
      return "Ce nœud est hors ligne : ses machines n'y tournent pas";
    case "target_stats_unavailable":
      return (
        "La mémoire des nœuds de destination est inconnue : le token n'a pas " +
        "Sys.Audit sur /nodes, donc aucun placement ne peut être justifié"
      );
    default:
      return blocker;
  }
}
