package dbtest

import (
	"github.com/google/uuid"

	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// EveningStart and EveningEnd are the times of day Shift mints a session at.
// Any pair would do — nothing under test depends on these hours — but they are
// the drop-in's own, so a fixture reads like a session somebody would run.
const (
	EveningStart = "T19:30:00"
	EveningEnd   = "T21:30:00"
)

// Shift builds a Shift on a date, running the evening of it.
//
// A Shift's date is the date of its start (ADR 0007), so a test cannot mint one
// by naming a date alone: it says when the session runs and the date follows.
// That is the whole point of the contract phase, and it would make every
// fixture in the suite spell out two timestamps to say "the second of August".
// This says it once.
func Shift(rotaID, date string) db.Shift {
	return db.Shift{
		ID:      uuid.New().String(),
		RotaID:  rotaID,
		Date:    date,
		StartAt: date + EveningStart,
		EndAt:   date + EveningEnd,
	}
}
