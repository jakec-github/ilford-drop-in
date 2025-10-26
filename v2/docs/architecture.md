# Structure and packages

To implement the tool we will create the following repository. Key functionality will be defined in pkg/core/services. Representations in pkg/core/model. We will have separate packages for the various clients and a utils package.

repo/
├── cmd/
│ ├── cli/ # CLI entrypoint
│ │ └── main.go
│ └── server/ # Web server entrypoint (future state only)
│ └── main.go
│
├── internal/ # Private application code
│ └── config/ # Configuration ingestion
│ ├── config.schema.json
│ └── config.go
│
├── pkg/ # Public packages (reusable across CLI/server)
│ ├── api/ # HTTP API for services (future state only)
│ ├── utils/ # General utilities
│ ├── db/ # Database layer or storage abstraction
│ ├── sheetsSQL/ # Abstracts basic sheets queries into SQL
│ ├── clients/
│ │ ├── gmailClient/
│ │ ├── sheetsClient/
│ │ └── formsClient/
│ └── core/ # Core business logic
│ ├── model/
│ ├── services/
│ ├─── defineRota.go
│ ├─── requestAvailability.go
│ ├─── sendAvailabilityReminders.go
│ ├─── viewResponses.go
│ ├─── allocateRota.go
│ ├─── viewRota.go
│ ├─── publishRota.go
│ └─── addCovers.go
│
├── gui/ # Single page app (future state only)
│
├── go.mod
├── go.sum
└── README.md

# Data model

Internal data stored in sheets will take the following format. The tool will not manage the schema. Users will have to create the sheet and tabs (with names matching these). Then add the column titles.

## rotations

| Column      | Type | Constraint  |
| ----------- | ---- | ----------- |
| id          | uuid | Primary key |
| start       | date |             |
| shift_count | int  |             |

## availability_requests

| Column       | Type | Constraint  |
| ------------ | ---- | ----------- |
| id           | uuid |             |
| rota_id      | uuid | Foreign key |
| shift_date   | date |             |
| volunteer_id | text |             |
| form_id      | text |             |
| form_url     | text |             |
| form_sent    | bool |             |

## allocations

| Column       | Type | Constraint  |
| ------------ | ---- | ----------- |
| id           | uuid | Primary key |
| rota_id      | uuid |             |
| shift_date   | date |             |
| role         | text |             |
| volunteer_id | text | nullable    |
| preallocated | text | nullable    |

## covers

| Column                | Type | Constraint  |
| --------------------- | ---- | ----------- |
| id                    | uuid | Primary key |
| rota_id               | uuid |             |
| shift_date            | uuid |             |
| covered_volunteer_id  | uuid | nullable    |
| covering_volunteer_id | uuid | nullable    |

# Config

