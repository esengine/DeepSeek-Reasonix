// @vitest-environment jsdom
import { act, cleanup, render, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import "./testkit";
import { WorkbenchPanel } from "./WorkbenchPanel";
import { MockPort } from "../port/mock";
import { SsePort } from "../port/sse";
import { openPage, unseenPageOpenings } from "./browser_open";

const page = { id: "b1", target: "t1", url: "https://example.com", title: "Failed page", active: false };
const other = { ...page, id: "b2", target: "t2", title: "Agent page", active: true };
const props = { scheme: "dark" as const, shown: false, changes: [], onSurfaces: vi.fn(), onCloseManual: vi.fn(), onExternal: vi.fn() };
// The changed-files pill opens the changes page, which a late browser must not take over.
const changesShown = () => document.querySelector('[role="tab"][aria-selected="true"]')?.textContent?.startsWith("改动") ?? false;
afterEach(() => { cleanup(); vi.restoreAllMocks(); vi.unstubAllGlobals(); });

function failedOpen() {
  let respond!: (response: Response) => void;
  vi.stubGlobal("fetch", vi.fn(() => new Promise<Response>((done) => { respond = done; })));
  const port = new MockPort();
  const wire = new SsePort("", "failed-open");
  vi.spyOn(port, "browserOpen").mockImplementation((url) => wire.browserOpen(url, true));
  const pending = openPage(port, page.url).catch(() => {});
  const reject = async (reason = { code: "browser.open_failed", params: { tab: page.id, url: page.url, error: "navigation failed" } }) => {
    await act(async () => { respond(new Response(JSON.stringify(reason), { status: 400 })); await pending; });
  };
  return { port, reject };
}

it.each([
  ["before", "navigation"], ["after", "navigation"],
  ["before", "activation"], ["after", "activation"],
])("keeps Files when failed tab arrives %s rejection, then follows later %s", async (order, reuse) => {
  const { port, reject } = failedOpen();
  const { rerender } = render(<WorkbenchPanel {...props} port={port} tabs={[other]} manual />);
  rerender(<WorkbenchPanel {...props} port={port} tabs={[other]} manual revealChanges={1} />);
  const first = { ...page, active: reuse === "navigation" };
  const agent = { ...other, active: !first.active };
  if (order === "after") await reject();
  rerender(<WorkbenchPanel {...props} port={port} tabs={[agent, first]} manual revealChanges={1} />);
  if (order === "before") await reject();
  await act(async () => {});
  expect(changesShown()).toBe(true);
  expect(unseenPageOpenings(port)).toHaveLength(0);
  const reused = { ...first, active: true, url: reuse === "navigation" ? `${page.url}/next` : page.url };
  rerender(<WorkbenchPanel {...props} port={port} tabs={[{ ...other, active: false }, reused]} manual revealChanges={1} />);
  await waitFor(() => expect(changesShown()).toBe(false));
  expect(document.querySelector('[data-target="browser:t1"]')?.closest('[role="tab"]')?.getAttribute("aria-selected")).toBe("true");
});

it.each(["browser.open_failed", "unrelated.failure"])("does not claim an agent tab from %s without a valid tab identity", async (code) => {
  const { port, reject } = failedOpen();
  const { rerender } = render(<WorkbenchPanel {...props} port={port} tabs={[]} manual />);
  rerender(<WorkbenchPanel {...props} port={port} tabs={[]} manual revealChanges={1} />);
  await reject({ code, params: { tab: code === "browser.open_failed" ? "" : page.id, url: page.url, error: "failed" } });
  expect(unseenPageOpenings(port)).toHaveLength(0);
  rerender(<WorkbenchPanel {...props} port={port} tabs={[page]} manual revealChanges={1} />);
  await waitFor(() => expect(changesShown()).toBe(false));
});

it.each(["before", "after"])("follows a genuine agent tab beside the failed user tab (event %s rejection)", async (order) => {
  const { port, reject } = failedOpen();
  const { rerender } = render(<WorkbenchPanel {...props} port={port} tabs={[]} manual />);
  rerender(<WorkbenchPanel {...props} port={port} tabs={[]} manual revealChanges={1} />);
  if (order === "after") await reject();
  rerender(<WorkbenchPanel {...props} port={port} tabs={[other, page]} manual revealChanges={1} />);
  if (order === "before") await reject();
  await waitFor(() => expect(changesShown()).toBe(false));
  expect(document.querySelector('[data-target="browser:t2"]')?.closest('[role="tab"]')?.getAttribute("aria-selected")).toBe("true");
  expect(unseenPageOpenings(port)).toHaveLength(0);
});
