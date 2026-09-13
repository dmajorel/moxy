import { IconAlertTriangle, IconArrowRight, IconCheck, IconX } from "@tabler/icons-react";
import { useEffect, useRef } from "react";

import type { MaintenancePlan, PlannedMove } from "@/api/types";
import { useMaintenancePlan } from "@/api/useDetail";
import { AlertBanner, Tag } from "@/components/ui";
import { ErrorView, LoadingView } from "@/components/StateViews";
import {
  FALLBACK,
  formatBytes,
  formatGuestName,
  formatGuestStatus,
  formatRatio,
} from "@/lib/format";

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
  const closeRef = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    closeRef.current?.focus();
  }, []);

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
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/45 p-6"
      onClick={onClose}
    >
      <div
        role="dialog"
        aria-modal="true"
        aria-label={`Plan de mise en maintenance de ${node}`}
        className="max-h-full w-[560px] max-w-full overflow-y-auto rounded-panel border-[0.5px] border-border bg-surface-2 px-5 py-4"
        onClick={(event) => {
          event.stopPropagation();
        }}
      >
        <div className="mb-1.5 flex items-center gap-2.5">
          <span className="flex size-8 items-center justify-center rounded-card bg-bg-warning text-text-warning-strong">
            <IconAlertTriangle size={18} aria-hidden />
          </span>
          <h2 className="text-[16px] font-medium text-text-primary">
            Mettre {node} en maintenance
          </h2>
          <button
            ref={closeRef}
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
        <table className="mb-3 w-full border-collapse text-[12px]">
          <thead>
            <tr className="text-left text-[11px] text-text-muted">
              <th className="py-1.5 pr-2 font-normal">ID</th>
              <th className="py-1.5 pr-2 font-normal">Machine</th>
              <th className="py-1.5 pr-2 font-normal" />
              <th className="py-1.5 pr-2 font-normal">Destination</th>
              <th className="py-1.5 pr-2 font-normal">RAM</th>
              <th className="py-1.5 font-normal">Migration</th>
            </tr>
          </thead>
          <tbody>
            {plan.moves.map((move) => (
              <tr key={move.vmid} className="border-t-[0.5px] border-border">
                <td className="py-2 pr-2 tabular-nums text-text-secondary">{move.vmid}</td>
                <td className="py-2 pr-2 text-text-primary">
                  {formatGuestName(move.vmid, move.name)}
                </td>
                <td className="py-2 pr-2 text-text-muted">
                  <IconArrowRight size={14} aria-hidden />
                </td>
                <td className="py-2 pr-2">
                  {move.placed ? (
                    <span className="text-text-primary">{move.target}</span>
                  ) : (
                    <Tag variant="warning">Aucune destination</Tag>
                  )}
                </td>
                <td className="py-2 pr-2 tabular-nums text-text-secondary">
                  {move.memory === 0 ? FALLBACK : formatBytes(move.memory)}
                </td>
                <td className="py-2">
                  <MigrationKind move={move} />
                </td>
              </tr>
            ))}
            {plan.staying.map((guest) => (
              <tr key={guest.vmid} className="border-t-[0.5px] border-border text-text-muted">
                <td className="py-2 pr-2 tabular-nums">{guest.vmid}</td>
                <td className="py-2 pr-2">{formatGuestName(guest.vmid, guest.name)}</td>
                <td className="py-2 pr-2" />
                <td className="py-2 pr-2">reste sur place</td>
                <td className="py-2 pr-2">
                  {/*
                    A stable key from the backend, translated here: "template"
                    is the only one the plan emits today, and it must read the
                    same word as everywhere else in the interface.
                  */}
                  <Tag>
                    {guest.reason === "template"
                      ? formatGuestStatus("template")
                      : guest.reason}
                  </Tag>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
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
