# Ilford Drop-in Rota — Web Frontend

A React + TypeScript frontend for viewing volunteer rotas at the Ilford Sunday Drop-in.

## Prerequisites

Install [Bun](https://bun.sh):

```bash
curl -fsSL https://bun.sh/install | bash
```

## Getting Started

```bash
bun install
bun run dev
```

The app will be available at http://localhost:5173.

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
