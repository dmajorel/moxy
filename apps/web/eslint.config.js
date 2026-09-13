// ESLint 10 flat config for the moxy frontend.
//
// Type-aware linting is enabled: this UI renders an API whose payloads are
// nullable on purpose (`Unknown<T>` in src/api/types.ts), and the rules that
// catch a mishandled null are exactly the ones that need type information.
import js from "@eslint/js";
import { defineConfig } from "eslint/config";
import globals from "globals";
import jsxA11y from "eslint-plugin-jsx-a11y";
import reactHooks from "eslint-plugin-react-hooks";
import tseslint from "typescript-eslint";

/**
 * The three places a literal number is geometry rather than a value.
 *
 * Each computes a coordinate, a width or a height for an SVG or an inline
 * style, which is what `style` and the rounding helpers exist for. Everywhere
 * else those are forbidden below.
 */
const GEOMETRY = [
  "src/components/AppShell.tsx",
  "src/components/ui/Sparkline.tsx",
  "src/components/ui/UsageBar.tsx",
];

/**
 * The one file allowed to put a colour in a style attribute.
 *
 * `clusters[].color` is configured on the server, validated as `#rrggbb` there
 * and passed through: it is a value, not a design decision, so it cannot be a
 * token — and it cannot be a Tailwind class assembled from a string either,
 * since the extractor would never generate it. ClusterAccent is where that
 * value becomes a CSS custom property, and the only place it may.
 */
const RUNTIME_COLOUR = ["src/components/ui/ClusterAccent.tsx"];

/**
 * What CLAUDE.md forbids, as selectors rather than as prose.
 *
 * Every one of these was broken at least once and caught by a human reading
 * the diff — the maintenance dialog's `bg-black/45`, a `toFixed` in the node
 * view, a `role="button"` on the cluster card. Review is not a mechanism.
 */
const COLOURS_ONLY = [
  {
    selector: "Literal[value=/#[0-9a-fA-F]{3,8}\\b/]",
    message:
      "No literal colour. Every colour is a token in src/styles/tokens.css, " +
      "exposed as a Tailwind utility; a colour that is missing is added there.",
  },
  {
    selector: "TemplateElement[value.raw=/#[0-9a-fA-F]{3,8}\\b/]",
    message:
      "No literal colour, in a template string either. See src/styles/tokens.css.",
  },
  {
    selector:
      "Literal[value=/\\b(bg|text|border|fill|stroke)-(red|green|blue|amber|yellow|orange|black|white|gray|grey|slate|zinc|neutral|stone)\\b/]",
    message:
      "No Tailwind palette colour. Use a token utility (bg-surface-2, " +
      "text-text-muted, border-border, bg-scrim…).",
  },
];

/** The colour rules, plus the ban on inline styles and on a faked button. */
const FORBIDDEN_SYNTAX = [
  ...COLOURS_ONLY,
  {
    selector:
      'JSXOpeningElement[name.name=/^(div|span|article|section|li)$/] > JSXAttribute[name.name="role"][value.value="button"]',
    message:
      'role="button" carries "Children Presentational: true": everything inside ' +
      "it leaves the accessibility tree. Put a real <button> on the title and " +
      "stretch its hit area — see ClusterCard.",
  },
  {
    selector: 'JSXAttribute[name.name="style"]',
    message:
      "No inline style. Layout and colour are Tailwind utilities reading the " +
      "tokens; the three files that compute geometry are exempt.",
  },
];

/**
 * Rounding and locale formatting, which belong to src/lib/format.ts alone.
 *
 * A `Math.round(ratio * 100)` written in a component creates a second
 * typographic convention that will drift from the first: there is one place
 * where it is decided how a size is written. Intl is banned outright — its
 * output varies between Node builds, and these strings are asserted character
 * by character.
 */
const FORBIDDEN_FORMATTING = [
  {
    object: "Math",
    property: "round",
    message: "Rounding belongs to src/lib/format.ts.",
  },
  { object: "Math", property: "floor", message: "Rounding belongs to src/lib/format.ts." },
  { object: "Math", property: "ceil", message: "Rounding belongs to src/lib/format.ts." },
  { object: "Math", property: "trunc", message: "Rounding belongs to src/lib/format.ts." },
  {
    property: "toFixed",
    message: "Decimals belong to src/lib/format.ts, which writes the French comma.",
  },
  {
    property: "toLocaleString",
    message:
      "ICU output drifts between Node builds and these strings are asserted " +
      "character by character. Format by hand in src/lib/format.ts.",
  },
];

