import { useState } from "react";

type SyncState = "idle" | "syncing" | "ok" | "error";

// AdminDashboard is the admin-only page. Its sole function for now is syncing
// the volunteer roster from the Google Sheet. The server reads the sheet with
// its own service account, so a sync is a plain authenticated POST (no OAuth
// redirect dance): the button fires the fetch and reflects the outcome inline.
export default function AdminDashboard() {
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
    <main className="admin-dashboard">
      <h1>Admin dashboard</h1>

      <section className="admin-panel">
        <h2>Volunteers</h2>
        <p>
          Pull the latest volunteer roster from the Google Sheet. Run this after
          editing the sheet.
        </p>
        <button
          className="button"
          type="button"
          onClick={sync}
          disabled={state === "syncing"}
        >
          {state === "syncing" ? "Syncing…" : "Sync volunteers"}
        </button>

        {state === "ok" && (
          <p className="sync-result sync-result--ok">Volunteers synced.</p>
        )}
        {state === "error" && (
          <p className="sync-result sync-result--error">
            Sync failed. Please try again.
          </p>
        )}
      </section>
    </main>
  );
}
