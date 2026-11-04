// @vitest-environment jsdom
import { act, cleanup, render, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import "./testkit";
import { WorkbenchPanel } from "./WorkbenchPanel";
import { MockPort } from "../port/mock";
import { openPage } from "./browser_open";

const page = { id: "b1", target: "t1", url: "https://example.com", title: "User page", active: false };
const other = { ...page, id: "b2", target: "t2", title: "Agent page", active: true };
const props = { scheme: "dark" as const, shown: false, changes: [], onSurfaces: vi.fn(), onCloseManual: vi.fn(), onExternal: vi.fn() };
// The changed-files pill opens the changes page, which a late browser must not take over.
const changesShown = () => document.querySelector('[role="tab"][aria-selected="true"]')?.textContent?.startsWith("改动") ?? false;
afterEach(() => { cleanup(); vi.restoreAllMocks(); });

it.each([
  ["navigation", "before"], ["navigation", "after"],
  ["activation", "before"], ["activation", "after"],
])("follows later agent %s of a superseded user tab (event %s response)", async (reuse, order) => {
  const port = new MockPort();
  let resolve!: (tab: typeof page) => void;
  vi.spyOn(port, "browserOpen").mockReturnValue(new Promise((done) => { resolve = done; }));
  const pending = openPage(port, page.url);
  const { rerender } = render(<WorkbenchPanel {...props} port={port} tabs={[other]} manual />);
  rerender(<WorkbenchPanel {...props} port={port} tabs={[other]} manual revealChanges={1} />);
  const first = { ...page, active: reuse === "navigation" };
  const agent = { ...other, active: !first.active };
  if (order === "after") await act(async () => { resolve(first); await pending; });
  rerender(<WorkbenchPanel {...props} port={port} tabs={[agent, first]} manual revealChanges={1} />);
  if (order === "before") await act(async () => { resolve(first); await pending; });
  await act(async () => {});
  expect(changesShown()).toBe(true);
  const reused = { ...first, active: true, url: reuse === "navigation" ? "https://example.com/next" : first.url };
  rerender(<WorkbenchPanel {...props} port={port} tabs={[{ ...other, active: false }, reused]} manual revealChanges={1} />);
  await waitFor(() => expect(changesShown()).toBe(false));
  expect(document.querySelector('[data-target="browser:t1"]')?.closest('[role="tab"]')?.getAttribute("aria-selected")).toBe("true");
});
