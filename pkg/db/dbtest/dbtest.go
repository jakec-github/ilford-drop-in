// Package dbtest provides a real-Postgres harness for integration tests.
// Each call to New mints a throwaway database on the local test server
// (scripts/test-db.sh) and runs the full migration set against it, so tests
// exercise the schema exactly as production would see it — including
// constraints and row locks that mocks cannot reproduce.
package dbtest

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net/url"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// defaultAdminURL matches scripts/test-db.sh. Override with TEST_DATABASE_URL.
const defaultAdminURL = "postgres://postgres:postgres@localhost:5432/ilford_dropin_test?sslmode=disable"

// New creates a fresh database on the test Postgres server, runs all
// migrations, and returns a connected *db.DB plus the database's connection
// URL (for tests that need a raw connection alongside the store). The
// database is dropped on test cleanup. Skips the test when the server is
// unreachable — run `scripts/test-db.sh start` to enable these tests.
func New(t *testing.T) (*db.DB, string) {
	t.Helper()
	ctx := context.Background()

	adminURL := os.Getenv("TEST_DATABASE_URL")
	if adminURL == "" {
		adminURL = defaultAdminURL
	}

	admin, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		t.Skipf("test Postgres unreachable (run scripts/test-db.sh start): %v", err)
	}

	name := fmt.Sprintf("ilford_test_%x", rand.Uint64())
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		admin.Close(ctx)
		t.Fatalf("failed to create test database %s: %v", name, err)
	}

	t.Cleanup(func() {
		_, err := admin.Exec(ctx, "DROP DATABASE "+name+" WITH (FORCE)")
		if err != nil {
			t.Errorf("failed to drop test database %s: %v", name, err)
		}
		admin.Close(ctx)
	})

	u, err := url.Parse(adminURL)
	if err != nil {
		t.Fatalf("failed to parse test database URL: %v", err)
	}
	u.Path = "/" + name
	testURL := u.String()

	database, err := db.NewDB(ctx, testURL)
	if err != nil {
		t.Fatalf("failed to connect to test database %s: %v", name, err)
	}
	t.Cleanup(database.Close)

	if err := database.RunMigrations(ctx); err != nil {
		t.Fatalf("failed to run migrations on test database %s: %v", name, err)
	}

	return database, testURL
}
