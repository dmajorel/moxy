/**
 * Screen 4 — the cross-cluster overview (appendix A.4 of the handoff).
 *
 * This is the screen Proxmox does not have: one card per cluster, side by side,
 * plus the three totals that tell an operator whether anything needs attention
 * before any cluster is opened.
 */
import { IconAlertTriangle } from "@tabler/icons-react";

import type { Overview, Series } from "@/api/types";
import { Tag } from "@/components/ui";
import { plural } from "@/lib/format";

import { ClusterCard } from "./ClusterCard";

export interface ClustersOverviewProps {
  overview: Overview;
  /**
   * The last hour of each cluster, by cluster id. A cluster missing from the
   * map simply has no curve yet: the card is served either way, as the backend
   * serves its last known state rather than an empty page.
   */
  usage?: Record<string, Series>;
  /** Given the cluster id when a card is activated. */
  onSelectCluster?: (id: string) => void;
  className?: string;
}

/** `11 nœuds` / `1 nœud`. Counters, not values: nothing for format.ts here. */
export function ClustersOverview({
  overview,
  usage,
  onSelectCluster,
  className,
}: ClustersOverviewProps) {
  const { totals, clusters, thresholds } = overview;
  const classes = ["py-1", className].filter(Boolean).join(" ");

  return (
    <section className={classes}>
      <div className="mb-3.5 flex flex-wrap items-center gap-2.5">
        <h2 className="text-[18px] font-medium text-text-primary">Clusters</h2>
        <Tag>{plural(totals.nodes, "nœud", "nœuds")}</Tag>
        {/* "VM" is invariable in French; only the count changes. */}
        <Tag>{plural(totals.vms, "VM", "VM")}</Tag>
        {totals.alerts > 0 ? (
          <Tag
            variant="warning"
            icon={<IconAlertTriangle size={11} stroke={1.75} aria-hidden />}
          >
            {plural(totals.alerts, "alerte", "alertes")}
          </Tag>
        ) : null}
      </div>

      {clusters.length === 0 ? (
        <div className="rounded-panel border-[0.5px] border-border bg-surface-2 px-4 py-6 text-center">
          <p className="text-[14px] font-medium text-text-primary">
            Aucun cluster configuré
          </p>
          <p className="mt-1.5 text-[12px] text-text-secondary">
            Déclarez un premier cluster côté serveur pour le voir apparaître ici.
          </p>
        </div>
      ) : (
        <div className="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-3">
          {clusters.map((cluster) => (
            <ClusterCard
              key={cluster.id}
              cluster={cluster}
              usage={usage?.[cluster.id] ?? null}
              threshold={thresholds.memory}
              onSelect={
                onSelectCluster === undefined
                  ? undefined
                  : () => {
                      onSelectCluster(cluster.id);
                    }
              }
            />
          ))}
        </div>
      )}
    </section>
  );
}
