import { useCallback, useEffect, useMemo, useState } from "react";
import { createRole, fetchRoles, updateRole } from "../api";
import type { ConfiguredRole, Role, RoleColour, RoleEdit } from "../types";

// RoleColourOf answers what a Role is drawn in. null covers both "the roles
// have not loaded yet" and "no configured Role goes by that name" — a rota can
// name a Role that has been renamed since it was allocated — and callers treat
// the two the same way, by falling back to their neutral colour.
export type RoleColourOf = (role: Role) => RoleColour | null;

// RoleIdOf answers which Role a name refers to. null while the roles are still
// loading, or for a name no configured Role goes by — a caller that has to
// reference a Role says so rather than sending a reference it made up.
export type RoleIdOf = (role: Role) => string | null;

interface UseRoles {
  // null while the first load is still in flight.
  roles: ConfiguredRole[] | null;
  colourOf: RoleColourOf;
  // The roster spells a Role out in a cell and every picker here follows it, so
  // a screen holds a name where the API wants the id a pin references
  // (issue #195). This is the one place that maps between them.
  idOf: RoleIdOf;
  error: string | null;
  // Adds a Role, then reloads. Rejects with the server's own message when the
  // write is refused — `a role called "Team lead" already exists` is the whole
  // explanation, and the caller shows it rather than inventing one.
  addRole: (role: RoleEdit) => Promise<void>;
  // Rewrites one Role by id, then reloads. There is no removeRole and there
  // will not be one: a Role is permanent (ADR 0006).
  saveRole: (id: string, role: RoleEdit) => Promise<void>;
}

// useRoles owns which Roles the drop-in offers. Most callers only read it — the
// rota and the roster colour their chips by it — and the settings screen is the
// one that writes.
//
// A failed load is not fatal to a caller that only colours things: the page is
// still readable in the fallback colour, so `error` is there to be reported
// alongside the rota rather than instead of it. The settings screen, which has
// nothing to show without it, reports it instead.
export function useRoles(): UseRoles {
  const [roles, setRoles] = useState<ConfiguredRole[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [reloads, setReloads] = useState(0);

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
  }, [reloads]);

  // Reloads whether or not the write landed, then re-throws so the caller can
  // say why. A refusal is the case that most needs the re-read: the server
  // turned it down because what is held here is out of date — another admin
  // took the name — so leaving the old list up would contradict the message
  // shown next to it.
  const write = useCallback(async (apply: () => Promise<void>) => {
    try {
      await apply();
    } finally {
      setReloads((n) => n + 1);
    }
  }, []);

  const addRole = useCallback(
    (role: RoleEdit) => write(() => createRole(role)),
    [write],
  );

  const saveRole = useCallback(
    (id: string, role: RoleEdit) => write(() => updateRole(id, role)),
    [write],
  );

  // Memoised on the loaded roles rather than rebuilt per render: the lookup is
  // passed down to every chip, and a new function each render would re-render
  // all of them.
  const colourOf = useMemo<RoleColourOf>(() => {
    const byName = new Map((roles ?? []).map((r) => [r.name, r.colour]));
    return (role) => byName.get(role) ?? null;
  }, [roles]);

  const idOf = useMemo<RoleIdOf>(() => {
    const byName = new Map((roles ?? []).map((r) => [r.name, r.id]));
    return (role) => byName.get(role) ?? null;
  }, [roles]);

  return { roles, colourOf, idOf, error, addRole, saveRole };
}
