# Organiser's guide

This guide walks you through setting up your organisation's rota in Rota Wise, then
running your first one. Each step happens on one of the tabs of the
**Organiser** page.

## Before you start

Whoever runs your Rota Wise server sets three things up for you:

- **Your organisation's address.** It looks like `https://<site>/org/<your-drop-in>/`.
  Share it with your volunteers: it is where they find the rota. Rota Wise never
  lists or links to an organisation, so this address is the only way in.
- **Who can sign in.** Organisers and Rota Editors sign in with a Google
  account, and only the email addresses on your organisation's lists are let in.
  Organisers can change everything; Rota Editors can only change who works a
  shift. Ask the person running the server to add or remove someone.
- **Your roster sheet.** A Google Sheet listing your volunteers, which Rota Wise
  reads (see Volunteers, below).

To sign in, open your organisation's address, add `auth/login` to the end, and choose
your Google account. Once you are signed in, the Organiser page is at
`organiser/volunteers` under the same address.

## Volunteers

Your volunteers live in the roster sheet, not in Rota Wise. To add someone,
change someone's details or stand someone down, edit the sheet, then press
**Sync** on the **Volunteers** tab so Rota Wise reads it again.

The sheet has one row per volunteer, with these columns:

| Column | What goes in it |
| --- | --- |
| Unique ID | Anything that never changes and is never reused for someone else. |
| First name, Last name | How they appear on the rota. |
| Roles | The jobs they do, separated by commas. Each must match a Role's name on Config exactly. |
| Status | `Active`, or anything else to leave them off new rotas. |
| Sex/Gender | Used by the optional rule that puts a male on every shift. |
| Email | Where their availability requests go. |
| Group key | People with the same key are asked, and rostered, together — a couple, say. Leave blank for most people. |

Other columns are ignored, so you can keep your own notes alongside.

After a sync, check the tab: it shows how many active volunteers you have and
how many hold each Role, then lists everybody on the sheet.

## Config

The **Config** tab, the last one, holds what every new rota is made from. Work
through it top to bottom the first time. Until the **Roles** and the **Rota**
are set, both are outlined in red and marked **Needed**, the rest of the tab
stays closed, and the rota page and the Allocation tab send you here.

Once set up, each card starts folded to its title and how many it holds
(**None** when it is empty); click the title to open it. The **+** stays on a
folded card, and opens it as it adds the new one. A card marked **Needed**
cannot be folded until it is filled in.

Everything on Config is edited where it stands. There is no Save button: a
change saves itself a moment after you stop typing, and the card says
**Saving…** and then **Saved**. Pencil buttons across the app edit something;
plus buttons add something.

### Time zone

Where the organisation runs. Every time on Config is the time on the clock
there, so 19:30 stays 19:30 when the clocks change. The first time Config opens
it is set to your browser's zone; pick another from the list if that is wrong.

### Roles

The jobs volunteers do on a shift. Press the **+** for each one and type its
name. A Role's name must match the roster sheet's `Roles` column exactly. Each
Role shows how many active volunteers hold it.

Choose names carefully: a Role can be renamed but never deleted. If you rename
one, change the sheet to match — until you do, nobody holds it.

The order of the list decides which Roles the allocator fills first when there
are not enough people to go round. Drag a Role by the handle on its right to
reorder them, or focus the handle and use the arrow keys.

After you add, rename or reorder a Role, the roster is re-read so volunteers
pick up the change.

### Rota

- The **shift pattern** says when shifts happen, as one entry per kind of
  shift. Each entry gives the first shift (`DTSTART`), how long it runs
  (`DURATION`) and how it repeats (`RRULE`) — for every Sunday at 19:30 for
  three hours:

  ```
  BEGIN:VEVENT
  DTSTART:20261101T193000
  DURATION:PT3H
  RRULE:FREQ=WEEKLY;BYDAY=SU
  END:VEVENT
  ```

  An event copied out of a calendar works too; anything in it the app does not
  need is pointed out and left alone. Each entry is read back in plain English
  as you type, and a calendar shows the shifts it makes. "Every other week"
  counts from each entry's own first shift. A pattern is saved only once every
  entry of it reads.
- The **shape** sets how many of each Role a shift asks for. These are the
  most the allocator will place, not a minimum: a seat can be left empty. It
  cannot be set until at least one Role exists.

