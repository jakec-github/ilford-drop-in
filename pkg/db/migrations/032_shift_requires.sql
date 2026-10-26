-- "Require a male" becomes "All shifts require", with the gender a choice on
-- the rule (issue #281). A stored male_required rule is renamed outright and
-- given the gender it always meant, so the rule an Organiser added keeps
-- applying through the deploy. Nothing reads the old kind afterwards.
UPDATE rota_defaults
SET allocation_settings = jsonb_set(
    allocation_settings,
    '{rules}',
    (
        SELECT jsonb_agg(
            CASE WHEN rule->>'kind' = 'male_required'
                THEN rule || '{"kind": "shift_requires", "option": "Male"}'
                ELSE rule
            END
            ORDER BY position
        )
        FROM jsonb_array_elements(allocation_settings->'rules') WITH ORDINALITY AS r(rule, position)
    )
)
WHERE jsonb_typeof(allocation_settings->'rules') = 'array'
  AND jsonb_array_length(allocation_settings->'rules') > 0;
