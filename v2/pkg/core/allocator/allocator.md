# Allocator

This problem is a form of optimisation problem. Finding an optimal solution is very difficult.
Instead of aiming to provide a perfect solution this algorithm aims to make sensible decisions that
solves suitably for most cases. Solvable edge cases where a solution isn't found will be rare if
ever encountered. In these cases manual intervention will be the best solution.

The requirements will likely change in the future so extensibility and maintainability are key.

## Inputs

- A set of shift dates to fill
- A set of custom overrides to apply to dates matching rrules
  - Pre-allocated volunteers in shifts (Denoted by string IDs, not necessarily referencing a volunteer)
  - Custom shift sizes
- A list of volunteers with their availability
- A list of previously filled shifts (historical data)
- Max volunteer allocation frequency

## Core Data Structures

### RotaState

Represents the current state during allocation:

- `Shifts` - Array of shifts being filled
- `VolunteerState` - Manages volunteer groups and exhaustion tracking (see VolunteerState below)
- `HistoricalShifts` - Previous rota data (read-only, for pattern analysis and fairness)
- `MaxAllocationFrequency` - Frequency ratio (e.g., 0.5 = 50%, 0.33 = 33%). The max allocation count is: `floor(len(Shifts) * MaxAllocationFrequency)`

### VolunteerState

Manages volunteer groups and tracks which are exhausted:

- `VolunteerGroups` - Array of volunteer groups available for allocation
- `ExhaustedVolunteerGroups` - Map tracking which groups are fully allocated (O(1) lookup)

### VolunteerGroup

Groups of volunteers that are allocated together (e.g., couples, families):

- `GroupKey` - Identifies the group (empty for individuals)
- `Members` - Array of Volunteer objects
- `AvailableShiftIndices` - Shifts this group is available for
- `AllocatedShiftIndices` - Shifts this group has been allocated to
- `HistoricalAllocationCount` - Historical allocations for fairness
- `HasTeamLead` - Whether any member is a team lead
- `MaleCount` - Number of male volunteers in the group

### Shift

Represents a single shift to be filled:

- `Date` - Shift date
- `Index` - Position in shifts array
- `Size` - Target number of volunteers (excludes team lead)
- `AllocatedGroups` - Groups assigned to this shift
- `PreAllocatedVolunteers` - String IDs of manually pre-assigned volunteers (count toward Size)
- `TeamLead` - Pointer to team lead volunteer (separate from Size, can be nil)
- `MaleCount` - Number of males in AllocatedGroups (excludes TeamLead and pre-allocated)
- `AvailableGroups` - Pointers to groups that expressed availability for this shift

**Important:** Team leads and pre-allocated volunteers have different treatment:

- Team leads are stored separately in `TeamLead` field and do NOT count toward `Size`
- Pre-allocated volunteers are string IDs that DO count toward `Size` but don't affect `TeamLead` or `MaleCount` metadata

## Algorithm

### Init Phase

1. **Define Criteria** - Configure which allocation criteria to apply with weights

2. **Group Volunteers** - Create VolunteerGroups

   - No group can have more than 1 team lead
   - Calculate `HasTeamLead` and `MaleCount` for each group
   - Discard invalid groups (no availability)

3. **Initialize Shifts**

   - Set `TeamLead` to nil
   - Populate `PreAllocatedVolunteers` from overrides
   - Set `Size` from defaults or overrides
   - Populate `AvailableGroups` - pointers to groups that are available for each shift
   - Initialize `MaleCount` to 0

4. **Rank Volunteer Groups** - Sort by priority (see Ranking section)

### Main Loop

1. Pop the first VolunteerGroup from the ranked list
2. Calculate shift affinity for all available shifts (see Affinity section)
3. Allocate group to the shift with highest affinity
4. Update shift state:
   - Add group to `AllocatedGroups`
   - If group has team lead, set `TeamLead` field
   - Update `MaleCount`
5. Re-rank and re-insert the group, or mark as exhausted if:
   - Allocated to all available shifts
   - Reached `MaxAllocationFrequency`
   - No valid shifts remaining

