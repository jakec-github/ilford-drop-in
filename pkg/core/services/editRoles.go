package services

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// RoleWriteStore is what creating and editing Roles needs. It is separate from
// RoleStore, which nearly every other store interface embeds: reading Roles is
// on almost every path there is, writing them is on one screen.
type RoleWriteStore interface {
	InsertRole(ctx context.Context, role db.Role) error
	UpdateRole(ctx context.Context, role db.Role) (bool, error)
}

// RoleParams is a Role as an admin states it. It is the same on the way in for
// both operations, because an edit and a creation say the same things: a Role
// must not be able to reach through an edit a state it could not have been
// created in.
//
// The id is not here. On a creation there is none yet, and on an edit it is
// what the Role is addressed by rather than something being set.
type RoleParams struct {
	Name string
	// Max is the ceiling — how many of this Role a Shift may ever hold. Nil is
	// uncapped, and is an answer rather than an omission: the Role a Shift's
	// size is spent on is the uncapped one.
	Max      *int
	Priority int
	// Colour is a palette token. Empty means the default, so a caller with no
	// picker still writes a Role that renders.
	Colour string
}

// validate turns an admin's answers into the row to write, or says why it will
// not. Priority is deliberately unchecked: it only orders Seats relative to one
// another, so no value of it is wrong.
func (p RoleParams) validate() (db.Role, error) {
	name := strings.TrimSpace(p.Name)
	if name == "" {
		// Said as what it costs: the roster is a Google Sheet naming Roles by
		// string, so a Role with no name is one nobody can spell in a cell.
		return db.Role{}, wrapf(ErrInvalidInput, "a role needs a name — it is what the volunteer roster spells in a cell")
	}

	if p.Max != nil && *p.Max < 1 {
		return db.Role{}, wrapf(ErrInvalidInput, "a role's maximum must be at least 1, or left off for no limit")
	}

	colour := p.Colour
	if colour == "" {
		colour = model.DefaultRoleColour
	}
	if !model.ValidRoleColour(colour) {
		return db.Role{}, wrapf(ErrInvalidInput, "%q is not one of the colours a role can be drawn in", p.Colour)
	}

	return db.Role{Name: name, Max: p.Max, Priority: p.Priority, Colour: colour}, nil
}

// CreateRole adds a Role and returns it as it now stands, id included — which
// is what the caller has to address it by from here on.
//
// The id is minted here rather than taken from the caller because it is the
// identity every later reference is written against: an allocation, an
// alteration, a Shape's Seat. Nothing outside this app gets to choose it.
//
// A Role created is a Role forever. There is no delete and no retire anywhere
// (ADR 0006), so nothing referencing a Role can ever dangle — the price is a
// picker that accumulates Roles the drop-in has stopped using.
func CreateRole(ctx context.Context, store RoleWriteStore, params RoleParams, logger *zap.Logger) (*model.Role, error) {
	row, err := params.validate()
	if err != nil {
		return nil, err
	}
	row.ID = uuid.New().String()

	if err := store.InsertRole(ctx, row); err != nil {
		if errors.Is(err, db.ErrDuplicateRoleName) {
			return nil, wrapf(ErrConflict, "a role called %q already exists", row.Name)
		}
		return nil, fmt.Errorf("failed to create role %q: %w", row.Name, err)
	}

	logger.Info("Role created",
		zap.String("role_id", row.ID),
		zap.String("name", row.Name),
		zap.Int("priority", row.Priority))

	return toModelRole(row), nil
}

// UpdateRole rewrites one Role's name, ceiling, priority and colour. The id
// never moves, so a rename is invisible to everything holding a reference — an
// allocated rota still reads as it was made.
//
// What it cannot fix is the half of the name contract this app does not own:
// volunteers hold Roles by name in the roster Sheet, so a rename here without
// the same edit there silently stops volunteers holding the Role. That is a
// warning the screen gives at the point of rename, not a refusal — an admin
// renaming a Role has decided to, and the roster validation warnings are the
// standing check afterwards.
func UpdateRole(ctx context.Context, store RoleWriteStore, id string, params RoleParams, logger *zap.Logger) (*model.Role, error) {
	row, err := params.validate()
	if err != nil {
		return nil, err
	}

	// Checked before the write so a typo is a miss rather than a driver error
	// about UUID syntax, which would surface as a 500.
	if _, err := uuid.Parse(id); err != nil {
		return nil, wrapf(ErrNotFound, "role %s not found", id)
	}
	row.ID = id

	updated, err := store.UpdateRole(ctx, row)
	if err != nil {
		if errors.Is(err, db.ErrDuplicateRoleName) {
			return nil, wrapf(ErrConflict, "a role called %q already exists", row.Name)
		}
		return nil, fmt.Errorf("failed to update role %s: %w", id, err)
	}
	if !updated {
		// Roles are never deleted, so nothing matching this id means the caller
		// named the wrong one rather than losing a race with a removal.
		return nil, wrapf(ErrNotFound, "role %s not found", id)
	}

	logger.Info("Role edited",
		zap.String("role_id", row.ID),
		zap.String("name", row.Name),
		zap.Int("priority", row.Priority))

	return toModelRole(row), nil
}

func toModelRole(row db.Role) *model.Role {
	return &model.Role{
		ID:       row.ID,
		Name:     row.Name,
		Max:      row.Max,
		Priority: row.Priority,
		Colour:   row.Colour,
	}
}
