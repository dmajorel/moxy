/**
 * The states every screen shares: first load, hard failure, stale data and
 * empty list.
 *
 * They exist as one module because they encode a single product decision — the
 * backend serves its last known reading rather than an empty page, so the UI
 * must be able to say "these data are old" without ever hiding them.
 *
 * Backend messages are English diagnostics (see CLAUDE.md). Nothing here shows
 * one as a label: the sentence the user reads is French, and the raw message is
 * tucked into a collapsed `<details>`.
 */
import { AlertBanner } from "@/components/ui";
import { explainError } from "@/lib/errors";

/** Hairline button of the mocks; no shadow, no invented colour. */
const BUTTON_CLASSES =
  "inline-flex items-center gap-1.5 rounded-card border-[0.5px] border-border " +
  "bg-surface-2 px-2.5 py-[5px] text-[12px] text-text-primary " +
  "hover:bg-surface-1 focus-visible:outline focus-visible:outline-1 " +
  "focus-visible:outline-offset-1 focus-visible:outline-accent";

/**
 * Very first load, when there is nothing to show yet.
 *
 * A skeleton rather than a spinner: the page keeps its shape and nothing
 * flickers when the data land a few hundred milliseconds later.
 */
export function LoadingView() {
  return (
    <div role="status" aria-live="polite">
      <p className="text-[12px] text-text-muted">Chargement…</p>
      <div aria-hidden className="mt-3 grid gap-2.5">
        <div className="h-[52px] rounded-card border-[0.5px] border-border bg-surface-1" />
        <div className="h-[52px] rounded-card border-[0.5px] border-border bg-surface-1" />
        <div className="h-[52px] rounded-card border-[0.5px] border-border bg-surface-1" />
      </div>
    </div>
  );
}

export interface ErrorViewProps {
  /** Its message is diagnostic material, never a label. */
  error: Error;
  /** When given, a retry button is offered. */
  onRetry?: () => void;
  /**
   * The way back to the overview, offered only for a vanished object.
   *
   * A screen showing a 404 is the one place where retrying leads nowhere: the
   * node or the guest is gone, and only the overview still says what the
   * cluster holds. The polling goes on regardless, so an object that comes
   * back fills the screen by itself.
   */
  onBack?: () => void;
}

/**
 * Hard failure: nothing could be loaded, so there is nothing to keep on screen.
 *
 * The sentence comes from lib/errors, which reads the class of the failure: a
 * deleted VM, an unreachable cluster and a stopped daemon are three different
 * errands, and one generic wording for all of them sent operators checking
 * that moxyd was running when moxyd had just answered.
 */
export function ErrorView({ error, onRetry, onBack }: ErrorViewProps) {
  const { title, body, offerBack } = explainError(error);
  const back = offerBack ? onBack : undefined;

  return (
    <section
      role="alert"
      className="mx-auto max-w-[520px] rounded-panel border-[0.5px] border-border bg-surface-2 px-4 py-3.5"
    >
      <h2 className="text-[14px] font-medium text-text-primary">{title}</h2>
      <p className="mt-1.5 text-[12px] text-text-secondary">{body}</p>

      {onRetry === undefined && back === undefined ? null : (
        <div className="mt-3 flex flex-wrap gap-2">
          {back === undefined ? null : (
            <button className={BUTTON_CLASSES} type="button" onClick={back}>
              Retour à la vue d’ensemble
            </button>
          )}
          {onRetry === undefined ? null : (
            <button className={BUTTON_CLASSES} type="button" onClick={onRetry}>
              Réessayer
            </button>
          )}
        </div>
      )}

      {/* Collapsed: the English message is for diagnosis, not for reading. */}
      <details className="mt-3">
        <summary className="cursor-pointer text-[11px] text-text-muted">
          Détail technique
        </summary>
        <p className="mt-1.5 font-mono text-[11px] break-words text-text-secondary">
          {error.message}
        </p>
      </details>
    </section>
  );
}

export interface StaleBannerProps {
  /** When the data on screen were last refreshed; null if never recorded. */
  lastUpdatedAt: Date | null;
  onRetry?: () => void;
}

/**
 * Shown *over* data that stay visible: the last attempt failed, the reading is
 * old, and the user is told so instead of being handed a blank page.
 */
export function StaleBanner({ lastUpdatedAt, onRetry }: StaleBannerProps) {
  const stamp = formatTimestamp(lastUpdatedAt);
  const sentence =
    stamp === null
      ? "Données précédentes · connexion perdue"
      : `Données du ${stamp} · connexion perdue`;

  return (
    <AlertBanner className="mb-3" variant="warning">
      <span className="flex w-full items-center gap-2">
        <span>{sentence}</span>
        {onRetry === undefined ? null : (
          <button
            className="ml-auto shrink-0 underline underline-offset-2 focus-visible:outline focus-visible:outline-1 focus-visible:outline-offset-1 focus-visible:outline-accent"
            type="button"
            onClick={onRetry}
          >
            Réessayer
          </button>
        )}
      </span>
    </AlertBanner>
  );
}

export interface EmptyViewProps {
  title: string;
  hint?: string;
}

/** A list with nothing in it: says so plainly, without dressing it as an error. */
export function EmptyView({ title, hint }: EmptyViewProps) {
  return (
    <div className="rounded-card border-[0.5px] border-border bg-surface-2 px-4 py-6 text-center">
      <p className="text-[13px] text-text-secondary">{title}</p>
      {hint === undefined ? null : (
        <p className="mt-1 text-[11px] text-text-muted">{hint}</p>
      )}
    </div>
  );
}

function pad(value: number): string {
  return String(value).padStart(2, "0");
}

/**
 * `12/09/2026 à 14:32`, built by hand rather than through `Intl`: ICU output
 * drifts between Node builds, and this string is asserted in the tests.
 * An unusable date yields null, so the banner falls back to a sentence with no
 * timestamp at all instead of printing `Invalid Date`.
 */
function formatTimestamp(date: Date | null): string | null {
  if (!(date instanceof Date) || Number.isNaN(date.getTime())) {
    return null;
  }
  const day = `${pad(date.getDate())}/${pad(date.getMonth() + 1)}/${date.getFullYear()}`;
  return `${day} à ${pad(date.getHours())}:${pad(date.getMinutes())}`;
}
