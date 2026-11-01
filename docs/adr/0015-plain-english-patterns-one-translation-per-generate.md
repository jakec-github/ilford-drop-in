# Plain-English patterns: one stateless translation per Generate

Status: proposed, 2026-11-01. Builds on [ADR 0014](0014-pattern-entries-carry-their-own-start-and-length.md);
ADR 0013's one evaluator stands.

## In plain words

An Organiser can describe the Shift pattern in plain English — "Sundays 7:30 to
10:30pm, and weekdays 9am to noon" — and press **Generate**. A language model
turns the description into the pattern's VEVENT entries, or lists everything
that stops it doing so, the way a password field lists the rules a password
breaks. They edit the text and press Generate again. Once it translates, the
pattern is saved and the server's own description of each entry replaces the
input; clicking that description opens the input again, filled in with it.

The same works for what an Override **matches**. What it sets — start time,
length, Shape, Pins — stays as form fields.

The raw editor stays, behind a natural/raw switch, and is all there is where
the model is not available.

## Decisions and their reasons

- **A form field, not a chat.** Each Generate is one stateless translation of
  the whole description: no history, no follow-up questions. A conversation
  would make the pattern depend on everything said so far; a field makes it a
  function of what is in the box, which an Organiser can read and fix, and which
  can be tested.

- **The reply is the pattern or every issue at once.** Issues are `missing`,
  `ambiguous` or `unsupported`, each a short message naming the part it is about,
  with optional examples. All of them come back together: rules that surface one
  at a time make the field feel like an interrogation.

- **Only the iCalendar is kept.** The typed description and the model's issues
  are not stored, and the readable version is always the server's
  description from the one evaluator (ADR 0013), never the model's. The model
  may misunderstand; the description it is checked against cannot disagree with
  what will be minted. Recording attempts for tracing and evaluation is a later,
  separate decision about an operational record — it would never become part of
  the pattern.

- **Translating never saves.** The translate endpoint returns the pattern and
  its entries as the preview does; the card saves it through the one path a raw
  edit uses, so the two modes cannot drift and a failed Generate cannot touch
  the saved pattern.

- **The server checks the model's work against the parser and lets it try
  again.** When a reply translates but the parser refuses it, the parser's
  problems go back to the model in the same exchange, up to three calls in all.
  Warnings ("UID is ignored") count as clean, and an `issues` reply is an answer,
  never retried. After the third failure the Organiser sees "Unable to generate a
  pattern from that description. Please try an alternative", followed by the
  parser's own messages.

- **Structured outputs define the reply.** The request carries a JSON Schema, an
  `anyOf` of the two shapes, so a reply always parses and cannot be both `ok`
  and `issues`. The schema cannot say a string is valid iCalendar — that stays
  the parser's job — and a `refusal` or `max_tokens` stop counts as a failed call.

- **Sonnet 5.5 at low effort, named in server config.** A wrong `ok` saves a bad
  pattern, so being right counts for more than price at a handful of Generates a
  month; in the playground Haiku 5.5 once raised an issue that was not there.
  Low effort because it is a short, bounded translation behind a button.
  Choosing between models on evidence is for when there are evaluations to
  choose by.

- **No provider abstraction.** The Anthropic Go SDK is called directly behind a
  small `Translator` interface. The interface is there for tests and the dev
  stack, not for a second provider.

- **Available by opt-in, per Organisation.** One server-wide API key; an
  Organisation gets the natural editor only when its entry in the deployment
  config says so, so a new deployment does not call a paid API until someone
  chooses it. Without the key or the opt-in the card opens in raw with the switch
  disabled and the reason on hover.

- **Spending is capped outside the app.** The key belongs to an Anthropic
  workspace of its own with a monthly spend limit. Reaching it is a failed call
  like any other — "Couldn't translate just now", with a link to raw — rather
  than something the app counts.

- **Bounded.** Thirty seconds for a whole Generate, every call included, tied to
  the request so a closed page stops it; about 2,000 output tokens a call; no
  streaming, since a half-written reply is no use to a form; a 1,000-character
  description, refused by the server before any call is made.

- **No prompt caching.** At this volume most Generates would pay the write and
  never read it back. It is one field to add once there are numbers to justify
  it.

- **The prompt is code.** Each translator's system prompt and schema are
  embedded in the server and versioned with it. The system prompt is the same on
  every call; what varies — today's date in the Organisation's zone, the zone,
  and the description — goes in the user turn, with the description marked as
  data.

- **An Override's translator matches; it never sets.** Natural language covers
  only an Override's rule, its `DTSTART;VALUE=DATE` and RRULE. Its setters stay
  form fields, keeping what an Override matches apart from what it changes
  (ADR 0014). Because a matcher picks out the pattern's Shifts, this translator
  is also given the saved pattern's descriptions, and is unavailable until there
  is one. It names a time only to pick one of several Shifts on a day. A rule it
  writes is saved like a typed one even if it matches nothing: the description
  and the calendar are the check, as they are for raw (ADR 0010).

## Considered and rejected

- **A chat that asks follow-up questions.** Tried in the playground: more turns,
  and a pattern that depended on the conversation rather than on any text the
  Organiser could see.

- **Storing the description as typed, and showing it back.** Two sources for
  one pattern that drift the moment either is edited in raw. The server's
  description is regenerated from what is stored, so it always matches.

- **Letting the model write the readable version.** It would describe what it
  meant rather than what was saved.

- **One flat reply object with optional fields.** Simpler to write, but it
  admits `ok` with issues; `anyOf` makes that unrepresentable.

- **Translating the Override's setters too.** One sentence would then say both
  which Shifts and what changes, the blur ADR 0014 kept out.

- **Refusing a translated Override rule that matches no Shift.** Stricter than
  the save endpoint for no gain an Organiser cannot get from the description.

## Consequences

- A dev-mode translator stands in for the model, so the natural editor can be
  driven on the dev stack, and tested, without a key or a bill. Neither
  `scripts/check.sh` nor CI calls the model.

- Natural is where the card opens when it is available; switching to raw is not
  remembered. The switch is disabled while a raw edit has unsaved problems, so
  switching never drops one.

- A pattern of more than about fifteen entries reopens as more than 1,000
  characters of description and has to be edited in raw.

- The playground version of the pattern prompt is in
  [`docs/design/llm/`](../design/llm/pattern-translator-playground.md); the
  embedded prompt is the one that runs.
