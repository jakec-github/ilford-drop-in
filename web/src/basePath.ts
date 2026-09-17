// BASE_PATH is the path the app's own pages are served under: "" at the root of
// a domain, "/something" where a deployment has put the site one level down
// (issue #201).
//
// The value belongs to the deployment and appears nowhere in this repo. The
// server rewrites index.html's <base> element as it serves the page, and this
// reads it back — one token, stated once, that the router and the two links
// leaving the app derive their URLs from.
//
// The API and /auth are not under it and need nothing from this: they are
// requested with root-absolute paths, and a leading slash resets the path when
// a URL is resolved, so the <base> element does not touch them.
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
