// @vitest-environment jsdom
import { lazy, Suspense } from "react";
import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import "./testkit";
import { Boundary } from "./Boundary";
import { SettingsUnavailable } from "./SettingsUnavailable";

// The settings screen arrives as its own chunk. An update that replaced the
// assets under a window still holding the old document makes the name it asks
// for a 404, and the rejected import throws during render — the one dead end
// that took the whole window rather than the panel: nothing caught the throw,
// React unmounted the tree, and the screen went white.
//
// A chunk that is not there is imported for real, so the rejection is the one
// the browser produces rather than one the test wrote.
const gone = "./Settings.gone.js";
const Missing = lazy(() => import(/* @vite-ignore */ gone));

afterEach(cleanup);

describe("a deferred screen that does not arrive", () => {
  it("says so in a panel and leaves the window standing", async () => {
    render(
      <div>
        <button data-action="chrome.settings">settings</button>
        <Boundary fallback={<SettingsUnavailable onClose={() => {}} />}>
          <Suspense fallback={<div className="prefs" aria-busy="true" />}>
            <Missing />
          </Suspense>
        </Boundary>
      </div>,
    );

    expect(await screen.findByText("设置没能打开")).toBeTruthy();
    // The way out is the one that actually repairs a stale document, and the
    // window it was opened from is still mounted to go back to.
    expect(screen.getByRole("button", { name: "重新载入" })).toBeTruthy();
    expect(document.querySelector('[data-action="chrome.settings"]')).toBeTruthy();
  });
});
