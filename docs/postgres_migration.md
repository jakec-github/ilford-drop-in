# Plan: Migrate from SheetsSQL to PostgreSQL

## Overview

Replace Google Sheets-based database (SheetsSQL) with PostgreSQL:

- **Production**: Neon (serverless PostgreSQL)
- **Test**: Local PostgreSQL (assumes postgres is already running)
- **Migration**: Export existing prod data from Sheets → import to Neon

## Current Architecture

The codebase has excellent separation:

- `pkg/sheetssql/` - Google Sheets database driver
- `pkg/db/` - Domain layer with interfaces (`RotationStore`, etc.)
- Services depend on interfaces, not concrete implementations

**3 Tables:**

- `rotation` (id, start, shift_count)
- `availability_request` (id, rota_id, shift_date, volunteer_id, form_id, form_url, form_sent)
- `allocation` (id, rota_id, shift_date, role, volunteer_id, custom_entry)

Note: The `cover` table is excluded - the feature is not yet implemented and the schema design is still in progress.

---

## Implementation Plan

### Phase 1: PostgreSQL Infrastructure

**1.1 Create PostgreSQL package** (`pkg/postgres/`)

```
pkg/postgres/
├── postgres.go      # DB connection, NewDB()
├── rotation.go      # Rotation operations
├── availability.go  # AvailabilityRequest operations
├── allocation.go    # Allocation operations
└── migrations/
    └── 001_initial_schema.sql
```

Note: `cover.go` excluded - feature not yet implemented.

**1.2 SQL Schema** (`pkg/postgres/migrations/001_initial_schema.sql`)

```sql
CREATE TABLE rotation (
    id UUID PRIMARY KEY,
    start DATE NOT NULL,
    shift_count INT NOT NULL
);

CREATE TABLE availability_request (
    id UUID NOT NULL,
    rota_id UUID NOT NULL REFERENCES rotation(id),
    shift_date DATE NOT NULL,
    volunteer_id TEXT NOT NULL,
    form_id TEXT NOT NULL,
    form_url TEXT NOT NULL,
    form_sent BOOLEAN NOT NULL DEFAULT FALSE,
    PRIMARY KEY (id, form_sent)  -- Composite key: append-only table with updates as new rows
);

CREATE TABLE allocation (
    id UUID PRIMARY KEY,
    rota_id UUID NOT NULL REFERENCES rotation(id),
    shift_date DATE NOT NULL,
    role TEXT NOT NULL,
    volunteer_id TEXT,
    custom_entry TEXT
);

CREATE INDEX idx_availability_request_rota ON availability_request(rota_id);
CREATE INDEX idx_allocation_rota ON allocation(rota_id);
```

**1.3 PostgreSQL DB Implementation**

```go
// pkg/postgres/postgres.go
type DB struct {
    pool *pgxpool.Pool
}

func NewDB(ctx context.Context, connString string) (*DB, error)
func (db *DB) Close()
func (db *DB) RunMigrations(migrationsPath string) error  // Executes SQL files in order

// Implement all db.* interfaces:
func (db *DB) GetRotations(ctx context.Context) ([]db.Rotation, error)
func (db *DB) InsertRotation(rotation *db.Rotation) error
// ... etc for all operations
```

**1.4 Dependencies**

Add to `go.mod`:

```
github.com/jackc/pgx/v5
```

Note: Migrations are plain SQL files executed manually or via a simple RunMigrations function - no migration library needed.

---

### Phase 2: Configuration Updates

**2.1 Update Config Structure** (`internal/config/config.go`)

```go
type Config struct {
    // Existing fields...

    // Database config (replaces DatabaseSheetID)
    DatabaseURL  string // PostgreSQL connection string
}
```

**2.2 Environment Config Files**

`drop_in_config.prod.yaml`:

```yaml
database_url: ${NEON_DATABASE_URL} # From environment variable
```

`drop_in_config.test.yaml`:

```yaml
database_url: postgres://localhost:5432/ilford_dropin_test?sslmode=disable
```

---

### Phase 3: Database Interface

**3.1 Update `pkg/db/db.go`**

```go
type Database interface {
    // All store interfaces combined
    RotationStore
    AvailabilityRequestStore
    AllocationStore
    Close() error
}
```

The `pkg/postgres` package will implement this interface directly.

---

### Phase 4: CLI Initialization Update

**4.1 Update `cmd/cli/main.go`**

Replace SheetsSQL initialization with PostgreSQL:

```go
func initApp() error {
    // ... existing config loading ...

    // Database initialization
    database, err := postgres.NewDB(ctx, cfg.DatabaseURL)
    if err != nil {
        return fmt.Errorf("failed to initialize database: %w", err)
    }

    app = &commands.AppContext{
        Database: database,
        // ... other fields
    }
}
```

---

### Phase 5: Data Migration

**5.1 Create Migration Command**

```go
// cmd/cli/commands/migrate_data.go
func MigrateDataCmd(app *AppContext) *cobra.Command {
    return &cobra.Command{
        Use:   "migrateData",
        Short: "Migrate data from SheetsSQL to PostgreSQL",
        RunE: func(cmd *cobra.Command, args []string) error {
            // 1. Connect to both databases
            // 2. Read all data from Sheets
            // 3. Insert into PostgreSQL
            // 4. Verify counts match
        },
    }
}
```

**6.2 Migration Steps (Manual)**

1. Export from existing prod Sheets:

   - Run with `--env prod` using SheetsSQL
   - Dump to JSON or CSV

2. Import to Neon:

   - Create Neon database
   - Run migrations
   - Import data

3. Verify:
   - Compare row counts
   - Spot-check key records

---

## File Changes Summary

| File                        | Change                                      |
| --------------------------- | ------------------------------------------- |
| `pkg/postgres/`             | **NEW** - PostgreSQL implementation         |
| `pkg/postgres/migrations/`  | **NEW** - SQL migrations                    |
| `pkg/db/db.go`              | Add `Database` interface                    |
| `internal/config/config.go` | Add `DatabaseURL`, remove `DatabaseSheetID` |
| `cmd/cli/main.go`           | Use PostgreSQL instead of SheetsSQL         |
| `drop_in_config.*.yaml`     | Add database config                         |
| `go.mod`                    | Add pgx dependency                          |

---

## Verification

### Local Development

```bash
# Start local PostgreSQL
docker run -d --name ilford-pg -p 5432:5432 \
  -e POSTGRES_DB=ilford_dropin_test \
  -e POSTGRES_PASSWORD=test \
  postgres:16

# Run with test config
go run cmd/cli/main.go --env test listVolunteers
```

### Tests

```bash
go test ./pkg/postgres/...
```

### Production

```bash
# Set Neon connection string
export NEON_DATABASE_URL="postgres://user:pass@ep-xxx.neon.tech/neondb?sslmode=require"

# Run migrations
go run cmd/cli/main.go --env prod migrate

# Verify
go run cmd/cli/main.go --env prod listVolunteers
```
