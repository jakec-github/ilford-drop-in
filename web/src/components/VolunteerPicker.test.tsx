import { describe, expect, mock, test } from "bun:test";
import { useState } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import VolunteerPicker from "./VolunteerPicker";
import type { PersonRef, Volunteer } from "../types";

function volunteer(id: string, fullName: string, active = true): Volunteer {
  return {
    id,
    name: fullName.split(" ")[0],
    fullName,
    roles: [],
    group: null,
    gender: null,
    active,
  };
}

const ROSTER = [
  volunteer("sam", "Sam Ahmed"),
  volunteer("amara", "Amara Okafor"),
  volunteer("liam", "Liam Kelly", false),
];

// Holds the pick as a caller does, and reports each one.
function Harness({
  volunteers = ROSTER,
  volunteersError = null,
  allowCustom,
  onChange = () => {},
}: {
  volunteers?: Volunteer[] | null;
  volunteersError?: string | null;
  allowCustom?: boolean;
  onChange?: (person: PersonRef | null) => void;
}) {
  const [value, setValue] = useState<PersonRef | null>(null);
  return (
    <VolunteerPicker
      label="Who"
      volunteers={volunteers}
      volunteersError={volunteersError}
      allowCustom={allowCustom}
      value={value}
      onChange={(person) => {
        setValue(person);
        onChange(person);
      }}
    />
  );
}

function field() {
  return screen.getByRole("combobox", { name: "Who" }) as HTMLInputElement;
}

function type(text: string) {
  fireEvent.focus(field());
  fireEvent.change(field(), { target: { value: text } });
}

function options() {
  return screen.queryAllByRole("option").map((o) => o.textContent);
}

describe("VolunteerPicker", () => {
  test("lists the active by name, then the inactive, marked", () => {
    render(<Harness />);
    fireEvent.focus(field());
    expect(options()).toEqual([
      "Amara Okafor",
      "Sam Ahmed",
      "Liam Kelly · not active",
    ]);
  });

  test("typing filters by any part of the name, and offers what was typed", () => {
    render(<Harness />);
    type("am");
    expect(options()).toEqual([
      "Amara Okafor",
      "Sam Ahmed",
      "Liam Kelly · not active",
      "Use “am” — not on the roster",
    ]);
  });

  test("a name the roster lacks is offered as somebody not on it", () => {
    const onChange = mock<(person: PersonRef | null) => void>();
    render(<Harness onChange={onChange} />);
    type("Redbridge youth group");
    expect(screen.getByText("Nobody on the roster matches.")).toBeTruthy();

    fireEvent.click(
      screen.getByRole("option", {
        name: "Use “Redbridge youth group” — not on the roster",
      }),
    );
    expect(onChange).toHaveBeenLastCalledWith({
      custom: "Redbridge youth group",
    });
    expect(field().value).toBe("Redbridge youth group");
  });

  test("arrow keys move through the list, Enter picks, Escape closes", () => {
    const onChange = mock<(person: PersonRef | null) => void>();
    render(<Harness onChange={onChange} />);
    type("a");
    // Typing makes the first match active; one step down is the second.
    fireEvent.keyDown(field(), { key: "ArrowDown" });
    fireEvent.keyDown(field(), { key: "Enter" });
    expect(onChange).toHaveBeenLastCalledWith({ volunteerId: "sam" });
    expect(field().value).toBe("Sam Ahmed");
    expect(screen.queryByRole("listbox")).toBeNull();

    fireEvent.change(field(), { target: { value: "Li" } });
    expect(screen.getByRole("listbox")).toBeTruthy();
    fireEvent.keyDown(field(), { key: "Escape" });
    expect(screen.queryByRole("listbox")).toBeNull();
  });

  test("editing a pick takes it back", () => {
    const onChange = mock<(person: PersonRef | null) => void>();
    render(<Harness onChange={onChange} />);
    type("Sam");
    fireEvent.click(screen.getByRole("option", { name: "Sam Ahmed" }));
    fireEvent.change(field(), { target: { value: "Sam A" } });
    expect(onChange).toHaveBeenLastCalledWith(null);
  });

  test("a roster that failed still takes a typed name", () => {
    render(<Harness volunteers={null} volunteersError="offline" />);
    type("Ada");
    expect(options()).toEqual(["Use “Ada” — not on the roster"]);
  });

  test("with custom entry off, only the roster is offered", () => {
    render(<Harness allowCustom={false} />);
    type("Redbridge");
    expect(options()).toEqual([]);
    expect(screen.getByText("Nobody on the roster matches.")).toBeTruthy();
  });

  test("with custom entry off, it waits for the roster", () => {
    render(
      <Harness allowCustom={false} volunteers={null} volunteersError={null} />,
    );
    expect(field()).toBeDisabled();
  });
});
