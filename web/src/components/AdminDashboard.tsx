import { useEffect, useState } from "react";

type SyncResult = "ok" | "error";

// readSyncResult decodes the ?synced=<result> the server appends when it
// redirects back here after a sync round-trip.
function readSyncResult(): SyncResult | null {
  const value = new URLSearchParams(window.location.search).get("synced");
  if (value === "1") return "ok";
  if (value === "error") return "error";
  return null;
}

// AdminDashboard is the admin-only page. Its sole function for now is syncing
// the volunteer roster from the Google Sheet. Sync is a full-page OAuth redirect
// dance (an incremental Sheets-scope grant against the admin's own Google
// account), so the trigger is a plain link, not a fetch; the server pulls the
// sheet with the admin's token and redirects back here with the outcome.
export default function AdminDashboard() {
  const [result] = useState<SyncResult | null>(readSyncResult);

  useEffect(() => {
    // Strip the ?synced param so refreshing the page doesn't resurrect the
    // banner for a sync that already happened.
    if (result !== null) {
      window.history.replaceState(null, "", "/admin");
    }
  }, [result]);

  return (
    <main className="admin-dashboard">
      <h1>Admin dashboard</h1>

      {result === "ok" && (
        <p className="sync-result sync-result--ok">Volunteers synced.</p>
      )}
      {result === "error" && (
        <p className="sync-result sync-result--error">
          Sync failed. Please try again.
        </p>
      )}

      <section className="admin-panel">
        <h2>Volunteers</h2>
        <p>
          Pull the latest volunteer roster from the Google Sheet. Run this after
          editing the sheet.
        </p>
        <a className="button" href="/auth/sync">
          Sync volunteers
        </a>
      </section>
    </main>
  );
}
