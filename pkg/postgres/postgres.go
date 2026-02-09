package postgres

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// DB provides database operations using PostgreSQL
type DB struct {
	pool *pgxpool.Pool
}

// NewDB creates a new PostgreSQL database connection
func NewDB(ctx context.Context, connString string) (*DB, error) {
	pool, err := pgxpool.New(ctx, connString)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &DB{pool: pool}, nil
}

// Close closes the database connection pool
func (db *DB) Close() {
	db.pool.Close()
}

// RunMigrations executes all pending SQL migration files in order.
// It tracks which migrations have been applied in a schema_migrations table.
func (db *DB) RunMigrations(ctx context.Context) error {
	// Create migrations tracking table if it doesn't exist
	_, err := db.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			filename TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create schema_migrations table: %w", err)
	}

	// Get list of already applied migrations
	rows, err := db.pool.Query(ctx, `SELECT filename FROM schema_migrations`)
	if err != nil {
		return fmt.Errorf("failed to query applied migrations: %w", err)
	}
	applied := make(map[string]bool)
	for rows.Next() {
		var filename string
		if err := rows.Scan(&filename); err != nil {
			rows.Close()
			return fmt.Errorf("failed to scan migration filename: %w", err)
		}
		applied[filename] = true
	}
	rows.Close()

	// Read and sort migration files
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("failed to read migrations directory: %w", err)
	}

	var sqlFiles []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			sqlFiles = append(sqlFiles, entry.Name())
		}
	}
	sort.Strings(sqlFiles)

	// Execute pending migrations
	for _, filename := range sqlFiles {
		if applied[filename] {
			continue
		}

		content, err := fs.ReadFile(migrationsFS, "migrations/"+filename)
		if err != nil {
			return fmt.Errorf("failed to read migration %s: %w", filename, err)
		}

		// Run migration in a transaction
		tx, err := db.pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("failed to begin transaction for %s: %w", filename, err)
		}

		_, err = tx.Exec(ctx, string(content))
		if err != nil {
			tx.Rollback(ctx)
			return fmt.Errorf("failed to execute migration %s: %w", filename, err)
		}

		_, err = tx.Exec(ctx, `INSERT INTO schema_migrations (filename) VALUES ($1)`, filename)
		if err != nil {
			tx.Rollback(ctx)
			return fmt.Errorf("failed to record migration %s: %w", filename, err)
		}

		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("failed to commit migration %s: %w", filename, err)
		}
	}

	return nil
}
