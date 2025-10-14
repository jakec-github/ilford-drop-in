# Ilford Drop-in v2

A CLI tool for managing volunteer rotas at the Ilford Drop-in Centre, which provides food and support to vulnerable community members in Ilford, London.

## Requirements

- Go >= 1.25

## Prerequisites

### Google Cloud Setup

1. **Create a Google Cloud Project**

   - Enable the Gmail, Google Sheets, and Google Forms APIs

2. **Create an OAuth 2.0 Desktop Client**

   - Download the credentials JSON file
   - Save it as `oauthClient.<env>.json` (e.g., `oauthClient.test.json`)

3. **Grant Access to Spreadsheets**
   - The Google account you authenticate with needs access to:
     - Volunteer spreadsheet
     - Database spreadsheet (for storing rotations, requests, allocations, covers)
     - Rota spreadsheet (for publishing the final schedule)

### Configuration Files

Create the following files in the v2 directory:

#### 1. OAuth Client Configuration

**File:** `oauthClient.<env>.json` (e.g., `oauthClient.test.json`)

This file is downloaded from Google Cloud Console when creating your Desktop OAuth client.

#### 2. Application Configuration

**File:** `drop_in_config.<env>.yaml` (e.g., `drop_in_config.test.yaml`)

```yaml
# Google Sheet IDs
volunteer_sheet_id: 'your-volunteer-sheet-id'
database_sheet_id: 'your-database-sheet-id'
rota_sheet_id: 'your-rota-sheet-id'

# Sheet tab names
volunteers_tab: 'Volunteers'

# Gmail settings
gmail_user_id: 'me'
gmail_sender: 'your-email@gmail.com' # Defaults to match the above value

# Optional rota overrides
rotaOverrides:
  rrule: 'FREQ=WEEKLY;BYDAY=SU' # Weekly shifts on Sunday
  prefilledAllocations: # Volunteers to be manually scheduled
      - "John Doe"
      - "Jane Smith"
    shiftSize: 5 # Custom shift size
```

**Note:** Replace `<env>` with your environment name (e.g., `test`, `prod`). The environment is passed as a flag when running commands.

## Test Data

For testing, you can use the sample volunteer data:

- `test_data/volunteers.csv` - Sample volunteer data for testing

## Installation

1. Clone the repository:

   ```bash
   cd /path/to/ilford-drop-in/v2
   ```

2. Install dependencies:

   ```bash
   go mod download
   ```

3. Build the CLI:
   ```bash
   go build -o cli cmd/cli/main.go
   ```

## Usage

All commands require the `-e` or `--env` flag to specify which environment configuration to use:

```bash
./cli -e <environment> <command> [args]
```

### Available Commands

#### List Volunteers

View all volunteers from the volunteer spreadsheet:

```bash
./cli -e test listVolunteers
```

#### Define Rota

Create a new rotation with a specified number of shifts:

```bash
./cli -e test defineRota 12
```

This will:

- Find the latest existing rotation
- Calculate the start date (next Sunday after the previous rotation ends)
- Create weekly shift dates
- Store the rotation in the database

#### Request Availability (Coming Soon)

Request availability from volunteers with a deadline:

```bash
./cli -e test requestAvailability 2025-10-15
```

#### Send Availability Reminders (Coming Soon)

Send reminders to volunteers who haven't responded:

```bash
./cli -e test sendAvailabilityReminders 2025-10-15
```

#### View Responses (Coming Soon)

View availability responses for a rota:

```bash
./cli -e test viewResponses [rota_id]
```

#### Generate Rota (Coming Soon)

Generate the rota schedule from availability responses:

```bash
./cli -e test generateRota [--seed <seed>] [--dry-run]
```

#### Publish Rota (Coming Soon)

Publish the latest rota to the public rota sheet:

```bash
./cli -e test publishRota
```

#### Add Cover (Coming Soon)

Add a volunteer cover/swap for a shift:

```bash
./cli -e test addCover <shift_date> <covered_volunteer_id> <covering_volunteer_id> [rota_id]
```

### Getting Help

```bash
./cli --help
./cli <command> --help
```

## Development

### Running Tests

```bash
# Run all tests
go test ./...

# Run tests for a specific package
go test -v ./pkg/core/services/

# Run tests with coverage
go test -cover ./...
```

### Logging

Logs are written to:

- **Console:** Human-readable colored output (INFO level and above)
- **File:** JSON format in `logs/<env>_<timestamp>.log` (DEBUG level and above)

### Code Quality

The project uses:

- Structured logging with zap
- Table-driven tests with testify
- Interface-based design for testability
- Clear separation between business logic and I/O

## Architecture

See `docs/architecture.md` for detailed architecture documentation.

Key architectural decisions:

- **SheetsSQL:** Treats Google Sheets as a SQL database with schema validation
- **Service Layer:** Reusable business logic that can be called from CLI or future HTTP API
- **Interface-Based Design:** Easy mocking and testing without real Google Sheets access
- **Environment-Based Config:** Separate configurations for test/prod environments

## Troubleshooting

### "failed to load config" error

- Ensure your config file is named correctly: `drop_in_config.<env>.yaml`
- Verify the file is in the v2 directory

### "failed to load OAuth client config" error

- Ensure your OAuth file is named correctly: `oauthClient.<env>.json`
- Verify you downloaded the correct file from Google Cloud Console

### Authentication issues

- Check that your Google account has access to all required spreadsheets
- Verify the OAuth client has the necessary scopes enabled

### Spreadsheet access errors

- Ensure the authenticated Google account has edit access to all spreadsheets
- Verify the sheet IDs in your config are correct
- Check that the required tabs exist in the spreadsheets
