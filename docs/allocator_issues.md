# Issues

## Hand rolled

- Requires hand tailored weights (Can't find values that work and they might not exist)
- Criteria are currently not written to take account of circumstances changing as the rota fills. Classic case: Require 5 volunteers each shift. Only have enough to guarantee 3 per shift. 4 slot needs massively downgrading. But only until all 3 slots have been filled in each shift. Then it needs to be massively upped so 4th are filled before 3rd.
- Current implementation always selects volunteer first. Maybe sometimes we want to select shift first. (Needs more thought)
- Cannot find "better" valid solutions.
- Black box problem. Difficult to understand why it made the decisions it did. ie. Why only put a given volunteer down for 2 shifts instead of 3?
- No backtracking.
- No flexibility in roles (both roles that a volunteer has (team lead) and can adopt on the day (food collection))
- Not possible to model systems where no shift matches a group but later the group becomes valid
- I think when i designed it I did not think enough about the difference between things that should be optimised in the final result and things that should be optimised at intermediate stages in order to avoid hitting constraints. This problem can be seen in the promotion and affinity hooks which are doing double duty between optimisation and heuristically avoiding constraints. Perhaps re-splitting "constraints" and "preferences" would help. I think I didn't do this first time round because there just weren't many preferences.
- Maybe there is a difference between constraint (something that can't be done) and requirement (something that must be done).
- Fair distribution between volunteers is a kind of "natural" preference which emerges mainly from the mechanism. It would be nice to make this a tunable preference.

## CP-SAT

- Groups are assigned to shifts. What role they are in is inferred from their volunteer role. This won't work with flexible roles in future.
  Instead individuals should probably be passed in and assigned. Grouping should be modelled as a constraint.
- I said that team lead should be a preference but I made a mistake. It should be a constraint that there are less than 2 and a preference for 1. (maybe covered by making as many assignments as possible)
- Without the custom warnings in the new system it is harder to check a rota. Either add them to the python or help the output make it more obvious. Also
  can't check the rota for issues after alterations. Maybe Python based checks are the way to go
- I can't actually read the constraints and preferences. Certainly couldn't write one
