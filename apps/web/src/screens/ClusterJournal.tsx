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
  const { data, isLoading, isStale, lastUpdatedAt } = useTasks(cluster);

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
        {/*
          The line changes mid-session -- "Mis à jour il y a 3 s" becoming
          "Connexion perdue" -- and an announcement is the only way a screen
          reader learns that the journal has stopped moving.
        */}
        <span role="status" className="ml-auto text-[11px] text-text-muted">
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
        // Nothing read and not loading means the attempt failed: isLoading is
        // exactly "no data and no error", so this branch cannot be reached
        // with error === null. An empty journal is a Tasks with no entry, and
        // the table below says so itself.
        <p className="text-[12px] text-text-muted">
          Journal indisponible pour le moment.
        </p>
      ) : (
        <TasksTable entries={data.entries} />
      )}
    </section>
  );
}
