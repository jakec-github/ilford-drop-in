import { afterAll, beforeEach, describe, expect, mock, test } from "bun:test";
import { render, screen } from "@testing-library/react";
import type { SubmitState } from "../hooks/useAvailabilityForm";
import type { AvailabilityFormState } from "../types";

// The hook is stood in for rather than ../api: RotaViewer.test.tsx mocks ../api
// with only what it needs, and a module mock reaches every test file in the
// run. What is under test here is what the button makes of the hook's answer.
let hookState: {
  form: AvailabilityFormState;
  changed: ReadonlySet<string>;
  submitState: SubmitState;
};

mock.module("../hooks/useAvailabilityForm", () => ({
  useAvailabilityForm: () => ({
    ...hookState,
    deadLink: null,
    error: null,
    selected: new Set(hookState.form.selectedShiftIds),
    setAvailable: () => {},
    submit: async () => {},
  }),
}));

const { default: AvailabilityForm } = await import("./AvailabilityForm");

afterAll(() => {
  mock.restore();
});

function formState(submitted: boolean): AvailabilityFormState {
  return {
    volunteerName: "Grace",
    groupMembers: [],
    shifts: [
      {
        id: "s1",
        date: "2026-08-23",
        start: "2026-08-23T19:30:00",
        end: "2026-08-23T21:30:00",
        closed: false,
      },
    ],
    selectedShiftIds: ["s1"],
    submitted,
    submittedAt: submitted ? "2026-08-01T10:00:00Z" : null,
  };
}

describe("AvailabilityForm send", () => {
  beforeEach(() => {
    hookState = {
      form: formState(false),
      changed: new Set(),
      submitState: "idle",
    };
  });

  // Every date starting on yes is a real answer, so a first send needs no edit.
  test("a first answer can be sent as it stands", () => {
    render(<AvailabilityForm token="t" />);
    expect(
      screen.getByRole("button", { name: "Send my availability" }),
    ).toBeEnabled();
  });

  // Sending the answer the server already holds tells it nothing, so the button
  // says it is in and waits for a change (issue #149). A send adopts what the
  // server returns, so this is also what the button says straight after one.
  test("an answer already sent cannot be sent again unchanged", () => {
    hookState.form = formState(true);
    render(<AvailabilityForm token="t" />);
    expect(screen.getByRole("button", { name: "Answer sent" })).toBeDisabled();
  });

  test("a changed answer can be sent again", () => {
    hookState.form = formState(true);
    hookState.changed = new Set(["s1"]);
    render(<AvailabilityForm token="t" />);
    expect(
      screen.getByRole("button", { name: "Send my changes" }),
    ).toBeEnabled();
  });
});
