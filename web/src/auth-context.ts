import { createContext, useContext } from "react";

// AuthState is the global admin session, exposed to every component so that
// admin-only UI can show or hide itself based on who (if anyone) is logged in.
export interface AuthState {
  // email of the logged-in admin, or null when logged out.
  email: string | null;
  // true until the initial session check has completed. UI that gates on login
  // should wait for this to avoid flashing logged-out state on first paint.
  loading: boolean;
  // logout clears the session and updates the global state.
  logout: () => Promise<void>;
}

export const AuthContext = createContext<AuthState | undefined>(undefined);

// useAuth reads the global admin session. Must be called within an AuthProvider.
export function useAuth(): AuthState {
  const ctx = useContext(AuthContext);
  if (ctx === undefined) {
    throw new Error("useAuth must be used within an AuthProvider");
  }
  return ctx;
}
