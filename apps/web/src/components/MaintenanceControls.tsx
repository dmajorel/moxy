import { IconTool } from "@tabler/icons-react";

import type { MaintenanceAction } from "@/api/types";
import type { MaintenanceCommand } from "@/api/useDetail";
import { AlertBanner } from "@/components/ui";
import { useFormat, useT } from "@/i18n/locale";
import { backendKind } from "@/lib/errors";

/**
 * The two halves of running a drain: the button that asks, and the report of
 * what came back.
 *
 * They are two components rather than one because they are not rendered in the
 * same place. In the plan dialog the button is a footer under the plan, with
 * the report just above it; on the node view the button sits in the header
 * actions, which is a single inline row, so the report has to go under the
 * heading. One component would have had to draw both layouts.
 *
 * Both take the whole command object rather than a phase and a callback: the
 * phase, the answer and the failure are three faces of one thing, and a screen
 * holding them apart is a screen that can show "sent" next to an error.
 */

/** The three colours are drawn from tokens; the state is always also written. */
const BUTTON_CLASSES =
  "flex items-center gap-1.5 rounded-card bg-fill-warning px-2.5 py-1.5 " +
  "text-[12px] font-medium text-text-on-warning hover:brightness-110 " +
  "focus:outline-none focus-visible:outline focus-visible:outline-1 " +
  "focus-visible:outline-offset-1 focus-visible:outline-accent " +
  "aria-disabled:cursor-default aria-disabled:opacity-70 " +
  "aria-disabled:hover:brightness-100";

export interface MaintenanceButtonProps {
  command: MaintenanceCommand;
  /** Which verb this button sends. The label follows it. */
  action: MaintenanceAction;
  className?: string;
}

/**
 * The amber button section A.3 has drawn since the first mockups.
 *
 * It is never rendered for a cluster that cannot run the command: the backend
 * says so per node through `maintenanceExecutable`, and the rule the repository
 * kept from ADR 0003 is that there is no disabled button with a tooltip.
 *
 * `aria-disabled` rather than `disabled`, and it is not a detail: a disabled
 * control drops the focus it holds, and inside the dialog's focus trap the
 * keyboard would then have nowhere to be. The click is refused by the handler
 * instead, and the label says which of the two reasons it is refused for.
 */
export function MaintenanceButton({
  command,
  action,
  className,
}: MaintenanceButtonProps) {
  const t = useT();
  const busy = command.phase === "running";
  // A request that went through is not sent twice. A refused one is: the
  // operator has just read why, and the command is idempotent on the CRM side.
  const spent = command.phase === "done";
  const label = busy
    ? t("maintenance.sending")
    : spent
      ? t("maintenance.sent")
      : t(action === "enable" ? "maintenance.enable" : "maintenance.disable");

  return (
    <button
      type="button"
      aria-disabled={busy || spent}
      aria-busy={busy}
      onClick={() => {
        if (busy || spent) {
          return;
        }
        command.run(action);
      }}
      className={[BUTTON_CLASSES, className].filter(Boolean).join(" ")}
    >
      <IconTool size={14} aria-hidden />
      {label}
    </button>
  );
}

export interface MaintenanceReportProps {
  command: MaintenanceCommand;
  className?: string;
}

/**
 * What came back, in the display language.
 *
 * `role="status"` throughout: the banner appears mid-session, after a click,
 * and a result nobody hears is a result a screen reader user has to go looking
 * for. Polite rather than assertive — nothing on screen goes away.
 *
 * The success wording says THE REQUEST WENT THROUGH and never "the node is
 * drained": `formatMaintenanceOutcome` owns that distinction, and the CRM does
 * the draining afterwards.
 */
export function MaintenanceReport({ command, className }: MaintenanceReportProps) {
  const t = useT();
  const fmt = useFormat();

  if (command.phase === "idle") {
    return null;
  }

  if (command.phase === "running") {
    return (
      <AlertBanner icon="refresh" role="status" className={className}>
        {t("maintenance.sending")}
      </AlertBanner>
    );
  }

  if (command.phase === "failed") {
    return (
      <AlertBanner variant="warning" role="status" className={className}>
        {t("maintenance.failed", {
          reason: fmt.formatMaintenanceError(backendKind(command.error)),
        })}
      </AlertBanner>
    );
  }

  const result = command.result;
  if (result === null) {
    return null;
  }
  // Empty is not the same as absent: the backend serves null when no command
  // ran at all, and an empty string when one ran and said nothing. Neither is
  // worth a heading over a blank box.
  const output = result.output !== null && result.output.trim() !== "" ? result.output : null;

  return (
    <div className={className}>
      <AlertBanner variant="neutral" icon="check" role="status">
        {fmt.formatMaintenanceOutcome(result)}
        {result.via !== "" && ` ${t("maintenance.via", { node: result.via })}`}
      </AlertBanner>
      {output !== null && (
        <>
          <p className="mt-2 mb-1 text-[11px] text-text-muted">
            {t("maintenance.outputTitle")}
          </p>
          {/* The node's own words, in English, capped upstream: shown as they
              came rather than paraphrased, under a label in the reader's
              language. */}
          <pre className="overflow-x-auto rounded-card bg-surface-0 px-2.5 py-1.5 font-mono text-[11px] whitespace-pre-wrap text-text-primary">
            {output}
          </pre>
        </>
      )}
    </div>
  );
}
