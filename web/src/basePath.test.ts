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
  test("is empty at the root of a domain, which is dev and every test", async () => {
    const { BASE_PATH } = await basePathWith("/");
    expect(BASE_PATH).toBe("");
  });

  test("is the path the server wrote into the base tag, without its trailing slash", async () => {
    const { BASE_PATH } = await basePathWith("/rota/");
    // The trailing slash belongs in the element, where relative asset
    // references resolve against it, and nowhere else — a URL built from it
    // must not double up.
    expect(BASE_PATH).toBe("/rota");
  });

  test("is empty when there is no base element at all", async () => {
    const { BASE_PATH } = await basePathWith(null);
    expect(BASE_PATH).toBe("");
  });
});

// The API and /auth stay at the root of the domain however the site is served,
// and what keeps a request there is its leading slash: an absolute path resets
// the path when the URL is resolved, so the <base> element never applies. Drop
// the slash and the request silently starts resolving against the base instead
// — which in dev, where the base is "/", still works, and under a base path
// asks a page's directory for the API. api.ts is the one place requests are
// made (views never call fetch), so this is the whole of the rule.
test("every request in api.ts names a root-absolute path", async () => {
  const source = await Bun.file(new URL("./api.ts", import.meta.url)).text();

  const paths = [...source.matchAll(/fetch\(\s*["'`]([^"'`]*)/g)].map(
    (m) => m[1],
  );
  expect(paths.length).toBeGreaterThan(0);
  expect(paths.filter((p) => !p.startsWith("/"))).toEqual([]);
});
