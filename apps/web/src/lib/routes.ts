/**
 * The URL is the selection.
 *
 * A supervision UI that cannot be linked to is a single-player tool. An
 * operator who opens a node during an incident has to be able to refresh, to
 * paste the address into an on-call channel, and to go back — and ten tabs
 * called "moxy" are ten tabs nobody can tell apart.
 *
 * Four routes, and nothing else in the path:
 *
 *   /                                   every cluster
 *   /clusters/{id}                      one cluster
 *   /clusters/{id}/nodes/{node}         one node
 *   /clusters/{id}/guests/{vmid}        one guest
 *
 * They mirror the API on purpose: an operator reading a moxy link and one
 * reading a moxyd log see the same shape. The guest route carries NO node,
 * unlike the tree's own selection — a guest migrates, and a link that pinned
 * the node it was on would rot the moment it moved. The hosting node is looked
 * up in the overview, which is where the truth about it lives.
 */

/** What a path designates, or that it designates nothing. */
export type Route =
  | { kind: "all" }
  | { kind: "cluster"; clusterId: string }
  | { kind: "node"; clusterId: string; node: string }
  | { kind: "guest"; clusterId: string; vmid: number }
  /** The path is not one of the four. `path` is kept for the message. */
  | { kind: "notFound"; path: string };

/** The bounds PVE itself puts on a guest id. */
const MIN_VMID = 100;
const MAX_VMID = 999_999_999;

/**
 * Whether a decoded segment carries a character no path may hold.
 *
 * Tested by code point rather than by a character class: a control character
 * written into a regular expression is invisible in the source, which is
 * precisely why the linter forbids one.
 */
function hasControlCharacter(value: string): boolean {
  for (let i = 0; i < value.length; i += 1) {
    const code = value.charCodeAt(i);
    if (code < 0x20 || code === 0x7f) return true;
  }
  return false;
}

/**
 * Reads a pathname into a route.
 *
 * Every segment is validated rather than trusted: a path comes from the
 * address bar, which is to say from anyone. An empty segment, a `.` or a `..`,
 * a percent-encoding that does not decode, a vmid that is not an integer in
 * PVE's range — all of them are `notFound`, never a request built from them.
 * It is the same rule the backend applies to the same shapes, for the same
 * reason.
 */
export function parsePath(pathname: string): Route {
  const segments = splitPath(pathname);
  if (segments === null) {
    return { kind: "notFound", path: pathname };
  }
  if (segments.length === 0) {
    return { kind: "all" };
  }
  if (segments[0] !== "clusters") {
    return { kind: "notFound", path: pathname };
  }

  const clusterId = segments[1];
  if (clusterId === undefined) {
    return { kind: "notFound", path: pathname };
  }
  if (segments.length === 2) {
    return { kind: "cluster", clusterId };
  }

  const collection = segments[2];
  const name = segments[3];
  if (segments.length !== 4 || name === undefined) {
    return { kind: "notFound", path: pathname };
  }
  if (collection === "nodes") {
    return { kind: "node", clusterId, node: name };
  }
  if (collection === "guests") {
    const vmid = parseVMID(name);
    return vmid === null
      ? { kind: "notFound", path: pathname }
      : { kind: "guest", clusterId, vmid };
  }
  return { kind: "notFound", path: pathname };
}

/**
 * Writes a route back into a pathname.
 *
 * Every segment is percent-encoded: a cluster id or a node name is operator
 * data, and one containing a slash would otherwise become two segments and
 * address something else entirely.
 */
export function formatPath(route: Route): string {
  switch (route.kind) {
    case "cluster":
      return `/clusters/${encodeURIComponent(route.clusterId)}`;
    case "node":
      return `/clusters/${encodeURIComponent(route.clusterId)}/nodes/${encodeURIComponent(route.node)}`;
    case "guest":
      return `/clusters/${encodeURIComponent(route.clusterId)}/guests/${String(route.vmid)}`;
    case "notFound":
      return route.path;
    case "all":
      return "/";
    default:
      return "/";
  }
}

/**
 * Splits a pathname into decoded segments, or null when one of them is
 * unusable.
 *
 * A trailing slash is tolerated — `/clusters/prod/` is the same place as
 * `/clusters/prod`, and an operator who types one should not land on an error
 * page — but an empty segment in the middle is not: `/clusters//nodes/x` says
 * something the caller did not mean.
 */
function splitPath(pathname: string): string[] | null {
  const trimmed = pathname.replace(/\/+$/, "");
  if (trimmed === "") return [];
  if (!trimmed.startsWith("/")) return null;

  const raw = trimmed.slice(1).split("/");
  const out: string[] = [];
  for (const segment of raw) {
    const decoded = decodeSegment(segment);
    if (decoded === null) return null;
    out.push(decoded);
  }
  return out;
}

/**
 * Decodes one segment, refusing what cannot designate an object: the empty
 * string, the two dot forms that mean "somewhere else", a control character,
 * and an encoding that does not decode at all.
 */
function decodeSegment(segment: string): string | null {
  let decoded: string;
  try {
    decoded = decodeURIComponent(segment);
  } catch {
    // A stray "%" raises a URIError, and is not a segment.
    return null;
  }
  if (decoded === "" || decoded === "." || decoded === "..") return null;
  // A slash smuggled in as %2F would make one segment address two.
  if (decoded.includes("/") || decoded.includes("\\")) return null;
  if (hasControlCharacter(decoded)) return null;
  return decoded;
}

/** A vmid is a decimal integer in PVE's own range, with nothing around it. */
function parseVMID(segment: string): number | null {
  if (!/^[0-9]{1,9}$/.test(segment)) return null;
  const vmid = Number(segment);
  if (!Number.isSafeInteger(vmid) || vmid < MIN_VMID || vmid > MAX_VMID) {
    return null;
  }
  return vmid;
}
