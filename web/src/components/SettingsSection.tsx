import type { ReactNode } from "react";
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
  children: ReactNode;
}) {
  return (
    <section
      className={`organiser-panel settings-section${incomplete ? " settings-section--incomplete" : ""}`}
    >
      <header className="settings-section-head">
        <div>
          <h2>
            {title}
            {incomplete && <span className="settings-needed">Needed</span>}
          </h2>
          <p className="settings-blurb">{blurb}</p>
        </div>
        {action}
      </header>
      {children}
    </section>
  );
}
