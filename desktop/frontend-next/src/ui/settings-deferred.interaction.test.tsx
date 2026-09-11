// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { MockHub } from "../port/mock_hub";

// The settings screen arrives as its own chunk. An update that replaced the
// assets under a window still holding the old document makes the name it asks
// for a 404, and the rejected import throws during render — the one dead end
// that took the whole window rather than the panel: nothing caught the throw,
// React unmounted the tree, and the screen went white.
//
// Throwing from the module factory is what a missing chunk does to the import:
// the promise lazy() is waiting on rejects. Its own file, because the mock
// replaces ./Settings for everything in it.
vi.mock("./Settings", () => {
  throw new Error("Failed to fetch dynamically imported module: Settings.js");
});

const { App } = await import("./App");

const opener = () => document.querySelector('[data-action="chrome.settings"]');

afterEach(cleanup);

describe("a deferred screen that does not arrive", () => {
  it("says so in a panel and leaves the window standing", async () => {
    const hub = new MockHub();
    // The fixture's port asks for a key on first sight, which is its own screen
    // and not the one under test. r1 is the runtime MockHub reports, so this is
    // the port the window will be handed.
    hub.portFor({ id: "r1" } as never).providerSetup = () => Promise.resolve(null);
    render(<App hub={hub as never} />);

    await waitFor(() => expect(opener()).toBeTruthy());
    await userEvent.click(opener() as HTMLElement);

    expect(await screen.findByText("设置没能打开")).toBeTruthy();
    // The way out is the one that actually repairs a stale document, and the
    // window it was opened from is still mounted to go back to.
    expect(screen.getByRole("button", { name: "重新载入" })).toBeTruthy();
    expect(opener()).toBeTruthy();
  });
});
