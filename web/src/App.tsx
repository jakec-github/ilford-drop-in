import { useEffect, useState } from "react";
import RotaViewer from "./components/RotaViewer";
import AdminDashboard from "./components/AdminDashboard";
import { fetchRota } from "./api";
import { useAuth } from "./auth-context";
import Button from "./ui/Button";
import type { RotaShift } from "./types";

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

// Header carries the shared auth state and, for admins, a link to the admin
// dashboard. An admin session is exactly a non-null email — only admins are
// issued one — so the link gates on that.
function Header() {
  const { email } = useAuth();

  return (
    <header className="app-header">
      <nav className="app-nav">
        {email !== null && <a href="/admin">Admin</a>}
      </nav>
      <AuthStatus />
    </header>
  );
}

// HomeView is the public rota page.
function HomeView() {
  const [rota, setRota] = useState<RotaShift[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    fetchRota()
      .then(setRota)
      .catch((err: unknown) => {
        setError(err instanceof Error ? err.message : "Failed to load rota");
      });
  }, []);

  if (error) {
    return <p className="app-status">Could not load the rota: {error}</p>;
  }
  if (rota === null) {
    return <p className="app-status">Loading rota…</p>;
  }
  return <RotaViewer rotaShifts={rota} />;
}

// AdminView renders the admin dashboard, but only for a logged-in admin. It
// waits for the initial session check so it doesn't flash the login prompt at
// an admin who is already signed in.
function AdminView() {
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
  return <AdminDashboard />;
}

function App() {
  // Routing is a bare pathname switch: both views are reached by full-page
  // navigation (login is a chain of server-side OAuth redirects), so a
  // client-side router earns its keep nowhere here.
  const isAdminRoute = window.location.pathname === "/admin";

  return (
    <>
      <Header />
      {isAdminRoute ? <AdminView /> : <HomeView />}
    </>
  );
}

export default App;
