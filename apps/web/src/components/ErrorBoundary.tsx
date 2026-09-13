import { Component, Fragment } from "react";
import type { ErrorInfo, ReactNode } from "react";

import { ErrorView } from "@/components/StateViews";

/**
 * The last line of defence against a blank page.
 *
 * React unmounts the WHOLE tree when a render throws and nothing catches it.
 * One missing field in one payload — a backend a version ahead, a proxy that
 * truncated a body, a renamed key — would therefore take the top bar and the
 * tree down with the screen that read it, on an application whose stated
 * philosophy is to serve the last known state rather than an empty page.
 *
 * This has to be a class: getDerivedStateFromError has no hook equivalent, and
 * that is still true in React 19. It is the one class component of the
 * codebase, and it exists for that reason alone.
 *
 * A boundary is NOT a substitute for the shape guards in api/client.ts. Those
 * turn a malformed payload into an ApiParseError, which the screens already
 * render as an explained failure with a retry; this catches what they miss,
 * and what they miss is by definition unforeseen.
 */
export interface ErrorBoundaryProps {
  children: ReactNode;
  /**
   * What the boundary is guarding, used in the log line. It is developer text,
   * never shown: the operator gets the sentence ErrorView builds.
   */
  label?: string;
}

interface ErrorBoundaryState {
  error: Error | null;
  /**
   * Bumped by "Réessayer" and used as the key of the subtree, which is what
   * makes React throw away the broken instances and mount fresh ones. Clearing
   * the error alone would re-render the very components that threw, with their
   * state intact, and they would throw again.
   */
  attempt: number;
}

export class ErrorBoundary extends Component<ErrorBoundaryProps, ErrorBoundaryState> {
  state: ErrorBoundaryState = { error: null, attempt: 0 };

  static getDerivedStateFromError(error: unknown): Partial<ErrorBoundaryState> {
    return { error: error instanceof Error ? error : new Error(String(error)) };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    // The console is the only place this can go: there is no telemetry, and
    // the component stack is what turns "a screen broke" into a file and a
    // line. The message shown to the operator carries none of it.
    console.error(
      `moxy: rendering ${this.props.label ?? "a screen"} failed`,
      error,
      info.componentStack,
    );
  }

  private readonly retry = () => {
    this.setState((state) => ({ error: null, attempt: state.attempt + 1 }));
  };

  render() {
    const { error, attempt } = this.state;
    if (error !== null) {
      return <ErrorView error={error} onRetry={this.retry} />;
    }
    // A keyed Fragment, not a wrapper element: the content pane and the
    // detail screens lay their children out directly, and a <div> inserted
    // here would change the layout of everything it guards.
    return <Fragment key={attempt}>{this.props.children}</Fragment>;
  }
}
