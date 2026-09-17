import { Link } from "wouter";
import { useAuth } from "../auth-context";
import { ORGANISER_TABS, type OrganiserTab } from "./organiserTabs";
import "./OrganiserPage.css";

function WipPanel({ title }: { title: string }) {
  return (
    <section className="organiser-panel">
      <h2>{title}</h2>
      <p>Work in progress — this tool has not been built yet.</p>
    </section>
  );
}

// OrganiserPage is the shell every Organiser tab shares: the gate, the heading
// and the tab bar. It waits for the initial session check so it doesn't flash
// the login prompt at an Organiser who is already signed in.
//
// A Rota Editor gets no more of the area than someone logged out does: every
// tool in it is an Organiser's, and none of it is theirs to see.
export default function OrganiserPage({ tab }: { tab: OrganiserTab }) {
  const { level, loading } = useAuth();

  if (loading) {
    return <p className="app-status">Loading…</p>;
  }
  if (level === null) {
    return (
      <p className="app-status">
        This page is for organisers. <a href="/auth/login">Log in</a>
      </p>
    );
  }
  if (level !== "organiser") {
    return (
      <p className="app-status">
        This page is for organisers. <Link href="/">Back to the rota</Link>
      </p>
    );
  }

  const { Panel } = tab;

  return (
    <main
      className={
        tab.wide ? "organiser-page organiser-page--wide" : "organiser-page"
      }
    >
      <h1>Organiser</h1>

      <nav className="organiser-tabs">
        {ORGANISER_TABS.map((t) => (
          <Link
            key={t.path}
            href={t.path}
            className={
              t.path === tab.path
                ? "organiser-tab organiser-tab--current"
                : "organiser-tab"
            }
            aria-current={t.path === tab.path ? "page" : undefined}
          >
            {t.label}
          </Link>
        ))}
      </nav>

      {Panel ? <Panel /> : <WipPanel title={tab.label} />}
    </main>
  );
}
