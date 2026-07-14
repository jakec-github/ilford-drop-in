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

const server = Bun.serve({
  port: 5173,
  async fetch(req) {
    const pathname = new URL(req.url).pathname;
    const resolved = pathname === "/" ? "/index.html" : pathname;

    const distFile = Bun.file(`./dist${resolved}`);
    if (await distFile.exists()) return new Response(distFile);

    const publicFile = Bun.file(`./public${resolved}`);
    if (await publicFile.exists()) return new Response(publicFile);

    return new Response(Bun.file("./dist/index.html"));
  },
});

console.log(`Dev server: http://localhost:${server.port}`);
