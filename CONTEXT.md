# Ilford Drop-In Rota

Scheduling system for a weekly charity drop-in: it collects volunteer availability,
allocates volunteers to shifts, and publishes the resulting rota.

## Language

**Shift**:
A planned session of the drop-in on a specific date, minted by a Rotation. Exists
independently of who is allocated to it. At most one Shift per date.
_Avoid_: shift date (as identity), session

**Shift View**:
A read-only projection of a Shift for display: the Shift plus its effective
assignees after Alterations, closed status, and change metadata.
_Avoid_: shift (for the projection), effective shift

**Rotation**:
A batch of consecutive Shifts over which availability is requested and allocation
runs. Its span and size are derived from the Shifts it minted. Rota is an
acceptable alias.

**Role**:
A job on a Shift — Team lead, Service volunteer, Food collector. A volunteer
holds the Roles they will do, and only a holder may be allocated to one. The
job and the holding of it share one name; there is no separate notion of being
qualified for a job you do not hold. A person fills at most one Role on a
Shift, however many they hold.
_Avoid_: position, qualification, badge

**Seat**:
One place on a Shift: one Role, at most one person.

**Shape**:
Which Roles a Shift needs and how many Seats of each. Owned by the Shift and
editable until its Rotation is allocated, fixed thereafter. Its counts are what
the allocator fills up to, not minimums — Seats are routinely left empty.
_Avoid_: shift size, template, structure

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
A Shift on a date the drop-in does not run (e.g. a holiday closure). Currently
declared by configured recurrence rules, not stored on the Shift.

**Rota Override**:
A configured recurrence rule that adjusts matching Shifts: marking them Closed,
setting their Shape, or preallocating people.

**Preallocation**:
A person pinned to a specific Shift before Allocation runs, naming the Role
they will fill and forcing the allocator to place them (their group included).
Has two sources — Config and Manual — that union into one set.
_Avoid_: pin (except as the informal verb, "pin to a Shift")

**Config Preallocation**:
A Preallocation declared by a Rota Override's recurrence rule. Authoritative:
a Manual Preallocation can never remove or replace it.

**Admin**:
A trusted person authorised to manage the rota and volunteer data, identified
by the email of their Google account against an explicit allowlist. Being an
Admin is a live fact about the allowlist, not a property of a credential. All
other visitors are anonymous; there are no other authenticated roles.
_Avoid_: user, staff

**Manual Preallocation**:
A Preallocation set ad hoc on a single Shift, editable only while the Shift's
Rotation is unallocated. Add-only — it can force a person on but never suppress
a Config Preallocation.
