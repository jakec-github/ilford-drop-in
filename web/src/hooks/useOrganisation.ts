import { useEffect, useState } from "react";
import { fetchOrganisation } from "../api";
import type { Organisation } from "../types";

// The drop-in a page is under never changes while it is open, so it is read
// once and every caller shares the answer.
let organisation: Promise<Organisation> | null = null;

function load(): Promise<Organisation> {
  organisation ??= fetchOrganisation().catch((err: unknown) => {
    // Let the next mount try again rather than keep a failure for the life of
    // the page.
    organisation = null;
    throw err;
  });
  return organisation;
}

// useOrganisation is which drop-in this site is: null until it has loaded, and
// null for good if it fails. The name is a heading and a tab title, so a page
// without it is still a working page and nothing waits on it.
export function useOrganisation(): Organisation | null {
  const [loaded, setLoaded] = useState<Organisation | null>(null);

  useEffect(() => {
    let current = true;
    load()
      .then((org) => {
        if (current) setLoaded(org);
      })
      .catch(() => {
        // Nothing to say: the page reads the same without its name.
      });
    return () => {
      current = false;
    };
  }, []);

  return loaded;
}
