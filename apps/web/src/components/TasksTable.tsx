import type { Task } from "@/api/types";
import { Tag } from "@/components/ui";
import { formatTaskLabel, formatTime, formatUptime } from "@/lib/format";

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
                {task.duration === null ? "—" : formatUptime(task.duration)}
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

function TaskStatus({ task }: { task: Task }) {
  if (task.end === null) {
    return <Tag variant="accent">En cours</Tag>;
  }
  if (task.ok === true) {
    return <Tag variant="success">OK</Tag>;
  }
  // PVE puts the raw error string in status; it is diagnostic material, so it
  // is exposed as a tooltip rather than stretching the row.
  return (
    <Tag variant="warning" className="max-w-[14rem] truncate">
      <span title={task.status}>Échec</span>
    </Tag>
  );
}
