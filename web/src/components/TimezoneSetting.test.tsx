import { describe, expect, mock, test } from "bun:test";
import { act, fireEvent, render, screen } from "@testing-library/react";
import { TimezoneForm } from "./TimezoneSetting";

const settle = () => act(() => new Promise<void>((r) => setTimeout(r, 0)));

function renderForm(
  zone: string,
  onSave = mock<(zone: string) => Promise<void>>(() => Promise.resolve()),
) {
  render(<TimezoneForm zone={zone} onSave={onSave} />);
  return onSave;
}

describe("TimezoneForm", () => {
  test("a zone nobody chose is set to the browser's", async () => {
    const onSave = renderForm("");
    await settle();
    expect(onSave).toHaveBeenCalledTimes(1);
    expect(onSave.mock.calls[0][0]).toBe(
      Intl.DateTimeFormat().resolvedOptions().timeZone,
    );
  });

  test("a chosen zone is left alone until it is changed", async () => {
    const onSave = renderForm("Europe/London");
    await settle();
    expect(onSave).not.toHaveBeenCalled();

    fireEvent.change(screen.getByLabelText("Time zone"), {
      target: { value: "America/New_York" },
    });
    await settle();
    expect(onSave.mock.calls).toEqual([["America/New_York"]]);
  });

  test("offers every zone the browser knows, and the one stored", () => {
    renderForm("Etc/Unlisted");
    const picker = screen.getByLabelText("Time zone") as HTMLSelectElement;
    const offered = Array.from(picker.options).map((o) => o.value);
    expect(offered).toContain("Europe/London");
    expect(offered).toContain("Etc/Unlisted");
    expect(picker.value).toBe("Etc/Unlisted");
  });

  test("a zone the server refuses is said, and not retried", async () => {
    const onSave = renderForm(
      "",
      mock(() => Promise.reject(new Error('cannot load time zone "X"'))),
    );
    await settle();
    await settle();
    expect(screen.getByText('cannot load time zone "X"')).toBeTruthy();
    expect(onSave).toHaveBeenCalledTimes(1);
  });
});
