import { afterEach, expect } from "bun:test";
import { cleanup } from "@testing-library/react";
import * as matchers from "@testing-library/jest-dom/matchers";

expect.extend(matchers);

// Each test renders into the shared happy-dom document; without this the
// previous test's tree is still mounted when the next one queries it.
afterEach(() => {
  cleanup();
});
