package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/pkg/core/shiftpattern"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// weeklySunday is every Sunday at 19:30 for two hours, the first on Sunday 2
// August 2026.
const weeklySunday = "BEGIN:VEVENT\nDTSTART:20260802T193000\nDURATION:PT2H\nRRULE:FREQ=WEEKLY;BYDAY=SU\nEND:VEVENT"

// patternEntry writes one VEVENT entry of a pattern.
func patternEntry(dtstart, duration, rrule string) string {
	return "BEGIN:VEVENT\nDTSTART:" + dtstart + "\nDURATION:" + duration + "\nRRULE:" + rrule + "\nEND:VEVENT"
}

func londonStore() *stubRotaDefaultsStore {
	return &stubRotaDefaultsStore{defaults: db.RotaDefaults{ShiftTimezone: "Europe/London"}}
}

// The pattern is stored as written, and the zone it was read in is left as it
// was.
func TestSaveShiftPattern(t *testing.T) {
	store := londonStore()

	saved, err := SaveShiftPattern(context.Background(), store, "  "+weeklySunday+"\n", zap.NewNop())
	require.NoError(t, err)

	assert.Equal(t, []string{weeklySunday}, store.saved)
	assert.Empty(t, store.savedZones)
	assert.True(t, saved.HasShiftPattern())
	assert.Equal(t, "Europe/London", saved.ShiftTimezone)
}

// A pattern is read in the Organisation's zone, so a TZID naming that zone is
// accepted and one naming another is not.
func TestSaveShiftPatternReadsTheOrganisationsZone(t *testing.T) {
	inLondon := "BEGIN:VEVENT\nDTSTART;TZID=Europe/London:20260802T193000\nDURATION:PT2H\nRRULE:FREQ=WEEKLY\nEND:VEVENT"

	_, err := SaveShiftPattern(context.Background(), londonStore(), inLondon, zap.NewNop())
	require.NoError(t, err)

	store := &stubRotaDefaultsStore{defaults: db.RotaDefaults{ShiftTimezone: "America/New_York"}}
	_, err = SaveShiftPattern(context.Background(), store, inLondon, zap.NewNop())
	require.ErrorIs(t, err, ErrInvalidInput)
	assert.Contains(t, err.Error(), "America/New_York")
	assert.Empty(t, store.saved)
}

// Every way of getting the pattern wrong is an ordinary mistake with a
// message, not a failure: an Organiser is typing into a form.
func TestSaveShiftPatternRefusesBadInput(t *testing.T) {
	cases := map[string]string{
		"no pattern":     "  ",
		"no length":      "BEGIN:VEVENT\nDTSTART:20260802T193000\nRRULE:FREQ=WEEKLY\nEND:VEVENT",
		"over a day":     patternEntry("20260802T193000", "PT25H", "FREQ=WEEKLY"),
		"a bare RRULE":   "RRULE:FREQ=WEEKLY;BYDAY=SU;BYHOUR=19;BYMINUTE=30",
		"the same start": weeklySunday + "\n" + patternEntry("20260802T193000", "PT1H", "FREQ=DAILY"),
	}

	for name, pattern := range cases {
		t.Run(name, func(t *testing.T) {
			store := londonStore()
			_, err := SaveShiftPattern(context.Background(), store, pattern, zap.NewNop())
			require.ErrorIs(t, err, ErrInvalidInput)
			assert.Empty(t, store.saved)
		})
	}
}

// A refused pattern says which entry is wrong and why, so the message is one
// an Organiser can act on.
func TestSaveShiftPatternNamesTheBadEntry(t *testing.T) {
	pattern := weeklySunday + "\n" + patternEntry("20260802T100000", "PT1H", "FREQ=HOURLY")

	_, err := SaveShiftPattern(context.Background(), londonStore(), pattern, zap.NewNop())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "entry 2: FREQ=HOURLY is refused")
}

// A write the database refuses is a failure rather than an Organiser's
// mistake, and stays one all the way out — the API answers 500, not 400.
func TestSaveShiftPatternSurfacesAWriteFailure(t *testing.T) {
	store := londonStore()
	store.writeErr = errors.New("connection refused")

	_, err := SaveShiftPattern(context.Background(), store, weeklySunday, zap.NewNop())
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrInvalidInput)
}

