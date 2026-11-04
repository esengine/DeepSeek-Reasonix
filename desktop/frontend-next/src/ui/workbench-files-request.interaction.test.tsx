// @vitest-environment jsdom
import { act, cleanup, render } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import "./testkit";
import { WorkbenchPanel } from "./WorkbenchPanel";
import { MockPort } from "../port/mock";
import type { BrowserTab } from "../port/port";
import { openPage } from "./browser_open";

const page = { id: "b1", target: "t1", url: "https://example.com", title: "Existing page", active: true };
const props = { scheme: "dark" as const, shown: false, changes: [], onSurfaces: vi.fn(), onCloseManual: vi.fn(), onExternal: vi.fn() };
// The changed-files pill opens the changes page, which a late browser must not take over.
const changesShown = () => document.querySelector('[role="tab"][aria-selected="true"]')?.textContent?.startsWith("改动") ?? false;
afterEach(() => { cleanup(); vi.restoreAllMocks(); });

it("gives an explicit files request priority over the existing agent page when opening the dock", () => {
  const port = new MockPort();
  const browser = vi.spyOn(port, "browserOpen");
  const { rerender } = render(<WorkbenchPanel {...props} port={port} tabs={[page]} manual={false} />);
  rerender(<WorkbenchPanel {...props} port={port} tabs={[page]} manual revealChanges={1} />);
  expect(changesShown()).toBe(true);
  expect(browser).not.toHaveBeenCalled();
});

it("keeps a newer files request visible when the previously followed link finishes opening", async () => {
  const port = new MockPort();
  let resolve!: (tab: BrowserTab) => void;
  vi.spyOn(port, "browserOpen").mockReturnValue(new Promise((done) => { resolve = done; }));
  const pending = openPage(port, page.url);
  const { rerender } = render(<WorkbenchPanel {...props} port={port} tabs={[]} manual />);
  rerender(<WorkbenchPanel {...props} port={port} tabs={[]} manual revealChanges={1} />);
  expect(changesShown()).toBe(true);
  await act(async () => { resolve(page); await pending; });
  expect(changesShown()).toBe(true);
});
