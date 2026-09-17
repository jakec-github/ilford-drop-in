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
    const { BASE_PATH, apiUrl } = await basePathWith("/");
    expect(BASE_PATH).toBe("");
    expect(apiUrl("/api/shifts")).toBe("/api/shifts");
  });

  test("is the path the server wrote into the base tag, without its trailing slash", async () => {
    const { BASE_PATH, apiUrl } = await basePathWith("/rota/");
    expect(BASE_PATH).toBe("/rota");
    // The trailing slash belongs in the element, where relative asset
    // references resolve against it, and nowhere else — a URL built from it
    // must not double up.
    expect(apiUrl("/api/shifts")).toBe("/rota/api/shifts");
    expect(apiUrl("/auth/login")).toBe("/rota/auth/login");
  });

  test("is empty when there is no base element at all", async () => {
    const { BASE_PATH } = await basePathWith(null);
    expect(BASE_PATH).toBe("");
  });
});

// Every request the app makes has to carry the base path, and there is nothing
// about a missed one that shows up in dev — the site is at the root there, so
// a raw "/api/..." works perfectly until it is deployed under a path, where it
// reaches the domain's root and 404s. api.ts is the one place requests are
// made (views never call fetch), so this is the whole of the rule.
test("every request in api.ts goes through apiUrl", async () => {
  const source = await Bun.file(new URL("./api.ts", import.meta.url)).text();

  const bare = [...source.matchAll(/fetch\(\s*(["'`]\/[^"'`]*)/g)].map(
    (m) => m[1],
  );
  expect(bare).toEqual([]);
});
