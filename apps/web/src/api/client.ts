/**
 * Fetch layer for moxyd's read API.
 *
 * Paths are relative on purpose: the dev server proxies /api to moxyd, so the
 * browser only ever talks to a single origin and no CORS handshake exists.
 *
 * Every error the backend reports is English, by design (see CLAUDE.md): it is
 * diagnostic material for logs and developer tooling, never a label. Turning a
 * failure into French prose is the rendering layer's job.
 */
import type {
  GuestDetail,
  MaintenanceAction,
  MaintenancePlan,
  MaintenanceResult,
  NodeDetail,
  Overview,
  Series,
  Tasks,
  Timeframe,
} from "@/api/types";

export const OVERVIEW_PATH = "/api/overview";
export const HEALTH_PATH = "/healthz";
/** Where the shared token of `auth.mode: "token"` is exchanged for a cookie. */
export const LOGIN_PATH = "/api/login";

/** The request reached a server that refused it, or never reached one at all. */
export class ApiRequestError extends Error {
  /**
   * The path that was requested.
   *
   * Required, and there is no default: a message built on OVERVIEW_PATH
   * whatever the call would have a detail route reporting
   * `GET /api/overview failed` the day a caller stopped passing its own text.
   */
  readonly path: string;
  /** HTTP status code, or 0 when no response was ever received. */
  readonly status: number;
  /** The backend's `{"error": "..."}` message, in English, when it sent one. */
  readonly detail: string | null;
  /**
   * The backend's `{"kind": "..."}`, the stable word the UI translates by.
   *
   * Null on every route but the maintenance one: a status code alone cannot
   * separate "no quorum" from "no HA manager", and the alternative — matching
   * on the English message — breaks the first time it is reworded.
   */
  readonly kind: string | null;

  constructor(
    path: string,
    status: number,
    detail: string | null,
    kind: string | null = null,
  ) {
    super(describeFailure(path, status, detail));
    this.name = "ApiRequestError";
    this.path = path;
    this.status = status;
    this.detail = detail;
    this.kind = kind;
  }
}

/** The response arrived but its body was unreadable or not the expected JSON. */
export class ApiParseError extends Error {
  constructor(message: string, options?: { cause?: unknown }) {
    super(message, options);
    this.name = "ApiParseError";
  }
}

/**
 * A cancelled request is not a failure: callers abort on unmount and before
 * starting a fresh poll, and must be able to tell that apart from a real error.
 */
export function isAbortError(cause: unknown): boolean {
  // Not an `instanceof Error` check: an abort surfaces as a DOMException,
  // whose inheritance chain varies between runtimes and test environments.
  return (
    typeof cause === "object" &&
    cause !== null &&
    "name" in cause &&
    cause.name === "AbortError"
  );
}

/**
 * Performs one GET and returns the decoded JSON, or throws.
 *
 * Shared by every endpoint so that transport failures, non-2xx statuses,
 * unreadable bodies and invalid JSON are reported the same way wherever they
 * happen; the caller only adds the shape check for what it expects.
 */
async function requestJSON(path: string, signal?: AbortSignal): Promise<unknown> {
  let response: Response;
  try {
    response = await fetch(path, {
      method: "GET",
      headers: { Accept: "application/json" },
      signal,
    });
  } catch (cause) {
    if (isAbortError(cause)) {
      throw cause;
    }
    throw new ApiRequestError(path, 0, null);
  }

  let body: string;
  try {
    body = await response.text();
  } catch (cause) {
    if (isAbortError(cause)) {
      throw cause;
    }
    if (!response.ok) {
      throw new ApiRequestError(path, response.status, null);
    }
    throw new ApiParseError(`GET ${path} returned a body that could not be read`, {
      cause,
    });
  }

  if (!response.ok) {
    throw new ApiRequestError(path, response.status, backendError(body));
  }

  try {
    return JSON.parse(body) as unknown;
  } catch (cause) {
    throw new ApiParseError(`GET ${path} returned a body that is not valid JSON`, {
      cause,
    });
  }
}

/**
 * Reads the aggregated overview. Rejects with ApiRequestError on a non-2xx
 * status or a transport failure, with ApiParseError on an unusable body, and
 * with the original AbortError when `signal` is aborted.
 */
export async function fetchOverview(signal?: AbortSignal): Promise<Overview> {
  const parsed = await requestJSON(OVERVIEW_PATH, signal);
  if (!isOverview(parsed)) {
    throw new ApiParseError(
      `GET ${OVERVIEW_PATH} returned JSON that is not an overview`,
    );
  }
  return parsed;
}

