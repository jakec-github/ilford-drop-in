import { useEffect, useState, type ReactNode } from "react";
import { fetchSession, logout as logoutRequest } from "./api";
import { AuthContext, type Session } from "./auth-context";

// AuthProvider checks the session once on mount and shares it with the whole
// tree via AuthContext, so status lives in one place rather than being
// re-fetched by each component that needs it.
export function AuthProvider({ children }: { children: ReactNode }) {
  const [session, setSession] = useState<Session | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    fetchSession()
      .then(setSession)
      .catch(() => setSession(null))
      .finally(() => setLoading(false));
  }, []);

  async function logout() {
    await logoutRequest();
    setSession(null);
  }

  return (
    <AuthContext.Provider
      value={{
        email: session?.email ?? null,
        level: session?.level ?? null,
        loading,
        logout,
      }}
    >
      {children}
    </AuthContext.Provider>
  );
}
