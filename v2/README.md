# Ilford Drop-in v2

CLI tool for managing volunteer rotas with Google Sheets integration.

## Project Structure

- `cmd/cli/` - CLI entrypoint
- `cmd/server/` - Web server entrypoint (future)
- `internal/config/` - Configuration ingestion
- `pkg/clients/` - Google API clients (Gmail, Sheets, Forms)
- `pkg/core/model/` - Data models
- `pkg/core/services/` - Business logic for commands
- `pkg/db/` - Database layer abstraction
- `pkg/sheetsSQL/` - SQL abstraction for Sheets
- `pkg/utils/` - General utilities

## Commands

- `defineRota` - Define a new rota rotation
- `requestAvailability` - Request availability from volunteers
- `sendAvailabilityReminders` - Send reminders to non-responders
- `viewResponses` - View volunteer responses
- `generateRota` - Generate the rota schedule
- `viewRota` - View the current rota
- `publishRota` - Publish rota to public sheet
- `addCover` - Add volunteer covers/swaps
