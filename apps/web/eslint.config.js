// ESLint 10 flat config for the moxy frontend.
//
// Type-aware linting is enabled: this UI renders an API whose payloads are
// nullable on purpose (`Unknown<T>` in src/api/types.ts), and the rules that
// catch a mishandled null are exactly the ones that need type information.
import js from "@eslint/js";
import globals from "globals";
import reactHooks from "eslint-plugin-react-hooks";
import tseslint from "typescript-eslint";

export default tseslint.config(
  {
    // Generated output and dependencies are never linted.
    ignores: ["dist/**", "node_modules/**", "coverage/**", "**/*.tsbuildinfo"],
  },

  // Application and test sources.
  {
    files: ["**/*.{ts,tsx}"],
    extends: [js.configs.recommended, tseslint.configs.recommendedTypeChecked],
    languageOptions: {
      globals: { ...globals.browser },
      parserOptions: {
        // projectService reads tsconfig.json, so lint and `tsc -b` agree on
        // what a file's types are instead of drifting apart.
        projectService: true,
        tsconfigRootDir: import.meta.dirname,
      },
    },
    plugins: { "react-hooks": reactHooks },
    rules: {
      // Both hook rules are errors, never warnings: a hook called conditionally
      // or an effect with wrong dependencies makes the polling loop miss or
      // duplicate refreshes, which is a correctness bug in a monitoring UI.
      "react-hooks/rules-of-hooks": "error",
      "react-hooks/exhaustive-deps": "error",

      // The rest of eslint-plugin-react-hooks v7 is the React Compiler rule
      // set. It is deliberately left off: the build does not run the compiler
      // (see vite.config.ts, plain @vitejs/plugin-react), so those rules would
      // report on patterns that are valid in this codebase. Turn them on the
      // day the compiler is enabled.

      // Unused variables are reported by the TypeScript-aware rule only; the
      // core rule does not understand type-only syntax and double-reports.
      "no-unused-vars": "off",
      "@typescript-eslint/no-unused-vars": [
        "error",
        { argsIgnorePattern: "^_", varsIgnorePattern: "^_" },
      ],

      // Nothing but Error may be thrown, and every promise must be handled:
      // a dropped rejection in a poller silently freezes the dashboard.
      "@typescript-eslint/no-floating-promises": "error",
      "no-console": ["error", { allow: ["warn", "error"] }],

      // A destructuring pattern is only reported when every binding it
      // introduces could be const; the default ("any") reports patterns where
      // one binding is reassigned and the other is not, which cannot be fixed
      // without splitting the destructuring in two.
      "prefer-const": ["error", { destructuring: "all" }],
    },
  },

  // Tests.
  {
    files: ["**/*.test.{ts,tsx}", "src/test/**"],
    rules: {
      // checksVoidReturn is off here only: vi.fn() and act() are typed as
      // void-returning, so an async callback handed to a mock or to act() is
      // reported even though awaiting it is exactly what the test does. The
      // check stays on in application code, where a promise dropped into a
      // void position really is a lost error.
      "@typescript-eslint/no-misused-promises": [
        "error",
        { checksVoidReturn: false },
      ],
    },
  },

  // Node-side tooling: Vite and Vitest configs read process.env.
  {
    files: ["vite.config.ts", "vitest.config.ts"],
    languageOptions: { globals: { ...globals.node } },
  },

  // This file. Kept out of type-aware linting because it is not part of the
  // tsconfig program; running it through projectService would only add a
  // second TypeScript program for a single config file.
  {
    files: ["**/*.js"],
    extends: [js.configs.recommended],
    languageOptions: {
      sourceType: "module",
      globals: { ...globals.node },
    },
  },
);
