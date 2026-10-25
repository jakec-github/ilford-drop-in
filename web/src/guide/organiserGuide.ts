import { readFileSync } from "node:fs";
import { join } from "node:path";

// The organiser's guide as HTML. Imported with { type: "macro" }, so this runs
// when the app is bundled, not in the browser: docs/organiser-guide.md is the
// only copy of the guide, and the build is what turns it into the page.
export function organiserGuideHtml(): string {
  const markdown = readFileSync(
    join(import.meta.dir, "../../../docs/organiser-guide.md"),
    "utf8",
  );
  return Bun.markdown.html(markdown, { headings: { ids: true } });
}
