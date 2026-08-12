import { watch } from "fs";

async function build() {
  const result = await Bun.build({
    entrypoints: ["./index.html"],
    outdir: "./dist",
    sourcemap: "inline",
    // Matches the production build: root-absolute asset references, so nested
    // routes survive a hard navigation here too. See web/build.ts.
    publicPath: "/",
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
// Every path the Go server owns: the API under its prefix, plus the three
// endpoints that deliberately sit outside it (see pkg/api/api.go). Anything
// missing here is not proxied — it falls through to the SPA fallback below and
// the caller gets index.html with a 200, which surfaces as "Unexpected token
// '<'" when it tries to parse it as JSON.
const apiPrefixes = ["/api", "/auth", "/calendars", "/health"];

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

    if (
      apiPrefixes.some((p) => pathname === p || pathname.startsWith(`${p}/`))
    ) {
      // redirect: "manual" so the 302 from /auth/login to Google reaches the
      // browser instead of being followed server-side here.
      //
      // signal ties this request to the browser's. Without it the proxy holds a
      // connection of its own that outlives the one it is answering, and the Go
      // server — whose client is this proxy, not the browser — never sees the
      // caller leave: r.Context() stays live, the solve runs to completion and
      // stores its draft for a request nobody is waiting on, holding the solve
      // slot the next reader queues behind (issue #197).
      return fetch(`http://localhost:${apiPort}${pathname}${url.search}`, {
        method: req.method,
        headers: req.headers,
        body: req.body,
        redirect: "manual",
        signal: req.signal,
      });
    }

    const resolved = pathname === "/" ? "/index.html" : pathname;

    const distFile = Bun.file(`./dist${resolved}`);
    if (await distFile.exists()) return new Response(distFile);

    const publicFile = Bun.file(`./public${resolved}`);
    if (await publicFile.exists()) return new Response(publicFile);

    return new Response(Bun.file("./dist/index.html"));
  },
});

console.log(`Dev server: http://localhost:${server.port}`);