/**
 * Hands the shared token to moxyd, which answers with the cookie that carries
 * it from then on.
 *
 * The token is written nowhere else: not in the URL, not in a query string, not
 * in localStorage. The cookie moxyd sets is `HttpOnly`, so this code cannot
 * read it back either — which is the point. A refusal is an ApiRequestError
 * with status 401, exactly like any other refusal, and it says nothing about
 * what was typed.
 *
 * The JSON content type is load-bearing on the other side: moxyd refuses a
 * login posted as a form, which is what a page on another site could send.
 */
export async function login(token: string, signal?: AbortSignal): Promise<void> {
  let response: Response;
  try {
    response = await fetch(LOGIN_PATH, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      // Explicit rather than implied: the whole exchange is about a cookie.
      credentials: "same-origin",
      body: JSON.stringify({ token }),
      signal,
    });
  } catch (cause) {
    if (isAbortError(cause)) {
      throw cause;
    }
    throw new ApiRequestError(LOGIN_PATH, 0, null);
  }

  if (!response.ok) {
    const body = await response.text().catch(() => "");
    throw new ApiRequestError(LOGIN_PATH, response.status, backendError(body));
  }
}

/**
 * Shape of the liveness answer.
 *
 * It belongs to the server package rather than to a model file, which is why it
 * is declared here and not in types.ts: nothing in the aggregate or detail
 * contract mirrors it.
 */
export interface Health {
  status: string;
  /** The revision moxyd was built from, e.g. "0862b0c". */
  version: string;
}

/**
 * Reads the liveness endpoint, of which the top bar only shows the version.
 *
 * Fails like every other call, and callers are expected to swallow it: a
 * version is a label, not a reading, and losing it must not be reported twice —
 * a daemon that cannot answer here is already reported by the overview.
 */
export async function fetchHealth(signal?: AbortSignal): Promise<Health> {
  const parsed = await requestJSON(HEALTH_PATH, signal);
  if (!isHealth(parsed)) {
    throw new ApiParseError(
      `GET ${HEALTH_PATH} returned JSON that is not a health answer`,
    );
  }
  return parsed;
}

/* ---------------------------------------------------------------- detail --- */

/**
 * Percent-encodes one path segment.
 *
 * Cluster ids and node names reach the URL verbatim, and the backend decodes
 * and validates them itself — it rejects empty segments, "." and "..". Encoding
 * here is what keeps a name holding a slash or an accent from silently becoming
 * a different route.
 */
function segment(value: string): string {
  return encodeURIComponent(value);
}

export function nodePath(cluster: string, node: string): string {
  return `/api/clusters/${segment(cluster)}/nodes/${segment(node)}`;
}

export function guestPath(cluster: string, vmid: number): string {
  return `/api/clusters/${segment(cluster)}/guests/${segment(String(vmid))}`;
}

export function maintenancePlanPath(cluster: string, node: string): string {
  return `${nodePath(cluster, node)}/maintenance/plan`;
}

/** The one route of this API that writes. `.../maintenance/plan` stays a read. */
export function maintenancePath(cluster: string, node: string): string {
  return `${nodePath(cluster, node)}/maintenance`;
}

export function clusterPath(cluster: string): string {
  return `/api/clusters/${segment(cluster)}`;
}

export function tasksPath(cluster: string, limit?: number): string {
  return withLimit(`/api/clusters/${segment(cluster)}/tasks`, limit);
}

/**
 * The log of one guest, which the backend reads from the node hosting it.
 *
 * A route of its own rather than a filter over the cluster journal: that
 * journal takes no filter upstream, so on a busy cluster a guest's own lines
 * fall off the end of any tail worth fetching.
 */
export function guestTasksPath(
  cluster: string,
  vmid: number,
  limit?: number,
): string {
  return withLimit(`${guestPath(cluster, vmid)}/tasks`, limit);
}

function withLimit(base: string, limit?: number): string {
  return limit === undefined ? base : `${base}?limit=${String(limit)}`;
}

function seriesPath(base: string, timeframe: Timeframe): string {
  return `${base}/rrd?timeframe=${timeframe}`;
}

