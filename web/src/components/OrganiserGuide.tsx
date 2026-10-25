import { organiserGuideHtml } from "../guide/organiserGuide" with { type: "macro" };
import Logo from "./Logo";
import "./OrganiserGuide.css";

// Rendered from the repo's own Markdown at build time, so the HTML is ours and
// not anybody's input.
const html = organiserGuideHtml();

// OrganiserGuide is the guide at /guide, outside every Organisation so it can
// be read before a drop-in is set up. It fetches nothing.
export default function OrganiserGuide() {
  return (
    <main className="organiser-guide">
      <a href="/" className="organiser-guide-home" aria-label="Rota Wise home">
        <Logo size={32} />
      </a>
      <article dangerouslySetInnerHTML={{ __html: html }} />
    </main>
  );
}
