/**
 * The token prompt of `auth.mode: "token"`.
 *
 * It is rendered instead of the application whenever moxyd answers 401, and
 * for no other failure: every other refusal has a sentence of its own in
 * lib/errors. The screen deliberately holds nothing — no state to remember, no
 * token kept in localStorage. What it posts is exchanged for an HttpOnly
 * cookie, which this code cannot read back and therefore cannot leak.
 *
 * It says, in as many words, that a shared token authorizes without
 * identifying anyone. An operator typing a secret deserves to know what it
 * proves.
 */
import { useCallback, useState } from "react";
import type { SyntheticEvent } from "react";

import { ApiRequestError, login } from "@/api/client";
import { Logo } from "@/components/ui";
import { explainError } from "@/lib/errors";

export interface LoginScreenProps {
  /** Called once moxyd has accepted the token and set the cookie. */
  onAuthenticated: () => void;
}

/** Hairline button of the mocks, in its accented form. */
const SUBMIT_CLASSES =
  "inline-flex items-center justify-center rounded-card border-[0.5px] " +
  "border-border bg-surface-2 px-3 py-[6px] text-[12px] text-text-primary " +
  "hover:bg-surface-1 focus-visible:outline focus-visible:outline-1 " +
  "focus-visible:outline-offset-1 focus-visible:outline-accent " +
  "disabled:opacity-60";

const FIELD_ID = "moxy-token";
const ERROR_ID = "moxy-token-error";

export function LoginScreen({ onAuthenticated }: LoginScreenProps) {
  const [token, setToken] = useState("");
  const [message, setMessage] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const submit = useCallback(
    (event: SyntheticEvent) => {
      event.preventDefault();
      if (busy || token.trim() === "") {
        return;
      }
      setBusy(true);
      setMessage(null);
      login(token).then(
        () => {
          // Dropped from memory as soon as it has been exchanged: from here on
          // the cookie is what authorizes, and nothing on the page holds the
          // secret.
          setToken("");
          setBusy(false);
          onAuthenticated();
        },
        (cause: unknown) => {
          setBusy(false);
          setMessage(describe(cause));
        },
      );
    },
    [busy, token, onAuthenticated],
  );

  return (
    <div className="flex min-h-full items-center justify-center px-4 py-10">
      <section className="w-full max-w-[380px] rounded-panel border-[0.5px] border-border bg-surface-2 px-4 py-4">
        <div className="flex items-center gap-2">
          <Logo className="text-brand" size={22} />
          <h1 className="text-[14px] font-medium text-text-primary">
            Authentification requise
          </h1>
        </div>
        <p className="mt-1.5 text-[12px] text-text-secondary">
          Saisissez le jeton d’accès configuré sur ce serveur pour consulter les
          clusters.
        </p>

        <form className="mt-3" onSubmit={submit}>
          <label
            className="block text-[12px] text-text-secondary"
            htmlFor={FIELD_ID}
          >
            Jeton d’accès
          </label>
          <input
            id={FIELD_ID}
            // A password field: masked on screen, and browsers offer to store
            // it in the password manager rather than in the form history.
            type="password"
            className="mt-1 w-full rounded-card border-[0.5px] border-border bg-surface-1 px-2.5 py-[6px] text-[12px] text-text-primary focus-visible:outline focus-visible:outline-1 focus-visible:outline-offset-1 focus-visible:outline-accent"
            value={token}
            autoComplete="current-password"
            spellCheck={false}
            aria-describedby={message === null ? undefined : ERROR_ID}
            aria-invalid={message === null ? undefined : true}
            onChange={(event) => {
              setToken(event.target.value);
            }}
          />

          {message === null ? null : (
            <p id={ERROR_ID} role="alert" className="mt-2 text-[12px] text-danger">
              {message}
            </p>
          )}

          <button
            className={`${SUBMIT_CLASSES} mt-3 w-full`}
            type="submit"
            disabled={busy || token.trim() === ""}
          >
            {busy ? "Connexion…" : "Se connecter"}
          </button>
        </form>

        {/*
          Said here rather than only in the README: the person typing the
          secret is the one who needs to know what it does not do.
        */}
        <p className="mt-3 text-[11px] text-text-muted">
          Ce jeton est partagé : il autorise l’accès, il n’identifie personne.
          Les actions faites depuis moxy ne sont donc attribuées à aucun compte.
        </p>
      </section>
    </div>
  );
}

/**
 * The sentence under the field.
 *
 * A refused token is the expected outcome and gets its own wording; anything
 * else is a real failure and borrows the explanation lib/errors already holds,
 * so there is no second table of sentences to keep in step.
 */
function describe(cause: unknown): string {
  if (cause instanceof ApiRequestError && cause.status === 401) {
    return "Jeton refusé. Vérifiez la valeur transmise par l’administrateur de ce serveur.";
  }
  if (cause instanceof Error) {
    return explainError(cause).body;
  }
  return explainError(new Error("login failed")).body;
}
