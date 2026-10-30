import { useState, type ReactNode } from "react";
import "./SettingsSection.css";

// SettingsSection is one thing an Organiser decides about how the Organisation runs.
// Each is its own card because they are independent: the Roles, the Rota
// Defaults, the pins made every rota — and nothing about one section should
// have to know how many others there are, or which screen it is being read on.
//
// It lives apart from the settings screen because one of its sections does not:
// the Rota Defaults card is on the define screen too, since defining a rota is
// spending them (issue #176).
export default function SettingsSection({
  title,
  blurb,
  action,
  incomplete = false,
  collapsible = false,
  summary,
  status,
  children,
}: {
  title: string;
  blurb: string;
  // A section setup cannot do without, still empty. It is drawn as wanting
  // attention, since nothing else in Config opens until it is filled in.
  incomplete?: boolean;
  // The one thing this section can be asked to do, in the header rather than
  // below the list — a list that grows would otherwise walk its own button off
  // the bottom of the screen.
  action?: ReactNode;
  // Config starts every section closed, so the page reads as a list of what
  // there is to decide. A section setup still needs stays open: closing it
  // would hide the one thing the page is asking for.
  collapsible?: boolean;
  // What a closed section says about itself beside its title: how many there
  // are, or None.
  summary?: ReactNode;
  // Whether the card saved itself, beside its title: a card edited where it
  // stands says so there rather than somewhere in its body.
  status?: ReactNode;
  children: ReactNode;
}) {
  const [opened, setOpened] = useState(false);
  // A section held open because setup needed it stays open once it is
  // filled in, rather than shutting under the hand that just filled it.
  if (incomplete && !opened) setOpened(true);
  const canClose = collapsible && !incomplete;
  const open = !canClose || opened;

  return (
    <section
      className={`organiser-panel settings-section${incomplete ? " settings-section--incomplete" : ""}${open ? "" : " settings-section--closed"}`}
    >
      <header className="settings-section-head">
        <div>
          <div className="settings-section-title">
            <h2>
              {canClose ? (
                <button
                  type="button"
                  className="settings-toggle"
                  aria-expanded={open}
                  onClick={() => setOpened(!open)}
                >
                  <svg
                    viewBox="0 0 10 10"
                    width="10"
                    height="10"
                    aria-hidden="true"
                  >
                    <path
                      d="M3.5 1L7.5 5L3.5 9"
                      fill="none"
                      stroke="currentColor"
                      strokeWidth="1.8"
                      strokeLinecap="round"
                      strokeLinejoin="round"
                    />
                  </svg>
                  {title}
                  {!open && summary !== undefined && (
                    <span className="settings-summary">{summary}</span>
                  )}
                </button>
              ) : (
                title
              )}
              {incomplete && <span className="settings-needed">Needed</span>}
            </h2>
            {status}
          </div>
          {open && <p className="settings-blurb">{blurb}</p>}
        </div>
        {action && (
          // Making a new one is something to watch happen, so the action
          // opens a closed section on its way to doing it.
          <div
            className="settings-section-action"
            onClickCapture={() => setOpened(true)}
          >
            {action}
          </div>
        )}
      </header>
      {/* Hidden rather than dropped, so a closed card keeps what it was
          still saving and an open one comes back as it was left. */}
      <div hidden={!open}>{children}</div>
    </section>
  );
}