export async function fetchNode(
  cluster: string,
  node: string,
  signal?: AbortSignal,
): Promise<NodeDetail> {
  const path = nodePath(cluster, node);
  const parsed = await requestJSON(path, signal);
  if (
    !isRecord(parsed) ||
    typeof parsed["name"] !== "string" ||
    typeof parsed["status"] !== "string" ||
    !Array.isArray(parsed["guests"]) ||
    !hasCPU(parsed["cpu"]) ||
    !hasUsage(parsed["memory"]) ||
    !hasUsage(parsed["swap"]) ||
    !hasUsage(parsed["rootfs"])
  ) {
    throw new ApiParseError(`GET ${path} returned JSON that is not a node`);
  }
  return parsed as unknown as NodeDetail;
}

export async function fetchGuest(
  cluster: string,
  vmid: number,
  signal?: AbortSignal,
): Promise<GuestDetail> {
  const path = guestPath(cluster, vmid);
  const parsed = await requestJSON(path, signal);
  if (
    !isRecord(parsed) ||
    typeof parsed["vmid"] !== "number" ||
    typeof parsed["name"] !== "string" ||
    typeof parsed["status"] !== "string" ||
    !hasCPU(parsed["cpu"]) ||
    !hasUsage(parsed["memory"]) ||
    !hasUsage(parsed["disk"]) ||
    !Array.isArray(parsed["tags"])
  ) {
    throw new ApiParseError(`GET ${path} returned JSON that is not a guest`);
  }
  return parsed as unknown as GuestDetail;
}

async function fetchSeries(path: string, signal?: AbortSignal): Promise<Series> {
  const parsed = await requestJSON(path, signal);
  if (!isRecord(parsed) || !Array.isArray(parsed["points"])) {
    throw new ApiParseError(`GET ${path} returned JSON that is not a series`);
  }
  return parsed as unknown as Series;
}

export function fetchNodeSeries(
  cluster: string,
  node: string,
  timeframe: Timeframe,
  signal?: AbortSignal,
): Promise<Series> {
  return fetchSeries(seriesPath(nodePath(cluster, node), timeframe), signal);
}

/**
 * The history of a whole cluster, which the backend folds from its nodes: PVE
 * has no cluster-wide RRD to ask for.
 */
export function fetchClusterSeries(
  cluster: string,
  timeframe: Timeframe,
  signal?: AbortSignal,
): Promise<Series> {
  return fetchSeries(seriesPath(clusterPath(cluster), timeframe), signal);
}

export function fetchGuestSeries(
  cluster: string,
  vmid: number,
  timeframe: Timeframe,
  signal?: AbortSignal,
): Promise<Series> {
  return fetchSeries(seriesPath(guestPath(cluster, vmid), timeframe), signal);
}

export async function fetchMaintenancePlan(
  cluster: string,
  node: string,
  signal?: AbortSignal,
): Promise<MaintenancePlan> {
  const path = maintenancePlanPath(cluster, node);
  const parsed = await requestJSON(path, signal);
  if (!isRecord(parsed) || !Array.isArray(parsed["moves"]) || !Array.isArray(parsed["targets"])) {
    throw new ApiParseError(`GET ${path} returned JSON that is not a maintenance plan`);
  }
  return parsed as unknown as MaintenancePlan;
}

/**
 * Asks moxyd to put a node into maintenance, or to take it out.
 *
 * The only call of this layer that changes anything upstream, and the reason
 * it is written out rather than folded into `requestJSON`: that helper is a
 * GET, and making it take a method would put a body and a verb on eight routes
 * that must never carry either.
 *
 * THE JSON CONTENT TYPE IS LOAD-BEARING, exactly as it is for `login`: moxyd
 * answers 415 without it. An HTML form on another site can post
 * `application/x-www-form-urlencoded`, `multipart/form-data` or `text/plain`
 * with no preflight; it cannot post `application/json`. Demanding it is what
 * makes a cross-site drain go through fetch(), which CORS holds at the door.
 *
 * A refusal is an ApiRequestError carrying the backend's `kind`, which is what
 * `formatMaintenanceError` turns into a sentence. The answer says the request
 * went through — never that the node is drained.
 */
