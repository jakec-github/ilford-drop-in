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

const server = Bun.serve({
  port: webPort,
  hostname: "0.0.0.0",
  async fetch(req) {
    const url = new URL(req.url);
    const pathname = url.pathname;

    if (
      apiPrefixes.some((p) => pathname === p || pathname.startsWith(`${p}/`))
    ) {
      // redirect: "manual" so the 302 from /auth/login to Google reaches the
      // browser instead of being followed server-side here.
      return fetch(`http://localhost:${apiPort}${pathname}${url.search}`, {
        method: req.method,
        headers: req.headers,
        body: req.body,
        redirect: "manual",
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
