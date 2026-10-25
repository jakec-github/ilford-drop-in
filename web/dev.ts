import { watch } from "fs";

async function build() {
  const result = await Bun.build({
    entrypoints: ["./index.html"],
    outdir: "./dist",
    sourcemap: "inline",
    // Matches the production build: relative asset references, resolved against
    // index.html's <base> element. See web/build.ts.
    publicPath: "./",
    define: { "process.env.NODE_ENV": '"development"' },
  });
  if (!result.success) {
    for (const msg of result.logs) console.error(msg);
  }
}

await build();

watch("./src", { recursive: true }, build);

const apiPort = process.env.API_PORT ?? "8080";
// WEB_PORT lets a second checkout (a git worktree, say) serve on its own port
// without colliding with the primary one. See docs/agents/worktrees.md.
const webPort = process.env.WEB_PORT ?? "5173";
// Each drop-in is served under /org/<id>/ (ADR 0011). Under it, these are the
// Go server's; everything else is the SPA, served from ./dist here so a change
// shows up on reload. Anything missing here is not proxied — it falls through to
// the SPA fallback below and the caller gets index.html with a 200, which
// surfaces as "Unexpected token '<'" when it tries to parse it as JSON.
const organisationAPIPrefixes = ["/api", "/auth", "/calendars"];
// Outside every Organisation, the paths that are still the Go server's.
const rootServerPrefixes = ["/auth", "/health"];
const organisationPath = /^\/org\/([a-z0-9][a-z0-9-]*)(\/.*)?$/;

// How long a proxied request may take before this server gives up on it.
//
// Bun.serve defaults to ten seconds, which is shorter than the app it fronts is
// allowed to take: a solve gets sixty (solveCeiling, pkg/api/draftRotaAllocation.go)
// and a draft read queueing behind a running solve can take longer still. Left
// at the default, every draft read slower than ten seconds reached the browser
// as "Failed to fetch" — the dev server closing the connection, not the app
// failing (issue #197). 255 is Bun's maximum, and comfortably past the ceiling
// the server holds itself to. Raise solveCeiling and this stops covering it.
const idleTimeout = 255;

const server = Bun.serve({
  port: webPort,
  hostname: "0.0.0.0",
  idleTimeout,
  async fetch(req) {
    const url = new URL(req.url);
    const pathname = url.pathname;

    const proxy = () =>
      // redirect: "manual" so the 302 from /auth/login to Google reaches the
      // browser instead of being followed server-side here.
      //
      // signal ties this request to the browser's. Without it the proxy holds a
      // connection of its own that outlives the one it is answering, and the Go
      // server — whose client is this proxy, not the browser — never sees the
      // caller leave: r.Context() stays live, the solve runs to completion and
      // stores its draft for a request nobody is waiting on, holding the solve
      // slot the next reader queues behind (issue #197).
      fetch(`http://localhost:${apiPort}${pathname}${url.search}`, {
        method: req.method,
        headers: req.headers,
        body: req.body,
        redirect: "manual",
        signal: req.signal,
      });

    // index.html as built, its <base> at the root: the app outside every
    // Organisation, which draws the splash at / and "Page not found" elsewhere.
    // As in prod (pkg/api/frontend.go), the 404 is the same page with the
    // status, whatever path asked for it.
    const rootIndex = async (status: number) =>
      new Response(await Bun.file("./dist/index.html").text(), {
        status,
        headers: { "Content-Type": "text/html; charset=utf-8" },
      });

    // Outside every Organisation, /auth and /health are the Go server's; the
    // rest is the frontend with its base at the root, assets included.
    const match = organisationPath.exec(pathname);
    if (!match) {
      if (
        rootServerPrefixes.some(
          (p) => pathname === p || pathname.startsWith(`${p}/`),
        )
      ) {
        return proxy();
      }
      if (pathname === "/") return rootIndex(200);
      if (pathname !== "/index.html") {
        const asset = Bun.file(`./dist${pathname}`);
        if (await asset.exists()) return new Response(asset);
      }
      return rootIndex(404);
    }

    const [, id, rest] = match;
    // The bare /org/<id> is the server's redirect to /org/<id>/.
    if (rest === undefined) return proxy();
    if (
      organisationAPIPrefixes.some(
        (p) => rest === p || rest.startsWith(`${p}/`),
      )
    ) {
      return proxy();
    }

    const resolved = rest === "/" ? "/index.html" : rest;

    const distFile = Bun.file(`./dist${resolved}`);
    if (resolved !== "/index.html" && (await distFile.exists())) {
      return new Response(distFile);
    }

    const publicFile = Bun.file(`./public${resolved}`);
    if (await publicFile.exists()) return new Response(publicFile);

    // An id the server does not run is the root's 404, as in prod, rather than
    // an app shell whose every request then fails.
    const known = await fetch(
      `http://localhost:${apiPort}/org/${id}/api/organisation`,
      { signal: req.signal },
    );
    if (!known.ok) return rootIndex(404);

    // The server rewrites index.html's <base> to the Organisation's path as it
    // serves it (pkg/api/frontend.go); this does the same to the dev build.
    const index = await Bun.file("./dist/index.html").text();
    return new Response(
      index.replace('<base href="/" />', `<base href="/org/${id}/" />`),
      { headers: { "Content-Type": "text/html; charset=utf-8" } },
    );
  },
});

console.log(`Dev server: http://localhost:${server.port}`);
