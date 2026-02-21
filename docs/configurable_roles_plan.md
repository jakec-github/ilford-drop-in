# Research: Making roles configurable

## Context

Currently only two roles exist, hardcoded as constants: `RoleTeamLead` ("Team lead") and `RoleVolunteer` ("Service volunteer"). The team lead role has special behaviour baked deeply into the allocator, services, CLI display, and sheets output. The goal is to understand the full scope of what would need to change to support additional configurable roles.

## Current role definition

**`pkg/core/model/models.go:3-12`** — Two hardcoded constants + `IsValid()` that only accepts these two values.

## Where roles are referenced

### 1. Data ingestion — reading volunteers from Google Sheets

| File | Lines | What it does |
|---|---|---|
| `pkg/clients/sheetsclient/volunteers.go` | 140-143 | Reads "Role" column, calls `role.IsValid()` — rejects anything that isn't "Team lead" or "Service volunteer" |

### 2. Model → Allocator conversion

| File | Lines | What it does |
|---|---|---|
| `pkg/core/services/allocateRota.go` | 340 | `IsTeamLead: vol.Role == model.RoleTeamLead` — converts enum to boolean |

### 3. Allocator layer — the deepest embedding

The allocator doesn't use the Role type at all. Instead it uses a **boolean `IsTeamLead`** on `Volunteer` and `HasTeamLead` on `VolunteerGroup`. This drives significant special-case logic:

| File | What it does |
|---|---|
| `pkg/core/allocator/types.go:112` | `Volunteer.IsTeamLead bool` field |
| `pkg/core/allocator/types.go:99` | `VolunteerGroup.HasTeamLead bool` field |
| `pkg/core/allocator/types.go:134-136` | `Shift.TeamLead *Volunteer` — dedicated pointer, separate from `AllocatedGroups` |
| `pkg/core/allocator/types.go:155` | `Shift.PreallocatedTeamLeadID string` |
| `pkg/core/allocator/types.go:160-170` | `CurrentSize()` — excludes team leads from count |
| `pkg/core/allocator/types.go:200-239` | `RemainingAvailableVolunteers()` — excludes team leads |
| `pkg/core/allocator/types.go:241-274` | `RemainingAvailableTeamLeads()` — counts only team lead groups |
| `pkg/core/allocator/types.go:276-324` | `RemainingAvailableMaleVolunteers()` — excludes team leads |
| `pkg/core/allocator/types.go:350-359` | `OrdinaryVolunteerCount()` — excludes team leads |
| `pkg/core/allocator/init.go:76,186,196` | `BuildVolunteerGroup` — sets `HasTeamLead` from members |
| `pkg/core/allocator/init.go:382-417` | Team lead preallocation — validates volunteer is team lead, sets `shift.TeamLead` |
| `pkg/core/allocator/allocator.go:187-195` | After allocating a group, extracts team lead member → sets `shift.TeamLead` |
| `pkg/core/allocator/criteria/teamlead.go` | **Entire file** — `TeamLeadCriterion`: promotes TL groups, validates 1-per-shift, calculates scarcity affinity, final validation |

### 4. Allocator output → DB allocations

| File | Lines | What it does |
|---|---|---|
| `pkg/core/services/allocateRota.go` | 354-356 | Skips team lead member from regular allocation list |
| `pkg/core/services/allocateRota.go` | 358-365 | Writes regular volunteers with `RoleVolunteer` |
| `pkg/core/services/allocateRota.go` | 371-380 | Writes team lead separately with `RoleTeamLead` |

### 5. Services layer — role-specific logic

| File | Lines | What it does |
|---|---|---|
| `pkg/core/services/changeRota.go` | 323-346 | `inferRole()` — inherits outgoing role on swap, downgrades duplicate team leads |
| `pkg/core/services/publishRota.go` | 176-181 | Routes allocations to "TeamLead" column vs "Volunteers" column |
| `pkg/core/services/viewResponses.go` | 515-522 | Counts team leads separately from volunteers, tracks `HasTeamLead` per shift |
| `pkg/core/services/utils/alterations.go` | 42-44 | Defaults empty role to `RoleVolunteer` when applying alterations |

### 6. CLI display

