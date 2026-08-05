import { useEffect, useMemo, useState } from "react";
import { fetchRoles } from "../api";
import type { ConfiguredRole, Role, RoleColour } from "../types";

// RoleColourOf answers what a Role is drawn in. null covers both "the roles
// have not loaded yet" and "no configured Role goes by that name" — a rota can
// name a Role that was retired since it was allocated — and callers treat the
// two the same way, by falling back to their neutral colour.
export type RoleColourOf = (role: Role) => RoleColour | null;

interface UseRoles {
  // null while the first load is still in flight.
  roles: ConfiguredRole[] | null;
  colourOf: RoleColourOf;
  error: string | null;
}

// useRoles owns which Roles the server configures. It is read-only and, unlike
// the rota it colours, nothing invalidates it: Roles change when the config
// changes, which restarts the server.
//
// A failed load is not fatal to the caller — the page it colours is still
// readable, in the fallback colour — so `error` is there to be reported
// alongside the rota rather than instead of it.
export function useRoles(): UseRoles {
  const [roles, setRoles] = useState<ConfiguredRole[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    fetchRoles()
      .then((loaded) => {
        if (cancelled) return;
        setRoles(loaded);
        setError(null);
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        setError(err instanceof Error ? err.message : "Failed to load roles");
      });
    return () => {
      cancelled = true;
    };
  }, []);

  // Memoised on the loaded roles rather than rebuilt per render: the lookup is
  // passed down to every chip, and a new function each render would re-render
  // all of them.
  const colourOf = useMemo<RoleColourOf>(() => {
    const byName = new Map((roles ?? []).map((r) => [r.name, r.colour]));
    return (role) => byName.get(role) ?? null;
  }, [roles]);

  return { roles, colourOf, error };
}
