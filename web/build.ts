import { readdirSync, copyFileSync, existsSync } from "fs";

const tsc = Bun.spawnSync(["tsc", "-b"], {
  stdio: ["inherit", "inherit", "inherit"],
});
if (tsc.exitCode !== 0) process.exit(tsc.exitCode ?? 1);

const result = await Bun.build({
  entrypoints: ["./index.html"],
  outdir: "./dist",
  minify: true,
  // Root-absolute asset references, so index.html works from any route depth —
  // see assertRootAbsoluteAssets below.
  publicPath: "/",
  define: { "process.env.NODE_ENV": '"production"' },
});

if (!result.success) {
  for (const msg of result.logs) console.error(msg);
  process.exit(1);
}

if (existsSync("./public")) {
  for (const file of readdirSync("./public")) {
    copyFileSync(`./public/${file}`, `./dist/${file}`);
  }
}

await assertRootAbsoluteAssets();

console.log("Build complete!");

// A relative asset reference in index.html resolves against the current path,
// so on a nested route like /admin/volunteers the browser asks for
// /admin/chunk-*.js. That is not in the build, the server's SPA fallback answers
// with index.html, and the page dies on a module script served as text/html.
// publicPath above makes every reference root-absolute; this fails the build if
// that ever stops holding.
async function assertRootAbsoluteAssets() {
  const html = await Bun.file("./dist/index.html").text();
  const relative = [...html.matchAll(/(?:src|href)="(\.[^"]*)"/g)].map(
    (m) => m[1],
  );
  if (relative.length > 0) {
    console.error(
      `Build failed: dist/index.html has relative asset references, which break nested routes on hard navigation:\n${relative.map((r) => `  ${r}`).join("\n")}`,
    );
    process.exit(1);
  }
}