Repeat until:

- All shifts are full
- OR all volunteer groups exhausted
- OR no further allocations possible without breaching constraints

### Outcome

The allocator provides an outcome report including:

- The completed rota
- Boolean indicating success (all shifts filled)
- List of unfilled shifts
- List of volunteer groups with remaining availability that weren't fully allocated

## Criteria System

The allocator is extensible via a criteria interface. Each criterion implements three methods:

1. **PromoteVolunteerGroup(state, group)** → float64

   - Returns value between -1 and 1
   - Multiplied by `GroupWeight`
   - Used in ranking to prioritize certain groups

2. **IsShiftValid(state, group, shift)** → bool

   - Returns false to block allocation
   - Hard constraint enforcement

3. **CalculateShiftAffinity(state, group, shift)** → float64
   - Returns value between 0 and 1
   - Multiplied by `AffinityWeight`
   - Used to select best shift for a group

### Implemented Criteria

#### ShiftSize

**Purpose:** Prevents overfilling and optimizes for unpopular shifts

**Validity:**

- Returns false if allocating would exceed shift capacity
- Only counts ordinary volunteers (team leads don't count toward Size)

**Affinity:**

- Formula: `remainingCapacity / remainingAvailableVolunteers`
- Higher when shift has more capacity relative to available volunteers
- Uses `Shift.RemainingAvailableVolunteers()` which counts actual volunteers and excludes:
  - Exhausted groups
  - Already allocated groups
  - Groups too large to fit

**Promotion:** None

#### TeamLead

**Purpose:** Ensures proper team lead distribution, prevents multiple team leads per shift

**Validity:**

- Returns false if shift already has a team lead and this group contains one
- Each shift gets at most one team lead

**Affinity:**

- Formula: `1.0 / remainingTeamLeads`
- Higher for shifts with fewer available team leads (unpopular for team leads)
- Uses `Shift.RemainingAvailableTeamLeads()` which counts groups with team leads
- Returns 0 if group has no team lead or shift already has one

**Promotion:**

- Returns 1.0 for groups with team leads
- Ensures team leads are allocated early

#### MaleBalance

**Purpose:** Ensures each shift has at least one male volunteer

**Validity:**

- Groups with males are always valid
- Shifts with males already are valid
- Returns false only if allocating this group (with no males) would fill the shift completely with no males
- Special handling: groups with only a team lead (no ordinary volunteers) won't fill the shift, so they're valid

**Affinity:**

- Formula: `need / remainingMaleVolunteers`
- `need` = 1.0 if shift has no males, decreases by 0.5 per male (min 0.1)
- Uses `Shift.RemainingAvailableMaleVolunteers()` which counts male volunteers from available groups
- Returns 0 if group has no males

**Promotion:**

- Returns 1.0 for groups with males
- Helps ensure early allocation for good distribution

#### NoDoubleShifts

**Purpose:** Prevents back-to-back shift allocations, optimizes for preserving future flexibility

**Validity:**

- Returns false if shift is immediately adjacent (index ± 1) to an already allocated shift
- **Considers historical data:** If shift 0 and group was in last historical shift, returns false
- Prevents double shifts across rota boundaries

**Affinity:**

- Formula: `remainingValidShifts / currentlyValidShifts`
- Calculates how many valid shift options would remain after this allocation
- Higher for shifts that preserve more options (edge shifts better than middle)
- **Historical integration:** When checking if shift 0 is valid, considers if group was in last historical shift
- Returns 0 if no currently valid shifts

**Promotion:** None

#### ShiftSpread

**Purpose:** Optimizes for distributing allocations evenly over time

**Validity:** Always valid (no constraints)

**Affinity:**

- Calculates minimum distance to any previously allocated shift
- Formula: `minDistance / maxDistance`
- Higher for shifts further from previous allocations

**Historical integration:**

- If no current allocations, uses distance from last historical allocation
- If has current allocations, considers both current and historical distances
- Uses `getLastHistoricalIndex()` to find most recent historical allocation
- `maxDistance` calculation spans from last historical allocation to last shift in new rota

**Promotion:** None

## Volunteer Group Ranking

Groups are ranked by summing:

1. **Current Rota Urgency** - `remainingNeededThisRota / remainingAvailability`
   - Prioritizes groups that need more allocations relative to their available shifts
   - Minimum value of 1.0 (groups that have met their quota still get base score)
   - Multiplied by `WeightCurrentRotaUrgency`

2. **Overall Frequency Fairness** - Based on `DesiredRemainingAllocations()`
   - Formula: `desiredRemaining / totalShiftsInCurrentRota`
   - Groups under their target frequency over time get higher priority
   - Clamped to range [-1.0, 1.0]
   - Multiplied by `WeightOverallFrequencyFairness`

3. **Group Promotion** - Promote groups over individuals
   - Groups with more than 1 member get +1.0 bonus
   - Schedule groups early to ensure space availability
   - Multiplied by `WeightPromoteGroup`

4. **Criterion Promotions** - Sum of all `PromoteVolunteerGroup()` results
   - Each criterion can return -1.0 to 1.0
   - Weighted by criterion's `GroupWeight()`

Higher scoring groups are allocated first.

## Allocation Functions

### IsShiftValidForGroup(state, group, shift, criteria)

Checks if a volunteer group can be allocated to a shift:

1. Return false if group not available for shift
2. Return false if group already allocated to shift
3. Run all `IsShiftValid()` checks - return false if any fail
4. Otherwise return true

### CalculateShiftAffinity(state, group, shift, criteria)

Computes the affinity score for a group-shift pairing:

1. Check validity using `IsShiftValidForGroup()` - return 0 if invalid
2. Sum all `CalculateShiftAffinity()` results (weighted)
3. Return total affinity

The shift with highest affinity is selected for allocation.

## Helper Methods

### Shift Methods

- `IsFull()` - Checks if shift reached target size
- `CurrentSize()` - Returns current volunteer count
- `RemainingAvailableVolunteers(state)` - Counts ordinary volunteers from available, non-exhausted, non-allocated groups that fit
- `RemainingAvailableTeamLeads(state)` - Counts groups with team leads that are available, non-exhausted, non-allocated
- `RemainingAvailableMaleVolunteers(state)` - Counts male volunteers from available, non-exhausted, non-allocated groups that fit

### VolunteerGroup Methods

- `IsAvailable(shiftIndex)` - Check availability for a shift
- `IsAllocated(shiftIndex)` - Check if already allocated to a shift
- `TotalAllocationCount()` - Historical + current allocations
- `RemainingCapacity(maxFrequency)` - How many more allocations possible
- `DesiredRemainingAllocations(totalHistorical, totalCurrent, targetFreq)` - Calculate ideal remaining allocations for fairness

## Key Design Decisions

1. **Pointer-based References** - Groups referenced by pointers for type safety and simplicity
2. **Volunteer State Management** - `VolunteerState` encapsulates volunteer groups and exhaustion tracking
3. **Team Leads Separate** - Team leads don't count toward shift Size, stored in separate field
4. **Pre-allocated as Strings** - Pre-allocated volunteers are just string IDs that count toward Size. The allocator will not produce valid rotas if automatically allocated volunteers are also pre-allocated.
5. **Historical Data Integration** - NoDoubleShifts and ShiftSpread criteria consider historical allocations
6. **Exhaustion Tracking** - `ExhaustedVolunteerGroups` map provides O(1) lookup to avoid re-checking invalid groups
7. **Availability Pre-computation** - `AvailableGroups` populated during init for efficiency

## Future Extensions

### Randomness

Ideally there would be a pseudorandom element based on a provided seed. This would allow
users to regenerate rotas if unhappy with the first result. Possibly a "temperature" style
approach where there's a chance of swapping ranking or affinity in close calls.

### Other Potential Features

- Check if a volunteer was pre-allocated during init
- NoDoubleShifts within calendar month (popular demand but probably not satisfiable)
