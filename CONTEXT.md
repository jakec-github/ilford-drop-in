# Rota Wise

Scheduling system for volunteer organisations: it collects volunteer availability,
allocates volunteers to shifts, and publishes the resulting rota.

## Language

**Shift**:
A planned session of the Organisation, minted by a Rotation from the Shift
pattern, running from a start to an end given as local time in the Organisation's
own timezone. Exists independently of who is allocated to it. Its date is the date
it starts. Shifts never overlap, but several may start on one date, so a date
does not identify a Shift (ADR 0013). Times are descriptive, not an allocator input, so unlike a
Shape they stay editable once the Rotation is allocated.
_Avoid_: shift date (as identity), session

**Shift View**:
A read-only projection of a Shift for display: the Shift plus its effective
assignees after Alterations, closed status, and change metadata.
_Avoid_: shift (for the projection), effective shift

**Rotation**:
A batch of consecutive Shifts over which availability is requested and allocation
runs. Defining one asks for how many Shifts, and takes the next that many the
Shift pattern generates, carrying on from exactly where the previous Rotation
stopped (ADR 0013). Its span and size are derived from the Shifts it minted. Rota
is an acceptable alias.

**Rota in Flight**:
The one Rotation that has not been allocated yet. Defining a Rotation is refused
while one exists, so there is at most one and every screen can address "the
rota" without a picker. A Rotation stops being in flight by being allocated or
by being Discarded.
_Avoid_: current rota, active rota, draft rota (which is a Draft Rota Allocation)

**Discard**:
Destroying the Rota in Flight and everything hanging off it — its Shifts, their
Shapes, its Pins, its Draft Rota Allocation, its Availability Round and
every response to it — in one transaction. Offered at any point before allocation, including after the
round has gone out, behind a confirmation naming how many volunteers' answers
will be lost. An allocated Rotation is never Discarded; the tool for changing one
is an Alteration.
_Avoid_: delete rota, cancel rota

**Role**:
A job on a Shift — Team lead, Service volunteer, Food collector. A volunteer
holds the Roles they will do, and the allocator places only holders in one. A
Pin or an Alteration may name any Role, held or not (ADR 0010). The
job and the holding of it share one name; there is no separate notion of being
qualified for a job you do not hold. A person fills at most one Role on a
Shift, however many they hold. A Role has an identity of its own: everything
the app stores — a Shape, a pin, an allocation, an alteration — references the
Role itself, so renaming one is a better label for the same job and every rota,
past ones included, reads under the new name. Only the roster spells a Role by
name. A Role is permanent — once created it always exists, so no reference to
one can ever dangle. A Role carries no count and no ceiling of its
own: how many of it a Shift asks for is that Shift's Shape.
_Avoid_: position, qualification, badge

**Seat**:
One place on a Shift: one Role, at most one person.

**Shape**:
Which Roles a Shift needs and how many Seats of each. Owned by the Shift and
editable until its Rotation is allocated, fixed thereafter. Its counts are what
the allocator fills up to, not minimums — Seats are routinely left empty — and
the only ceiling on how many of a Role a Shift may hold. It is a ceiling on the
allocator alone. Pins may go past it, a Shape may be shrunk below the
pins already on its Shift, and an Alteration made after the rota is published
is not held to it (ADR 0010).
_Avoid_: shift size, template, structure

**Rota Defaults**:
The settings an Organiser keeps for the Organisation as a whole — the Roles that exist,
the Shift pattern, the default Shape, the Overrides and the Allocation Settings. They seed each new Rotation and its Shifts at definition;
nothing copies them back afterwards, so editing them changes what the next rota
starts from, never what an existing one holds.
_Avoid_: config, template, preset

**Shift pattern**:
When the Organisation's Shifts start, as a list of entries. Each entry is a
VEVENT with its own DTSTART (the first Shift), DURATION (how long each runs)
and RRULE (how it repeats) — "every Sunday at 19:30 for 3 hours, from 1 Nov
2026". Read in local wall-clock time in the Organisation's time zone, a setting
of its own; a Shift that runs past midnight belongs to the day it starts. Every
Shift comes from it: there are no one-off Shifts (ADR 0013, ADR 0014).
_Avoid_: schedule, cadence, recurrence (alone)

