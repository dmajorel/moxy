import { expect } from "vitest";
import * as axeMatchers from "vitest-axe/matchers.js";

import "@testing-library/jest-dom/vitest";

// Registered once for the whole suite, like jest-dom's own matchers: a file
// that calls expect.extend itself would re-register them on every worker.
expect.extend(axeMatchers);

/**
 * The suite speaks French, whatever the machine running it speaks.
 *
 * jsdom reports `en-US`, so without this the interface under test would come up
 * in English and every assertion written against the mockups of appendix A —
 * which are French — would fail on a correctly working application. Worse, the
 * language of the suite would then depend on the host, which is how a green
 * pipeline and a red laptop end up disagreeing.
 *
 * French is also the source language, so this is not an arbitrary pin: it is
 * the language the assertions are written in. A test that wants the other one
 * stubs `navigator.languages` itself — lang.test.ts and App.test.tsx both do,
 * which is what keeps the English path genuinely covered.
 */
Object.defineProperty(navigator, "languages", {
  value: ["fr-FR", "fr"],
  configurable: true,
});
Object.defineProperty(navigator, "language", {
  value: "fr-FR",
  configurable: true,
});
