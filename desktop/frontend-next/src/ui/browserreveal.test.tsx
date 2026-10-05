// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest";
import { renderHook } from "@testing-library/react";
import { useRevealAgentPages, useRevealBrowserOpen } from "./browserreveal";
import type { Item } from "../state/session";
import { UNREAD_TABS } from "./BrowserPanel";
import type { BrowserTab } from "../port/session";

const tab = (target: string): BrowserTab => ({ id: target, target, url: "https://x/" + target, title: target, active: true });

describe("the browser column", () => {
  it("opens when the agent opens a page, and not for the pages already there", () => {
    const reveal = vi.fn();
    const { rerender } = renderHook(({ pages }) => useRevealAgentPages(pages, true, reveal), {
      initialProps: { pages: UNREAD_TABS },
    });
    rerender({ pages: [tab("a")] });
    expect(reveal).not.toHaveBeenCalled();
    rerender({ pages: [tab("a"), tab("b")] });
    expect(reveal).toHaveBeenCalledTimes(1);
  });

  it("stays shut when a page closes or the same pages are read again", () => {
    const reveal = vi.fn();
    const { rerender } = renderHook(({ pages }) => useRevealAgentPages(pages, true, reveal), {
      initialProps: { pages: [tab("a"), tab("b")] },
    });
    rerender({ pages: [tab("a")] });
    rerender({ pages: [tab("a")] });
    expect(reveal).not.toHaveBeenCalled();
  });

  it("does not open for a pane nobody is looking at", () => {
    const reveal = vi.fn();
    const { rerender } = renderHook(({ pages }) => useRevealAgentPages(pages, false, reveal), {
      initialProps: { pages: [] as BrowserTab[] },
    });
    rerender({ pages: [tab("a")] });
    expect(reveal).not.toHaveBeenCalled();
  });
});

describe("an agent opening a page", () => {
  const call = (id: string, running: boolean, name = "browser_open") =>
    ({ t: "tool", id, running, children: [], tool: { id, name, readOnly: false } }) as Item;

  it("opens the column while the call runs, once per call, even into a tab already open", () => {
    const reveal = vi.fn();
    const { rerender } = renderHook(({ items }) => useRevealBrowserOpen(items, true, reveal), {
      initialProps: { items: [] as Item[] },
    });
    rerender({ items: [call("c1", true)] });
    rerender({ items: [call("c1", false)] });
    expect(reveal).toHaveBeenCalledTimes(1);
  });

  it("ignores history, other tools, and a pane nobody is looking at", () => {
    const reveal = vi.fn();
    renderHook(() => useRevealBrowserOpen([call("h1", false), call("b1", true, "bash")], true, reveal));
    renderHook(() => useRevealBrowserOpen([call("c2", true)], false, reveal));
    expect(reveal).not.toHaveBeenCalled();
  });
});
