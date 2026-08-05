import { Link } from "wouter";
import { useAuth } from "../auth-context";
import { ADMIN_TABS, type AdminTab } from "./adminTabs";
import "./AdminPage.css";

function WipPanel({ title }: { title: string }) {
  return (
    <section className="admin-panel">
      <h2>{title}</h2>
      <p>Work in progress — this tool has not been built yet.</p>
    </section>
  );
}

// AdminPage is the shell every admin tab shares: the admin-only gate, the
// heading and the tab bar. It waits for the initial session check so it doesn't
// flash the login prompt at an admin who is already signed in.
export default function AdminPage({ tab }: { tab: AdminTab }) {
  const { email, loading } = useAuth();

  if (loading) {
    return <p className="app-status">Loading…</p>;
  }
  if (email === null) {
    return (
      <p className="app-status">
        This page is for admins. <a href="/auth/login">Admin login</a>
      </p>
    );
  }

  const { Panel } = tab;

  return (
    <main className={tab.wide ? "admin-page admin-page--wide" : "admin-page"}>
      <h1>Admin</h1>

      <nav className="admin-tabs">
        {ADMIN_TABS.map((t) => (
          <Link
            key={t.path}
            href={t.path}
            className={
              t.path === tab.path ? "admin-tab admin-tab--current" : "admin-tab"
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
