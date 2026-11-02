import { useId, useState } from "react";
import type { KeyboardEvent, ReactNode } from "react";
import type { PersonRef, Volunteer } from "../types";
import "./VolunteerPicker.css";

// VolunteerPicker is the one way an Organiser picks somebody outside the
// published rota (issue #336). It is a search box over the
// roster whose last row offers to use whatever was typed as somebody not on
// it, so a custom entry is no longer a mode to be found at the bottom of a
// list — it is what typing a name the roster lacks leads to.
//
// It says who, and nothing else. Which Role they take, and how the chosen
// person is drawn beside it, differ between the screens that use it, so each
// keeps its own.
export default function VolunteerPicker({
  id,
  label,
  volunteers,
  volunteersError,
  value,
  onChange,
  allowCustom = true,
  placeholder,
  className,
}: {
  id?: string;
  // The accessible name, for a caller with no visible label of its own. One
  // with a <label htmlFor> passes id instead.
  label?: string;
  // null while the roster is loading, or when it failed. Already filtered to
  // the people this screen may offer.
  volunteers: Volunteer[] | null;
  volunteersError: string | null;
  value: PersonRef | null;
  onChange: (person: PersonRef | null) => void;
  // Off where only somebody on the roster is an answer.
  allowCustom?: boolean;
  placeholder?: string;
  className?: string;
}) {
  const ownId = useId();
  const inputId = id ?? ownId;
  const listId = `${inputId}-list`;
  // What has been typed since the last pick, or null to show the pick itself.
  // Holding it this way lets a caller clear the field by setting value to null.
  const [typed, setTyped] = useState<string | null>(null);
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(-1);

  const loading = volunteers === null && volunteersError === null;
  const query = (typed ?? "").trim();
  const shown = typed ?? nameOf(value, volunteers);

  // Active volunteers first, then the inactive, each by name. The inactive are
  // listed rather than hidden: being inactive stops the allocator choosing
  // somebody, not a person picking them (ADR 0010).
  const matches = (volunteers ?? [])
    .filter((v) => v.fullName.toLowerCase().includes(query.toLowerCase()))
    .sort(
      (a, b) =>
        Number(b.active) - Number(a.active) ||
        a.fullName.localeCompare(b.fullName),
    );
  const rows: PersonRef[] = matches.map((v) => ({ volunteerId: v.id }));
  if (allowCustom && query !== "") rows.push({ custom: query });

  function pick(person: PersonRef) {
    setTyped(null);
    setOpen(false);
    setActive(-1);
    onChange(person);
  }

  function move(to: number) {
    setOpen(true);
    setActive(to);
    document
      .getElementById(`${listId}-${to}`)
      ?.scrollIntoView?.({ block: "nearest" });
  }

  function onKeyDown(e: KeyboardEvent<HTMLInputElement>) {
    if (e.key === "ArrowDown") {
      e.preventDefault();
      if (rows.length > 0) move(Math.min(active + 1, rows.length - 1));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      if (rows.length > 0) move(Math.max(active - 1, 0));
    } else if (e.key === "Enter") {
      // Picking, not submitting the form the picker sits in.
      if (open && rows[active]) {
        e.preventDefault();
        pick(rows[active]);
      }
    } else if (e.key === "Escape" && open) {
      // Closes the list, not the dialog or popover around it.
      e.preventDefault();
      e.stopPropagation();
      setOpen(false);
    }
  }

  let hint: ReactNode = null;
  if (loading) hint = "Loading the roster…";
  else if (volunteers !== null && query !== "" && matches.length === 0)
    hint = "Nobody on the roster matches.";
  else if (allowCustom && query === "")
    hint = "Not on the roster? Just type their name.";

  const expanded = open && (rows.length > 0 || hint !== null);

  return (
    <div className={["volunteer-picker", className].filter(Boolean).join(" ")}>
      <div className="volunteer-picker-field">
        <svg
          className="volunteer-picker-icon"
          viewBox="0 0 16 16"
          aria-hidden="true"
        >
          <circle cx="7" cy="7" r="4.5" />
          <path d="M10.5 10.5 14 14" />
        </svg>
        <input
          id={inputId}
          className="volunteer-picker-input"
          type="text"
          role="combobox"
          aria-label={label}
          aria-autocomplete="list"
          aria-expanded={expanded}
          aria-controls={listId}
          aria-activedescendant={
            expanded && rows[active] ? `${listId}-${active}` : undefined
          }
          autoComplete="off"
          // Typing a name needs nothing from the roster, so only a picker
          // that takes nobody else waits for it.
          disabled={!allowCustom && volunteers === null}
          placeholder={
            placeholder ??
            (loading
              ? "Loading the roster…"
              : allowCustom
                ? "Search the roster, or type any name"
                : "Search the roster")
          }
          value={shown}
          onFocus={() => setOpen(true)}
          onBlur={() => {
            setOpen(false);
            // Wandering off mid-search leaves the pick as it was.
            if (value !== null) setTyped(null);
          }}
          onChange={(e) => {
            setTyped(e.target.value);
            setOpen(true);
            setActive(e.target.value.trim() === "" ? -1 : 0);
            if (value !== null) onChange(null);
          }}
          onKeyDown={onKeyDown}
        />
      </div>
      {expanded && (
        <div className="volunteer-picker-popup">
          {hint && <p className="volunteer-picker-hint">{hint}</p>}
          <ul id={listId} className="volunteer-picker-list" role="listbox">
            {rows.map((person, i) => {
              const v =
                "volunteerId" in person
                  ? matches.find((m) => m.id === person.volunteerId)
                  : undefined;
              return (
                <li
                  key={v ? v.id : "custom"}
                  id={`${listId}-${i}`}
                  role="option"
                  aria-selected={i === active}
                  className={
                    v
                      ? "volunteer-picker-option"
                      : "volunteer-picker-option volunteer-picker-custom"
                  }
                  // Keeps the focus in the field, so the press is not a blur.
                  onMouseDown={(e) => e.preventDefault()}
                  onMouseEnter={() => setActive(i)}
                  onClick={() => pick(person)}
                >
                  {v ? (
                    <>
                      <Highlighted text={v.fullName} query={query} />
                      {!v.active && (
                        <span className="volunteer-picker-note">
                          {" "}
                          · not active
                        </span>
                      )}
                    </>
                  ) : (
                    <>Use “{query}” — not on the roster</>
                  )}
                </li>
              );
            })}
          </ul>
        </div>
      )}
    </div>
  );
}

// The name the field shows for a pick: a volunteer's full name, or a custom
// entry as typed.
function nameOf(person: PersonRef | null, volunteers: Volunteer[] | null) {
  if (person === null) return "";
  if ("custom" in person) return person.custom;
  return volunteers?.find((v) => v.id === person.volunteerId)?.fullName ?? "";
}

function Highlighted({ text, query }: { text: string; query: string }) {
  const at =
    query === "" ? -1 : text.toLowerCase().indexOf(query.toLowerCase());
  if (at < 0) return <>{text}</>;
  return (
    <>
      {text.slice(0, at)}
      <mark className="volunteer-picker-match">
        {text.slice(at, at + query.length)}
      </mark>
      {text.slice(at + query.length)}
    </>
  );
}
