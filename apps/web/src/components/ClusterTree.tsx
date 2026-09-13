import { useEffect, useMemo, useRef, useState } from "react";
import type { KeyboardEvent, ReactNode } from "react";
import {
  IconChevronDown,
  IconChevronRight,
  IconTemplate,
  IconTool,
  IconTopologyStar3,
} from "@tabler/icons-react";

import type { ClusterOverview, Guest, Node } from "@/api/types";
import {
  formatClusterStatus,
  formatGuestName,
  formatGuestStatus,
  formatNodeStatus,
} from "@/lib/format";
import {
  countMatches,
  filterClusters,
  formatMatchCount,
  normalizeQuery,
} from "@/lib/search";
import { ClusterAccent, StatusDot, Tag } from "@/components/ui";

/**
 * The three-level sidebar tree of section 2 of the handoff: cluster → node → VM.
 *
 * It reproduces the mental geography of the native Proxmox UI on purpose — an
 * administrator must not have to relearn where things live — while fixing the
 * two defects appendix A.2 calls out: four state icons replaced by a single 7px
 * dot, and guests listed under their own full name — no vmid taking the width of
 * a narrow panel, and no rewriting of the name the administrator knows.
 *
 * The component owns its expansion state only. The selection is controlled by
 * the caller, and comes from the URL: the address bar is what the main pane
 * and this tree both read, so a row clicked here is a navigation, and a link
 * pasted into the address bar opens the same row.
 */
export type TreeSelection =
  | { kind: "all" }
  | { kind: "cluster"; clusterId: string }
  | { kind: "node"; clusterId: string; node: string }
  | { kind: "guest"; clusterId: string; node: string; vmid: number };

interface ClusterTreeProps {
  clusters: ClusterOverview[];
  selection: TreeSelection;
  onSelect: (selection: TreeSelection) => void;
  /**
   * The global search query. Rows that do not answer it are hidden, the ones
   * that do are shown with the path down to them, and everything left is
   * expanded: a result buried in a collapsed branch is a result nobody sees.
   */
  query?: string;
  className?: string;
}

/** Displayed labels are French, sentence case. */
const TREE_LABEL = "Arborescence des clusters";
/*
 * A cluster is declared in the server configuration, never from the browser:
 * doing it here would mean carrying a hypervisor token through the browser.
 * The empty tree says where clusters come from instead of offering a button.
 */
const EMPTY_TREE_LABEL = "Aucun cluster configuré";
const MAINTENANCE_ICON_LABEL = "Maintenance planifiée";
const TEMPLATE_ICON_LABEL = formatGuestStatus("template");

/*
 * Row identity. Every visible row has a stable key used for expansion, for the
 * roving tabindex and for keyboard navigation, so the tree is manipulated as a
 * flat list even though it renders as three levels.
 */

function clusterKey(clusterId: string): string {
  return `cluster:${clusterId}`;
}

function nodeKey(clusterId: string, node: string): string {
  return `node:${clusterId}/${node}`;
}

function guestKey(clusterId: string, node: string, vmid: number): string {
  return `guest:${clusterId}/${node}/${vmid}`;
}

/** The key of the row the caller selected, or "" when nothing in the tree is. */
function selectionKey(selection: TreeSelection): string {
  switch (selection.kind) {
    case "cluster":
      return clusterKey(selection.clusterId);
    case "node":
      return nodeKey(selection.clusterId, selection.node);
    case "guest":
      return guestKey(selection.clusterId, selection.node, selection.vmid);
    case "all":
      return "";
    default:
      return "";
  }
}

/**
 * Rows that must be open for the current selection to be visible.
 *
 * A selected container opens itself as well: selecting a node is how one asks
 * to see its guests, which is what the mockup of appendix A.2 shows.
 */
function requiredExpansion(selection: TreeSelection): string[] {
  switch (selection.kind) {
    case "cluster":
      return [clusterKey(selection.clusterId)];
    case "node":
      return [
        clusterKey(selection.clusterId),
        nodeKey(selection.clusterId, selection.node),
      ];
    case "guest":
      return [
        clusterKey(selection.clusterId),
        nodeKey(selection.clusterId, selection.node),
      ];
    case "all":
      return [];
    default:
      return [];
  }
}

/**
 * A node in maintenance counts as online.
 *
 * Same convention as ComputeTotals in the backend (internal/aggregate/derive.go):
 * such a node still answers, still votes and still runs its guests — it merely
 * refuses to take new ones. Counting it as down would turn a planned operation
 * into a false alarm, and would paint an amber counter on a healthy cluster.
 */
function isNodeOnline(node: Node): boolean {
  return node.status === "online" || node.status === "maintenance";
}

interface NodeCounter {
  online: number;
  total: number;
  /** Green only when every node answers; amber as soon as one does not. */
  variant: "success" | "warning";
}

