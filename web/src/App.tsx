import { Link, Redirect, Route, Switch, useLocation } from "wouter";
import RotaViewer from "./components/RotaViewer";
import OrganiserPage from "./components/OrganiserPage";
import AvailabilityForm from "./components/AvailabilityForm";
import { ORGANISER_TABS } from "./components/organiserTabs";
import { useRota } from "./hooks/useRota";
import { useAuth } from "./auth-context";
import Button from "./ui/Button";

// AuthStatus shows a login link when logged out, or the signed-in email plus a
// logout button when logged in. It reads the global auth state so login status
// is shared with the rest of the UI. The whole OAuth dance is server-side
// redirects, so login is a plain link.
function AuthStatus() {
  const { email, loading, logout } = useAuth();

  // Wait for the initial session check so we don't flash "Log in" at someone
  // who is already signed in.
  if (loading) return null;

  if (email === null) {
    return (
      <a className="auth-status" href="/auth/login">
        Log in
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
// rota and the Organiser area — whichever of the two you are not currently on.
// Only an Organiser is offered the way in: a Rota Editor's work is all on the
// rota page, and nothing in the Organiser area is theirs.
function Header() {
  const { level } = useAuth();
  const [location] = useLocation();
  const onOrganiser = location.startsWith("/organiser");

  return (
    <header className="app-header">
      <nav className="app-nav">
        {onOrganiser ? (
          <Link href="/">Rota</Link>
        ) : (
          level === "organiser" && <Link href="/organiser">Organiser</Link>
        )}
      </nav>
      <AuthStatus />
    </header>
  );
}

// HomeView is the public rota page. Signing in also reveals shifts whose rota
// has not been allocated yet, and unlocks editing — as much of it as the
// level allows.
function HomeView() {
  const { level } = useAuth();
  const { shifts, error, change, setClosed, setTimes, setShape } = useRota();

  if (error) {
    return <p className="app-status">Could not load the rota: {error}</p>;
  }
  if (shifts === null) {
    return <p className="app-status">Loading rota…</p>;
  }
  return (
    <RotaViewer
      rotaShifts={shifts}
      level={level}
      onChange={change}
      onSetClosed={setClosed}
      onSetTimes={setTimes}
      onSetShape={setShape}
    />
  );
}

function App() {
  return (
    <Switch>
      {/* The volunteer's own page, outside the shell the rest of the app
          shares. Whoever opens this link is a volunteer and has nowhere else
          to go: a nav bar offering "Log in" would be noise on the one
          screen a volunteer ever sees. */}
      <Route path="/availability/:token">
        {(params) => <AvailabilityForm token={params.token} />}
      </Route>

      <Route>
        <>
          <Header />
          <Switch>
            <Route path="/" component={HomeView} />

            {/* /organiser is the Organiser area's front door, not a page of its
                own: it lands on the first tab. */}
            <Route path="/organiser">
              <Redirect to={ORGANISER_TABS[0].path} replace />
            </Route>

            {ORGANISER_TABS.map((tab) => (
              <Route key={tab.path} path={tab.path}>
                <OrganiserPage tab={tab} />
              </Route>
            ))}

            <Route>
              <p className="app-status">
                Page not found. <Link href="/">Back to the rota</Link>
              </p>
            </Route>
          </Switch>
        </>
      </Route>
    </Switch>
  );
}

export default App;
