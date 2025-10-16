# Current state

I have a CLI tool written in TS that manages rota generation. Data about volunteers is stored in a google spreadsheet that the team keeps up-to-date. The script uses a couple of other spreadsheets to track state as it creates rotas. The rota is published to a public spreadsheet once created

The tool has the following commands:

`createForms`
Reads volunteers sheet to get active volunteers
Creates a form for each active volunteer requesting them to select dates in the upcoming rotation that they cannot do

`sendForms`
Emails each form to each volunteer

`allocateRota`
Uses the availability data to generate a rota and publishes it to a spreadsheet
There are a lot of requirements and optimisations to consider at this point

There are a handful of additional commands for adding people and viewing responses. The tool has limited config that allows admins to define rrules with prefilled allocations.

## Limitations of the existing model

Poorly thought out data model. Basically just human readable spreadsheets which hasn’t proved easily extensible.

Poor code quality as the project has evolved over time with little time to work on it. Some simplifications I made early on became burdens as the project grew. Absolutely no tests which make changes increasingly risky.

The rota generation algorithm is clunky and sub-optimal. Quite difficult to work with.

# Proposed state

I have a new repo with an improved version of the CLI tool.

## Data model

The tool has an improved data model for internal state. It continues to use Google sheets but uses a defined set of normalised tables. Each table is a tab in a single sheet for convenience. Reads and writes to this sheet are treated as database queries and perhaps even written in SQL. Continuing to use sheets gives me solid data durability and the ability to easily inspect data. Also reduced overhead.

## Code quality

Code is designed with the current spec (And some future considerations in mind). Extensive unit testing is included. Modules/packages are more sensible.

## Commands

`defineRota`
A new command to make the creation of rotas explicit. Since the schedule is always the same this just accepts a number of weeks and starts after the latest rota.

`requestAvailability`
Combination of the existing createForms and sendForms commands. This command should know whose availability has already been requested in the case where new volunteers have been made active and only reach out to them (This won’t be perfect but is good enough).

`sendAvailabilityReminders`
Fetch volunteers who have not responded and nudge them via email to respond.

`viewResponses`
Allows me to see who has responded and when they are available for each shift in the upcoming rota. This already exists. Should be ported over.

`allocateRota`
Retrieves volunteer availability and generates a suitable rota.

Requirements:

- Schedule one team lead per shift
- Schedule n total volunteers per shift (n Defined on the rota)
- Only schedule team lead as team lead
- Only schedule volunteers on dates they are available
- Always schedule grouped volunteers together
- Do not schedule volunteers on 2 shifts in a row (including checking previous rota)
- Apply adjustments from config

Optimisations:

- Spread out males to make sure we have some on every shift
- Spread out shifts so that shifts don’t get clumped together

This function should be built flexibly. Ideally at least some requirements/optimisations should be defined separately and then integrated into the function for quick swapping out. Requirements may change e.g. introducing a shadow lead or giving team leads ordinary allocations.

The rota should then be viewable. Should have a dry run mode that just shows the rota that it would generate.

`viewRota`
Just show the output of the last command again (may reflect manual alterations or just serve as a reminder)

`publishRota`
Once I am happy with the rota I want to be able to automatically publish it to the public spreadsheet.

`addCover`
We should possibly have a command that I can use to reflect swaps and covers by volunteers. This would update the underlying data and republish the rota. But the downside is that only I can make the change.

## Config

In addition to specifying secrets, sheet URLs etc. Users should be able to override the behaviour of the generator using rrules. Currently, they can prefill some of the allocations in a shift but in this version they should also be able to adjust the desired shift size.

## Extensible

Modularised so that I can use the same code to power a web app or potentially swap out the data layer etc. in the future.

Well tested to make maintenance safer. Solid at the unit level. Property testing on rota generation. Possible integration testing against a dummy sheet.

Written in golang. Honestly, this is mainly to get some more experience using it.

## Limitations

This design aims to minimise developer cost and maintenance. As such it does not need to support all scenarios and workflows. It also does not correctly support DB transactions, concurrent writes and high request loads. These limitations have been explicitly considered and are deemed acceptable for this case.

## Future state

Some simple extensions would be:
Coverage of food collection and hot food rotas
Allowing rotas to be specified in terms of months
Ensuring shift allocations are spread evenly between volunteers over several consecutive rotas.

In future I would like to extend this tool to run as a server. This will allow it to operate automatically and serve a mobile-first web app.

Desired web app features:

- Auth via google
- `viewRota` page
- Calendar subscription
- Admin tools

Other:

- RSS calendar feeds for volunteers
- Automated lifecycle events
