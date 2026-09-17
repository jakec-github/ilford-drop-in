import { createContext, useContext } from "react";

// AccessLevel is how much a signed-in person may do. An Organiser makes every
// edit the app offers; a Rota Editor changes shifts and nothing else —
// Alterations and Cover on an allocated rota, Preallocations on the rota in
// flight. The server re-checks the level on every request, so hiding a control
// here is about not offering what would be refused, never about enforcing it.
export type AccessLevel = "organiser" | "rotaEditor";

// Session is who is logged in, as GET /auth/me reports it.
export interface Session {
  email: string;
  level: AccessLevel;
}

// AuthState is the global session, exposed to every component so that UI can
// show or hide itself based on who (if anyone) is logged in and what they may do.
export interface AuthState {
  // email of the logged-in person, or null when logged out.
  email: string | null;
  // what they may do, or null when logged out. Null exactly when email is.
  level: AccessLevel | null;
  // true until the initial session check has completed. UI that gates on login
  // should wait for this to avoid flashing logged-out state on first paint.
  loading: boolean;
  // logout clears the session and updates the global state.
  logout: () => Promise<void>;
}

export const AuthContext = createContext<AuthState | undefined>(undefined);

// useAuth reads the global session. Must be called within an AuthProvider.
export function useAuth(): AuthState {
  const ctx = useContext(AuthContext);
  if (ctx === undefined) {
    throw new Error("useAuth must be used within an AuthProvider");
  }
  return ctx;
}
