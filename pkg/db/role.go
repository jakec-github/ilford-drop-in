package db

import (
	"context"
	"fmt"
)

// Role is a job on a Shift, as the database holds it — Team lead, Service
// volunteer, Food collector. A volunteer holds the Roles they will do, and only
// a holder may be allocated to one.
//
// Roles were a `roles:` list in the config file until ticket #126; they are rows
// now so an admin can edit them on a screen (ADR 0006). The id is the identity
// other tables reference, so a rename never breaks a reference; the name is what
// the volunteer roster spells, since the roster is a Google Sheet holding Role
// names in a cell.
//
// It mirrors model.Role field for field. The two exist separately because this
// package deliberately knows nothing about the domain packages — the conversion
// is services.RoleTable, in one place.
type Role struct {
	ID   string // UUID
	Name string
	// Max is the ceiling — how many of this Role a Shift may ever hold. Nil
	// means uncapped.
	Max      *int
	Priority int
	// Colour is a palette token, never a colour value.
	Colour string
}

// ListRoles returns every Role in priority order — the order their Seats are
// filled — with the name breaking a tie, so two Roles given the same priority
// still come back in a stable order rather than whatever the planner chose.
//
// An empty result is an ordinary answer: no migration seeds Roles, so a
// database nobody has configured yet has none.
func (d *DB) ListRoles(ctx context.Context) ([]Role, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT id, name, max, priority, colour
		FROM role
		ORDER BY priority, name
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query roles: %w", err)
	}
	defer rows.Close()

	var roles []Role
	for rows.Next() {
		var role Role
		if err := rows.Scan(&role.ID, &role.Name, &role.Max, &role.Priority, &role.Colour); err != nil {
			return nil, fmt.Errorf("failed to scan role: %w", err)
		}
		roles = append(roles, role)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating roles: %w", err)
	}

	return roles, nil
}

// InsertRole writes one Role. There is no update and no delete here yet: Roles
// are permanent by design, and editing them is the settings screen's job in the
// next ticket. Until then the writers are the dev seed and an operator's psql.
func (d *DB) InsertRole(ctx context.Context, role Role) error {
	_, err := d.pool.Exec(ctx, `
		INSERT INTO role (id, name, max, priority, colour)
		VALUES ($1, $2, $3, $4, $5)
	`, role.ID, role.Name, role.Max, role.Priority, role.Colour)
	if err != nil {
		return fmt.Errorf("failed to insert role %q: %w", role.Name, err)
	}
	return nil
}
