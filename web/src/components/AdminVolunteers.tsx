import { useState } from "react";
import Button from "../ui/Button";
import "./AdminVolunteers.css";

type SyncState = "idle" | "syncing" | "ok" | "error";

// AdminVolunteers is the volunteers tab. Its sole function for now is syncing
// the volunteer roster from the Google Sheet. The server reads the sheet with
// its own service account, so a sync is a plain authenticated POST (no OAuth
// redirect dance): the button fires the fetch and reflects the outcome inline.
export default function AdminVolunteers() {
  const [state, setState] = useState<SyncState>("idle");

  async function sync() {
    setState("syncing");
    try {
      const res = await fetch("/auth/sync", { method: "POST" });
      setState(res.ok ? "ok" : "error");
    } catch {
      setState("error");
    }
  }

  return (
    <section className="admin-panel">
      <h2>Volunteers</h2>
      <p>
        Pull the latest volunteer roster from the Google Sheet. Run this after
        editing the sheet.
      </p>
      <Button onClick={sync} disabled={state === "syncing"}>
        {state === "syncing" ? "Syncing…" : "Sync volunteers"}
      </Button>

      {state === "ok" && (
        <p className="sync-result sync-result--ok">Volunteers synced.</p>
      )}
      {state === "error" && (
        <p className="sync-result sync-result--error">
          Sync failed. Please try again.
        </p>
      )}
    </section>
  );
}
