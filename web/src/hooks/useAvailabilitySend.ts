import { useCallback, useEffect, useRef, useState } from "react";
import { useLocation, useSearch } from "wouter";
import { fetchSend, sendUrl } from "../api";
import type { AvailabilitySend, SendMode } from "../types";

// How often a running send is asked how far it has got. A send moves once every
// three seconds — Gmail's throttle — so anything much tighter is asking a
// question whose answer cannot have changed.
const POLL_INTERVAL_MS = 2000;

interface UseAvailabilitySend {
  // The send this page came back to, or null when it did not come back to one.
  send: AvailabilitySend | null;
  // Set when a send never started: the admin declined at the consent screen, or
  // Google refused. Distinct from a send that ran and failed on some addresses,
  // which is reported inside the send itself.
  error: string | null;
  // Starts a send. It navigates the whole page rather than fetching, because the
  // server answers with a redirect to Google's consent screen.
  start: (mode: SendMode, deadline: string, volunteerId?: string) => void;
  // Clears the finished send off the screen by taking its id out of the URL, so
  // a reload does not bring the same report back.
  dismiss: () => void;
}

// useAvailabilitySend owns the half of a send that happens after the redirect.
//
// A send is not a request this page makes: it is a full-page trip out to Google
// for the gmail.send grant, which lands back here with a job id in the query.
// Everything the admin sees of it — how far it has got, who it reached, who it
// failed on — is read back from that id. The URL is therefore the state, which
// is why dismissing means navigating rather than setting a flag: a reload has to
// land on the same screen the admin last saw.
//
// onFinished is called once the send stops, because the round it acted on has
// changed underneath the page — every volunteer it reached now carries a sent
// stamp they did not have before.
export function useAvailabilitySend(
  onFinished?: () => void,
): UseAvailabilitySend {
  const [location, navigate] = useLocation();
  const params = new URLSearchParams(useSearch());
  const jobID = params.get("send");
  const consentError = params.get("sendError");

  const [send, setSend] = useState<AvailabilitySend | null>(null);
  const [pollError, setPollError] = useState<string | null>(null);

  // Held in a ref rather than depended on: it is a reload callback whose
  // identity changes every render, and restarting the poll on it would restart
  // the reload it triggers.
  const onFinishedRef = useRef(onFinished);
  useEffect(() => {
    onFinishedRef.current = onFinished;
  });

  useEffect(() => {
    if (jobID === null) return;

    let cancelled = false;
    let timer: ReturnType<typeof setTimeout> | undefined;

    const poll = () => {
      fetchSend(jobID)
        .then((latest) => {
          if (cancelled) return;
          setSend(latest);
          if (latest.finished) {
            onFinishedRef.current?.();
            return;
          }
          timer = setTimeout(poll, POLL_INTERVAL_MS);
        })
        .catch((err: unknown) => {
          if (cancelled) return;
          setPollError(
            err instanceof Error ? err.message : "Failed to read the send",
          );
        });
    };

    poll();

    return () => {
      cancelled = true;
      if (timer !== undefined) clearTimeout(timer);
    };
  }, [jobID]);

  const start = useCallback(
    (mode: SendMode, deadline: string, volunteerId?: string) => {
      // A hard navigation, not a wouter one: the target is a server redirect out
      // to Google, which the SPA router has no way of following.
      window.location.assign(sendUrl(mode, deadline, volunteerId));
    },
    [],
  );

  // Navigating to the bare path drops both query parameters, which is what
  // takes the report off the screen — everything below is read from them.
  const dismiss = useCallback(() => {
    navigate(location, { replace: true });
  }, [navigate, location]);

  return {
    send: jobID === null ? null : send,
    error: consentError ?? (jobID === null ? null : pollError),
    start,
    dismiss,
  };
}
