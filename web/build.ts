import { readdirSync, copyFileSync } from "fs";

const tsc = Bun.spawnSync(["tsc", "-b"], {
  stdio: ["inherit", "inherit", "inherit"],
});
if (tsc.exitCode !== 0) process.exit(tsc.exitCode ?? 1);

const result = await Bun.build({
  entrypoints: ["./index.html"],
  outdir: "./dist",
  minify: true,
  define: { "process.env.NODE_ENV": '"production"' },
});

if (!result.success) {
  for (const msg of result.logs) console.error(msg);
  process.exit(1);
}

for (const file of readdirSync("./public")) {
  copyFileSync(`./public/${file}`, `./dist/${file}`);
}

console.log("Build complete!");
