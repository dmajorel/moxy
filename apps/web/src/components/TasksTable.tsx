import type { Task, TaskOutcome } from "@/api/types";
import type { DataColumn, TagVariant } from "@/components/ui";
import { DataTable, Tag } from "@/components/ui";
import { useFormat, useT } from "@/i18n/locale";
import { FALLBACK, formatTaskTime } from "@/lib/format";
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
  const t = useT();
  const { formatTaskLabel, formatUptime } = useFormat();
  // One clock for the table: "today" is a comparison, and it must not be made
  // against a different instant for each row.
  const now = useNow(60_000);

  /** The columns, in the order the handoff draws them. */
  const columns: DataColumn[] = [
    { key: "time", header: t("tasks.column.time"), numeric: true, nowrap: true, tone: "secondary" },
    { key: "label", header: t("tasks.column.label") },
    {
      key: "duration",
      header: t("tasks.column.duration"),
      numeric: true,
      nowrap: true,
      tone: "secondary",
    },
    { key: "outcome", header: t("tasks.column.outcome") },
  ];

  return (
    <DataTable
      caption={t("tasks.caption")}
      columns={columns}
      emptyHint={emptyHint ?? t("tasks.empty")}
      className={className}
      rows={entries.map((task) => ({
        key: task.upid,
        cells: {
          /*
            The date appears only when the task is not from today. The tooltip
            carries the timestamp the backend served, which is UTC and
            RFC 3339: the column is local time, and nothing on screen said
            which was which.
          */
          time: <span title={task.start}>{formatTaskTime(task.start, now)}</span>,
          label: formatTaskLabel(task),
          duration: task.duration === null ? FALLBACK : formatUptime(task.duration),
          outcome: <TaskStatus task={task} />,
        },
      }))}
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
  const { formatTaskOutcome } = useFormat();
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
