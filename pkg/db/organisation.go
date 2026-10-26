package db

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

// unassignedOrganisation is the placeholder migration 030 put every existing
// row in, because the real id of the Organisation they belonged to is a deployment's
// business and appears nowhere in this repo.
const unassignedOrganisation = "unassigned"

// SyncOrganisations makes the organisation table hold every configured id, so
// the rows bound to one have something to reference (ADR 0011). It inserts the
// ids it has not seen and deletes nothing: an Organisation dropped from config
// keeps its data, and its URL simply stops being served.
//
// It also adopts the rows a pre-Organisation database already held. If the
// placeholder is still present and exactly one configured id is new, the
// placeholder is renamed to it and every reference follows by ON UPDATE
// CASCADE. If several are new there is no telling which Organisation the old rota
// was, and it refuses rather than guess. It reports the id it adopted into, ""
// when it adopted nothing.
//
// One transaction, so a refused adoption inserts nothing either.
func (d *DB) SyncOrganisations(ctx context.Context, ids []string) (string, error) {
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, `SELECT id FROM organisation`)
	if err != nil {
		return "", fmt.Errorf("failed to read organisations: %w", err)
	}
	var known []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return "", fmt.Errorf("failed to scan organisation: %w", err)
		}
		known = append(known, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("error iterating organisations: %w", err)
	}

	var unseen []string
	for _, id := range ids {
		if !slices.Contains(known, id) {
			unseen = append(unseen, id)
		}
	}

	adopted := ""
	if slices.Contains(known, unassignedOrganisation) && !slices.Contains(ids, unassignedOrganisation) {
		switch len(unseen) {
		case 0:
			// Nothing new to adopt into. The placeholder stays, holding the old
			// rows, until a config names a new Organisation for them.
		case 1:
			adopted = unseen[0]
			if _, err := tx.Exec(ctx, `UPDATE organisation SET id = $1 WHERE id = $2`, adopted, unassignedOrganisation); err != nil {
				return "", fmt.Errorf("failed to adopt the existing rota into organisation %q: %w", adopted, err)
			}
			unseen = nil
		default:
			return "", fmt.Errorf("the database holds a rota from before organisations existed, and the config names %d organisations it has not seen (%s): add them one at a time, starting with the one that rota belongs to",
				len(unseen), strings.Join(unseen, ", "))
		}
	}

	for _, id := range unseen {
		if _, err := tx.Exec(ctx, `INSERT INTO organisation (id) VALUES ($1)`, id); err != nil {
			return "", fmt.Errorf("failed to add organisation %q: %w", id, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("failed to commit transaction: %w", err)
	}
	return adopted, nil
}
