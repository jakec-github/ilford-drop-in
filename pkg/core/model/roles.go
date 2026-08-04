package model

import "sort"

// Role is a job on a Shift — Team lead, Service volunteer, Food collector. A
// volunteer holds the Roles they will do, and only a holder may be allocated to
// one. Roles are configured rather than hardcoded; the yaml tags are here so
// `internal/config` can unmarshal straight into the domain type instead of
// maintaining a parallel struct.
type Role struct {
	Name string `yaml:"name"`
	// Max is the ceiling — how many of this Role a Shift may ever hold, however
	// its Shape is edited. Nil means uncapped.
	Max *int `yaml:"max,omitempty"`
	// Priority orders the filling of Seats when people are scarce, lowest first.
	Priority int `yaml:"priority"`
}

// Capped reports whether the Role has a ceiling.
func (r Role) Capped() bool { return r.Max != nil }

// Roles is the configured set of Roles: ordered by priority, indexed by name.
// The zero value is an empty set, which answers every query rather than
// panicking — callers with no config in scope hold one.
type Roles struct {
	ordered []Role
	byName  map[string]Role
}

// NewRoles builds the lookup table from configured Roles in any order. It does
// not validate them; `config.Validate` owns the rules (unique names, unique
// priorities, exactly one uncapped Role).
func NewRoles(roles []Role) Roles {
	ordered := make([]Role, len(roles))
	copy(ordered, roles)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Priority < ordered[j].Priority })

	byName := make(map[string]Role, len(ordered))
	for _, r := range ordered {
		byName[r.Name] = r
	}

	return Roles{ordered: ordered, byName: byName}
}

// ByName looks a Role up by its configured name. Names are matched exactly:
// the roster, the config and the solver all speak the same string.
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

// UncappedName is the name of the uncapped Role, or "" where none is
// configured. It is what anything unlabelled falls back to: an alteration
// written before Roles were data, or a volunteer joining a shift in nobody's
// place.
func (r Roles) UncappedName() string {
	role, ok := r.Uncapped()
	if !ok {
		return ""
	}
	return role.Name
}

// Uncapped returns the single Role with no ceiling — the one whose Seats a
// Shift's size is spent on. Config permits exactly one in S1.
func (r Roles) Uncapped() (Role, bool) {
	for _, role := range r.ordered {
		if !role.Capped() {
			return role, true
		}
	}
	return Role{}, false
}
