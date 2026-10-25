package api

import (
	"context"
	"io/fs"
	"net/http"
	"regexp"

	"go.uber.org/zap"
)

// organisationPrefix is where every Organisation is served: /org/<id>/. It was
// the shape of the single drop-in's base path before there could be several,
// and calendar subscriptions and emailed links already point under it, so it
// is permanent (ADR 0011).
const organisationPrefix = "/org/"

// validOrganisationID is the shape an Organisation id takes, the same rule
// config and the organisation table hold it to.
var validOrganisationID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// organisationPath is an Organisation's base path: /org/<id>, no trailing slash.
func organisationPath(id string) string {
	return organisationPrefix + id
}

// Pinger reports whether the database is reachable, for GET /health.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Mount puts the whole server together: each Organisation under its own path,
// and the handful of things that belong to no Organisation at the root.
//
// What sits at the root, and why:
//
//   - /auth/login, /auth/callback and /auth/logout. The callback URI is
//     registered by hand in the Google console, so one shared callback is a
//     one-time step however many Organisations there are; a per-Organisation
//     callback would be a console change each time, forever. Signing in proves
//     who you are, which is the same everywhere.
//   - /health, because it is infrastructure, not site: the deploy workflow,
//     scripts/deploy-config.sh and scripts/dev-stack.sh all poll it.
//   - Everything else is the frontend, served with its base at the root. With
//     no Organisation in its base the app renders the splash at / and its
//     "Page not found" anywhere else; the splash names and links to no
//     Organisation, since an Organisation's address is the only way in
//     (ADR 0011).
//
// An Organisation id that is not configured falls through to that same 404
// page as any other missing path, byte for byte, so ids cannot be probed for.
func Mount(auth *Authenticator, organisations []*Handler, frontend fs.FS, health Pinger, logger *zap.Logger) http.Handler {
	root := http.NewServeMux()
	root.Handle("GET /health", healthHandler(health, logger))
	auth.registerRoutes(root)
	if hasFrontend(frontend) {
		root.Handle("/", rootFrontendHandler(frontend))
	}

	for _, h := range organisations {
		base := h.basePath()
		root.Handle(base+"/", http.StripPrefix(base, h.Routes()))
		// The bare path and the path with its slash are the same page, and the
		// second is what every relative asset reference in index.html resolves
		// against. Permanent: the Organisation lives there for as long as its id
		// is what it is.
		root.Handle(base, http.RedirectHandler(base+"/", http.StatusMovedPermanently))
	}

	return root
}
