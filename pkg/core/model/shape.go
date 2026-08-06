package model

// Seat is one entry in a Shape: how many places of one Role a Shift asks for.
//
// It carries the whole Role rather than a name or an id because the two things
// a Seat is read for want different halves of it — a screen wants the name and
// the colour, the solver wants the name and the ceiling, and a save wants the
// id — and a Seat that carried only one of them would send every reader back to
// the Roles table.
type Seat struct {
	Role  Role
	Count int
}

// Shape is what a Shift asks for: which Roles, and how many Seats of each, in
// the order the Seats are filled.
//
// It replaces the single `defaultShiftSize` of the config file, which could only
// ever describe a rota with one Role — every other Role's count had to be
// inferred from its ceiling, which meant a capped Role always asked for exactly
// its ceiling and could never ask for less (ADR 0006, issue #129).
//
// An empty Shape asks for nothing. That is the state a deployment nobody has
// configured is in, and it blocks allocation rather than anything else.
type Shape []Seat
