# Issue 45 (widened): shift_id as the sole identity for allocations and alterations at every seam

> Implementation plan for issue #45, scope widened by maintainer decision on
> 2026-07-16. Not yet implemented — an agent picking this up should follow the
> standard PR workflow in `CLAUDE.md`, verifying the file/line references below
> against current main first (they were accurate as of the #44 merge).

## Context

Issue #45 as written only re-keyed the *read* pipeline on `shift_id`, leaving the write path resolving shift ids from `(rota_id, date)` via SQL subselects ("the DB-seam resolution may remain"). The maintainer has widened the scope: **every** seam that sets or gets allocations/alterations must be keyed by `shift_id`, as if the system had been designed around shift entities from the start. Components that inherently think in dates — the CP-SAT allocator and the HTTP/CLI inputs — do the date→shift_id mapping on their own side, before crossing the seam.

The schema already supports this: migrations 007/008 made `shift(id, date UNIQUE, rota_id)` the authority, and `allocation`/`alteration` carry only a NOT NULL `shift_id` FK. Only Go code is still date-keyed. PR #44 (merged 2026-07-16) added `GetShiftsInRange`, `GetAllocationsByShiftIDs`, `GetAlterationsByShiftIDs` and rewired `listShifts` to enumerate from the shift table — this work builds directly on it.

A pleasant consequence of the widened scope: the read/write struct-split problem disappears. With writes also carrying `ShiftID`, `db.Allocation`/`db.Alteration` become single clean shapes with no denormalised `RotaID`/`ShiftDate` at all.

**Branch:** `issue-45-shift-id-identity`, cut from up-to-date main. Update issue #45's body (as `jakec-agent`, per `docs/agents/issue-tracker.md`) to reflect the widened scope before opening the PR.

## Target struct shapes (`pkg/db/models.go`)

```go
type Allocation struct { ID, ShiftID, Role, VolunteerID, CustomEntry string }
type Alteration struct { ID, ShiftID, Direction, VolunteerID, CustomValue, CoverID, SetTime, Role string }
```

`RotaID`/`ShiftDate` gone from both. `db.Shift` (`ID, RotaID, Date`) is the only carrier of rota/date; callers hold shifts and map `shiftID → date` where display needs it.

## Step 1 — DB layer (`pkg/db`)

- **`allocation.go`**: `GetAllocationsByShiftIDs` drops its `JOIN shift`, becoming `SELECT id, shift_id, role, volunteer_id, custom_entry FROM allocation WHERE shift_id = ANY($1)`. Refactor to an unexported `getAllocationsByShiftIDs(ctx, q querier, ...)` helper so the tx wrapper can share it (mirroring the existing `getAllocationsInRange` pattern). `scanAllocations` scans `shift_id`. **Delete** `GetAllocationsInRange`, `getAllocationsInRange`, `GetAllocationsByRotaID`. Move `shiftDateWhere` to `shift.go` (its only remaining user is `GetShiftsInRange`).
- **`alteration.go`**: same treatment — `GetAlterationsByShiftIDs` loses the join (keeps `ORDER BY set_time`), shared unexported helper, scanner scans `shift_id`. **Delete** `GetAlterationsInRange`, `getAlterationsInRange`, `GetAlterationsByRotaID`.
- **`allocation.go` `InsertAllocationsAndSetAllocated`**: keep the signature's `rotaID` param (still needed for the FOR UPDATE double-allocation guard and setting `rotation.allocated_datetime` — rota-level state, untouched). The INSERT becomes a plain 5-value insert using `a.ShiftID`; the `(SELECT id FROM shift WHERE rota_id=… AND date=…)` subselect goes. An unknown `ShiftID` now fails via the FK constraint.
- **`cover.go` `insertCoverAndAlterations`**: same — insert `a.ShiftID` directly, subselect removed.
- **`tx.go`**: `RotaChangeStore` swaps `GetAllocationsInRange`/`GetAlterationsInRange` for `GetAllocationsByShiftIDs`/`GetAlterationsByShiftIDs`, implemented on `rotaTx` via the shared helpers.

## Step 2 — `utils.ApplyAlterations` (`pkg/core/services/utils/alterations.go`)

Re-key from date to shift id: `ApplyAlterations(allocationsByShiftID map[string][]db.Allocation, alterations []db.Alteration)`, using `alt.ShiftID` as the key. The "add" branch's synthetic allocation sets `ShiftID: alt.ShiftID` (unconditionally — today's code only sets `ShiftDate` inside the volunteer/custom branches, but one is always taken, so behaviour is equivalent).

## Step 3 — Service flows

