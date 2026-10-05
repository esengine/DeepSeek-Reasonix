// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { App } from "./App";
import { MockHub } from "../port/mock_hub";

afterEach(() => {
  cleanup();
  sessionStorage.clear();
});

function hub() {
  const h = new MockHub();
  const build = h.portFor.bind(h);
  h.portFor = (rt) => {
    const port = build(rt);
    port.providerSetup = async () => null;
    return port;
  };
  return h;
}

const title = () => document.querySelector(".crumb b")?.textContent ?? "";
const settled = () =>
  waitFor(() => {
    const t = title();
    if (!t) throw new Error("no title yet");
    return t;
  });

// A reload keeps the kernel's panes open; which one this tab was looking at is
// the tab's own answer, and only the tab can give it back.
describe("a reloaded window", () => {
  it("comes back to the pane it was showing, not the first one open", async () => {
    const kernel = hub();
    const first = render(<App hub={kernel} />);
    const opening = await settled();
    const other = [...document.querySelectorAll(".sessrow")].find((r) => !r.textContent?.includes(opening));
    expect(other, "the fixture needs a second session").toBeTruthy();
    await userEvent.click(other!);
    const chosen = await waitFor(() => {
      const t = title();
      if (t === opening) throw new Error("still the first pane");
      return t;
    });
    first.unmount();

    render(<App hub={kernel} />);
    await waitFor(() => expect(title()).toBe(chosen));
  });
});
