import { Link, Redirect, Route, Switch, useLocation } from "wouter";
import RotaViewer from "./components/RotaViewer";
import AdminPage from "./components/AdminPage";
import { ADMIN_TABS } from "./components/adminTabs";
import { useRota } from "./hooks/useRota";
import { useAuth } from "./auth-context";
import Button from "./ui/Button";

// AuthStatus shows a login link when logged out, or the admin's email plus a
// logout button when logged in. It reads the global auth state so login status
// is shared with the rest of the UI. The whole OAuth dance is server-side
// redirects, so login is a plain link.
function AuthStatus() {
  const { email, loading, logout } = useAuth();

  // Wait for the initial session check so we don't flash "Admin login" at an
  // admin who is already signed in.
  if (loading) return null;

  if (email === null) {
    return (
      <a className="auth-status" href="/auth/login">
        Admin login
      </a>
    );
  }

  return (
    <span className="auth-status">
      {email}
      <Button size="small" onClick={logout}>
        Log out
      </Button>
    </span>
  );
}

// Header carries the shared auth state plus the one link that moves between the
// public rota and the admin area — whichever of the two you are not currently
// on. An admin session is exactly a non-null email — only admins are issued one
// — so the admin link gates on that.
function Header() {
  const { email } = useAuth();
  const [location] = useLocation();
  const onAdmin = location.startsWith("/admin");

  return (
    <header className="app-header">
      <nav className="app-nav">
        {onAdmin ? (
          <Link href="/">Rota</Link>
        ) : (
          email !== null && <Link href="/admin">Admin</Link>
        )}
      </nav>
      <AuthStatus />
    </header>
  );
}

// HomeView is the public rota page. An admin session (a non-null email) also
// reveals shifts whose rota has not been allocated yet, and unlocks editing.
function HomeView() {
  const { email } = useAuth();
  const { shifts, error, change } = useRota();

  if (error) {
    return <p className="app-status">Could not load the rota: {error}</p>;
  }
  if (shifts === null) {
    return <p className="app-status">Loading rota…</p>;
  }
  return (
    <RotaViewer
      rotaShifts={shifts}
      isAdmin={email !== null}
      onChange={change}
    />
  );
}

function App() {
  return (
    <>
      <Header />
      <Switch>
        <Route path="/" component={HomeView} />

        {/* /admin is the admin area's front door, not a page of its own: it
            lands on the first tab. */}
        <Route path="/admin">
          <Redirect to={ADMIN_TABS[0].path} replace />
        </Route>

        {ADMIN_TABS.map((tab) => (
          <Route key={tab.path} path={tab.path}>
            <AdminPage tab={tab} />
          </Route>
        ))}

        <Route>
          <p className="app-status">
            Page not found. <Link href="/">Back to the rota</Link>
          </p>
        </Route>
      </Switch>
    </>
  );
}

export default App;
