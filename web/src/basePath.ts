// BASE_PATH is the path the whole site is served under: "" at the root of a
// domain, "/something" where a deployment has put the site one level down
// (issue #201).
//
// The value belongs to the deployment and appears nowhere in this repo. The
// server rewrites index.html's <base> element as it serves the page, and this
// reads it back — one token, stated once, that the whole frontend derives its
// URLs from.
//
// It is the element's href rather than document.baseURI because href is already
// a path; baseURI is an absolute URL, so using it would mean stripping the
// origin back off to arrive at the same string. Read once at import: the <base>
// element is in <head>, parsed long before this module runs, and nothing
// changes it afterwards.
export const BASE_PATH = readBasePath();

function readBasePath(): string {
  const href = document.querySelector("base")?.getAttribute("href") ?? "/";
  // "/rota/" becomes "/rota", and "/" becomes "". The trailing slash is right
  // in the element, where every relative asset reference resolves against it,
  // and wrong everywhere below, where it would double against a leading slash.
  return href.replace(/\/+$/, "");
}

// apiUrl turns a path the Go server serves — anything under /api or /auth — into
// one this deployment will actually answer. Every fetch in api.ts goes through
// it, and so do the two plain links that bypass the router, because a request
// to /auth/login on a site served at /rota/ is a request to somebody else's
// site.
export function apiUrl(path: string): string {
  return BASE_PATH + path;
}
