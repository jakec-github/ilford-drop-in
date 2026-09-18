# Allocation rules bind the allocator, not the Rota Editor

Status: accepted, 2026-09-18 (#212). Amended 2026-09-18 (#214).

A Shape's Seats and the Roles a Volunteer holds on the roster are **allocator
inputs**. They bound what the solver may produce, and what a Preallocation may
promise it. They do **not** bound a Rota Editor changing a published rota.

This is already how the server behaves — ADR 0005's "structure is enforced,
standing is advisory", amended by #185 — but it had never been stated as one
rule spanning both halves of the system, and the frontend did not follow it.
Every Role picker on the rota page was gated on a hardcoded `"Team lead"` and
its single Seat, so a deployment whose Roles were named anything else could
not add, replace, move or pin anybody (#212). The gate was invented on the
client: no server endpoint it called had it.

## Decisions and their reasons

- **The two halves have different masters.** Allocation is a solve: it runs
  against rules, and a rule it cannot satisfy is a rota it cannot produce, so
  the inputs have to be true. Editing a published rota is a record of what
  actually happened, made by a person who was there and knows more than the
  roster does. A rule that is load-bearing for the first is an opinion to the
  second.

- **Therefore: a Rota Editor is constrained by structure and nothing else.**
  One person is on a Shift at most once. That is the whole of it. They may
  place somebody in a Role the roster does not record them as holding (it
  warns, and proceeds — ADR 0005), and they may put a Shift over the strength
  its Shape asks for (nothing checks it — #185, because the Shape froze at
  allocation and an extra pair of hands still has to be recordable).

- **And: a Preallocation is held to every allocator rule.** It is an
  instruction to a solve that has not run yet. Pinning somebody into a Role
  they do not hold, or past the Seats their Shift's Shape gives that Role, is
  refused — the one exception being that the pin *grants* the Role for that
  Shift once it is made (ADR 0005, #109). The Shape is editable at that point,
  so a refusal always has a way through.

- **Which Roles a picker offers differs between the two; that a Seat has a
  Role at all does not.** A Seat is a Seat *in a Role* — that is what a Shape
  says — so whoever fills one states which. The rule holds whether they are on
  the roster or not: a custom entry pinned before allocation names its Role, and
  a custom entry added as cover afterwards names one too (#214). Being off the
  roster narrows nothing, because there is no roster entry to narrow by; it is
  not a licence to leave the Role unsaid. Only a removal states none, having
  nobody arriving to state one for.

- **A screen mirrors what the endpoint behind it will accept, and invents
  nothing.** A client-side gate the server does not have is indistinguishable
  from a feature until the day a deployment configures its way into the gap, at
  which point the app is simply broken and nothing on the server can explain
  why. So: the Role picker on a published-rota change offers every configured
  Role; the Role picker on a pin offers the Roles that Volunteer holds which
  the Shift still has a free Seat for, and says which of their Roles are full.

- **Rejected: enforce the Shape on both.** It reads consistent and is the rule
  #212 was originally written asking for. It is wrong twice over. An allocated
  Shift's Shape is frozen, so a Shift at full strength would have no Role left
  to offer and a cover would become unrecordable — exactly the failure #185
  exists to prevent. And it would put the rule in two places, where the client
  copy silently drifts from the server's.

- **Rejected: drop the pin's gates too, for symmetry.** The solver would then
  be handed a Seat no Shape has, and fail the whole allocation at the moment
  there is least time to fix it. The asymmetry is the point: these are two
  different activities that happen to touch the same rows.

## Consequences

- No Role name is written in `web/src`. Which Roles exist comes from
  `GET /api/roles` via `useRoles`; how many of one a Shift has comes from that
  Shift's own `shape` on the rota payload.

- An Assignee may carry no Role at all — an Allocation predating the `role`
  column — and the frontend carries that through as an empty Role rather than
  inventing one. A picker offering to change it still lists it, so opening a
  dialog never silently rewrites what the rota says.

- New rules land on one side or the other deliberately. "Can the solver do
  this?" and "can a person record this?" are separate questions, and a rule
  added to the allocator does not become a rule about covers by default.
