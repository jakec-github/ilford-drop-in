# Ilford drop-in scripts

IN DEVELOPMENT

This repository contains a set of scripts to help organise the Ilford drop in centre which provides food and support to vulnerable community members in Ilford, London.

## Requirements

- yarn ~1
- node >=18

## Prerequisites

### Google resources

- A gmail account
- A google cloud project
  - The gmail, spreadsheet and form APIs need enabling
- A service account in the project with access to the relevant spreadsheets
  - Ilford drop-in
  - Form IDs
  - Ilford drop-in rota
- A desktop oAuth2 client

### Codebase

A secrets directory in this directory with the following files:

- `confidential.json`
- `serviceAccount.json`
- `oauthClient.json`

`confidential.json` must include:

- "volunteerSheetID": \<Volunteer spreadsheet ID\>
- "serviceVolunteersTab": \<Tab name for service volunteers\>
- "rotaSheetID": \<Rota spreadsheet ID\>
- "formSheetID": \<Form spreadsheet ID\>
- "gmailUserID": \<User ID for gmail\>
- "gmailSender": \<Sender for gmail\>

`serviceAccount.json` is supplied by google when creating the service account. Ask for this if needed

`oauthClient.json` can be downloaded from the google cloud console page

Run `yarn install` before executing the script

## Executing

Use `yarn run script`

Pass the help flag to see commands and options `yarn run script --help`
