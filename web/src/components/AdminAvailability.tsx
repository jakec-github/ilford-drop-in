import { useState } from "react";
import Button from "../ui/Button";
import { useAvailabilityRound } from "../hooks/useAvailabilityRound";
import type { AvailabilityEntry, AvailabilityRound } from "../types";
import "./AdminAvailability.css";

function formatRange(round: AvailabilityRound): string {
  const format = (date: string) =>
    new Date(date).toLocaleDateString("en-GB", {
      day: "numeric",
      month: "short",
    });
  return `${format(round.start)} – ${format(round.end)}`;
}

// CopyLink is how a round is actually distributed until sending is built, and
// stays the fallback for a volunteer whose email bounces. The link is also shown
// in full: copying can fail silently on an insecure origin or a locked-down
// browser, and an admin who can read it can always select it by hand.
function CopyLink({ link }: { link: string }) {
  const [copied, setCopied] = useState(false);

  return (
    <div className="round-link">
      <code className="round-link-url">{link}</code>
      <Button
        size="small"
        onClick={() => {
          void navigator.clipboard.writeText(link).then(
            () => setCopied(true),
            () => setCopied(false),
          );
        }}
      >
        {copied ? "Copied" : "Copy"}
      </Button>
    </div>
  );
}

// One volunteer's row: whether they have answered, and the link to chase them
// with if not.
//
// A volunteer whose group partner has already answered is marked as covered
// rather than as missing — the group answers as a unit, so chasing them would be
// chasing an answer we already have.
function RoundRow({ entry }: { entry: AvailabilityEntry }) {
  return (
    <li className="round-row">
      <div className="round-row-head">
        <span className="round-name">{entry.volunteerName}</span>
        {entry.replied ? (
          <span className="round-tag round-tag--replied">Replied</span>
        ) : entry.coveredBy.length > 0 ? (
          <span className="round-tag round-tag--covered">
            Covered by {entry.coveredBy.join(" and ")}
          </span>
        ) : (
          <span className="round-tag">No reply</span>
        )}
      </div>
      {entry.replied && (
        <p className="round-answer">
          {entry.availableShiftIds.length === 0
            ? "Available for none of the dates"
            : `Available for ${entry.availableShiftIds.length} of the dates`}
        </p>
      )}
      <CopyLink link={entry.link} />
    </li>
  );
}

// AdminAvailability is the availability tab: start a round for the latest rota,
// then watch the answers come in.
//
// The links are a product feature, not a debug affordance — they are how a round
// is distributed before sending exists, and how a volunteer who lost their email
// gets another one.
export default function AdminAvailability() {
  const { round, error, mintState, mint } = useAvailabilityRound();

  const replied = round?.entries.filter((e) => e.replied).length ?? 0;
  const total = round?.entries.length ?? 0;

  return (
    <section className="admin-panel round">
      <header className="round-head">
        <h2>Availability</h2>
        {round && !round.allocated && (
          <Button
            size="small"
            onClick={() => void mint()}
            disabled={mintState === "minting"}
          >
            {mintState === "minting"
              ? "Starting…"
              : total === 0
                ? "Start round"
                : "Top up round"}
          </Button>
        )}
      </header>

      {error && (
        <p className="round-message round-message--error">
          Could not load the round: {error}
        </p>
      )}

      {round === null && !error && (
        <p className="round-message">Loading the round…</p>
      )}

      {round && (
        <>
          <p className="round-caption">
            Rota {formatRange(round)} · {round.shifts.length} dates
            {round.allocated && " · already allocated, links have closed"}
          </p>

          {total === 0 ? (
            <p className="round-message">
              Nobody has been asked yet. Starting a round gives every active
              volunteer their own link.
            </p>
          ) : (
            <>
              <p className="round-progress">
                {replied} of {total} have answered
              </p>
              <ul className="round-list">
                {round.entries.map((entry) => (
                  <RoundRow key={entry.volunteerId} entry={entry} />
                ))}
              </ul>
            </>
          )}
        </>
      )}
    </section>
  );
}
