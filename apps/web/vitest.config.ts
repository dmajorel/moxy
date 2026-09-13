import { mergeConfig, defineConfig } from "vitest/config";

import viteConfig from "./vite.config";

/**
 * The test configuration is the build configuration, plus the test block.
 *
 * It used to be a second file repeating the plugins and the `@` alias, so an
 * alias added for the application was invisible to the tests until someone
 * remembered to add it twice. mergeConfig is what keeps the two from drifting.
 */
export default mergeConfig(
  viteConfig,
  defineConfig({
    test: {
      environment: "jsdom",
      globals: true,
      setupFiles: ["./src/test/setup.ts"],
      coverage: {
        provider: "v8",
        // text for the terminal, lcov for whatever reads a report.
        reporter: ["text-summary", "lcov"],
        reportsDirectory: "./coverage",
        include: ["src/**/*.{ts,tsx}"],
        exclude: [
          // Nothing to cover: the entry point mounts React, the test setup is
          // the harness itself, and the type module is erased at compile time.
          "src/main.tsx",
          "src/test/**",
          "src/api/types.ts",
          "src/vite-env.d.ts",
        ],
        // A floor, not a target. It is set just under what the suite reaches
        // today so that a change which drops coverage by a few points fails
        // rather than passing unnoticed; raising it is a deliberate act.
        thresholds: {
          lines: 85,
          functions: 85,
          branches: 85,
          statements: 85,
        },
      },
    },
  }),
);
