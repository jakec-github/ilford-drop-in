import { afterEach, describe, expect, test } from "bun:test";

// basePath.ts reads the <base> element once, at import, so each case here needs
// its own module instance — hence the dynamic import after the element is in
// place, with the registry cleared between cases.
async function basePathWith(href: string | null) {
  document.head.querySelector("base")?.remove();
  if (href !== null) {
    const base = document.createElement("base");
    base.setAttribute("href", href);
    document.head.appendChild(base);
  }
  // A cache-busting query gives each case a fresh evaluation of the module.
  return (await import(
    `./basePath?t=${Math.random()}`
  )) as typeof import("./basePath");
}

afterEach(() => {
  document.head.querySelector("base")?.remove();
});

describe("BASE_PATH", () => {
  test("is empty when the base tag is the root, as in every test", async () => {
    const { BASE_PATH } = await basePathWith("/");
    expect(BASE_PATH).toBe("");
  });

  test("is the path the server wrote into the base tag, without its trailing slash", async () => {
    const { BASE_PATH } = await basePathWith("/org/east/");
    // The trailing slash belongs in the element, where relative asset
    // references resolve against it, and nowhere else — a URL built from it
    // must not double up.
    expect(BASE_PATH).toBe("/org/east");
  });

  test("is empty when there is no base element at all", async () => {
    const { BASE_PATH } = await basePathWith(null);
    expect(BASE_PATH).toBe("");
  });
});

// Each drop-in's API is under its own path, so every request names it through
// BASE_PATH. A request at the root would reach no Organisation at all — the
// server answers anything outside /org/<id>/ but /auth and /health with a 404 —
// and one written relative to the page would resolve against the page's
// directory rather than the Organisation's. Signing out is the one exception:
// the session is the whole site's, so ending it is too. api.ts is the one place
// requests are made (views never call fetch), so this is the whole of the rule.
test("every request in api.ts is under the drop-in's own path", async () => {
  const source = await Bun.file(new URL("./api.ts", import.meta.url)).text();

  const paths = [...source.matchAll(/fetch\(\s*["'`]([^"'`]*)/g)].map(
    (m) => m[1],
  );
  expect(paths.length).toBeGreaterThan(0);
  expect(
    paths.filter((p) => !p.startsWith("${BASE_PATH}/") && p !== "/auth/logout"),
  ).toEqual([]);
});