export default defineConfig(
  {
    // Generated output and dependencies are never linted.
    ignores: ["dist/**", "node_modules/**", "coverage/**", "**/*.tsbuildinfo"],
  },

  // Application and test sources.
  {
    files: ["**/*.{ts,tsx}"],
    extends: [js.configs.recommended, tseslint.configs.strictTypeChecked],
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

      // A number in a template is unambiguous; the rule's value is catching an
      // object, a null or an `any` stringified into a sentence.
      "@typescript-eslint/restrict-template-expressions": [
        "error",
        { allowNumber: true },
      ],

      // OFF, deliberately.
      //
      // Every payload this UI renders comes through an `as unknown as` cast in
      // api/client.ts: what TypeScript believes about a field is what the
      // CONTRACT claims, not what the bytes contain. The guards this rule
      // calls unnecessary — `NODE_STATUS_LABELS[status] ?? "Inconnu"`, a null
      // check on an array the type says is always there — are the ones that
      // keep a backend a version ahead from blanking a screen. Removing them
      // to satisfy the linter would undo the error-boundary work.
      "@typescript-eslint/no-unnecessary-condition": "off",

      // A destructuring pattern is only reported when every binding it
      // introduces could be const; the default ("any") reports patterns where
      // one binding is reassigned and the other is not, which cannot be fixed
      // without splitting the destructuring in two.
      "prefer-const": ["error", { destructuring: "all" }],

      // A switch over a union must handle every member. The screens switch on
      // TreeSelection["kind"] and AlertKind, and a new member added to either
      // would otherwise fall silently into the default branch — which is how a
      // new alert kind renders as "Alerte" and nobody notices.
      "@typescript-eslint/switch-exhaustiveness-check": [
        "error",
        { allowDefaultCaseForExhaustiveSwitch: true, considerDefaultExhaustiveForUnions: true },
      ],
    },
  },

  // The rules of CLAUDE.md, mechanised.
  //
  // No colour anywhere in src/ but the stylesheet that defines them, tests
  // included — except that a test may NAME a forbidden class in order to
  // assert its absence, which is the one legitimate use of the string.
  {
    files: ["src/**/*.{ts,tsx}"],
    ignores: ["src/styles/**", "**/*.test.{ts,tsx}"],
    rules: { "no-restricted-syntax": ["error", ...FORBIDDEN_SYNTAX] },
  },

  // Rounding and locale formatting belong to the formatting layer alone. It
  // and the three geometry files are where they are the point rather than a
  // second typographic convention waiting to drift.
  {
    files: ["src/**/*.{ts,tsx}"],
    // Tests are exempt: they compute — a contrast ratio, an expected index —
    // rather than render, and rounding there is arithmetic, not a second
    // typographic convention.
    ignores: [
      "src/lib/format.ts",
      "src/lib/series.ts",
      "**/*.test.{ts,tsx}",
      ...GEOMETRY,
    ],
    rules: {
      "no-restricted-properties": ["error", ...FORBIDDEN_FORMATTING],
      "no-restricted-globals": [
        "error",
        {
          name: "Intl",
          message:
            "ICU output drifts between Node builds. Format by hand in " +
            "src/lib/format.ts.",
        },
      ],
    },
  },

  // The three files that compute geometry: an inline style is what a width in
  // pixels or an SVG coordinate IS. The colour rules above still apply.
  {
    files: GEOMETRY,
    rules: { "no-restricted-syntax": ["error", ...COLOURS_ONLY] },
  },

  // The accent a cluster configures, which reaches the DOM as a custom
  // property. The colour rules above still apply: no literal may be written
  // there either, only the value the API served.
  {
    files: RUNTIME_COLOUR,
    rules: { "no-restricted-syntax": ["error", ...COLOURS_ONLY] },
  },

  // Accessibility, on the components rather than on the tests.
  //
  // It would have caught two of the defects this project found by reading:
  // the cluster card carrying role="button" over a heading and a list, and a
  // StatusDot rendered as a role="img" with an empty name.
  {
    files: ["src/**/*.tsx"],
    extends: [jsxA11y.flatConfigs.recommended],
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

      // A test builds fixtures: a non-null assertion on something it has just
      // written, and a computed delete to remove one field from a payload, are
      // the shortest way to say what the case is about.
      "@typescript-eslint/no-non-null-assertion": "off",
      "@typescript-eslint/no-dynamic-delete": "off",
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
    files: ["eslint.config.js"],
    extends: [js.configs.recommended],
    languageOptions: {
      sourceType: "module",
      globals: { ...globals.node },
    },
  },

  // public/ holds the files Vite copies to the root of the bundle untouched.
  // The only one is the anti-flash theme script, which index.html loads before
  // the bundle exists: a classic script running in the browser, so neither a
  // module nor part of the tsconfig program.
  {
    files: ["public/*.js"],
    extends: [js.configs.recommended],
    languageOptions: {
      sourceType: "script",
      globals: { ...globals.browser },
    },
  },
);