func TestSaveTimezone(t *testing.T) {
	store := &stubRotaDefaultsStore{defaults: db.RotaDefaults{ShiftPattern: weeklySunday}}

	saved, err := SaveTimezone(context.Background(), store, " America/New_York ", zap.NewNop())
	require.NoError(t, err)

	assert.Equal(t, []string{"America/New_York"}, store.savedZones)
	assert.Equal(t, "America/New_York", saved.ShiftTimezone)
	assert.Equal(t, weeklySunday, saved.ShiftPattern, "the pattern is left alone")
}

// A zone the server cannot load is refused, and so is "Local": it means
// wherever the server runs, which is not where anybody volunteers.
func TestSaveTimezoneRefusesAZoneItCannotLoad(t *testing.T) {
	for _, zone := range []string{"", "  ", "Local", "Not/AZone", "Europe/Londn"} {
		t.Run(zone, func(t *testing.T) {
			store := &stubRotaDefaultsStore{}
			_, err := SaveTimezone(context.Background(), store, zone, zap.NewNop())
			require.ErrorIs(t, err, ErrInvalidInput)
			assert.Empty(t, store.savedZones)
		})
	}
}

func TestPreviewShiftPattern(t *testing.T) {
	pattern := patternEntry("20261101T193000", "PT3H", "FREQ=WEEKLY;BYDAY=SU") + "\n" +
		patternEntry("20261107T100000", "PT1H30M", "FREQ=MONTHLY;BYDAY=1SA")

	preview, err := PreviewShiftPattern(context.Background(), londonStore(), ShiftPatternPreviewParams{
		Pattern: pattern,
		From:    "2026-11-01",
		To:      "2026-11-15",
	})
	require.NoError(t, err)

	require.Len(t, preview.Reading.Entries, 2)
	assert.Equal(t, "Every Sunday at 19:30 for 3 hours, from 1 Nov 2026", preview.Reading.Entries[0].Description)
	assert.Equal(t, "Every month on the first Saturday at 10:00 for 1 hour 30 minutes, from 7 Nov 2026", preview.Reading.Entries[1].Description)
	assert.Equal(t, "2026-11-01", preview.StartsFrom)
	assert.Equal(t, []shiftpattern.Start{
		{At: time.Date(2026, 11, 1, 19, 30, 0, 0, time.UTC), Length: 3 * time.Hour},
		{At: time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC), Length: 90 * time.Minute},
		{At: time.Date(2026, 11, 8, 19, 30, 0, 0, time.UTC), Length: 3 * time.Hour},
	}, preview.Starts)
}

// A pattern with a problem is answered with it, entry by entry, rather than
// refused: it is a pattern still being written. It generates nothing, because
// it could not be saved.
func TestPreviewShiftPatternAnswersProblemsByEntry(t *testing.T) {
	pattern := weeklySunday + "\nBEGIN:VEVENT\nDTSTART:20261101T100000\nDURATION:PT1H\nRRULE:FREQ=DAILY\nRDATE:20261101T100000\nEND:VEVENT"

	preview, err := PreviewShiftPattern(context.Background(), londonStore(), ShiftPatternPreviewParams{
		Pattern: pattern,
		From:    "2026-11-01",
		To:      "2026-12-01",
	})
	require.NoError(t, err)

	require.Len(t, preview.Reading.Entries, 2)
	assert.Empty(t, preview.Reading.Entries[0].Problems)
	assert.NotEmpty(t, preview.Reading.Entries[1].Problems)
	assert.Empty(t, preview.Starts)
	assert.Empty(t, preview.StartsFrom)
}

func TestPreviewShiftPatternRefusesARangeItCannotRead(t *testing.T) {
	cases := map[string][2]string{
		"not a date":    {"2026-11-01", "soon"},
		"backwards":     {"2026-12-01", "2026-11-01"},
		"over a year":   {"2026-01-01", "2027-06-01"},
		"an empty span": {"2026-11-01", "2026-11-01"},
	}
	for name, span := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := PreviewShiftPattern(context.Background(), londonStore(), ShiftPatternPreviewParams{Pattern: weeklySunday, From: span[0], To: span[1]})
			require.ErrorIs(t, err, ErrInvalidInput)
		})
	}
}
