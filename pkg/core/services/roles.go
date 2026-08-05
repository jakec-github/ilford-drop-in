package services

import (
	"context"
	"fmt"

	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// RoleStore reads the Roles the drop-in offers. Every store interface in this
// package embeds it, because a Role name is resolved on nearly every path there
// is: the roster, pins, the solver contract, the rota's chips.
type RoleStore interface {
	ListRoles(ctx context.Context) ([]db.Role, error)
}

// RoleTable reads the Roles and indexes them for lookup by name and by
// priority. It is the one place database rows become the domain's lookup table,
// and the successor to `config.RoleTable()` — Roles moved out of the config file
// and into the database in ticket #126 (ADR 0006), so this reads per call rather
// than off something loaded at startup. Roles are small, few and rarely edited;
// the read is a single unindexed scan of a table with a handful of rows.
//
// A database with no Roles yields the empty table rather than an error. That is
// what a deployment nobody has configured yet looks like, and the pages that
// merely name Roles should still render there; the paths that cannot work
// without Roles — allocation above all — are the ones that say so.
func RoleTable(ctx context.Context, store RoleStore) (model.Roles, error) {
	rows, err := store.ListRoles(ctx)
	if err != nil {
		return model.Roles{}, fmt.Errorf("failed to read roles: %w", err)
	}

	roles := make([]model.Role, 0, len(rows))
	for _, row := range rows {
		roles = append(roles, model.Role{
			ID:       row.ID,
			Name:     row.Name,
			Max:      row.Max,
			Priority: row.Priority,
			Colour:   row.Colour,
		})
	}

	return model.NewRoles(roles), nil
}
