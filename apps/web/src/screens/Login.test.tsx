import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { LOGIN_PATH } from "@/api/client";

import { LoginScreen } from "./Login";

const token = "6f1c0b9d4a2e8f37b5c1d0e9a7f26384";

function stubFetch(impl: typeof fetch): ReturnType<typeof vi.fn> {
  const stub = vi.fn(impl);
  vi.stubGlobal("fetch", stub);
  return stub;
}

function type(value: string) {
  fireEvent.change(screen.getByLabelText("Jeton d’accès"), { target: { value } });
}

function submit() {
  fireEvent.click(screen.getByRole("button", { name: "Se connecter" }));
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("LoginScreen", () => {
  it("posts the token as JSON and tells the caller it worked", async () => {
    const stub = stubFetch(() => Promise.resolve(new Response(null, { status: 204 })));
    const onAuthenticated = vi.fn();
    render(<LoginScreen onAuthenticated={onAuthenticated} />);

    type(token);
    submit();

    await waitFor(() => {
      expect(onAuthenticated).toHaveBeenCalledTimes(1);
    });
    const [url, init] = stub.mock.calls[0] as [string, RequestInit];
    expect(url).toBe(LOGIN_PATH);
    expect(init.method).toBe("POST");
    expect(init.body).toBe(JSON.stringify({ token }));
    // A form content type is what a page on another site could post without a
    // preflight, and moxyd refuses one.
    expect(new Headers(init.headers).get("Content-Type")).toBe("application/json");
  });

  it("says the token was refused, and keeps the screen", async () => {
    stubFetch(() =>
      Promise.resolve(
        new Response(JSON.stringify({ error: "unauthorized" }), { status: 401 }),
      ),
    );
    const onAuthenticated = vi.fn();
    render(<LoginScreen onAuthenticated={onAuthenticated} />);

    type("wrong");
    submit();

    expect(await screen.findByRole("alert")).toHaveTextContent("Jeton refusé");
    expect(onAuthenticated).not.toHaveBeenCalled();
  });

  // A daemon that is not answering at all is a different errand from a refused
  // token, and the sentence comes from the one table lib/errors holds.
  it("distinguishes an unreachable daemon from a refusal", async () => {
    stubFetch(() => Promise.reject(new TypeError("network")));
    render(<LoginScreen onAuthenticated={vi.fn()} />);

    type(token);
    submit();

    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("moxy");
    expect(alert).not.toHaveTextContent("Jeton refusé");
  });

  it("never keeps the token once it has been exchanged", async () => {
    stubFetch(() => Promise.resolve(new Response(null, { status: 204 })));
    render(<LoginScreen onAuthenticated={vi.fn()} />);

    type(token);
    submit();

    await waitFor(() => {
      expect(screen.getByLabelText("Jeton d’accès")).toHaveValue("");
    });
    // Not in browser storage either: the cookie moxyd sets is HttpOnly, and
    // nothing here has a second copy of the secret.
    expect(window.localStorage.length).toBe(0);
    expect(window.sessionStorage.length).toBe(0);
  });

  it("does not post an empty token", () => {
    const stub = stubFetch(() => Promise.resolve(new Response(null, { status: 204 })));
    render(<LoginScreen onAuthenticated={vi.fn()} />);

    expect(screen.getByRole("button", { name: "Se connecter" })).toBeDisabled();
    submit();

    expect(stub).not.toHaveBeenCalled();
  });

  // The honest sentence: what a shared token proves, and what it does not.
  it("says that a shared token identifies nobody", () => {
    render(<LoginScreen onAuthenticated={vi.fn()} />);

    expect(screen.getByText(/n’identifie personne/)).toBeInTheDocument();
  });

  it("masks what is typed", () => {
    render(<LoginScreen onAuthenticated={vi.fn()} />);

    expect(screen.getByLabelText("Jeton d’accès")).toHaveAttribute("type", "password");
  });
});
