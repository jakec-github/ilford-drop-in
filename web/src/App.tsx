import { useEffect, useState } from "react";
import RotaViewer from "./components/RotaViewer";
import { fetchRota } from "./api";
import { useAuth } from "./auth-context";
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
      <button type="button" onClick={logout}>
        Log out
      </button>
    </span>
  );
}

function App() {
  const [rota, setRota] = useState<RotaShift[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    fetchRota()
      .then(setRota)
      .catch((err: unknown) => {
        setError(err instanceof Error ? err.message : "Failed to load rota");
      });
  }, []);

  return (
    <>
      <header className="app-header">
        <AuthStatus />
      </header>
      {error ? (
        <p className="app-status">Could not load the rota: {error}</p>
      ) : rota === null ? (
        <p className="app-status">Loading rota…</p>
      ) : (
        <RotaViewer rotaShifts={rota} />
      )}
    </>
  );
}

export default App;
