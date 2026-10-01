# Config allows, the allocator obeys, people decide

Status: proposed, 2026-10-01. Would supersede ADR 0009's rule that a
Preallocation is held to every allocator rule; the rest of 0009 stands.

## In plain words

A rota is put together from three sources:

1. **Rota Defaults and Allocation Settings** — set by an Organiser once, and
   applied to every rota: how many people a shift needs and in which Roles, when
   shifts run, how often someone may be asked to work.
2. **Specific decisions** — made by an Organiser or Rota Editor about one rota
   or one shift: a Shift's own Shape or times, closing a Shift, pinning somebody
   to a Shift before allocation, changing who is on a Shift after it.
3. **The allocator** — fills whatever Seats are left, from the people who said
   they were available.

They are trusted differently, on purpose:

- **The settings should be able to say what you mean.** They aim to describe as
  many ways of running a rota as is practical.
- **The allocator follows the rules exactly.** It never breaks a rule to fill a
  Seat. If the rules leave a Seat it cannot fill, the Seat stays empty and you
  fill it by hand.
- **Allocation always produces a rota.** However strict the rules, you get a
  rota back. It may have gaps; it will not be an error.
- **People can overrule the rules.** A pin or a change made by an Organiser or
  Rota Editor is a decision somebody has already taken. It is not checked
  against the rules. Pin someone two weeks running, put three people in a Seat
  meant for two, or give a shift to someone who said they were away: the app
  records it. The allocator works around what you decided and does not undo it.
- **A pin pins one person.** Pinning one member of a couple does not force
  the other onto the Shift. The allocator brings them along only if it could
  have placed them anyway: they are available, and no rule stops it.
- **A Shape is for the allocator.** It says how many people the allocator
  aims for. It does not limit how many you may pin, and you can shrink a Shape
  below the pins already on its Shift.
- **The exceptions:** nobody can be on the same Shift twice, and nobody can be
  pinned to a Closed Shift. Reopen the Shift first.

## Rules for implementers

### The allocator

- **Every rule is a restriction the allocator can meet by doing less.** A rule
  says what the allocator must not do. A rule that demands something (a male
  on every shift, a team lead on every shift) must have an escape that leaving
  a Seat empty satisfies — see `male_required`. If leaving everything empty
  would break a rule, the rule can make a rota infeasible and is written wrong.
- **Rules govern the allocator's choices, never people's decisions.** Every
  constraint exempts what a pin forces. A pinned person still counts towards
  their own totals, so the allocator adds no *more* of them where a rule says
  no. But a pin is never what makes a rule unsatisfiable. Concretely:
  - `no_back_to_back`, `one_shift_per_month`, `max_frequency`: a pinned
    (volunteer, shift) pair is exempt; the rest of that volunteer's shifts are
    still constrained, with the pins counted.
  - `seat_capacity`: pins may overfill a Role. The allocator places nobody else
    in that Role, and does not go negative.
  - `availability`: already exempt.
  - `grouping`: a pin forces only the person it names. Their group-mates
    are allocator choices, held to every rule. Where the rules allow it they
    join the pinned person; where they don't, the group splits for that Shift.
    The pin wins over keeping the group together.
  - Demanding rules (`male_required`): a shift whose pins decide it entirely
    is exempt (already the case).
- **Infeasible is a bug, not a result.** If a solve comes back infeasible,
  either a rule is missing its escape or it constrains a pin. Fix that rule; do
  not add a refusal to stop the input reaching it.

### The settings

- **Incomplete settings may refuse; restrictive settings may not.** Refusing to
  allocate because something was never stated (no shift times, no default
  Shape) is fine: the app is not yet told what a rota is. Refusing because the
  stated rules are too tight to fill is not. That gives a rota with gaps.
- **Validate settings when they are saved, not when a solve runs.** If a
  setting is invalid on its own terms, say so at the form, while the person who
  can fix it is looking at it.

### People's decisions (Preallocations and Alterations)

- **Refuse only what cannot be represented, never what is merely against the
  rules.** Today that is:
  - **The same person on a Shift twice.** A person can fill only one Seat, and
    one attendance per (person, Shift) is what the data model and the solver are
    both built on.
  - **A Seat with no Role.** Every Assignee names one of the configured Roles,
    because colours, the calendar, the Shape counts and the response grid key
    off it. It need not be a Role the person holds, nor one the Shift's Shape
    asks for.
  - **A Shift that does not run.** A Closed Shift has no Seats; open it first.
    Closing is the more deliberate of the two decisions, so it wins.
- **Everything else proceeds.** The person not holding the Role; the Shift going
  past its Shape; the volunteer being unavailable, inactive, pinned back to
  back, or over the frequency cap; one member of a group pinned without the
  rest; a Shape edited down below the pins already on it. None of these is
  refused.
- **Warning is allowed; blocking is not.** A screen may tell somebody that a
  decision breaks a rule. It must still let them make it. Systematically
  flagging broken rules is future work.

### Precedence

When two sources disagree, the more specific wins: a decision about a Shift
beats a default, and a person's decision beats the allocator's. A Standing
Preallocation is a default. Once it seeds a rota, its pins are ordinary
decisions, and the rota keeps them whatever later happens to the default.

## Why

- A rule exists to steer a choice. Where a person has already made the choice,
  the rule has nothing left to steer. All it can do is refuse a decision made
  by someone who knows something the roster does not.
- ADR 0009 refused rule-breaking pins because the solver would be handed an
  input it could not satisfy, and fail when there is least time to fix it. That
  is answered by changing the solver, not the pin: if every constraint exempts
  what pins force, no pin can make a solve infeasible.
- One rule across both halves is easier to explain than 0009's split, where a
  pin was held to rules an Alteration made a day later was not.

## Consequences

- `pkg/core/services/preallocations.go` (and the Standing Preallocation
  equivalent) stop refusing a pin when the volunteer does not hold its Role, or
  when it would go past the Shape's Seats. `seatsHoldThePins` stops refusing a
  Shape edit.
- The pin services stop refusing an inactive volunteer. The solver is sent
  active volunteers only (`solveRota.go`), so it must also be sent anyone
  inactive who holds a pin, scoped to their pinned Shifts.
- pyallocator: `no_back_to_back`, `one_shift_per_month`, `max_frequency` and
  `seat_capacity` exempt pinned pairs. `preallocations` forces the pinned
  person only, no longer their group. `grouping` exempts a pinned person's
  Shift. A group-mate is never forced there; the objective rewards placing
  them heavily enough that they join whenever the rules allow. A hard
  "if possible" is not something CP-SAT can say, and a soft term gets the
  same result without risking an infeasible solve. `problem.py` stops rejecting a pin to a
  Role the Shape has no Seat for. Each change gets a test where the pin breaks
  the rule and the solve still succeeds.
- The pin picker offers every configured Role, as the Alteration picker already
  does.
- CONTEXT.md (Preallocation, Rota Editor) and ADR 0009 are updated to point
  here.