function nodeCounter(cluster: ClusterOverview): NodeCounter {
  const nodes = Array.isArray(cluster.nodes) ? cluster.nodes : [];
  const online = nodes.filter(isNodeOnline).length;
  const total = nodes.length;
  // 0/0 — an unreachable cluster whose node list never arrived — is not a
  // success: nothing is known to be up, so the counter stays amber.
  return { online, total, variant: total > 0 && online === total ? "success" : "warning" };
}

type RowKind = "cluster" | "node" | "guest";

interface TreeRow {
  key: string;
  kind: RowKind;
  level: 1 | 2 | 3;
  /** Key of the row one level up, used by ArrowLeft. */
  parentKey: string | null;
  /** A container with no child is not expandable and exposes no aria-expanded. */
  expandable: boolean;
  expanded: boolean;
  posInSet: number;
  setSize: number;
  /** What onSelect emits for this row. */
  selection: TreeSelection;
  cluster: ClusterOverview;
  node: Node | null;
  guest: Guest | null;
}

/**
 * Flattens the visible part of the tree, parents before children.
 *
 * `isExpanded` is a predicate rather than a set, because a search answers it
 * for every key at once: the filter has already decided what is worth showing,
 * and everything it kept is open.
 *
 * Nothing here assumes a list is non-empty: an unreachable cluster can have no
 * node at all, and `guests` is optional in the API payload — it is absent until
 * the backend reports the guests of a node.
 */
function buildRows(
  clusters: ClusterOverview[],
  isExpanded: (key: string) => boolean,
): TreeRow[] {
  const rows: TreeRow[] = [];
  const clusterList = Array.isArray(clusters) ? clusters : [];

  clusterList.forEach((cluster, clusterIndex) => {
    const nodes = Array.isArray(cluster.nodes) ? cluster.nodes : [];
    const key = clusterKey(cluster.id);
    const clusterExpanded = isExpanded(key) && nodes.length > 0;

    rows.push({
      key,
      kind: "cluster",
      level: 1,
      parentKey: null,
      expandable: nodes.length > 0,
      expanded: clusterExpanded,
      posInSet: clusterIndex + 1,
      setSize: clusterList.length,
      selection: { kind: "cluster", clusterId: cluster.id },
      cluster,
      node: null,
      guest: null,
    });

    if (!clusterExpanded) {
      return;
    }

    nodes.forEach((node, nodeIndex) => {
      const guests = Array.isArray(node.guests) ? node.guests : [];
      const childKey = nodeKey(cluster.id, node.name);
      const isNodeExpanded = isExpanded(childKey) && guests.length > 0;

      rows.push({
        key: childKey,
        kind: "node",
        level: 2,
        parentKey: key,
        expandable: guests.length > 0,
        expanded: isNodeExpanded,
        posInSet: nodeIndex + 1,
        setSize: nodes.length,
        selection: { kind: "node", clusterId: cluster.id, node: node.name },
        cluster,
        node,
        guest: null,
      });

      if (!isNodeExpanded) {
        return;
      }

      guests.forEach((guest, guestIndex) => {
        rows.push({
          key: guestKey(cluster.id, node.name, guest.vmid),
          kind: "guest",
          level: 3,
          parentKey: childKey,
          expandable: false,
          expanded: false,
          posInSet: guestIndex + 1,
          setSize: guests.length,
          selection: {
            kind: "guest",
            clusterId: cluster.id,
            node: node.name,
            vmid: guest.vmid,
          },
          cluster,
          node,
          guest,
        });
      });
    });
  });

  return rows;
}

const ROW_CLASSES =
  "flex w-full items-center gap-1.5 rounded-card pr-2 py-1 text-left " +
  "text-[12px] hover:bg-fill-ghost-selected focus:outline-none " +
  "focus-visible:outline-1 focus-visible:outline-accent";

/** Indentation of the mockup: 8px, 20px, 36px. */
const INDENT_CLASSES: Record<RowKind, string> = {
  cluster: "pl-2 mt-1.5",
  node: "pl-5",
  guest: "pl-9",
};

/**
 * Only one text colour class is ever emitted per row: two competing `text-*`
 * utilities would leave the winner to the order of the generated stylesheet.
 */
function toneClasses(kind: RowKind, isSelected: boolean): string {
  if (isSelected) {
    return "bg-bg-accent text-text-accent font-medium";
  }
  return kind === "cluster"
    ? "text-text-primary font-medium"
    : "text-text-secondary";
}

