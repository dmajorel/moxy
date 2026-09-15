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
import { useFormat, useT } from "@/i18n/locale";
import { useNow } from "@/lib/useNow";

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

export function ClustersOverview({
  overview,
  usage,
  onSelectCluster,
  className,
}: ClustersOverviewProps) {
  const t = useT();
  const { plural } = useFormat();
  const { totals, clusters, thresholds } = overview;
  // One ticking clock for the whole grid rather than one per card: the cards
  // show the age of their reading, and during an outage that is precisely the
  // figure that must keep moving while nothing else re-renders.
  const now = useNow();
  const classes = ["py-1", className].filter(Boolean).join(" ");

  return (
    <section className={classes}>
      <div className="mb-3.5 flex flex-wrap items-center gap-2.5">
        <h2 className="text-[18px] font-medium text-text-primary">
          {t("overview.title")}
        </h2>
        <Tag>{plural(totals.nodes, "node")}</Tag>
        <Tag>{plural(totals.vms, "vm")}</Tag>
        {totals.alerts > 0 ? (
          <Tag
            variant="warning"
            icon={<IconAlertTriangle size={11} stroke={1.75} aria-hidden />}
          >
            {plural(totals.alerts, "alert")}
          </Tag>
        ) : null}
      </div>

      {/*
        No empty state of its own: App renders EmptyView instead of this screen
        when there is no cluster to show, and a second wording for the same
        situation is a second wording to keep in step.
      */}
      <div className="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-3">
        {clusters.map((cluster) => (
          <ClusterCard
            key={cluster.id}
            cluster={cluster}
            usage={usage?.[cluster.id] ?? null}
            thresholds={thresholds}
            now={now}
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
    </section>
  );
}
