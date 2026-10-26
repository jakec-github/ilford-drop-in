# Allocation Rules are added from a library, not switched on

Status: proposed, 2026-10-26. Supersedes the switch model in
[ADR 0006](0006-domain-settings-in-the-app.md); the rest of 0006 stands.

## In plain words

An Organiser no longer switches a fixed set of global rules on and off. They add
**Allocation Rules** from a **Rule Library**, as many as they like, each with its
own values. A **Volunteer Rule** has a **Scope** — only the volunteers it names,
or everyone except them — so one kind can say different things about different
people: "a gap of 1, everyone except Alice" and "a gap of 3, only Alice". A
**Shift Rule** applies to every Shift. **Allocation Settings** survives as the
umbrella for everything an Organiser decides about how the allocator solves:
the rules today, the weights on its preferences in time.

The global switches were policy. Per-person rules are people's circumstances —
Bob can only do once a month, Alice needs a fortnight between turns — and those
could not be said at all while every rule applied to everyone or no one.

## Decisions and their reasons

- **Every Allocation Rule applies; none overrides another.** A volunteer
  covered by two caps is held to both. Loosening a rule for one person means
  taking them out of its Scope and giving them a rule of their own. Each rule
  then reads as true on its own, there is no precedence to learn or to explain
  on screen, and the solver simply adds the constraints together.

- **A Scope is one list in one mode.** Only these, or everyone except these —
  never both on one rule. A rule naming some to include and some to exclude
  cannot be read at a glance, and means nothing an include list does not
  already say.

- **A Scope names volunteers, not groups.** A rule is about a person; the
  reason for it is theirs. Because a group works together, a rule binds the
  rest of the group in effect, and the screen says so — but the rule does not
  claim to be about them, and stops affecting them the day the roster stops
  grouping them, without anyone editing it.

- **A name that leaves the roster stays on the rule, covering nobody.** The
  roster is a Google Sheet; it moves under the rules. A stale name is shown as
  no longer on the roster and is ignored by the solve until somebody removes it
  or the volunteer returns. It never blocks allocation. A stale *pin* does
  (ADR 0010), because a pin promises a placement; a rule promises nothing, so a
  rule about nobody has nothing left to break. Likewise an "only" Scope naming
  nobody is allowed and flagged rather than refused: it is representable and
  harmless.

- **Every Rule Kind can be satisfied by leaving Seats empty.** No combination
  of Allocation Rules may make the solve infeasible. Rules now repeat and
  overlap, and an Organiser handed "no rota" by five interacting rules has no
  way to tell which one did it. A kind that demands something of a Shift must
  carry an escape — "Require a male" holds a Seat open for one to be added by
  hand — before it may join the library. Pins stay exempt from every rule
  (ADR 0010).

- **The day-one library.**
  - *Cap how often somebody works* — a percentage of the rota, unchanged. The
    per-person limits people actually state ("once a month", "every other
    week") belong to the two kinds below, which mean the same whatever a rota's
    length; the cap is left doing what it is good at, spreading work across a
    rota of any length.
  - *A gap of x Shifts* — between any two Shifts a volunteer works, at least x
    they do not. x = 1 is the old no-back-to-back. A Closed Shift counts toward
    the gap: the point is rest, and a closed week is still a week off. The gap
    reaches back into the end of the previous rota.
  - *At most x Shifts a month* — by calendar month, counting Shifts already
    worked in the previous rota. x = 1 is the old one-shift-per-month.
  - *Require a male* — a Shift Rule, unchanged.

- **A rule may carry a note.** Free text saying why it was added, which the
  allocator never reads. Rules about people's circumstances outlive the memory
  of why they were made, and the Organiser reviewing the list is often not the
  one who wrote it.

## Considered and rejected

- **Most specific wins.** An include-Scope overriding an exclude-Scope would
  make loosening one person a single step, at the cost of a screen that has to
  explain why a rule it shows is not applying to Bob.

- **Scoping by group**, so picking Alice picks Bob. It shows what the solve
  does, but a rule would silently change what it says every time the roster's
  groups change. **Refusing a Scope that splits a group** was rejected too: it
  refuses something representable, against the spirit of ADR 0010.

- **One rule per Shift-Rule kind**, while Shift Rules have no Scope. Tidier
  today, undone the day a Shift Rule gets one. Two identical Shift Rules are
  harmless, so there is nothing to refuse.

- **Carrying the old switches over as rules.** It would keep the next solve
  identical to the last, and was the recommendation. Rejected for simplicity:
  the switches are dropped and the Organiser rebuilds the rules in production
  by hand, so the overhaul ships with none.

- **A per-rota count for the cap** ("at most 2 Shifts a rota"). Reads more
  naturally for one person, but means different things on rotas of different
  lengths, and the gap and monthly kinds already carry the per-person limits.

## Consequences

- The switch map in `allocation_settings`, and the `SwitchableConstraints` /
  `SWITCHABLE_CONSTRAINTS` registries keyed by it, give way to stored rules and
  a library of kinds. How rules are stored is the implementing ticket's call;
  the rule that a stored answer the build no longer understands is ignored with
  a warning, never an error, carries over from ADR 0006 unchanged.

- Until the Organiser rebuilds them, production solves with no optional rules —
  the same as a new deployment.

- The gap and monthly kinds need more history than their predecessors: the
  last x Shifts of the previous rota, and how many Shifts each group worked per
  month, rather than only the last Shift and which months were worked.

- Shift Rules applying to every Shift is "for now". Giving them a Scope (a
  subset of Shifts) is a field on the rule, not a new model.