export function ClusterTree({
  clusters,
  selection,
  onSelect,
  query = "",
  className,
}: ClusterTreeProps) {
  const searching = normalizeQuery(query) !== "";
  const shown = useMemo(() => filterClusters(clusters, query), [clusters, query]);
  const matchCount = useMemo(() => countMatches(clusters, query), [clusters, query]);
  const selectedKey = selectionKey(selection);
  // Joined rather than kept as an array so the effect below compares by value.
  const expansionSeed = requiredExpansion(selection).join("\n");

  const [expanded, setExpanded] = useState<ReadonlySet<string>>(
    () => new Set(expansionSeed ? expansionSeed.split("\n") : []),
  );
  const [activeKey, setActiveKey] = useState(selectedKey);
  const itemRefs = useRef(new Map<string, HTMLDivElement>());

  // The selection moves on its own whenever the URL does — a click in the main
  // pane, a pasted link, the back button.
  // Its ancestors are opened, and are then left alone: a cluster the user
  // collapses afterwards stays collapsed until the selection moves again.
  useEffect(() => {
    setActiveKey(selectedKey);
    if (!expansionSeed) {
      return;
    }
    const required = expansionSeed.split("\n");
    setExpanded((previous) => {
      if (required.every((key) => previous.has(key))) {
        return previous;
      }
      const next = new Set(previous);
      for (const key of required) {
        next.add(key);
      }
      return next;
    });
  }, [selectedKey, expansionSeed]);

  // While searching, every remaining branch is open: the filter has already
  // decided what is worth showing, and leaving a result folded away would be
  // the same as not showing it.
  const rows = buildRows(shown, (key) => searching || expanded.has(key));
  // The roving tabindex falls back to the first row when the active one is
  // hidden or gone, so the tree always keeps exactly one tab stop.
  const activeIndex = rows.findIndex((row) => row.key === activeKey);
  const currentKey = activeIndex === -1 ? (rows[0]?.key ?? "") : activeKey;

  function focusRow(key: string) {
    setActiveKey(key);
    itemRefs.current.get(key)?.focus();
  }

  function toggle(key: string) {
    setExpanded((previous) => {
      const next = new Set(previous);
      if (next.has(key)) {
        next.delete(key);
      } else {
        next.add(key);
      }
      return next;
    });
  }

  function onKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    if (rows.length === 0) {
      return;
    }
    // The key press belongs to the row that holds the focus, which is the
    // source of truth: the roving tabindex only says where focus will land on
    // the way in, not where it is now.
    const origin =
      event.target instanceof Element
        ? event.target.closest("[data-tree-key]")?.getAttribute("data-tree-key")
        : null;
    const fromTarget = rows.findIndex((candidate) => candidate.key === origin);
    const index = fromTarget !== -1 ? fromTarget : activeIndex === -1 ? 0 : activeIndex;
    const row = rows[index];
    if (row === undefined) {
      return;
    }

    switch (event.key) {
      case "ArrowDown": {
        event.preventDefault();
        const next = rows[index + 1];
        if (next !== undefined) {
          focusRow(next.key);
        }
        break;
      }
      case "ArrowUp": {
        event.preventDefault();
        const previous = rows[index - 1];
        if (previous !== undefined) {
          focusRow(previous.key);
        }
        break;
      }
      case "ArrowRight": {
        event.preventDefault();
        if (!row.expandable) {
          break;
        }
        if (row.expanded) {
          // Already open: the next visible row is this row's first child.
          const child = rows[index + 1];
          if (child !== undefined) {
            focusRow(child.key);
          }
        } else {
          toggle(row.key);
        }
        break;
      }
      case "ArrowLeft": {
        event.preventDefault();
        if (row.expandable && row.expanded) {
          toggle(row.key);
        } else if (row.parentKey !== null) {
          focusRow(row.parentKey);
        }
        break;
      }
      case "Home": {
        event.preventDefault();
        const first = rows[0];
        if (first !== undefined) {
          focusRow(first.key);
        }
        break;
      }
      case "End": {
        event.preventDefault();
        const last = rows[rows.length - 1];
        if (last !== undefined) {
          focusRow(last.key);
        }
        break;
      }
      case "Enter":
      case " ": {
        event.preventDefault();
        onSelect(row.selection);
        break;
      }
      default:
        break;
    }
  }

  const classes = ["flex flex-col", className].filter(Boolean).join(" ");

  return (
    <div className={classes}>
      {/*
        The tree container is deliberately NOT focusable: the pattern is a
        roving tabindex on the items, so the tree keeps exactly one tab stop
        and the arrows move between rows. Its keydown routes for them.
      */}
      {/* eslint-disable-next-line jsx-a11y/interactive-supports-focus -- roving tabindex */}
      <div role="tree" aria-label={TREE_LABEL} onKeyDown={onKeyDown}>
        {rows.map((row) => {
          const isSelected = row.key === selectedKey;
          return (
            // A treeitem IS interactive, and the keys are handled once on the
            // tree above rather than on every row: the rule looks for a
            // listener on the element it is reading.
            // eslint-disable-next-line jsx-a11y/click-events-have-key-events -- keys are delegated to the tree
            <div
              key={row.key}
              ref={(element) => {
                if (element === null) {
                  itemRefs.current.delete(row.key);
                } else {
                  itemRefs.current.set(row.key, element);
                }
              }}
              role="treeitem"
              data-tree-key={row.key}
              aria-level={row.level}
              aria-posinset={row.posInSet}
              aria-setsize={row.setSize}
              aria-selected={isSelected}
              aria-expanded={row.expandable ? row.expanded : undefined}
              tabIndex={row.key === currentKey ? 0 : -1}
              className={[
                ROW_CLASSES,
                INDENT_CLASSES[row.kind],
                toneClasses(row.kind, isSelected),
              ].join(" ")}
              onClick={() => {
                setActiveKey(row.key);
                onSelect(row.selection);
              }}
            >
              <RowContent row={row} onToggle={toggle} />
            </div>
          );
        })}
      </div>

      {/*
        The count is announced rather than only drawn: a filter that silently
        empties a list leaves a screen reader with no way to know why.
      */}
      <p
        aria-live="polite"
        className={
          searching ? "px-2 py-1 text-[11px] text-text-muted" : "sr-only"
        }
      >
        {searching ? formatMatchCount(matchCount) : ""}
      </p>

      {rows.length === 0 && !searching ? (
        <p className="px-2 py-1 text-[12px] text-text-muted">
          {EMPTY_TREE_LABEL}
        </p>
      ) : null}
    </div>
  );
}

