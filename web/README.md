# Ilford Drop-in Rota — Web Frontend

A React + TypeScript frontend for viewing volunteer rotas at the Ilford Sunday Drop-in.

## Prerequisites

Install [Bun](https://bun.sh):

```bash
curl -fsSL https://bun.sh/install | bash
```

## Getting Started

The app loads its data from the Go web server, so the easiest way to run it is
the full dev stack from the repository root:

```bash
scripts/dev.sh test   # or: scripts/dev.sh prod
```

This starts the web server (and, for `test`, the PostgreSQL container) plus
the frontend dev server. The app will be available at http://localhost:5173.

To run the frontend on its own (API requests will fail unless the web server
is already running):

```bash
bun install
bun run dev
```

The dev server proxies API requests (`/shifts`, `/alterations`, `/calendars`)
to the web server on localhost:8080; set `API_PORT` to override.

## Build

```bash
bun run build
```

Output goes to `dist/`. Type checking runs before the bundle step.

## Current Features

- Mobile-first rota viewer with desktop table layout
- Highlights the next upcoming shift
- Filter by volunteer name
- Dark mode support

## Stack

- React + TypeScript
- Bun (runtime, package manager, bundler)