**Start from**:
The date an entry of the Shift pattern or an Override counts from — its own
DTSTART, where "every other week" starts. Nothing occurs before it. An entry's
is a time as well as a date, and must be a start its RRULE generates; an
Override's is a date (ADR 0014).
_Avoid_: anchor

**Override**:
A rule in Rota Defaults — a `DTSTART;VALUE=DATE:` line and an RRULE — that
picks out some of the Shifts the pattern generates and changes them as a rota
is defined: their start time within the day, their length, their Shape, and
Pins. One that states a time matches the Shift generated at that time; one that
does not matches every Shift starting on the date. It matches the generated
start, never a moved one. Overrides are a list, and a later one wins. Like the
rest of Rota Defaults it seeds and never reaches back, and like a Pin it is a
person's decision, not held to the allocator's rules (ADR 0013, ADR 0014).
_Avoid_: standing preallocation (retired), exception, rule (alone)

**Allocation Settings**:
Everything an Organiser decides about how the allocator solves — its Allocation
Rules today, and in time how it weighs its preferences. Live and global rather
than recorded per Rotation: an allocated rota is its Allocations, not the
settings that produced them.

**Rule Library**:
The fixed catalogue of Rule Kinds an Organiser can add Allocation Rules from.
What a kind means is the app's; whether and how often it applies is the
Organiser's. The fundamental constraints — one Seat per person per Shift, groups
working together, availability, Seat capacity, Closed Shifts, Pins —
are not in it: they are what makes a rota a rota, and are never added. Every
kind in it can be satisfied by leaving Seats empty, so no set of Allocation
Rules can stop a rota being allocated.

**Rule Kind**:
One entry in the Rule Library, such as "cap how often somebody works", and the
values it asks for. Each is either a Shift Rule kind or a Volunteer Rule kind.

**Allocation Rule**:
One Rule Kind added by an Organiser, with its own values (and, for a Volunteer
Rule, its own Scope). A kind may be added any number of times. Every Allocation
Rule applies; none overrides another, so a volunteer covered by two caps is held
to both, and loosening a rule for one person means taking them out of its Scope
and giving them a rule of their own. May carry a note saying why it was added,
which the allocator never reads. Binds the allocator alone (ADR 0010).
_Avoid_: constraint, toggle, setting (for one rule)

**Shift Rule**:
An Allocation Rule about what each Shift must contain, such as requiring a male
on it. Applies to every Shift.

**Volunteer Rule**:
An Allocation Rule about what any one volunteer may be given, such as no
back-to-back Shifts. Applies to the volunteers in its Scope. It is about those
people, but since a group works together, it binds the rest of their group in
effect.

**Scope**:
Which volunteers a Volunteer Rule covers, as two lists: volunteers to include
and volunteers to exclude. At most one of them names anybody. With both empty
the rule covers everyone; with an include list, only the volunteers on it; with
an exclude list, everyone except them. A rule is added covering everyone and
narrowed later, so an Organiser never decides up front what kind of Scope it
has. Any volunteer on the roster may be named, active or not. A named volunteer
who has left the roster stays named, shown as no longer on the roster, and is
covered by nothing until the name is removed or they return, so an include list
naming only such volunteers covers nobody, and is allowed.

**Allocation**:
The assignment of one volunteer (or custom entry) to one Role on one Shift,
produced by the allocator.

**Alteration**:
A single post-allocation change to a Shift: adding or removing one person. One
that adds somebody names the Role of the Seat they take, whether they are on the
roster or not. One that removes a volunteer names none: a volunteer holds at
most one Seat on a Shift, so there is no question which they leave. One that
removes a custom entry names the Role of the Seat it leaves, since the entry
may hold several, and takes exactly that one Seat off. A custom entry's removal
recorded before this rule names no Role and took every copy of the entry off,
and it is still read that way. Alterations are never edited or deleted; the effective state of a Shift is its
Allocations with Alterations applied in order. An Alteration records what
happened on the day rather than instructing a solve, so the allocator's rules
do not bind it: it may place someone in a Role the roster does not record them
as holding, or take a Shift past the Seats its Shape asks for (ADR 0009).
_Avoid_: change, edit