interface RowContentProps {
  row: TreeRow;
  onToggle: (key: string) => void;
}

/** The inside of a row: chevron, status, label, and the trailing badge. */
function RowContent({ row, onToggle }: RowContentProps): ReactNode {
  if (row.kind === "cluster") {
    const counter = nodeCounter(row.cluster);
    return (
      <>
        <Chevron row={row} onToggle={onToggle} />
        <IconTopologyStar3 size={13} stroke={1.75} aria-hidden />
        {/*
          The configured accent, between the generic cluster glyph and the
          name it belongs to. Decorative: the name is right next to it.
        */}
        <ClusterAccent color={row.cluster.color} />
        <span className="truncate" title={row.cluster.name}>
          {row.cluster.name}
        </span>
        {/* Read out by assistive tech; the colour of the counter carries the
            same information for everyone else. */}
        <span className="sr-only">{formatClusterStatus(row.cluster.status)}</span>
        <Tag variant={counter.variant} className="ml-auto">
          {counter.online}/{counter.total}
        </Tag>
      </>
    );
  }

  if (row.kind === "node" && row.node !== null) {
    const node = row.node;
    return (
      <>
        <Chevron row={row} onToggle={onToggle} />
        <StatusDot status={node.status} title={formatNodeStatus(node.status)} />
        <span className="truncate" title={node.name}>
          {node.name}
        </span>
        {node.status === "maintenance" ? (
          // The amber dot says something is off; only the wrench says what.
          // Section 2 requires both.
          <IconTool
            size={12}
            stroke={1.75}
            className="ml-auto shrink-0 text-text-warning-strong"
            role="img"
            aria-label={MAINTENANCE_ICON_LABEL}
          />
        ) : null}
      </>
    );
  }

  if (row.kind === "guest" && row.guest !== null) {
    const guest = row.guest;
    return (
      <>
        <span className="w-3 shrink-0" aria-hidden />
        {guest.status === "template" ? (
          <IconTemplate
            size={12}
            stroke={1.75}
            className="shrink-0"
            role="img"
            aria-label={TEMPLATE_ICON_LABEL}
          />
        ) : (
          <StatusDot status={guest.status} title={formatGuestStatus(guest.status)} />
        )}
        <span className="truncate" title={formatGuestName(guest.vmid, guest.name)}>
          {formatGuestName(guest.vmid, guest.name)}
        </span>
      </>
    );
  }

  return null;
}

/**
 * Expansion affordance. It is not a nested button: a control inside a treeitem
 * would add a tab stop the tree pattern does not have. Keyboard users expand
 * with the arrow keys, which the tree handles itself.
 */
function Chevron({ row, onToggle }: RowContentProps): ReactNode {
  if (!row.expandable) {
    return <span className="w-3 shrink-0" aria-hidden />;
  }
  const Icon = row.expanded ? IconChevronDown : IconChevronRight;
  return (
    <span
      className="flex w-3 shrink-0 items-center"
      role="presentation"
      onClick={(event) => {
        // The row selects; the chevron only opens and closes.
        event.stopPropagation();
        onToggle(row.key);
      }}
    >
      <Icon size={12} stroke={1.75} aria-hidden />
    </span>
  );
}