Changing the Rota only affects rotas you define afterwards. A rota you have
already defined keeps what it started with, and you change it on the rota
itself.

### Overrides

An override changes some of the shifts your pattern makes, each time you define
a rota — for example, pinning the person who leads on the first Sunday of the
month, starting the last Saturday of the month two hours early, or asking for
fewer people over Christmas. Press the **+** and a new override appears, ready
to edit. Each has two parts.

**Matches** says which shifts it changes:

- A rule: the date it counts from, then which dates it picks, such as

  ```
  DTSTART;VALUE=DATE:20261101
  RRULE:FREQ=MONTHLY;BYDAY=1SU
  ```

  for the first Sunday of every month from 1 November. A new Override starts
  from today. Nothing before its date is matched. It is read back in plain
  English as you type, and saved only once it reads. A rule that names a time
  (`BYHOUR` and `BYMINUTE` together) matches only the shift that starts then.
  One that names no time matches every shift on its dates.

**Sets** says what is different about those shifts:

- Tick **Start time** to move them to another time on the same day. They still
  run for the pattern's length from there, unless you also tick **Length**.
- Tick **Length** to make them longer or shorter. A shift long enough runs on
  past midnight.
- Tick **Shape** to ask for a different number of each Role. It starts from the
  Rota's shape.
- Add **Pins** for anyone who should always be placed on them.

Overrides apply from the top of the list down, so when two change the same
shift, the lower one wins. Drag one by the handle on its right to reorder
them, or focus the handle and use the arrow keys. When you define a rota, its
preview marks each shift an override changes, says where a moved shift would
have started, and warns you about an override that matches nothing. A moved or
lengthened shift that would run into another is marked, and the rota cannot be
defined until you change one of them. The next rota carries on from where your
pattern got to, whatever an override moved. Everyone else you pin on the rota itself.

### Allocation rules

The optional rules the allocator follows — for example, capping how often one
person works, or keeping anybody off back-to-back shifts. Press the **+** to
pick one from the rule library. They only guide the allocator. You can always
place someone by hand.

## Your first rota

Everything from here happens on the **Allocation** tab.

### 1. Define the rota

Under **Define the next rota**, enter how many shifts it covers. The shifts it
would take are listed as you type: the next ones your shift pattern makes after
the last rota stopped. Press **Define rota**.

For a break, press **Leave a gap…** and pick a date no shift may start before.

This creates the shifts from your Rota and gives every active volunteer
their own availability link. Nobody is emailed yet.

There is only ever one rota being worked on — **the rota in flight**. If you
make a mistake, **Discard rota** throws it away, along with any answers already
collected, and you can start again.

### 2. Adjust the shifts

Each shift in the **Draft rota** has its own controls:

- Press the date to change when that shift runs.
- **Shape** changes how many people that one shift asks for.
- **Close** marks a date the organisation is not running, such as a bank holiday.
- **+** pins someone on the shift before the allocator runs, whatever
  the rules say.

You can keep adjusting the shifts while answers come in.

### 3. Collect availability

In **Availability**, press **Send round** to email every volunteer their link.
Each one opens a short form, with no sign-in, where they tick the dates they can
do. They can come back and change their answer until the rota is allocated.

Volunteers who forget can be nudged with **Send reminders**. Use **Regenerate volunteers**
after adding new volunteers to the sheet, so they get a link too.

The responses grid shows who has replied and, for each shift and Role, how many
people are available against how many are needed. A negative figure is a
shortfall to chase.

### 4. Check the draft

The draft rota fills itself in from the answers so far, following your rules,
and updates as things change. It says how many seats it filled and which are
still empty. Use pins to settle anything you want decided a particular way.

### 5. Allocate

When you are happy, press **Allocate rota**. The draft becomes the rota:
volunteers see it on your organisation's address, and in their calendar if they
subscribed. The availability links close.

## After the rota is published

Plans change. On the rota page, an Organiser or a Rota Editor can add someone to
a shift or take someone off. Taking somebody off without putting anyone in their
place asks for a reason, so everyone can see why the gap is there.

When it is time for the next rota, go back to the Allocation tab and define it.

## What volunteers see

- **The rota page**, at your organisation's address. Volunteers can pick their own
  name to see just their shifts, and copy a calendar link that keeps their
  phone's calendar up to date.
- **The availability form**, reached only through the link emailed to them. It
  never asks them to sign in.
