# Legacy tickets

> **This file is no longer the issue tracker.** Work is tracked as GitHub issues
> (see `docs/agents/issue-tracker.md`). The migrated items became issues #4–#13
> on 2026-07-13 (the shift table idea became #3 earlier); the items below were
> deliberately left unmigrated. Don't add new entries here.

CHANGE ROTA NEEDS MANUAL TESTING

## Allocator improvements

- Proper scoring of shifts. Negative outcomes. Activation functions etc.
- Improve shift spread criteria where it looks at the expected distribution of shifts based on the frequency and promotes shifts on that basis
- A good example of where the individual affinities system might need some more thinking. Look at the shiftSize. in a resource constrained scenario we want all shifts to reach the average. So affinities for shifts that haven't reached that yet need to be much higher than those that have. Once all have reached that stage you still want a clear signal but if they all have tiny affinities then any differences get drowned out. I think the solution is that affinity calculations have to keep track of the general state of the rota to know whether or not to give a particular shift a particular affinity. This could be done by calculating this from the state each time the hook is run. That requires no changes to the pattern. Or they could be stateful...
- Could we further improve things by adding a hook to criteria that allows the weight to be updated each iteration. ie. The criteria calculates its own urgency.
- Provide transparency. Current implementation is a bit of a black box. Hard to determine why some are less assigned than others.

## Allocator bugs

- Technically there is an edge case bug where all shifts have an affinity of 0 and the shift race interprets that as a no valid shifts scenario

## Tech debt

- Further dedupe grouping logic (bit tricky as it is done in and outside the allocator)
- Give all the clients the same signature and make them fetch the token independently.
