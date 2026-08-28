// @types/bun defers to the bun-types package, and tsc's automatic @types scan
// does not pick that transitive package up on its own here — so it is pulled
// in explicitly, which is also what makes "bun:test" itself resolve as a
// module for describe/test/expect/mock imports in test files.
/// <reference types="bun-types" />

// jest-dom ships type augmentation for Jest's own `expect` only; this mirrors
// its shape onto bun:test's `Matchers` interface so `expect(el).toBeVisible()`
// etc. type-check under Bun's test runner too.
import type { TestingLibraryMatchers } from "@testing-library/jest-dom/matchers";

declare module "bun:test" {
  // Pure declaration merging: the augmentation is the extends clause itself.
  // eslint-disable-next-line @typescript-eslint/no-empty-object-type
  interface Matchers<T = unknown> extends TestingLibraryMatchers<unknown, T> {}
}
