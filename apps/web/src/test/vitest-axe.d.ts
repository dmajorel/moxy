/**
 * The type of the matcher `test/setup.ts` registers.
 *
 * vitest-axe still ships its augmentation against the old `namespace Vi`,
 * which Vitest 5 no longer reads: its matchers live on `interface Matchers` of
 * the "vitest" module. Declaring the one matcher we use is shorter than
 * pinning an older type surface, and the parameter list below has to match
 * Vitest's own declaration exactly for the merge to happen.
 */
declare module "vitest" {
  interface Matchers<
    R extends void | Promise<void> = void | Promise<void>,
    // Declaration merging requires an identical parameter list, name included,
    // so this one cannot be renamed out of the way of the unused-vars rule.
    // eslint-disable-next-line @typescript-eslint/no-unused-vars
    T = unknown,
  > {
    toHaveNoViolations: () => R;
  }
}

export {};