**Cover**:
The audit record for a set of Alterations — who made the change and, where they
gave one, why. Only a removal that leaves nobody in the person's place has to
give a reason; every other change names who fills the gap, so there is nothing
left to explain.
_Avoid_: swap

**Availability Request**:
An ask issued to one volunteer covering all Shifts in one Rotation's batch,
answered on a tokenised page the server serves itself. The token is the
volunteer's identity — they never log in — and stops working once the Rotation
is allocated.

**Availability Round**:
The set of Availability Requests for one Rotation. A Rotation is given its round
as it is defined, so every rota has one from the moment it exists; minting again
tops it up for volunteers who have joined the roster since. Minting a round and
notifying the volunteers in it are separate acts: a minted request exists, with
its link, before anyone has been told about it.

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
A Shift on a date the Organisation does not run (e.g. a holiday closure). Held on
the Shift itself and set by hand; no Override closes a Shift. Being Closed is an allocator input, so it is
editable only while the Rotation is unallocated.

**Pin**:
A person pinned to a specific Shift before Allocation runs, naming the Role
they will fill and forcing the allocator to place them. It pins that one
person: their group joins them only where the allocator could have placed them
anyway. It records a decision already taken, so it is not held to the
allocator's rules. The pinned person may be unavailable, inactive, not a holder
of the Role, or past the Shape's Seats, and the solve works around them
(ADR 0010). A Closed Shift cannot be pinned to.
It references the Role rather than naming it, so renaming one leaves every
promise made in it intact. Every Pin is the same kind of thing whether made by
hand or by an Override, and an Organiser or a Rota Editor may remove any of them. A Volunteer is pinned to
a Shift at most once; a custom entry may be pinned to one more than once, since
it is usually an organisation and an organisation may send several people.
_Avoid_: preallocation

**Organisation**:
One volunteer organisation that runs its rota on the app: its own Roles, Rota Defaults,
Rotations, roster and allowlists, sharing nothing with any other. Listed in the
deployment's config, served under `/org/<id>/`, and never linked to or listed
anywhere else, so its address is the only way in (ADR 0011). Being an Organiser
or a Rota Editor is always of one Organisation.
_Avoid_: tenant, site, org (alone, outside URLs)

**Organiser**:
A trusted person authorised to make every edit the app offers — the Rota
Defaults, defining and discarding a Rotation, its Availability Round, the Draft
Rota Allocation and allocating it, and everything a Rota Editor can do.
Identified by the email of their Google account against an explicit allowlist.
Being an Organiser is a live fact about the allowlist, not a property of a
credential. Someone on both allowlists is an Organiser.
_Avoid_: admin (reserved for a possible future level), user, staff

**Rota Editor**:
A trusted person authorised to change who works a Shift and nothing else:
Alterations and Cover on an allocated Rotation, and Pins on the Rota
in Flight. What a Shift is — when it runs, whether it is Closed, its Shape — is
an Organiser's, because those are allocator inputs rather than who is on the
rota; so is every Organiser screen, the Draft Rota Allocation included.
Within the Alterations that are theirs they are deliberately unconstrained —
one person on a Shift at most once is the whole rule, and the Shape and the
roster are advice. Their Pins are the same: a Pin is not held to the
allocator's rules either (ADR 0010).
Identified and re-checked like an Organiser, against an allowlist of its own.
All other visitors are anonymous; there are no other authenticated levels.
_Avoid_: admin, editor (alone), user, staff

**Draft Rota Allocation**:
A speculative allocation of a whole unallocated Rotation, replaced entire each
time it is solved and shown only to Organisers. Named for the rota because that is
its scope: it is made of draft Allocations, but it is never partial, and no
single one of them means anything on its own. It is **dirty** when an allocator
input has moved under the Rotation since it was solved — an availability
response, a Shape, a Shift opened, closed or moved, a
Pin, a Role, the Allocation Settings — and reading a dirty draft
solves it again, waiting for any solve already running first, so what a reader
is handed is never stale. It becomes the rota only when an Organiser allocates,
which re-solves and commits only if the result still matches what they were
shown.
_Avoid_: draft allocation (that is one of its seats), speculative allocation,
provisional rota, preview, stale (a draft is dirty)
