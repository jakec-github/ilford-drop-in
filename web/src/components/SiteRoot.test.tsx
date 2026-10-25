import { afterEach, beforeEach, describe, expect, mock, test } from "bun:test";
import { render, screen } from "@testing-library/react";
import SiteRoot from "./SiteRoot";

const realFetch = globalThis.fetch;
const fetchMock = mock(() => Promise.reject(new Error("no fetch expected")));

beforeEach(() => {
  fetchMock.mockClear();
  globalThis.fetch = fetchMock as unknown as typeof fetch;
});

afterEach(() => {
  globalThis.fetch = realFetch;
});

describe("SiteRoot", () => {
  // The root names no Organisation: an address is the only way in. The guide
  // is the one place it links to.
  test("the splash links only to the guide and asks the server nothing", () => {
    const { container } = render(<SiteRoot pathname="/" />);

    expect(screen.getByRole("heading", { name: "Rota Wise" })).toBeVisible();
    const links = [...container.querySelectorAll("a")];
    expect(links.map((a) => a.getAttribute("href"))).toEqual(["/guide"]);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  test("/guide is the organiser's guide, rendered from its Markdown", () => {
    render(<SiteRoot pathname="/guide" />);

    expect(
      screen.getByRole("heading", { level: 1, name: "Organiser's guide" }),
    ).toBeVisible();
    expect(screen.getByRole("heading", { name: "Your first rota" })).toBeVisible();
    expect(screen.queryByText(/history/i)).toBeNull();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  // A missing page and an unknown Organisation draw the same thing, so which
  // drop-ins a server runs cannot be found out by asking it.
  test("any other path is the same page not found", () => {
    const missing = render(<SiteRoot pathname="/no-such-page" />).container
      .innerHTML;
    const unknown = render(<SiteRoot pathname="/org/north/" />).container
      .innerHTML;

    expect(unknown).toBe(missing);
    expect(
      screen.getAllByRole("heading", { name: "Page not found" }),
    ).toHaveLength(2);
    expect(fetchMock).not.toHaveBeenCalled();
  });
});
