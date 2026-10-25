package api

import (
	"net/http"
)

// maxRequestBody bounds every request body the server reads. The largest thing
// anyone sends is a Shift's Shape or a volunteer's answers — a few kilobytes —
// so a megabyte is generous, and it stops one request holding a body of any
// size in memory. The volunteer's form takes a body before it knows whether
// the link is real, so without this anybody could.
const maxRequestBody = 1 << 20

// contentSecurityPolicy is what a page of this site may load and do. Everything
// the app loads is its own — the build has no inline script and no third-party
// resource — so it is 'self' throughout.
//
// form-action names Google because starting a send is a form whose answer is a
// redirect to Google's consent screen, and browsers hold that redirect to the
// policy too. frame-ancestors keeps the Organiser screens out of anyone else's
// frame, where a page could trick a click onto them.
const contentSecurityPolicy = "default-src 'self'; " +
	"img-src 'self' data:; " +
	"object-src 'none'; " +
	"base-uri 'self'; " +
	"form-action 'self' https://accounts.google.com; " +
	"frame-ancestors 'none'"

// harden wraps the whole server in the protections every request gets,
// whichever Organisation or none it is for:
//
//   - Cross-origin protection: a write — anything but GET, HEAD or OPTIONS —
//     from a page on another origin is refused. The session cookie is
//     SameSite=Lax, which keeps it off most cross-site requests but not off one
//     from a sibling host such as preprod's, which browsers count as the same
//     site. This checks the origin itself, from the Sec-Fetch-Site header
//     browsers send or, failing that, Origin against Host. A request with
//     neither is not from a browser and passes — curl and the tests among them.
//   - A cap on the body (maxRequestBody).
//   - Security headers on every response. Strict-Transport-Security only when
//     hosted (secure): it is a promise about HTTPS, and dev runs over HTTP.
func harden(next http.Handler, secure bool) http.Handler {
	protected := http.NewCrossOriginProtection().Handler(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := w.Header()
		header.Set("Content-Security-Policy", contentSecurityPolicy)
		header.Set("X-Content-Type-Options", "nosniff")
		header.Set("X-Frame-Options", "DENY")
		// An availability link carries its token in the path, and it is a bearer
		// credential: nothing the page links to should be told where it came from.
		header.Set("Referrer-Policy", "no-referrer")
		if secure {
			header.Set("Strict-Transport-Security", "max-age=31536000")
		}
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
		}
		protected.ServeHTTP(w, r)
	})
}
