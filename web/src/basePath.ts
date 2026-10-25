// BASE_PATH is the path this drop-in is served under: "/org/<id>" (ADR 0011).
// One server runs several Organisations and one build serves them all, so the
// page cannot know which it is until it is served.
//
// The server rewrites index.html's <base> element as it serves the page, and
// this reads it back — one token, stated once, that the router, every API
// request and the links leaving the app derive their URLs from. Only signing
// out is not under it: the session is the whole site's.
//
// It is the element's href rather than document.baseURI because href is already
// a path; baseURI is an absolute URL, so using it would mean stripping the
// origin back off to arrive at the same string. Read once at import: the <base>
// element is in <head>, parsed long before this module runs, and nothing
// changes it afterwards.
export const BASE_PATH = readBasePath();

function readBasePath(): string {
  const href = document.querySelector("base")?.getAttribute("href") ?? "/";
  // "/org/east/" becomes "/org/east", and "/" becomes "". The trailing slash is right
  // in the element, where every relative asset reference resolves against it,
  // and wrong everywhere below, where it would double against a leading slash.
  return href.replace(/\/+$/, "");
}
