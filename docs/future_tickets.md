# Future tickets

Include availability cut off date on rotas so that we know which form responses to ignore in future. (Possibly just close forms)

Proper scoring of shifts. Negative outcomes. Activation functions etc.

Improve volunteer ranking by switching to the winner-stays-on selection method. Reduces complexity and makes the implementation more flexible. Doesn't impose a significant performance penalty as first thought.

Distinguish between incomplete rotas and invalid rotas after allocation.

SkipEmail should have name changed and should not set form_sent to true

Running help command should not init the full app (log in etc.)

Deduplicate the rrule resolution logic into a util

Improve shift spread criteria where it looks at the expected distriubtion of shifts based on the frequency and promotes shifts on that basis

Further dedupe grouping logic (bit tricky as it is done in and outside the allocator)

Give all the clients the same signature and make them fetch the token independently.

Check closed shifts when requesting availability

If I try to allocate a rota and allocations already exist for that rota it should fail

Always use the same sheet for the latest rota so the link to latest is stable

Highlight custom entries with square brackets or something

Covers to add:

- Plus Angela Beckles on 2025-11-16
- swap Ruth on dec 14th for Jenny on dec 21st
- Plus Partha Mulay on 2025-11-30
- Plus Abena Assibey on 2025-12-21
- Swap Burhan with Kanom on 2025-11-30 (Recheal suggested she might do this)
- Swap Burhan on 4th Jan with Kate B on 18th of Jan

Quick rewrite of the alterations/covers plan.

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
