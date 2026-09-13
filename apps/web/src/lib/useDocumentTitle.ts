import { useEffect } from "react";

/** The product name, which closes every title. */
const SUFFIX = "moxy";

/**
 * Sets the browser tab title from the parts of the current screen, most
 * specific first: `prox-pprd-2302-cit · Préproduction · moxy`.
 *
 * The tab title is the only label an operator sees on a window that is not in
 * front of them. Ten tabs all called "moxy" are ten tabs nobody can tell
 * apart, which is exactly the state an incident leaves them in.
 *
 * Empty and null parts are dropped, so a screen whose cluster name has not
 * arrived yet renders a shorter title rather than a `· ·`.
 */
export function useDocumentTitle(...parts: (string | null | undefined)[]): void {
  const title = [...parts, SUFFIX]
    .filter((part): part is string => typeof part === "string" && part.trim() !== "")
    .join(" · ");

  useEffect(() => {
    document.title = title;
  }, [title]);
}
