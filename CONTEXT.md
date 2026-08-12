# Ilford Drop-In Rota

Scheduling system for a weekly charity drop-in: it collects volunteer availability,
allocates volunteers to shifts, and publishes the resulting rota.

## Language

**Shift**:
A planned session of the drop-in, minted by a Rotation, running from a start to
an end given as local time in the drop-in's own timezone. Exists independently
of who is allocated to it. Its date is the date it starts, and there is at most
one Shift per date. Times are descriptive, not an allocator input, so unlike a
Shape they stay editable once the Rotation is allocated.
_Avoid_: shift date (as identity), session

**Shift View**:
A read-only projection of a Shift for display: the Shift plus its effective
assignees after Alterations, closed status, and change metadata.
_Avoid_: shift (for the projection), effective shift

**Rotation**:
A batch of consecutive Shifts over which availability is requested and allocation
runs. Its span and size are derived from the Shifts it minted. Rota is an
acceptable alias.

**Rota in Flight**:
The one Rotation that has not been allocated yet. Defining a Rotation is refused
while one exists, so there is at most one and every screen can address "the
rota" without a picker. A Rotation stops being in flight by being allocated or
by being Discarded.
_Avoid_: current rota, active rota, draft rota (which is a Draft Rota Allocation)

**Discard**:
Destroying the Rota in Flight and everything hanging off it — its Shifts, their
Shapes, its Preallocations, its Draft Rota Allocation, its Availability Round and
every response to it — in one transaction. Offered at any point before allocation, including after the
round has gone out, behind a confirmation naming how many volunteers' answers
will be lost. An allocated Rotation is never Discarded; the tool for changing one
is an Alteration.
_Avoid_: delete rota, cancel rota

**Role**:
A job on a Shift — Team lead, Service volunteer, Food collector. A volunteer
holds the Roles they will do, and only a holder may be allocated to one, bar a
Preallocation, which grants the Role it names for the one Shift it names. The
job and the holding of it share one name; there is no separate notion of being
qualified for a job you do not hold. A person fills at most one Role on a
Shift, however many they hold. A Role has an identity of its own: its name is
what the roster and past rotas record, but what a Shape asks for is the Role
itself, so renaming one leaves both readable. A Role is permanent — once
created it always exists, so no reference to one can ever dangle and a past
rota always reads as it was made. A Role carries no count and no ceiling of its
own: how many of it a Shift asks for is that Shift's Shape.
_Avoid_: position, qualification, badge

**Seat**:
One place on a Shift: one Role, at most one person.

**Shape**:
Which Roles a Shift needs and how many Seats of each. Owned by the Shift and
editable until its Rotation is allocated, fixed thereafter. Its counts are what
the allocator fills up to, not minimums — Seats are routinely left empty — and
the only ceiling on how many of a Role a Shift may hold.
_Avoid_: shift size, template, structure

**Rota Defaults**:
The settings an Admin keeps for the drop-in as a whole — the Roles that exist,
the default Shape, the default shift times, the Standing Preallocations and the
Allocation Settings. They seed each new Rotation and its Shifts at definition;
nothing copies them back afterwards, so editing them changes what the next rota
starts from, never what an existing one holds.
_Avoid_: config, template, preset

**Allocation Settings**:
Which of the optional allocator rules apply, and the values they need. Live and
global rather than recorded per Rotation: an allocated rota is its Allocations,
not the settings that produced them.

**Allocation**:
The assignment of one volunteer (or custom entry) to one Role on one Shift,
produced by the allocator.

**Alteration**:
A single post-allocation change to a Shift: adding or removing one person.
Alterations are never edited or deleted; the effective state of a Shift is its
Allocations with Alterations applied in order.
_Avoid_: change, edit

**Cover**:
The audited reason for a set of Alterations — who requested the change and why.
_Avoid_: swap

**Availability Request**:
An ask issued to one volunteer covering all Shifts in one Rotation's batch,
answered on a tokenised page the server serves itself. The token is the
volunteer's identity — they never log in — and stops working once the Rotation
is allocated.

**Availability Round**:
The set of Availability Requests for one Rotation. Minting a round and notifying
the volunteers in it are separate acts: a minted request exists, with its link,
before anyone has been told about it.

**Availability Response**:
One volunteer's submission answering their Availability Request. Responses are
never edited — resubmitting appends another, and the latest before the cut-off
wins. A response with no Shift Availability means available for nothing, which
is distinct from not having answered.
_Avoid_: form response, answer (for the whole submission)

**Shift Availability**:
One Shift a single Availability Response said yes to. Only positives are
recorded, so a Shift absent from a response is a no; each response therefore
states every open Shift it accepts, never a change since last time.

**Closed**:
A Shift on a date the drop-in does not run (e.g. a holiday closure). Held on
the Shift itself and set by hand. Being Closed is an allocator input, so it is
editable only while the Rotation is unallocated.

**Preallocation**:
A person pinned to a specific Shift before Allocation runs, naming the Role
they will fill and forcing the allocator to place them (their group included).
It records a decision already taken, so it settles both questions the allocator
would otherwise ask of the roster: the pinned person is available for that
Shift whatever they answered, and holds the Role it names for that Shift alone.
It references the Role rather than naming it, so renaming one leaves every
promise made in it intact. Every Preallocation is the same kind of thing however
it came to exist, and an Admin may remove any of them. A Volunteer is pinned to
a Shift at most once; a custom entry may be pinned to one more than once, since
it is usually an organisation and an organisation may send several people.
_Avoid_: pin (except as the informal verb, "pin to a Shift")

**Standing Preallocation**:
A Preallocation an Admin expects to make every rota, kept in Rota Defaults and
used to seed real ones when a Rotation is defined. It is a convenience at
definition, not a standing fact: once seeded, the Preallocations it made are
ordinary and outlive any later change to it.

**Admin**:
A trusted person authorised to manage the rota and volunteer data, identified
by the email of their Google account against an explicit allowlist. Being an
Admin is a live fact about the allowlist, not a property of a credential. All
other visitors are anonymous; there are no other authenticated roles.
_Avoid_: user, staff

**Draft Rota Allocation**:
A speculative allocation of a whole unallocated Rotation, replaced entire each
time it is solved and shown only to Admins. Named for the rota because that is
its scope: it is made of draft Allocations, but it is never partial, and no
single one of them means anything on its own. It is **dirty** when an allocator
input has moved under the Rotation since it was solved — an availability
response, a Shape, a Shift opened, closed or moved to another day, a
Preallocation, a Role, the Allocation Settings — and reading a dirty draft
solves it again, waiting for any solve already running first, so what a reader
is handed is never stale. It becomes the rota only when an Admin allocates,
which re-solves and commits only if the result still matches what they were
shown.
_Avoid_: draft allocation (that is one of its seats), speculative allocation,
provisional rota, preview, stale (a draft is dirty)
