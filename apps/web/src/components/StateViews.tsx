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
import { ApiRequestError } from "@/api/client";
import { AlertBanner } from "@/components/ui";

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
}

/** Hard failure: nothing could be loaded, so there is nothing to keep on screen. */
/**
 * What to tell the operator, by cause.
 *
 * A refusal is not an outage, and saying "check that the service is running"
 * when the token simply lacks a privilege sends someone hunting the network for
 * hours. The 403 case names the likely cause, because there is essentially only
 * one: the ACL on /nodes overriding the one inherited from /.
 */
function explain(error: Error): { title: string; body: string } {
  if (error instanceof ApiRequestError && error.status === 403) {
    return {
      title: "Droits insuffisants sur ce nœud",
      body:
        "Proxmox a refusé la requête. Le token a besoin de Sys.Audit sur /nodes, " +
        "et un rôle posé sur /nodes remplace celui hérité de / au lieu de s'y " +
        "ajouter : un rôle ne portant que Sys.Modify efface Sys.Audit. " +
        "Voir « Privilèges PVE requis » dans le README.",
    };
  }
  return {
    title: "Impossible de charger les données",
    body:
      "Le service moxy n’a pas répondu. Vérifiez qu’il est démarré et que les " +
      "clusters sont joignables, puis réessayez.",
  };
}

export function ErrorView({ error, onRetry }: ErrorViewProps) {
  const { title, body } = explain(error);

  return (
    <section
      role="alert"
      className="mx-auto max-w-[520px] rounded-panel border-[0.5px] border-border bg-surface-2 px-4 py-3.5"
    >
      <h2 className="text-[14px] font-medium text-text-primary">{title}</h2>
      <p className="mt-1.5 text-[12px] text-text-secondary">{body}</p>

      {onRetry === undefined ? null : (
        <button className={`mt-3 ${BUTTON_CLASSES}`} type="button" onClick={onRetry}>
          Réessayer
        </button>
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
