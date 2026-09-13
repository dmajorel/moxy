import type { Task, TaskOutcome } from "@/api/types";
import type { TagVariant } from "@/components/ui";
import { Tag } from "@/components/ui";
import {
  FALLBACK,
  formatTaskLabel,
  formatTaskOutcome,
  formatTime,
  formatUptime,
} from "@/lib/format";

/**
 * Recent tasks of a cluster.
 *
 * The column that matters is **duration**, which the backend computes: the
 * native interface shows a start and an end and leaves the operator to
 * subtract two timestamps in their head. That is the exact failing this table
 * exists to fix.
 */
export interface TasksTableProps {
  entries: Task[];
  /** Shown when the list is empty. */
  emptyHint?: string;
  className?: string;
}

export function TasksTable({ entries, emptyHint, className }: TasksTableProps) {
  if (entries.length === 0) {
    return (
      <p className={["text-[12px] text-text-muted", className].filter(Boolean).join(" ")}>
        {emptyHint ?? "Aucune tâche récente."}
      </p>
    );
  }

  return (
    <div className={["overflow-x-auto", className].filter(Boolean).join(" ")}>
      <table className="w-full border-collapse text-[12px]">
        <thead>
          <tr className="text-left text-[11px] text-text-muted">
            <th className="py-1.5 pr-3 font-normal">Heure</th>
            <th className="py-1.5 pr-3 font-normal">Description</th>
            <th className="py-1.5 pr-3 font-normal">Durée</th>
            <th className="py-1.5 font-normal">État</th>
          </tr>
        </thead>
        <tbody>
          {entries.map((task) => (
            <tr key={task.upid} className="border-t-[0.5px] border-border">
              <td className="py-1.5 pr-3 whitespace-nowrap tabular-nums text-text-secondary">
                {formatTime(task.start)}
              </td>
              <td className="py-1.5 pr-3 text-text-primary">
                {formatTaskLabel(task)}
              </td>
              <td className="py-1.5 pr-3 whitespace-nowrap tabular-nums text-text-secondary">
                {task.duration === null ? FALLBACK : formatUptime(task.duration)}
              </td>
              <td className="py-1.5">
                <TaskStatus task={task} />
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

const OUTCOME_VARIANTS: Record<TaskOutcome, TagVariant> = {
  running: "accent",
  ok: "success",
  // Amber, as PVE's own interface renders it: the job ran, and something in it
  // deserves a look. Red would say it did not happen.
  warnings: "warning",
  failed: "danger",
};

function TaskStatus({ task }: { task: Task }) {
  const variant = OUTCOME_VARIANTS[task.outcome] ?? "neutral";
  const label = formatTaskOutcome(task.outcome, task.warnings);
  if (task.outcome === "running" || task.outcome === "ok") {
    return <Tag variant={variant}>{label}</Tag>;
  }
  // PVE puts the raw string in status — the error message, or "WARNINGS: 2".
  // It is diagnostic material, so it is exposed as a tooltip rather than
  // stretching the row.
  return (
    <Tag variant={variant} className="max-w-[14rem] truncate">
      <span title={task.status}>{label}</span>
    </Tag>
  );
}
