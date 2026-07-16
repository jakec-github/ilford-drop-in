package utils

import (
	"time"

	"github.com/teambition/rrule-go"
)

// rruleSearchBufferDays widens the occurrence search window by a week on each
// side of the rota so overrides still land on shifts at the very edges of the
// rota's date range.
const rruleSearchBufferDays = 7

// ParseRRule parses a rota-override rrule string, returning an error if the
// syntax is invalid. It is the single place rrule strings are parsed, shared by
// config validation and the shift-override matchers; callers wrap the error
// with whatever context they need.
func ParseRRule(rruleStr string) (*rrule.RRule, error) {
	return rrule.StrToRRule(rruleStr)
}

// NewRRuleMatcher parses an override's rrule and returns a matcher reporting
// whether a date (formatted "2006-01-02") falls on one of the rrule's
// occurrences across the span of shiftDates, widened by a one-week buffer at
// each end. shiftDates is expected in ascending order — its first and last
// entries bound the search — and an empty slice yields a matcher that matches
// nothing.
//
// It returns an error only when the rrule cannot be parsed, leaving the
// fail-hard versus warn-and-skip decision to the caller. Any DTSTART carried in
// the rrule string is overridden so the search is pinned to the rota window.
func NewRRuleMatcher(rruleStr string, shiftDates []time.Time) (func(dateStr string) bool, error) {
	rule, err := ParseRRule(rruleStr)
	if err != nil {
		return nil, err
	}

	if len(shiftDates) == 0 {
		return func(string) bool { return false }, nil
	}

	searchStart := shiftDates[0].AddDate(0, 0, -rruleSearchBufferDays)
	searchEnd := shiftDates[len(shiftDates)-1].AddDate(0, 0, rruleSearchBufferDays)
	rule.DTStart(searchStart)

	matched := make(map[string]struct{})
	for _, occurrence := range rule.Between(searchStart, searchEnd, true) {
		matched[occurrence.Format("2006-01-02")] = struct{}{}
	}

	return func(dateStr string) bool {
		_, ok := matched[dateStr]
		return ok
	}, nil
}
