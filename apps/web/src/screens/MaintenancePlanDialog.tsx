import { IconAlertTriangle, IconArrowRight, IconCheck, IconX } from "@tabler/icons-react";
import { useEffect, useId, useRef } from "react";

import type { MaintenancePlan, PlannedMove } from "@/api/types";
import { useMaintenancePlan } from "@/api/useDetail";
import type { DataColumn } from "@/components/ui";
import { AlertBanner, DataTable, Tag } from "@/components/ui";
import { ErrorView, LoadingView } from "@/components/StateViews";
import { useFormat, useT } from "@/i18n/locale";
import type { Translator } from "@/i18n/messages";
import type { Format } from "@/lib/format";
import { FALLBACK, formatGuestName } from "@/lib/format";
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
  const t = useT();
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
            {t("plan.title", { node })}
          </h2>
          <button
            type="button"
            onClick={onClose}
            aria-label={t("plan.close")}
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

/**
 * The moves and the guests staying put, in one table: they are the same list
 * seen from two sides, and splitting them would make the reader count twice.
 * The third column holds the arrow alone and has nothing to name.
 */
function planColumns(t: Translator): DataColumn[] {
  return [
    { key: "vmid", header: t("plan.column.vmid"), numeric: true, tone: "secondary" },
    { key: "name", header: t("plan.column.name") },
    { key: "arrow", tone: "muted" },
    { key: "target", header: t("plan.column.target") },
    { key: "memory", header: t("plan.column.memory"), numeric: true, tone: "secondary" },
    { key: "method", header: t("plan.column.method") },
  ];
}

