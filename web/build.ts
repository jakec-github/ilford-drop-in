import { readdirSync, copyFileSync, existsSync } from "fs";

// BASE_TAG is the <base> element index.html carries, and the one token the
// server rewrites when the site is served under a base path — the same literal
// as pkg/api.baseTag and the string web/src/basePath.ts reads back. It is
// asserted here because nothing else would notice it going missing: the page
// still renders, then dies on the first asset it cannot find.
const BASE_TAG = '<base href="/" />';

const tsc = Bun.spawnSync(["tsc", "-b"], {
  stdio: ["inherit", "inherit", "inherit"],
});
if (tsc.exitCode !== 0) process.exit(tsc.exitCode ?? 1);

const result = await Bun.build({
  entrypoints: ["./index.html"],
  outdir: "./dist",
  minify: true,
  // Relative asset references, resolved against index.html's <base> element —
  // see assertAssetsResolveAgainstTheBaseTag below.
  publicPath: "./",
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

await assertAssetsResolveAgainstTheBaseTag();

console.log("Build complete!");

// A relative asset reference in index.html resolves against the document's base
// URL. Without a <base> element that is the current path, so on a nested route
// like /organiser/volunteers the browser asks for /organiser/chunk-*.js — not in the
// build, answered by the server's SPA fallback with index.html, and the page
// dies on a module script served as text/html.
//
// The base tag is what fixes that, and it is also what lets the whole site move
// under a path without the build knowing which one (issue #201). So the two
// halves are asserted together: every reference relative, and a base tag for
// them to resolve against.
async function assertAssetsResolveAgainstTheBaseTag() {
  const html = await Bun.file("./dist/index.html").text();

  if (!html.includes(BASE_TAG)) {
    console.error(
      `Build failed: dist/index.html does not carry ${BASE_TAG}, so its relative asset references have nothing to resolve against on a nested route, and the server has no token to rewrite when the site is served under a base path.`,
    );
    process.exit(1);
  }

  // "/" is the base tag's own href, which is the one root-absolute reference
  // that belongs here — it is the token the server rewrites.
  const absolute = [...html.matchAll(/(?:src|href)="(\/[^"]*)"/g)]
    .map((m) => m[1])
    .filter((ref) => ref !== "/");
  if (absolute.length > 0) {
    console.error(
      `Build failed: dist/index.html has root-absolute asset references, which point outside the site when it is served under a base path:\n${absolute.map((r) => `  ${r}`).join("\n")}`,
    );
    process.exit(1);
  }
}
