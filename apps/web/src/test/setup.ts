import { expect } from "vitest";
import * as axeMatchers from "vitest-axe/matchers.js";

import "@testing-library/jest-dom/vitest";

// Registered once for the whole suite, like jest-dom's own matchers: a file
// that calls expect.extend itself would re-register them on every worker.
expect.extend(axeMatchers);