export async function requestMaintenance(
  cluster: string,
  node: string,
  action: MaintenanceAction,
  signal?: AbortSignal,
): Promise<MaintenanceResult> {
  const path = maintenancePath(cluster, node);
  let response: Response;
  try {
    response = await fetch(path, {
      method: "POST",
      headers: { "Content-Type": "application/json", Accept: "application/json" },
      credentials: "same-origin",
      body: JSON.stringify({ action }),
      signal,
    });
  } catch (cause) {
    if (isAbortError(cause)) {
      throw cause;
    }
    throw new ApiRequestError(path, 0, null);
  }

  const body = await response.text().catch(() => "");
  if (!response.ok) {
    const failure = backendFailure(body);
    throw new ApiRequestError(path, response.status, failure.detail, failure.kind);
  }

  let parsed: unknown;
  try {
    parsed = JSON.parse(body);
  } catch (cause) {
    throw new ApiParseError(`POST ${path} returned a body that is not valid JSON`, {
      cause,
    });
  }
  if (
    !isRecord(parsed) ||
    typeof parsed["action"] !== "string" ||
    typeof parsed["accepted"] !== "boolean" ||
    typeof parsed["alreadyInState"] !== "boolean"
  ) {
    throw new ApiParseError(
      `POST ${path} returned JSON that is not a maintenance result`,
    );
  }
  return parsed as unknown as MaintenanceResult;
}

export function fetchTasks(
  cluster: string,
  limit?: number,
  signal?: AbortSignal,
): Promise<Tasks> {
  return fetchTaskList(tasksPath(cluster, limit), signal);
}

export function fetchGuestTasks(
  cluster: string,
  vmid: number,
  limit?: number,
  signal?: AbortSignal,
): Promise<Tasks> {
  return fetchTaskList(guestTasksPath(cluster, vmid, limit), signal);
}

async function fetchTaskList(path: string, signal?: AbortSignal): Promise<Tasks> {
  const parsed = await requestJSON(path, signal);
  if (!isRecord(parsed) || !Array.isArray(parsed["entries"])) {
    throw new ApiParseError(`GET ${path} returned JSON that is not a task list`);
  }
  return parsed as unknown as Tasks;
}

function describeFailure(
  path: string,
  status: number,
  detail: string | null,
): string {
  const subject =
    status === 0
      ? `GET ${path} could not reach the server`
      : `GET ${path} failed with HTTP ${String(status)}`;
  return detail === null ? subject : `${subject}: ${detail}`;
}

/** Extracts the backend's `{"error": "..."}` payload, if that is what this is. */
function backendError(body: string): string | null {
  return backendFailure(body).detail;
}

/**
 * The same payload, kind included.
 *
 * Kept apart from `backendError` so that the eight read routes keep the one
 * line they need: `kind` is omitted everywhere it would say nothing, and only
 * the maintenance route ever fills it in.
 */
function backendFailure(body: string): { detail: string | null; kind: string | null } {
  if (body.trim() === "") {
    return { detail: null, kind: null };
  }
  let parsed: unknown;
  try {
    parsed = JSON.parse(body);
  } catch {
    return { detail: null, kind: null };
  }
  if (!isRecord(parsed)) {
    return { detail: null, kind: null };
  }
  const message = parsed["error"];
  const kind = parsed["kind"];
  return {
    detail: typeof message === "string" && message !== "" ? message : null,
    kind: typeof kind === "string" && kind !== "" ? kind : null,
  };
}

/**
 * A shallow shape check, not a schema validation: it catches a proxy serving an
 * HTML error page or a stray JSON literal, which is the realistic failure, and
 * leaves the field-by-field contract to types.ts.
 */
function isOverview(value: unknown): value is Overview {
  return (
    isRecord(value) &&
    typeof value["generatedAt"] === "string" &&
    isRecord(value["totals"]) &&
    Array.isArray(value["clusters"])
  );
}

function isHealth(value: unknown): value is Health {
  return (
    isRecord(value) &&
    typeof value["status"] === "string" &&
    typeof value["version"] === "string"
  );
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

/**
 * The two shapes the detail screens dereference on their FIRST render.
 *
 * They are guarded — and only they — because a missing one is not a field the
 * UI renders as a dash but a TypeError thrown mid-render: `node.cpu.ratio`
 * with no `cpu` used to unmount the whole application, top bar and tree
 * included. Turning it into an ApiParseError means the screen explains itself
 * and offers a retry, the way every other failure already does.
 *
 * This is deliberately NOT a schema. The contract is types.ts, checked against
 * generated fixtures by types.contract.test.ts; re-stating it here would be a
 * second definition to keep in step. What is checked is what would crash.
 */
function hasCPU(value: unknown): boolean {
  return isRecord(value) && typeof value["ratio"] === "number";
}

/**
 * A used/total pair. `used` is deliberately not checked for a number: a
 * guest's boot disk reports it as null when no agent measured it, which is a
 * value the UI renders, not a crash.
 */
function hasUsage(value: unknown): boolean {
  return isRecord(value) && typeof value["total"] === "number";
}
