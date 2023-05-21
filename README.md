# Ilford drop-in scripts

IN DEVELOPMENT

This repository contains a set of scripts to help organise the Ilford drop in centre which provides food and support to vulnerable community members in Ilford, London.

## Requirements

- yarn ~1
- node >=18

## Prerequisites

A google service account has been created with access to volunteer info

Create a secrets directory with the following files:

- `confidentialData.json`
- `serviceAccount.json`

`confidendtial.json` must include:

- `"spreadsheetID": \<Volunteer spreadsheet ID\>,
- `"serviceVolunteersTab": \<Tab name for service volunteers\>,

`serviceAccount.json` is supplied by google when creating the service account. Ask for this if needed

Run `yarn install` before executing the script
