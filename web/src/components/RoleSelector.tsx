import { useId } from "react";
import type { Role, RoleColour } from "../types";
import "../ui/Chip.css";
import "./RoleSelector.css";

// RoleSelector picks one Role from those offered, as a row of chips in each
// Role's own colour — the colour the shift draws them in — with one filled. It
// is a radio group underneath, so the arrow keys move along it.
//
// Which Roles are offered is the caller's to say, and it says every one the
// Organisation has, for anybody: holding a Role is a rule, and rules bind the
// allocator, not the Organiser (ADR 0010). The roster only decides which one
// starts selected. Kept in the order given, so a Role stays where it was when
// the person changes.
export default function RoleSelector({
  label,
  showLabel = true,
  options,
  value,
  onChange,
  colourOf,
  labelOf = (role) => role,
}: {
  label: string;
  // Off where the row says what it is without one, as on the Overrides card.
  showLabel?: boolean;
  options: Role[];
  value: Role;
  onChange: (role: Role) => void;
  // null, or absent, draws a Role uncoloured: one the Organisation has since
  // retired, or the Role-less "No role".
  colourOf?: (role: Role) => RoleColour | null;
  labelOf?: (role: Role) => string;
}) {
  const id = useId();
  return (
    <div className="role-selector-wrap">
      {showLabel && (
        <span id={`${id}-label`} className="role-selector-label">
          {label}
        </span>
      )}
      <div
        className="role-selector"
        role="radiogroup"
        aria-labelledby={showLabel ? `${id}-label` : undefined}
        aria-label={showLabel ? undefined : label}
      >
        {options.map((role) => (
          <label
            key={role}
            className={[
              "chip",
              "chip--pill",
              "role-option",
              role === value ? "selected" : "",
            ]
              .filter(Boolean)
              .join(" ")}
            data-role-colour={colourOf?.(role) ?? undefined}
          >
            <input
              type="radio"
              name={id}
              value={role}
              checked={role === value}
              onChange={() => onChange(role)}
            />
            {labelOf(role)}
          </label>
        ))}
      </div>
    </div>
  );
}
