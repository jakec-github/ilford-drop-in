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

- Remove all references to sheets SQl. Waiting to be happy with the new DB.
- Add tests for --no-email flag
- Deduplicate the rrule resolution logic into a util
- Further dedupe grouping logic (bit tricky as it is done in and outside the allocator)
- Give all the clients the same signature and make them fetch the token independently.

## General improvements

- Check closed shifts when requesting availability
- If I try to allocate a rota and allocations already exist for that rota it should fail
- Always use the same sheet for the latest rota so the link to latest is stable
- Trim values from the volunteer sheet of whitespace

## Alterations/covers plan

alterations schema

- id
- shift_date
- rota_id (maybe)
- type add/remove
- volunteer_id
- custom_value
- cover_id (maybe)
- sequence_number (Required to make sure they are applicable in order. How to increment? Maybe datetime is better)

covers schema

- id
- datetime
- reason
- user_id

join_table (probably not needed)

- cover_id
- alteration_id

API (might need in/out customs)

- date
- in
- out
- swap_date
- reason

Do we warn on rota invalidation?
