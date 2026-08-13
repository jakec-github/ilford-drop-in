import { useCallback, useEffect, useMemo, useSyncExternalStore } from "react";
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

// What every caller of this hook is looking at. One value rather than two
// pieces of state, so a reader can never see a list from one read beside the
// error from another.
interface RolesSnapshot {
  roles: ConfiguredRole[] | null;
  error: string | null;
}

// The Roles are one list, so there is one copy of it here rather than one per
// component that asked. More than one section of a screen reads it — the
// Settings screen lists the Roles and, right above them, the Rota Defaults card
// offers to shape a shift out of them — and with a list per hook, adding the
// first Role left the card beside it still believing there were none until the
// page was reloaded.
//
// A module-level store rather than a context: this is server data behind a
// per-resource hook, which is where `CLAUDE.md` puts it, and every consumer
// wants the same answer whether or not it happens to sit under the same
// provider.
let snapshot: RolesSnapshot = { roles: null, error: null };
const readers = new Set<() => void>();

// Replaced rather than mutated, so the value identity is what tells React a
// reader has something new to render.
function publish(next: RolesSnapshot) {
  snapshot = next;
  for (const reader of readers) reader();
}

function subscribe(reader: () => void): () => void {
  readers.add(reader);
  return () => {
    readers.delete(reader);
  };
}

function currentSnapshot(): RolesSnapshot {
  return snapshot;
}

// The read in flight, if there is one. Mounting three consumers at once is one
// request, and a write's reload joins a read already running rather than racing
// it.
let reading: Promise<void> | null = null;

function load(): Promise<void> {
  if (reading) return reading;
  reading = fetchRoles()
    .then((loaded) => {
      publish({ roles: loaded, error: null });
    })
    .catch((err: unknown) => {
      // The list that is already up survives a failed re-read: it is the last
      // thing the server actually said, and blanking every chip on the rota
      // because a refresh failed would be the worse answer.
      publish({
        roles: snapshot.roles,
        error: err instanceof Error ? err.message : "Failed to load roles",
      });
    })
    .finally(() => {
      reading = null;
    });
  return reading;
}

// A read that is guaranteed to have started after this call. It is what a write
// reloads with: joining a read already in flight could answer with the list as
// it was fetched before the write landed, which is exactly the list the write
// just made wrong. load() never rejects, so the chaining needs no catch.
function reread(): Promise<void> {
  const current = reading;
  return current ? current.then(load) : load();
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
  // The store above is external to React, and this is the hook React provides
  // for reading one: it subscribes, re-renders on publish, and needs no effect
  // to catch up on a change that landed while this component was rendering.
  const { roles, error } = useSyncExternalStore(subscribe, currentSnapshot);

  // Mounting re-reads, as it did when each caller kept its own list: opening a
  // screen is the moment to find out what has changed since. The dedupe inside
  // load() is what stops several sections of one screen each asking.
  useEffect(() => {
    void load();
  }, []);

  // Reloads whether or not the write landed, then re-throws so the caller can
  // say why. A refusal is the case that most needs the re-read: the server
  // turned it down because what is held here is out of date — another admin
  // took the name — so leaving the old list up would contradict the message
  // shown next to it.
  const write = useCallback(async (apply: () => Promise<void>) => {
    try {
      await apply();
    } finally {
      await reread();
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
