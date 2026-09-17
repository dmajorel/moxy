/**
 * Turns a failure of the API layer into the sentence an operator reads.
 *
 * The backend classifies and reports in English — `not found`, `upstream
 * unavailable` — and the wording is the frontend's job, exactly as it is for
 * `formatErrorKind` in format.ts. What is new here is that the *class* of the
 * failure is read too: a deleted VM, an unreachable cluster and a stopped
 * daemon are three different errands, and telling an operator to check that
 * moxyd is running when moxyd has just answered `404` costs an evening.
 *
 * The table below is the whole contract: one entry per class, no sentence built
 * inside a component.
 */
import { ApiParseError, ApiRequestError } from "@/api/client";
import type { MessageKey, Translator } from "@/i18n/messages";

/**
 * Why a screen could not be filled.
 *
 * Mirrors what the API layer can actually produce: the statuses
 * `internal/server/detail.go` writes, plus the two failures that never carry
 * one — a request that reached no server at all, and a body that was not the
 * JSON we asked for.
 */
export type FailureKind =
  | "unreachable"
  | "unauthorized"
  | "notFound"
  | "forbidden"
  | "upstream"
  | "timeout"
  | "unsupported"
  | "invalid"
  | "internal"
  | "unreadable"
  | "unknown";

export interface FailureExplanation {
  kind: FailureKind;
  /** Heading of the error view, in the display language, sentence case. */
  title: string;
  /** The sentence under it: what happened, and what to do about it. */
  body: string;
  /**
   * Whether going back is the way out.
   *
   * True for a vanished object only: retrying a node that no longer exists
   * will keep failing, while the overview still says what the cluster holds.
   */
  offerBack: boolean;
}

/**
 * The stable word the backend classified this failure with, when it sent one.
 *
 * Only the maintenance route fills `kind` in — a status code alone cannot
 * separate "no quorum" from "no HA manager" — and it is deliberately not
 * folded into `explainError`: that one explains why a SCREEN could not be
 * filled and offers a way back, which is not what a refused action needs.
 */
export function backendKind(error: Error | null): string | null {
  return error instanceof ApiRequestError ? error.kind : null;
}

/** What class of failure this is, or "unknown" for anything unclassified. */
export function classifyError(error: Error): FailureKind {
  if (error instanceof ApiParseError) {
    return "unreadable";
  }
  if (!(error instanceof ApiRequestError)) {
    return "unknown";
  }
  switch (error.status) {
    // No response at all: the fetch itself failed, so nothing answered.
    case 0:
      return "unreachable";
    case 400:
      return "invalid";
    // moxyd runs with `auth` configured and this caller has not proved
    // anything yet. In token mode the shell answers it with the login screen;
    // anywhere else the sentence below is what an operator gets.
    case 401:
      return "unauthorized";
    case 403:
      return "forbidden";
    case 404:
      return "notFound";
    case 501:
      return "unsupported";
    case 502:
      return "upstream";
    case 504:
      return "timeout";
    default:
      // moxyd answered and failed on its own account; a 4xx we do not know
      // about is not worth a sentence of its own.
      return error.status >= 500 ? "internal" : "unknown";
  }
}

/**
 * The heading, the sentence and the way out, for one failure.
 *
 * The wording itself lives in `i18n/messages.ts` under `error.<kind>.title` and
 * `error.<kind>.body`; this only decides which kind is being explained. The
 * translator is passed in rather than read from a context because the two
 * callers are a component and a promise handler inside one, and a module that
 * classifies errors has no business being a hook.
 */
export function explainError(error: Error, t: Translator): FailureExplanation {
  const kind = classifyError(error);
  const title: MessageKey = `error.${kind}.title`;
  const body: MessageKey = `error.${kind}.body`;
  return { kind, title: t(title), body: t(body), offerBack: kind === "notFound" };
}