| File | Lines | What it does |
|---|---|---|
| `cmd/cli/commands/allocate_rota.go` | 115,134,155 | Displays team lead separately, excludes from volunteer list |
| `cmd/cli/commands/publish_rota.go` | 53 | "Team lead" table header |
| `cmd/cli/commands/view_responses.go` | 112-119 | "Team Lead" availability row with ✓/✗ |

### 7. Sheets output

| File | Lines | What it does |
|---|---|---|
| `pkg/clients/sheetsclient/rotas.go` | 14 | `PublishedRotaRow.TeamLead` — dedicated field |
| `pkg/clients/sheetsclient/rotas.go` | 103,174,179,199,289 | "Team lead" column header in published sheets |

### 8. Config

| File | Lines | What it does |
|---|---|---|
| `internal/config/config.go` | 20 | `PreallocatedTeamLeadID` in `RotaOverride` — special field for preallocating a team lead to a shift |

### 9. Database

| File | Lines | What it does |
|---|---|---|
| `pkg/db/models.go` | 26 | `Allocation.Role string` — stores role per allocation record |
| `pkg/db/models.go` | 50 | `Alteration.Role string` — stores role on "add" alterations |

### 10. Tests (extensive)

- `pkg/core/allocator/criteria/teamlead_test.go` — 15+ tests for TeamLeadCriterion
- `pkg/core/allocator/init_test.go` — group formation with team leads
- `pkg/core/allocator/e2e/allocator_test.go` — end-to-end with team lead couples
- `pkg/core/services/allocateRota_test.go` — allocation with/without team leads
- `pkg/core/services/viewResponses_test.go` — team lead availability calculations
- `pkg/core/services/changeRota_test.go` — team lead swap logic

## Key design constraints causing coupling

1. **Team leads don't count toward shift size** — shift size = only ordinary volunteers
2. **Exactly 1 team lead per shift** — validated at allocation time and in final validation
3. **Team lead is a separate pointer on `Shift`** — not just another allocated member
4. **Groups can have at most 1 team lead** — validated during group initialisation
5. **Team lead scarcity optimisation** — TeamLeadCriterion allocates TL groups first to scarce shifts
6. **Sheets output has a dedicated "Team lead" column** — not a generic role column

## What would need to change for configurable roles

### Config additions
- Define roles in config with properties: name, display label, count-per-shift requirement (e.g. "exactly 1", "at least 1", "0 or more"), whether they count toward shift size, whether they get a dedicated column in sheets output

### Model layer
- Remove hardcoded `RoleTeamLead`/`RoleVolunteer` constants (or keep as defaults)
- Make `IsValid()` check against configured roles rather than hardcoded list

### Allocator layer (heaviest changes)
- Replace `IsTeamLead bool` on `Volunteer` with a `Role string` (or `Roles []string`)
- Replace `HasTeamLead bool` on `VolunteerGroup` with role-aware metadata
- Replace `Shift.TeamLead *Volunteer` with a generic `Shift.RoleAssignments map[string]*Volunteer` or similar
- Generalise `CurrentSize()`, `RemainingAvailableVolunteers()`, etc. to be role-aware
- Generalise or replace `TeamLeadCriterion` with a configurable role criterion
- Update preallocation logic to support any role, not just team lead

### Services layer
- `convertToAllocatorVolunteers()` — map role string instead of boolean
- `convertToDBAllocations()` — use actual role from allocation instead of hardcoded constants
- `inferRole()` in changeRota — generalise deduplication logic
- `publishRota` — route allocations to columns based on role config
- `viewResponses` — track availability per configured role

### Sheets output
- Make columns dynamic based on configured roles rather than hardcoded "Team lead" + "Volunteers"

### CLI display
- Generalise display to show allocations per role

## Estimated scope

This is a **large refactor** touching ~30+ files. The allocator layer is the most complex part — the `IsTeamLead` boolean and `Shift.TeamLead` pointer are deeply woven into allocation logic, counting methods, criteria, and validation. The rest (services, CLI, sheets) would follow from the allocator changes.

A phased approach would be advisable:
1. **Phase 1**: Add role config and make model/validation role-aware
2. **Phase 2**: Refactor allocator to use role strings instead of booleans
3. **Phase 3**: Update services, CLI, and sheets output
4. **Phase 4**: Add criterion configurability (which roles need exactly-1-per-shift, etc.)
