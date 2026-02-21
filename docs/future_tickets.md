# Future tickets

## Allocator improvements

- Proper scoring of shifts. Negative outcomes. Activation functions etc.
- Improve volunteer ranking by switching to the winner-stays-on selection method. Reduces complexity and makes the implementation more flexible. Doesn't impose a significant performance penalty as first thought.
- Distinguish between incomplete rotas and invalid rotas after allocation.
- Improve shift spread criteria where it looks at the expected distribution of shifts based on the frequency and promotes shifts on that basis
- A good example of where the individual affinities system might need some more thinking. Look at the shiftSize. in a resource constrained scenario we want all shifts to reach the average. So affinities for shifts that haven't reached that yet need to be much higher than those that have. Once all have reached that stage you still want a clear signal but if they all have tiny affinities then any differences get drowned out. I think the solution is that affinity calculations have to keep track of the general state of the rota to know whether or not to give a particular shift a particular affinity. This could be done by calculating this from the state each time the hook is run. That requires no changes to the pattern. Or they could be stateful...

## Allocator bugs

- Technically there is an edge case bug where all shifts have an affinity of 0 and the shift race interprets that as a no valid shifts scenario

## Tech debt

- Fix issues in DB audit
- Assess potential issues with concurrent users
- Deduplicate the rrule resolution logic into a util
- Further dedupe grouping logic (bit tricky as it is done in and outside the allocator)
- Give all the clients the same signature and make them fetch the token independently.

## General improvements

- Check closed shifts when requesting availability
- If I try to allocate a rota and allocations already exist for that rota it should fail
- Always use the same sheet for the latest rota so the link to latest is stable
- Trim values from the volunteer sheet of whitespace

## DB Usage Audit Results

HIGH Priority - Missing Transactions

1. AllocateRota (pkg/core/services/allocateRota.go) - InsertAllocations and SetRotationAllocatedDatetime are two separate DB calls that should be atomic. If
   the second fails, you'd have allocations without the rota being marked as allocated.
2. ChangeRota (pkg/core/services/changeRota.go) - InsertCover and InsertAlterations are separate calls. If alterations insert fails, you'd have an orphaned
   cover record.

MEDIUM Priority - Missing DB-Level Filtering

Every GetAllocations, GetAlterations, and GetAvailabilityRequests call fetches all records from the table and filters in Go code. This works now but won't
scale. Affected services:

- publishRota.go - fetches all allocations/alterations, filters by rota ID in Go
- changeRota.go - fetches all allocations/alterations, filters by rota ID in Go
- viewResponses.go - fetches all availability requests, filters in Go
- sendAvailabilityReminders.go - fetches all availability requests, filters in Go
- allocateRota.go - fetches all allocations, filters in Go

LOW Priority - N+1 Pattern

- sendAvailabilityReminders.go calls HasResponse() (which hits the Forms API) individually for each volunteer, and calls it twice for the same form IDs.
