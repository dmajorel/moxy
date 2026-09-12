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
import type { Overview } from "@/api/types";

export const OVERVIEW_PATH = "/api/overview";

/** The request reached a server that refused it, or never reached one at all. */
export class ApiRequestError extends Error {
  /** HTTP status code, or 0 when no response was ever received. */
  readonly status: number;
  /** The backend's `{"error": "..."}` message, in English, when it sent one. */
  readonly detail: string | null;

  constructor(status: number, detail: string | null, message?: string) {
    super(message ?? describeFailure(status, detail));
    this.name = "ApiRequestError";
    this.status = status;
    this.detail = detail;
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
 * Reads the aggregated overview. Rejects with ApiRequestError on a non-2xx
 * status or a transport failure, with ApiParseError on an unusable body, and
 * with the original AbortError when `signal` is aborted.
 */
export async function fetchOverview(signal?: AbortSignal): Promise<Overview> {
  let response: Response;
  try {
    response = await fetch(OVERVIEW_PATH, {
      method: "GET",
      headers: { Accept: "application/json" },
      signal,
    });
  } catch (cause) {
    if (isAbortError(cause)) {
      throw cause;
    }
    throw new ApiRequestError(
      0,
      null,
      `GET ${OVERVIEW_PATH} could not reach the server`,
    );
  }

  let body: string;
  try {
    body = await response.text();
  } catch (cause) {
    if (isAbortError(cause)) {
      throw cause;
    }
    if (!response.ok) {
      throw new ApiRequestError(response.status, null);
    }
    throw new ApiParseError(
      `GET ${OVERVIEW_PATH} returned a body that could not be read`,
      { cause },
    );
  }

  if (!response.ok) {
    throw new ApiRequestError(response.status, backendError(body));
  }

  let parsed: unknown;
  try {
    parsed = JSON.parse(body);
  } catch (cause) {
    throw new ApiParseError(
      `GET ${OVERVIEW_PATH} returned a body that is not valid JSON`,
      { cause },
    );
  }

  if (!isOverview(parsed)) {
    throw new ApiParseError(
      `GET ${OVERVIEW_PATH} returned JSON that is not an overview`,
    );
  }
  return parsed;
}

function describeFailure(status: number, detail: string | null): string {
  const subject =
    status === 0
      ? `GET ${OVERVIEW_PATH} failed`
      : `GET ${OVERVIEW_PATH} failed with HTTP ${String(status)}`;
  return detail === null ? subject : `${subject}: ${detail}`;
}

/** Extracts the backend's `{"error": "..."}` payload, if that is what this is. */
function backendError(body: string): string | null {
  if (body.trim() === "") {
    return null;
  }
  let parsed: unknown;
  try {
    parsed = JSON.parse(body);
  } catch {
    return null;
  }
  if (!isRecord(parsed)) {
    return null;
  }
  const message = parsed["error"];
  return typeof message === "string" && message !== "" ? message : null;
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

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
