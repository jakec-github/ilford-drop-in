import { useEffect, useState } from "react";
import RotaViewer from "./components/RotaViewer";
import { fetchRota, fetchCurrentAdmin, logout } from "./api";
import type { RotaShift } from "./types";

// AuthStatus shows a login link when logged out, or the admin's email plus a
// logout button when logged in. The whole OAuth dance is server-side redirects,
// so login is a plain link.
function AuthStatus() {
  const [email, setEmail] = useState<string | null>(null);

  useEffect(() => {
    fetchCurrentAdmin()
      .then(setEmail)
      .catch(() => setEmail(null));
  }, []);

  async function handleLogout() {
    await logout();
    setEmail(null);
  }

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
      <button type="button" onClick={handleLogout}>
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
