# Allocator

This problem is a form of optimisation problem. Finding an optial solution is very difficult.
Instead of aiming to provide a perfect solution this algorithm aims to make sensible decisions that
solves suitably for most cases. Solvable edge cases where a solution isn't found will be rare if
ever encountered. In these cases manual intervention will be the best solution.

The requirements will likely change in the future so extensiblity and maintainability are key.

## Inputs

- A set of shift dates to fill
- A set of custom overrides to apply to dates matching rrules
  - Prefilled allocations in shifts (Denoted by a string. Not necessarily referencing a volunteer)
  - Custom shift sizes
- A list of volunteers with their availability
- A list of previously filled shifts
- Max volunteer allocation frequency

## Algorithm

### Init

Define criteria that will be applied

Group volunteers by key into VolunteerGroups

- No group can have more than 1 team lead
- Initially unassigned
- Sort by ranking. See below
- Discard invalid Volunteer Groups (gave no availability)

Initialise Shifts

- Set team lead to a null value
- Set volunteers to an empty list
- Set desired total size
- Add pre-populated volunteers

### Main loop

Pop the first VolunteerGroup off the list.

Find shift with highest affinity for the VolunteerGroup and allocate them.

Re-sort Volunteer group back into the list or do not replace for exhausted groups.

Repeat until:

- all shifts are full
- OR all volunteer groups have been maximally allocated
- OR no further allocations can be made without breaching constraints.

Report outcome

### Outcome

Once the rota is completed the allocator should provide an outcome report. It should include:

- the rota generated
- a boolean indicating whether a full rota was succssfully generated
- a list of a shifts that have not been filled
- a list of volunteer groups who have not been assigned maximally and have spare availability

### Custom criteria

We want the rota allocator to be extensible and adjustable in future. As such criteria are defined in a reasonably generic way.

The rota allocator will accept a list of criteria. Each criterion will have 3 optional hooks. These will accept all state about the rota. Criteria can be initialised with a Group weight and an Affinity weight.

VolunteerGroupPromotion - Returns number between -1 and 1 multiplied by the weight
ShiftValidity - Returns boolean. If false, shift is not allocatable
VolunteerGroupShiftAffinity - Returns number between 0 and 1 multiplied by the weight

#### ShiftSize

Prevents overfilling of shifts.
Optimises for unpopular shifts.

#### Team lead allocation.

Prevents overallocation of team lead.
Optimises for unpopular team lead allocations if group contains a team lead

#### Male allocation

Ensures each shift has at least one male.
Optmises to spread male volunteers maximally
Promotes male volunteers

#### No double shifts

Prevents allocation to a shift right next to another allocated shift
Optimises for shifts that don't reduce valid shifts for the next allocation

#### Shift spread optimisation

Optimises for shifts that are further from previous allocations.

### Volunteer group ranking

Runs all VolunteerGroupPromotion hooks on the criteria and sums the results with built in weighted checks for availability, large groups and whether or not this volunteer has met their desired shift count.

A higher scoring Volunteer group should be allocated first.

### Shift affinity calculating

Returns 0 is the Volunteer group is unavailable or has already been assigned.

Each custom criteria's ShiftValidity hook is run. If any of them return false a 0 is returned.

Each custom criteria's VolunteerGroupShiftAffinity hook is run. The results are summed and returned.

## Future extensions

### Randomness

Ideally there would be a pseudorandom chance element based on a provided seed. This would allow
users to re-allocate rotas if they are unhappy with the first result. Possibly a "temperature" style approach where there is a chance of swapping ranking or affinity in close calls.

### Other

- Check if a volunteer was pre-allocated on init
- NoDoubleShifts in calendar month. Popular demand but not feasible right now
