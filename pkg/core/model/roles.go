package model

import "sort"

// Role is a job on a Shift — Team lead, Service volunteer, Food collector. A
// volunteer holds the Roles they will do, and only a holder may be allocated to
// one. Roles are rows in the database rather than hardcoded or configured
// (ADR 0006), and permanent: once created a Role always exists, so no reference
// to one can dangle.
type Role struct {
	// ID is the identity other tables reference, so renaming a Role never
	// breaks a reference. Empty on a Role built by a test or a caller with no
	// database in scope, which cares only about the name.
	ID   string
	Name string
	// Max is the ceiling — how many of this Role a Shift may ever hold, however
	// its Shape is edited. Nil means uncapped.
	Max *int
	// Priority orders the filling of Seats when people are scarce, lowest first.
	Priority int
	// Colour is the palette token this Role is drawn in — see RoleColours. May
	// be empty on the way in; NewRoles fills it in, so every Role the lookup
	// table hands out carries one.
	Colour string
}

// The palette a Role's colour is chosen from. Named tokens rather than free-form
// hex because the app owns the values: each token has a light and a dark value,
// both verified at 4.5:1 against their background, so contrast stays a decision
// made here rather than one a config editor makes by accident. `web/src/index.css`
// holds the values; when Role authoring moves into the app, these are the
// swatches a picker offers.
const (
	ColourViolet = "violet"
	ColourTeal   = "teal"
	ColourBlue   = "blue"
	ColourIndigo = "indigo"
	ColourCyan   = "cyan"
	ColourGreen  = "green"
	ColourAmber  = "amber"
	ColourOrange = "orange"
	ColourRose   = "rose"
	ColourPink   = "pink"
	ColourBrown  = "brown"
	ColourSlate  = "slate"
)

// DefaultRoleColour is what a Role configured without one is drawn in. Slate is
// deliberately the dullest token in the palette: an uncoloured Role should look
// unstyled rather than collide with a Role somebody chose a colour for.
const DefaultRoleColour = ColourSlate

// RoleColours is the palette, in the order a picker would offer it.
var RoleColours = []string{
	ColourViolet,
	ColourTeal,
	ColourBlue,
	ColourIndigo,
	ColourCyan,
	ColourGreen,
	ColourAmber,
	ColourOrange,
	ColourRose,
	ColourPink,
	ColourBrown,
	ColourSlate,
}

// ValidRoleColour reports whether a colour names a palette token. An empty
// colour is not one: it means "unset", which NewRoles fills in rather than
// validating.
func ValidRoleColour(colour string) bool {
	for _, c := range RoleColours {
		if c == colour {
			return true
		}
	}
	return false
}

// Capped reports whether the Role has a ceiling.
func (r Role) Capped() bool { return r.Max != nil }

// Roles is the set of Roles the drop-in offers: ordered by priority, indexed by
// name. The zero value is an empty set, which answers every query rather than
// panicking — callers with no Roles in scope hold one.
type Roles struct {
	ordered []Role
	byName  map[string]Role
}

// NewRoles builds the lookup table from Roles in any order. It does not
// validate them: the database holds the rules it can (a unique name, a positive
// max), and a table built from a set that breaks the rest still answers rather
// than failing a read path nobody could fix from.
//
// It does fill in an unset colour, so every Role read back out of the table has
// one. Defaulting here rather than at the read means a caller building the
// table straight from Roles — a test, a seed — gets the same answer as the
// server.
func NewRoles(roles []Role) Roles {
	ordered := make([]Role, len(roles))
	copy(ordered, roles)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Priority < ordered[j].Priority })

	for i := range ordered {
		if ordered[i].Colour == "" {
			ordered[i].Colour = DefaultRoleColour
		}
	}

	byName := make(map[string]Role, len(ordered))
	for _, r := range ordered {
		byName[r.Name] = r
	}

	return Roles{ordered: ordered, byName: byName}
}

// ByName looks a Role up by name. Names are matched exactly: the roster, the
// stored Roles and the solver all speak the same string.
func (r Roles) ByName(name string) (Role, bool) {
	role, ok := r.byName[name]
	return role, ok
}

// ByPriority returns the Roles in the order their Seats are filled.
func (r Roles) ByPriority() []Role {
	ordered := make([]Role, len(r.ordered))
	copy(ordered, r.ordered)
	return ordered
}

// UncappedName is the name of the uncapped Role, or "" where none exists. It
// is what anything unlabelled falls back to: an alteration
// written before Roles were data, or a volunteer joining a shift in nobody's
// place.
func (r Roles) UncappedName() string {
	role, ok := r.Uncapped()
	if !ok {
		return ""
	}
	return role.Name
}

// Uncapped returns the first Role with no ceiling — the one whose Seats a
// Shift's size is spent on. Slice 4 replaces `defaultShiftSize` with a
// per-Shift Shape naming its own counts, after which a second uncapped Role is
// meaningful; until then the first is the answer.
func (r Roles) Uncapped() (Role, bool) {
	for _, role := range r.ordered {
		if !role.Capped() {
			return role, true
		}
	}
	return Role{}, false
}
