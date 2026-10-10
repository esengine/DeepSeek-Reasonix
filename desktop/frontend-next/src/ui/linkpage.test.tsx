// @vitest-environment jsdom
import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useCallback, useState } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import "./testkit";
import { MockPort } from "../port/mock";
import * as hostModule from "../port/host";
import type { BrowserTab } from "../port/port";
import { HttpError } from "../port/http_error";
import { useLinkRouting } from "./links";
import { WorkbenchPanel } from "./WorkbenchPanel";
import { UNREAD_TABS } from "./BrowserPanel";

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

type Pending = { url: string; settle: (error?: unknown) => void; tab: BrowserTab };

// A kernel whose tabs exist in creation order and whose replies, like the
// poll that lists them, arrive in whatever order the test chooses.
function kernel() {
  vi.spyOn(hostModule.host(), "drawsBrowserViews").mockReturnValue(true);
  const port = new MockPort();
  const created: BrowserTab[] = [];
  const pending: Pending[] = [];
  const open = vi.spyOn(port, "browserOpen").mockImplementation((url: string) => {
    const tab: BrowserTab = {
      id: `k${created.length + 1}`,
      target: `t${created.length + 1}`,
      url,
      title: url === "about:blank" ? "Blank" : url.endsWith("/b") ? "Second link" : "Link page",
      active: created.length === 0,
    };
    created.push(tab);
    return new Promise<BrowserTab>((resolve, reject) => pending.push({ url, tab, settle: (error) => error ? reject(error) : resolve(tab) }));
  });
  return { port, created, pending, open };
}

function Harness({ port, tabs }: { port: MockPort; tabs: BrowserTab[] }) {
  const [manual, setManual] = useState(false);
  useLinkRouting(port, useCallback(() => setManual(true), []), vi.fn());
  return (
    <>
      <button onClick={() => setManual(true)}>show browser</button>
      <a href="https://example.com/a">go</a>
      <a href="https://example.com/b">go again</a>
      <WorkbenchPanel port={port} tabs={tabs} manual={manual} shown={false} scheme="light" changes={[]} onCloseManual={vi.fn()} onSurfaces={vi.fn()} onExternal={vi.fn()} />
    </>
  );
}

const selected = () => screen.getAllByRole("tab").find((t) => t.getAttribute("aria-selected") === "true")?.textContent ?? "";

