-- Contract phase of the shift-time refactor (issue #135, ADR 0007).
--
-- shift.date is dropped. A Shift's date is the date of its start, and the rule
-- that the drop-in runs at most one session on a day becomes a unique index on
-- that derived value — one line the database enforces, with no application
-- discipline behind it, because `timestamp::date` is IMMUTABLE.
--
-- Numbered 020 rather than 018: two other settings migrations are in flight and
-- have claimed the numbers between. The ledger is keyed by filename, so the gap
-- costs nothing and renumbering an applied migration would cost a database.

-- A Shift with no start has no date once the column goes, so every one of them
-- has to be given times here. The first source is the settings, exactly as the
-- expand phase (016) did: that migration backfilled nothing on a deployment
-- whose admin had not filled the Settings screen in yet, and this is where
-- those Shifts are caught up now that they have.
UPDATE shift s
SET start_at = s.date + d.shift_start_time,
    end_at = s.date + d.shift_end_time
FROM rota_defaults d
WHERE s.start_at IS NULL
  AND d.shift_start_time IS NOT NULL
  AND d.shift_end_time IS NOT NULL;

-- Whatever is left belongs to a deployment where nobody has ever said when the
-- drop-in runs, and it gets the whole of the day it was minted on.
--
-- Failing the migration instead would be the tidier rule and the wrong one. It
-- takes the site down for a value that has an ordinary first state of "unset"
-- (ADR 0006), and it takes it down at the one moment the fix — the Settings
-- screen, or the per-Shift times this same ticket ships — is unreachable,
-- because both are served by the server that would refuse to boot. Midnight to
-- midnight is not a guess at the hours: it is the honest statement that the day
-- is known and the hours are not, and it preserves the date, which is the only
-- thing every existing reader was taking from the column anyway. An admin
-- corrects one on the rota screen.
DO $$
DECLARE
    guessed INT;
BEGIN
    UPDATE shift
    SET start_at = date::timestamp,
        end_at = date::timestamp + INTERVAL '1 day'
    WHERE start_at IS NULL;

    GET DIAGNOSTICS guessed = ROW_COUNT;
    IF guessed > 0 THEN
        RAISE NOTICE 'shift times: % shift(s) now run the whole day, because the drop-in''s shift times have never been set; set them on Admin -> Settings and correct those shifts on the rota', guessed;
    END IF;
END $$;

-- Every Shift now says when it runs, which is what makes its date derivable and
-- what retires the "both or neither" check: neither is no longer reachable.
ALTER TABLE shift ALTER COLUMN start_at SET NOT NULL;
ALTER TABLE shift ALTER COLUMN end_at SET NOT NULL;
ALTER TABLE shift DROP CONSTRAINT shift_times_set_together;

-- One Shift per date, stated on the derived value rather than a stored copy.
-- This inherits the job the UNIQUE on shift.date was doing for concurrent rota
-- definition (issue #41, hazard B1) — two rotas minting the same date still
-- cannot both commit — and takes on a second: it is what refuses an admin
-- moving a Shift onto another Shift's day.
CREATE UNIQUE INDEX shift_start_date_key ON shift ((start_at::date));

-- The last stored copy of a fact the Shift's start already states. Dropping the
-- column drops the UNIQUE constraint defined on it, which the index above has
-- just replaced.
ALTER TABLE shift DROP COLUMN date;
