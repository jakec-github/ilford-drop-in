import { watch } from "fs";

async function build() {
  const result = await Bun.build({
    entrypoints: ["./index.html"],
    outdir: "./dist",
    sourcemap: "inline",
    define: { "process.env.NODE_ENV": '"development"' },
  });
  if (!result.success) {
    for (const msg of result.logs) console.error(msg);
  }
}

await build();

watch("./src", { recursive: true }, build);

const apiPort = process.env.API_PORT ?? "8080";
const apiPrefixes = ["/shifts", "/alterations", "/calendars", "/auth"];

const server = Bun.serve({
  port: 5173,
  hostname: "0.0.0.0",
  async fetch(req) {
    const url = new URL(req.url);
    const pathname = url.pathname;

    if (apiPrefixes.some((p) => pathname === p || pathname.startsWith(`${p}/`))) {
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
