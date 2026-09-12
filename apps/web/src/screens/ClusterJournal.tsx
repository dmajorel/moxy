import { useTasks } from "@/api/useDetail";
import { TasksTable } from "@/components/TasksTable";
import { formatRelativeTime } from "@/lib/format";

/**
 * Cluster task journal, refreshed on the same cadence as everything else.
 *
 * The handoff allows polling or SSE for this; polling is what the backend
 * already absorbs through its short cache, and it keeps the wire boring.
 */
export interface ClusterJournalProps {
  cluster: string;
  className?: string;
}

export function ClusterJournal({ cluster, className }: ClusterJournalProps) {
  const { data, error, isLoading, isStale, lastUpdatedAt } = useTasks(cluster);

  return (
    <section
      className={[
        "rounded-card border-[0.5px] border-border bg-surface-2 px-3 py-2.5",
        className,
      ]
        .filter(Boolean)
        .join(" ")}
    >
      <div className="mb-1.5 flex flex-wrap items-baseline gap-3">
        <h2 className="text-[12px] font-medium text-text-primary">Journal du cluster</h2>
        <span className="ml-auto text-[11px] text-text-muted">
          {isStale
            ? "Connexion perdue"
            : lastUpdatedAt === null
              ? ""
              : `Mis à jour ${formatRelativeTime(lastUpdatedAt)}`}
        </span>
      </div>

      {isLoading ? (
        <p className="text-[12px] text-text-muted">Chargement…</p>
      ) : data === null ? (
        <p className="text-[12px] text-text-muted">
          {error === null ? "Aucune tâche." : "Journal indisponible pour le moment."}
        </p>
      ) : (
        <TasksTable entries={data.entries} emptyHint="Aucune tâche récente." />
      )}
    </section>
  );
}
