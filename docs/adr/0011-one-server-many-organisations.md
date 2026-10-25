# One server, many Organisations

Status: accepted, 2026-10-25 (#256).

## In plain words

One server and one database now run the rota for several drop-ins. Each is an
**Organisation**, listed in `drop_in_config.yaml` and served under its own
address, `/org/<id>/`. Nothing in the app links from one Organisation to
another, and nothing lists them: the root of the site is a plain page that names
none. Signing in once proves who you are everywhere; what you may do is decided
per Organisation, from that Organisation's own allowlists.

## Decisions and their reasons

- **One database, an `organisation_id` on the data.** The alternatives were a
  process per Organisation or a database per Organisation behind one process.
  Both isolate more strongly, and both multiply ports, migrations, connection
  pools and disk on a droplet that is already short of disk. The cost of
  sharing is that a query that forgets its Organisation leaks another one's
  rota, so the Organisation is not an optional filter: a store is *bound* to one
  (`db.DB.ForOrganisation`), every query it runs is scoped by it, and an unbound
  store refuses to run a query at all.

- **The column sits on the roots, and on the Shift.** Role, Rotation, Shift,
  Rota Defaults, Standing Preallocation and the published rota sheet carry it.
  Everything else hangs off a Shift or a Rotation and is scoped through it. The
  Shift carries its own copy, rather than reaching it through its Rotation, so
  that one-Shift-per-date can be an index per Organisation; a composite foreign
  key keeps that copy equal to its Rotation's.

- **Uniqueness is per Organisation.** A Role name, a Shift's date and a Standing
  Preallocation are unique within an Organisation, not across all of them. Rota
  Defaults and the published rota sheet were singleton rows; they are one row
  per Organisation.

- **The Organisation is in the path, not in the request.** `/org/<id>/` prefixes
  the pages, the calendar feeds and now the API. A field on each request was
  considered and rejected: a path prefix is one piece of routing every
  Organisation route sits behind, where a field is a check every handler must
  remember, and it is known before a handler runs, which is when authorisation
  has to happen. Only `/auth` (one callback URI registered with Google, shared
  by every Organisation) and `/health` (infrastructure) stay at the root.

- **The `/org/<id>/` shape is the old `basePath`.** Calendar subscriptions and
  emailed links already point there, and they are the URLs that cannot be
  corrected once they have left the building. A deployment's Organisation id
  must therefore equal the last segment of the base path it used before, and the
  prefix must have been `/org/`.

- **A session is identity; authority is per Organisation.** The cookie still
  carries only an email. Each request checks it against the allowlists of the
  Organisation in its path, so someone organising two drop-ins signs in once,
  and being signed in reveals nothing about any Organisation you are not on.
  Login is refused only to someone on no Organisation's allowlist at all.

- **Not discoverable means not listed or linked, not secret.** An unknown
  Organisation id is the same 404 as any missing page, for pages, API and
  calendars alike, so ids cannot be probed for. An id may still be guessable;
  the allowlists are what protect an Organisation, not its name.

- **Which Organisations exist is configuration; their rows are not deleted.**
  The `organisation` table holds only the id, so foreign keys have something to
  point at. The display name, the allowlists and the sheets stay in config,
  where an operator sets them (ADR 0006). The server inserts any configured id
  it has not seen at startup and never deletes one: an Organisation dropped from
  config keeps its data and its URL simply 404s.

- **Existing data is adopted, not assigned by name in a migration.** The
  migration that adds the column puts every existing row in a placeholder
  Organisation, because a deployment's real id is its own business and appears
  nowhere in this repo. At startup, if the placeholder still exists and the
  config names exactly one Organisation the database has not seen, the
  placeholder is renamed to it, and every reference follows by `ON UPDATE
  CASCADE`. With more than one such Organisation the server refuses to start
  rather than guess which drop-in the old rota belonged to.

- **The config change is a hard cutover.** The old top-level keys
  (`server.basePath`, `server.organiserEmails`, `server.rotaEditorEmails`,
  `volunteerSheetID`, `serviceVolunteersTab`, `rotaSheetID`) are refused by name
  rather than warned about like other unknown keys: ignoring an allowlist would
  boot a server nobody can sign in to. The unused `gmailUserID` and
  `gmailSender` are simply gone — mail has been sent as the signed-in Organiser
  since the gmail.send grant — and warn like any unknown key.
