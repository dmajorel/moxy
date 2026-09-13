import type { Task, TaskOutcome } from "@/api/types";
import type { DataTableColumn, TagVariant } from "@/components/ui";
import { DataTable, Tag } from "@/components/ui";
import {
  FALLBACK,
  formatTaskLabel,
  formatTaskOutcome,
  formatTaskTime,
  formatUptime,
} from "@/lib/format";
import { useNow } from "@/lib/useNow";

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
  // One clock for the table: "today" is a comparison, and it must not be made
  // against a different instant for each row.
  const now = useNow(60_000);

  const columns: DataTableColumn<Task>[] = [
    {
      header: "Heure",
      cellClassName: "whitespace-nowrap tabular-nums text-text-secondary",
      // The date appears only when the task is not from today. The tooltip
      // carries the timestamp the backend served, which is UTC and RFC 3339:
      // the column is local time, and nothing on screen said which was which.
      render: (task) => (
        <span title={task.start}>{formatTaskTime(task.start, now)}</span>
      ),
    },
    {
      header: "Description",
      cellClassName: "text-text-primary",
      render: (task) => formatTaskLabel(task),
    },
    {
      header: "Durée",
      cellClassName: "whitespace-nowrap tabular-nums text-text-secondary",
      render: (task) =>
        task.duration === null ? FALLBACK : formatUptime(task.duration),
    },
    {
      header: "État",
      render: (task) => <TaskStatus task={task} />,
    },
  ];

  return (
    <DataTable
      caption="Tâches récentes, de la plus récente à la plus ancienne"
      columns={columns}
      rows={entries}
      rowKey={(task) => task.upid}
      emptyHint={emptyHint ?? "Aucune tâche récente."}
      className={className}
    />
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
