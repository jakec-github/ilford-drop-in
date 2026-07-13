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
runs. Its span and size are derived from the Shifts it minted.
_Avoid_: rota (in code; fine colloquially)

**Allocation**:
The assignment of one volunteer (or custom entry) to one role on one Shift,
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
An ask sent to one volunteer covering all Shifts in one Rotation's batch,
answered via a single form.

**Closed**:
A Shift on a date the drop-in does not run (e.g. a holiday closure). Currently
declared by configured recurrence rules, not stored on the Shift.

**Rota Override**:
A configured recurrence rule that adjusts matching Shifts: marking them Closed,
resizing them, or preallocating people.