describe("a link followed while the session has no browser tab", () => {
  it("is the page that ends up selected, whichever reply and poll arrive first", async () => {
    const { port, created, pending, open } = kernel();
    const { rerender } = render(<Harness port={port} tabs={[]} />);
    screen.getByText("go").click();
    await waitFor(() => expect(open).toHaveBeenCalled());
    await act(async () => {
      for (const p of [...pending].reverse()) p.settle();
    });
    rerender(<Harness port={port} tabs={[...created]} />);
    await waitFor(() => expect(selected()).toBe("Link page"));
    expect(open.mock.calls.map((c) => c[0])).toEqual(["https://example.com/a"]);
  });

  it("is selected when the poll lists the new tab before the open call answers", async () => {
    const { port, created, pending, open } = kernel();
    const { rerender } = render(<Harness port={port} tabs={[]} />);
    screen.getByText("go").click();
    await waitFor(() => expect(open).toHaveBeenCalled());
    rerender(<Harness port={port} tabs={[...created]} />);
    await act(async () => {
      for (const p of pending) p.settle();
    });
    await waitFor(() => expect(selected()).toBe("Link page"));
    expect(open).toHaveBeenCalledTimes(1);
  });

  it("leaves the agent's active tab alone when it already has one", async () => {
    const { port, created, pending, open } = kernel();
    const agent: BrowserTab = { id: "a", target: "ta", url: "https://agent.example", title: "Agent page", active: true };
    const { rerender } = render(<Harness port={port} tabs={[agent]} />);
    screen.getByText("go").click();
    await waitFor(() => expect(open).toHaveBeenCalledTimes(1));
    await act(async () => pending[0].settle());
    rerender(<Harness port={port} tabs={[agent, { ...created[0], active: false }]} />);
    await waitFor(() => expect(selected()).toBe("Link page"));
    expect(screen.getByRole("tab", { name: "Agent page" })).toBeTruthy();
  });

  it.each([0, 1])("keeps the latest followed link selected when reply %i arrives first", async (first) => {
    const { port, created, pending, open } = kernel();
    const { rerender } = render(<Harness port={port} tabs={[]} />);
    screen.getByText("go").click();
    await waitFor(() => expect(pending).toHaveLength(1));
    screen.getByText("go again").click();
    await waitFor(() => expect(pending).toHaveLength(2));
    rerender(<Harness port={port} tabs={[...created]} />);
    await waitFor(() => expect(selected()).toBe("Second link"));
    await act(async () => pending[first].settle());
    await act(async () => pending[1 - first].settle());
    expect(selected()).toBe("Second link");
    expect(open.mock.calls.map((call) => call[0])).toEqual(["https://example.com/a", "https://example.com/b"]);
  });

  it("does not surface an older opening failure over the latest followed page", async () => {
    const { port, created, pending } = kernel();
    vi.spyOn(port, "openExternal").mockResolvedValue(undefined);
    const { rerender } = render(<Harness port={port} tabs={[]} />);
    screen.getByText("go").click();
    await waitFor(() => expect(pending).toHaveLength(1));
    screen.getByText("go again").click();
    await waitFor(() => expect(pending).toHaveLength(2));
    rerender(<Harness port={port} tabs={[...created]} />);
    await act(async () => pending[1].settle());
    await act(async () => pending[0].settle(new Error("first page disconnected")));
    expect(selected()).toBe("Second link");
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("follows the latest reply when the first tab read is still outstanding", async () => {
    const { port, created, pending } = kernel();
    const { rerender } = render(<Harness port={port} tabs={UNREAD_TABS} />);
    screen.getByText("go").click();
    await waitFor(() => expect(pending).toHaveLength(1));
    screen.getByText("go again").click();
    await waitFor(() => expect(pending).toHaveLength(2));
    await act(async () => pending[1].settle());
    rerender(<Harness port={port} tabs={[...created]} />);
    await act(async () => pending[0].settle());
    await waitFor(() => expect(selected()).toBe("Second link"));
  });

  it("still reports the latest opening failure after an older page succeeds", async () => {
    const { port, pending } = kernel();
    vi.spyOn(port, "openExternal").mockResolvedValue(undefined);
    render(<Harness port={port} tabs={[]} />);
    screen.getByText("go").click();
    await waitFor(() => expect(pending).toHaveLength(1));
    screen.getByText("go again").click();
    await waitFor(() => expect(pending).toHaveLength(2));
    await act(async () => pending[0].settle());
    await act(async () => pending[1].settle(new HttpError(400, "/browser/open: 400", { code: "browser.engine_failed", params: {} })));
    expect(await screen.findByRole("alert")).toBeTruthy();
  });

  it("is still followed by a blank page of its own when the person adds a tab by hand", async () => {
    const { port, pending, open } = kernel();
    const user = userEvent.setup();
    render(<Harness port={port} tabs={[]} />);
    await user.click(screen.getByText("show browser"));
    await waitFor(() => expect(pending).toHaveLength(1));
    await act(async () => pending[0].settle());
    await user.click(await screen.findByRole("button", { name: /新建|新标签|新增|添加/ }));
    await waitFor(() => expect(open).toHaveBeenCalledTimes(2));
    expect(open.mock.calls[1][0]).toBe("about:blank");
  });

  it("is refused once, and the column says so rather than asking for a second browser", async () => {
    const { port, open } = kernel();
    open.mockRejectedValue(new HttpError(400, "/browser/open: 400", { code: "browser.engine_failed", params: {} }));
    vi.spyOn(port, "openExternal").mockResolvedValue(undefined);
    render(<Harness port={port} tabs={[]} />);
    screen.getByText("go").click();
    expect(await screen.findByRole("alert")).toBeTruthy();
    expect(open).toHaveBeenCalledTimes(1);
  });

  it("closed, then followed again, opens that link once more", async () => {
    const { port, pending, open } = kernel();
    const { rerender } = render(<Harness port={port} tabs={[]} />);
    screen.getByText("go").click();
    await act(async () => pending[0].settle());
    rerender(<Harness port={port} tabs={[]} />);
    screen.getByText("go").click();
    await waitFor(() => expect(open).toHaveBeenCalledTimes(2));
    expect(open.mock.calls[1][0]).toBe("https://example.com/a");
  });
});
