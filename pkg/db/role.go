package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ErrDuplicateRoleName reports that a write would have given two Roles the same
// name. It is named because it is the one write failure a caller answers
// differently from "the database is unhappy": an admin typing a name that is
// already taken has made an ordinary mistake and is told so, and translating
// the driver's error code is this package's job rather than every caller's.
var ErrDuplicateRoleName = errors.New("a role with that name already exists")

// uniqueViolation is Postgres's SQLSTATE for a broken unique constraint.
const uniqueViolation = "23505"

// isDuplicateName reports whether an error is the role-name unique index
// refusing a write. The constraint is named rather than any unique violation
// being assumed, so a future index on the table cannot quietly start reporting
// itself as a name clash.
func isDuplicateName(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) &&
		pgErr.Code == uniqueViolation &&
		pgErr.ConstraintName == "role_name_key"
}

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

// InsertRole writes one Role. A name another Role already holds comes back as
// ErrDuplicateRoleName; anything else is wrapped as it arrived.
//
// There is no delete, and there never will be: Roles are permanent by design
// (ADR 0006), so nothing that references one can dangle.
//
// The Roles are an allocator input — which jobs exist, in what order they are
// filled and how many of each anyone may do — so writing one makes the rota in
// flight's draft stale (issue #142).
func (d *DB) InsertRole(ctx context.Context, role Role) error {
	return d.inTx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO role (id, name, max, priority, colour)
			VALUES ($1, $2, $3, $4, $5)
		`, role.ID, role.Name, role.Max, role.Priority, role.Colour)
		if err != nil {
			if isDuplicateName(err) {
				return fmt.Errorf("failed to insert role %q: %w", role.Name, ErrDuplicateRoleName)
			}
			return fmt.Errorf("failed to insert role %q: %w", role.Name, err)
		}
		return markAllRotaInputsChanged(ctx, tx)
	})
}

// UpdateRole rewrites every editable field of one Role, addressed by its id.
// The id itself never moves — it is what allocations, alterations and pins were
// written against — so a rename here is invisible to everything holding a
// reference.
//
// It takes the whole Role rather than the fields that changed because the
// settings screen edits them together, and because a nil Max has to mean
// "uncapped" rather than "leave the ceiling alone": taking a ceiling off is the
// commonest edit after a rename, and a partial update could not express it.
//
// The bool reports whether a row matched. An unknown id is a miss rather than
// an error: Roles are never deleted, so it means the caller named the wrong
// one, which is a 404 rather than a failure.
func (d *DB) UpdateRole(ctx context.Context, role Role) (bool, error) {
	var written bool
	err := d.inTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE role
			SET name = $2, max = $3, priority = $4, colour = $5
			WHERE id = $1
		`, role.ID, role.Name, role.Max, role.Priority, role.Colour)
		if err != nil {
			if isDuplicateName(err) {
				return fmt.Errorf("failed to update role %q: %w", role.Name, ErrDuplicateRoleName)
			}
			return fmt.Errorf("failed to update role %q: %w", role.Name, err)
		}
		written = tag.RowsAffected() > 0
		if !written {
			return nil
		}
		// A cap, a priority or a colour: the first two are what the solver
		// works to, so the rota in flight's draft is stale (issue #142). A
		// rename is stamped with them rather than picked apart, and costs one
		// re-solve.
		return markAllRotaInputsChanged(ctx, tx)
	})
	if err != nil {
		return false, err
	}
	return written, nil
}