function PlanBody({ plan, clusterName }: { plan: MaintenancePlan; clusterName: string }) {
  const t = useT();
  const fmt = useFormat();
  const unplaced = plan.moves.filter((move) => !move.placed);
  // An interruption has to be announced before the click, not discovered after.
  const restarts = plan.moves.filter((move) => move.method === "restart").length;

  return (
    <>
      <p className="mb-3.5 text-[13px] leading-relaxed text-text-secondary">
        {t("plan.intro", { cluster: clusterName })}
      </p>

      {plan.blockers.length > 0 && (
        <AlertBanner variant="warning" className="mb-3">
          {plan.blockers.map((blocker) => blockerLabel(blocker, t)).join(" · ")}
        </AlertBanner>
      )}

      {restarts > 0 && (
        <AlertBanner variant="warning" className="mb-3">
          {restarts === 1
            ? t("plan.restartOne")
            : t("plan.restartMany", { count: restarts })}
        </AlertBanner>
      )}
      <DataTable
        caption={t("plan.caption")}
        columns={planColumns(t)}
        emptyHint={t("plan.empty")}
        className="mb-3"
        rows={[
          ...plan.moves.map((move) => ({
            key: `move:${String(move.vmid)}`,
            cells: {
              vmid: move.vmid,
              name: formatGuestName(move.vmid, move.name),
              arrow: <IconArrowRight size={14} aria-hidden />,
              target: move.placed ? (
                move.target
              ) : (
                <Tag variant="warning">{t("plan.noTarget")}</Tag>
              ),
              // A guest whose memory PVE reports as 0 is a guest whose memory
              // nobody measured, not one using none.
              memory: move.memory === 0 ? FALLBACK : fmt.formatBytes(move.memory),
              method: <MigrationKind move={move} />,
            },
          })),
          ...plan.staying.map((guest) => ({
            key: `staying:${String(guest.vmid)}`,
            // The whole line is an aside: these guests are not going anywhere.
            tone: "muted" as const,
            cells: {
              vmid: guest.vmid,
              name: formatGuestName(guest.vmid, guest.name),
              target: t("plan.stayPut"),
              method: <Tag>{fmt.formatStayingReason(guest.reason)}</Tag>,
            },
          })),
        ]}
      />

      <AlertBanner
        variant={plan.feasible ? "neutral" : "warning"}
        icon={plan.feasible ? "check" : "alert"}
        className="mb-3"
      >
        {plan.feasible
          ? capacityVerdict(plan, t, fmt)
          : shortfall(plan, unplaced.length, t, fmt)}
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
                  ? `${fmt.formatRatio(target.before.ratio)} → ${fmt.formatRatio(target.after.ratio)}`
                  : FALLBACK}
                {target.incoming > 0 && (
                  <span className="ml-1 text-text-muted">
                    (+{target.incoming})
                  </span>
                )}
                {target.exceeds && (
                  <span className="ml-1.5 text-text-warning-strong">
                    {t("plan.saturated")}
                  </span>
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
 * What moxy cannot do, and what to run instead.
 *
 * Offering a disabled "Lancer la maintenance" button would suggest the feature
 * is merely switched off. It is not available at all through the API, and the
 * honest thing is to say so and give the command.
 */
function HandOver({ plan }: { plan: MaintenancePlan }) {
  const t = useT();
  const command = `ha-manager crm-command node-maintenance enable ${plan.node}`;
  // What the CRM will not do for you. A guest it does not manage -- or manages
  // but has disabled -- stays on the drained node until somebody moves it.
  const manual = plan.moves.filter((move) => move.placed && move.ha === false);
  const noManager = plan.moves.length > 0 && plan.moves.every((move) => move.ha === null);

  return (
    <div className="rounded-card border-[0.5px] border-border bg-surface-1 px-3 py-2.5">
      <p className="mb-1.5 flex items-center gap-1.5 text-[12px] font-medium text-text-primary">
        <IconCheck size={14} aria-hidden />
        {t("plan.handOverTitle")}
      </p>
      <p className="mb-2 text-[12px] leading-relaxed text-text-secondary">
        {t("plan.handOverBody")}
      </p>
      <code className="block overflow-x-auto rounded-card bg-surface-0 px-2.5 py-1.5 font-mono text-[11px] text-text-primary">
        {command}
      </code>

      {noManager && (
        <p className="mt-2.5 text-[12px] leading-relaxed text-text-warning-strong">
          {t("plan.noManager")}
        </p>
      )}

      {manual.length > 0 && (
        <>
          <p className="mt-2.5 mb-1.5 text-[12px] leading-relaxed text-text-secondary">
            {manual.length === 1
              ? t("plan.manualOne")
              : t("plan.manualMany", { count: manual.length })}
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
  const t = useT();
  const who =
    move.ha === null ? (
      <span className="text-text-muted">{FALLBACK}</span>
    ) : move.ha ? (
      <Tag variant="success">{t("plan.automatic")}</Tag>
    ) : (
      <Tag variant="warning">{t("plan.manual")}</Tag>
    );

  return (
    <span className="flex flex-wrap items-center gap-1">
      {who}
      {move.method === "restart" && <Tag variant="warning">{t("plan.restart")}</Tag>}
      {move.method === "offline" && <Tag>{t("plan.offline")}</Tag>}
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

function capacityVerdict(plan: MaintenancePlan, t: Translator, fmt: Format): string {
  const receiving = plan.targets.filter((target) => target.incoming > 0);
  if (receiving.length === 0) {
    return t("plan.noMigration");
  }
  const targets = receiving
    .map((target) =>
      t("plan.targetAfter", {
        name: target.name,
        ratio: fmt.formatRatio(target.after.ratio),
      }),
    )
    .join(", ");
  return t("plan.capacityOk", { targets });
}

function shortfall(
  plan: MaintenancePlan,
  unplaced: number,
  t: Translator,
  fmt: Format,
): string {
  if (unplaced === 0) {
    return t("plan.cannotDrain");
  }
  const threshold = fmt.formatRatio(plan.threshold);
  return unplaced === 1
    ? t("plan.shortfallOne", { threshold })
    : t("plan.shortfallMany", { count: unplaced, threshold });
}

function blockerLabel(blocker: string, t: Translator): string {
  switch (blocker) {
    case "no_target":
      return t("plan.blocker.noTarget");
    case "source_offline":
      return t("plan.blocker.sourceOffline");
    case "target_stats_unavailable":
      return t("plan.blocker.targetStatsUnavailable");
    default:
      return blocker;
  }
}