- **`listShifts.go`**: `shiftsInRange` (post-#44) already carries `ID`+`Date`+`Allocated` at the grouping point. Group allocations, `alterationCounts`, `lastChanged` by `ShiftID`; drive the output loop by iterating `shiftsInRange` (already date-ordered by the DB) instead of the separately-sorted `shiftDates` slice, looking everything up by `s.ID`. `shiftDates []time.Time` is still derived for the `isShiftClosed` rrule window. The `services.Shift` output type keeps `Date` (API contract unchanged).
- **`publishRota.go`**: replace `rotaShiftDates` + `GetAllocationsByRotaID`/`GetAlterationsByRotaID` with `GetShiftsByRotaID` → collect ids → `GetAllocationsByShiftIDs`/`GetAlterationsByShiftIDs`. Group by `ShiftID`; row-building iterates the shifts (date-ordered) using `shift.Date` for display and `isShiftClosed`. Update `PublishRotaStore`.
- **`changeRota.go`**:
  - `resolveShiftRota` → `resolveShift`, returning the whole `*db.Shift` (ID for keying, RotaID for the lock, Date for output). Resolving before the lock is safe: shifts are immutable once minted.
  - `buildEffectiveState(store, shift *db.Shift)` fetches by `GetAllocationsByShiftIDs([shift.ID])` / `GetAlterationsByShiftIDs([shift.ID])` and can now return `[]db.Allocation` directly (it was always single-date; the map indirection goes). `validateDateChanges` takes the slice.
  - `buildAlterationsForDate` takes `shiftID` instead of `rotaID`+`dateStr`; alterations carry `ShiftID`.
  - `ChangeRotaResult` gains `DatesByShiftID map[string]string` (from the one or two resolved shifts) so the API and CLI can still render dates.
- **`allocateRota.go` / `allocationHelpers.go`**:
  - `AllocateRota` fetches `GetShiftsByRotaID` once; allocator input dates come from `utils.ShiftDatesFromShifts` (exists); build `shiftIDByDate map[string]string`. The allocator internals stay date-based — this map **is** the internal mapping the user asked for.
  - `convertToDBAllocations(shiftIDByDate, allocatedShifts) ([]db.Allocation, error)` sets `ShiftID` from the map and returns a clear error for a solver-output date with no minted shift (this failure previously surfaced as the subselect tripping NOT NULL; it must stay loud, now with a better message).
  - `buildHistoricalShifts` fetches the previous rota's shifts (`GetShiftsByRotaID`), then allocations/alterations by shift ids; groups by `ShiftID`; builds `allocator.Shift{Date: dateByShiftID[id]}`.
  - `AllocateRotaStore` swaps the ByRotaID getters for the ByShiftIDs ones.
  - `rotaShiftDates` + `shiftReader` (`pkg/core/services/shifts.go`) lose their last callers — delete.

## Step 4 — API + CLI (dates stay the external language)

- `pkg/api/alterations.go`: `toAlterationResponses(alterations, datesByShiftID)` sources `shiftDate` from the result map — JSON contract unchanged. Request stays date-keyed.
- `cmd/cli/commands/change_rota.go:68`: print the date via `result.DatesByShiftID[alt.ShiftID]`.
- `allocate_rota.go` / other CLI commands: unaffected (compiler will confirm).

## Step 5 — Tests

- **Unit/mock tests**: update mock stores in `listShifts_test.go`, `publishRota_test.go`, `changeRota_test.go`, `allocationHelpers_test.go`, `api/api_test.go` to the new interfaces (delete ByRotaID/InRange mocks, add/keep ByShiftIDs); fixtures swap `RotaID`/`ShiftDate` for `ShiftID`. `utils/alterations_test.go` re-keys its maps.
- **Integration tests**: `changeRota_integration_test.go` `seedAllocatedRota` passes `ShiftID` explicitly (it already mints shifts with known ids). Add: (a) insert with an unknown `ShiftID` fails via FK violation; (b) `convertToDBAllocations` with a date missing from the shift map returns the explicit error.
- Sweep for stragglers with the compiler: `go build ./...` after the struct change finds every remaining `.ShiftDate`/`.RotaID` consumer.

## Risks / gotchas

- **Swap across two rotas**: `ChangeRota` locks both rota ids — now taken from the two resolved shifts' `RotaID`; behaviour unchanged.
- **FOR UPDATE double-allocation guard** in `InsertAllocationsAndSetAllocated` is untouched — only the per-row INSERT changes.
- **Missing-shift failure moves layers**: previously a DB NOT-NULL error from the subselect; now an explicit service-layer error in `convertToDBAllocations` (allocate) and the existing `ErrNotFound` in `resolveShift` (change-rota). Both must remain loud, covered by tests.
- `GetShiftByDate` relies on the `date UNIQUE` constraint — unchanged, still the API-side date→shift resolver.

## Verification

1. `go build ./...` and `go vet ./...`.
2. `go test ./...` — unit + integration (dbtest harness spins up the test DB).
3. End-to-end: against the dev/test DB, run the CLI flows — `define-rota`, `allocate-rota` (dry-run config), `change-rota` on an allocated date, then `GET /shifts` and publish — confirming dates in output are correct and sourced from shift entities.
4. Confirm the create-alteration HTTP response still returns the correct `shiftDate` (api test asserts it).

## PR

Push `issue-45-shift-id-identity`, open PR titled after the (updated) ticket with `Closes #45`, request review from `jakec-github`, switch back to main.