Config will be kept in `drop_in_config.yaml`. The CLI runner will look for the file in the directory it is being executed or in the user’s home directory. This file should be managed by the user and comply with the following JSON schema. Config ingestion may vary for the web server but is undefined at this stage

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "title": "config",
  "description": "Config for ilford-drop-in",
  "type": "object",
  "properties": {
    "volunteerSheetID": {
      "description": "The ID of the sheet storing volunteer data",
      "type": "string"
    },
    "serviceVolunteersTab": {
      "description": "The name of the tab in the volunteers sheet storing volunteer data",
      "type": "string"
    },
    "rotaSheetID": {
      "description": "The ID of the sheet used to publish rotas",
      "type": "string"
    },
    "databaseSheetID": {
      "description": "Database sheet ID",
      "type": "string"
    },
    "rotaOverrides": {
      "description": "Overrides to apply when generating rotas",
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "rrule": {
            "description": "An rrule to match shifts",
            "type": "string"
          },
          "customPreallocations": {
            "description": "A list of volunteer names to add to the rota",
            "type": "array",
            "items": { "type": "string" }
          },
          "shiftSize": {
            "description": "Custom total volunteer count for these shifts",
            "type": "number"
          }
        }
      }
    },
    "gmailUserID": {
      "description": "The ID of the gmail user to send emails from",
      "type": "string"
    },
    "gmailSender": {
      "description": "The sender's email address if it differs from the ID",
      "type": "string"
    },
    "maxAllocationFrequency": {
      "description": "Maximum frequency a volunteer can be allocated (e.g., 0.25 = 1 in 4 shifts)",
      "type": "number",
      "minimum": 0,
      "maximum": 1,
      "exclusiveMinimum": true
    },
    "defaultShiftSize": {
      "description": "Default number of volunteers per shift (excluding team lead)",
      "type": "integer",
      "minimum": 1
    }
  },
  "required": [
    "volunteerSheetID",
    "serviceVolunteersTab",
    "rotaSheetID",
    "databaseSheetID",
    "gmailUserID",
    "maxAllocationFrequency",
    "defaultShiftSize"
  ]
}
```

In future the gmail IDs should be replaceable by retrieving the details from the authenticated user. To reduce the scope of the migration this project may continue to use values set manually in config. If not, they should be removed.

## Secret ingestion

The integration with Google tooling requires some additional configs. Users must also supply the serviceAccount.json and oauthClient.json supplied by google. The application will ingest these in the same way. We may go down the route of doing everything through oauth in which case the service account can be discarded.

## Volunteers schema

The volunteers sheet is designed to be human friendly and maintained manually. It will contain column with the following titles and data:

| Column     | Type |
| ---------- | ---- |
| First name | text |
| Last name  | text |
| Role       | text |
| Status     | text |
| Sex/Gender | text |
| Email      | text |
| Unique ID  | text |
| Group key  | text |

Other columns should be ignored.

# Supported services

The tool will support the following actions and queries. For each one there is a rough series of steps required.

`defineRota`

- Accepts:
  - count of shifts - int
- DB query: Fetches rota list
- Finds latest rota
- Calculates start date of the next rota
- DB query: Appends rota
- Calculates shifts for new rota
- Shows new shifts

`requestAvailability`

- Accepts:
  - deadline - string
- DB query: Fetches rota list
- Finds latest rota
- DB query: Fetches availability requests
- Finds availability requests for latest rota
- Sheets query: Fetches volunteers
- Finds active volunteers
- Finds any active volunteers who do not have an availability request record for the current rota
- Calculates shifts for the latest rota
- Forms query: Creates availability forms for this rota for those volunteers
- DB query: Appends availability requests for all missing volunteers with form_sent set to false
- Finds all the previously created availability requests with form_sent set to false
- Gmail query: Emails all unsent forms. Includes deadline
- DB query: Appends a new availability request for emailed volunteers with form_sent set to true
- Shows volunteers who have been sent forms and those it failed to send to

`sendAvailabilityReminders`

- Accepts:
  - deadline - string
- DB query: Fetches rota list
- Finds latest rota
- DB query: Fetches availability requests
- Finds availability requests for latest rota
- Forms query: Gets responses matching form IDs
- Finds forms that do not have a response
- DB query: Fetches volunteers
- Find volunteers matching unresponded availability requests
- Gmail query: Email those volunteers a reminder including the form
- Show volunteers who have been reminded

`viewResponses`

- Accepts:
  - rota id - string (optional)
- DB query: Fetches rota list
- Resolves rota
- DB query: Fetches availability requests
- Finds availability requests for resolved rota
- Forms query: Gets responses matching form IDs
- Sheets query: Fetches volunteers
- Matches volunteer ids to names
- Shows responses

`allocateRota`

- Accepts:
  - dry_run boolean (default is false)
- DB query: Fetches rota list
- Finds latest rota
- DB query: Fetches availability requests
- Finds availability requests for resolved rota
- Forms query: Gets responses matching form IDs
- Sheets query: Fetches volunteers
- Matches volunteer ids to names
- Allocates rota using allocator package
- Jumps to last step if dry run
- Resolves all allocations
- DB query: Appends allocations
- Shows rota

`publishRota`

- Accepts:
  - Zilch
- DB query: Fetches rota list
- Finds latest rota
- DB query: Fetches allocations
- Finds allocations for latest rota
- DB query: Fetches covers
- Sheets query: Fetches volunteers
- Matches volunteer ids to names
- Rebuilds rota
- Sheets query: Publishes rota
- Shows confirmation

`addCover`

- Accepts:
  - shift_date - string
  - covered_volunteer_id - string
  - covering_volunteer_id - string
  - rota id - string (optional)
- DB query: Fetches rota list
- Resolves rota
- DB query: Fetches allocations
- Finds allocations for latest rota
- Verifies volunteers can cover
- DBquery: appends cover
- DB query: Fetches covers
- Sheets query: Fetches volunteers
- Matches volunteer ids to names
- Rebuilds rota
- Sheets query: Publishes rota
- Shows confirmation
