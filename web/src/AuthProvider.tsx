import { useEffect, useState, type ReactNode } from "react";
import { fetchCurrentAdmin, logout as logoutRequest } from "./api";
import { AuthContext } from "./auth-context";

// AuthProvider checks the admin session once on mount and shares it with the
// whole tree via AuthContext, so status lives in one place rather than being
// re-fetched by each component that needs it.
export function AuthProvider({ children }: { children: ReactNode }) {
  const [email, setEmail] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    fetchCurrentAdmin()
      .then(setEmail)
      .catch(() => setEmail(null))
      .finally(() => setLoading(false));
  }, []);

  async function logout() {
    await logoutRequest();
    setEmail(null);
  }

  return (
    <AuthContext.Provider value={{ email, loading, logout }}>
      {children}
    </AuthContext.Provider>
  );
}
