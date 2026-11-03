// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import "./testkit";
import { SessionFilter } from "./SessionFilter";
import { inScope } from "./sessionScope";
import type { TreeSession } from "../port/hub";

afterEach(cleanup);

const counts = { all: 24, live: 2, waiting: 1, unread: 3, pinned: 1 };
const trigger = () => document.querySelector<HTMLButtonElement>(".session-filter-trigger")!;
const row = (v: string) => document.querySelector<HTMLElement>(`[data-action="session.filter"][data-value="${v}"]`)!;

describe("the session filter", () => {
  it("reads the current filter on its button and lists every filter with its count", () => {
    render(<SessionFilter scope="all" counts={counts} onScope={vi.fn()} />);
    expect(trigger().textContent).toContain("全部");
    fireEvent.click(trigger());
    expect(["all", "live", "waiting", "unread", "pinned", "archived"].every((v) => !!row(v))).toBe(true);
    expect(row("live").textContent).toContain("2");
    expect(row("waiting").textContent).toContain("1");
    expect(row("unread").textContent).toContain("3");
    expect(row("archived").textContent).not.toMatch(/\d/);
  });

  it("chooses a filter through the same data-action and value the old buttons carried", () => {
    const onScope = vi.fn();
    render(<SessionFilter scope="all" counts={counts} onScope={onScope} />);
    fireEvent.click(trigger());
    fireEvent.click(row("unread"));
    expect(onScope).toHaveBeenCalledWith("unread");
  });

  it("offers a one-press clear beside any filter but All", () => {
    const onScope = vi.fn();
    const { rerender } = render(<SessionFilter scope="all" counts={counts} onScope={onScope} />);
    expect(screen.queryByRole("button", { name: "清除筛选" })).toBeNull();
    rerender(<SessionFilter scope="live" counts={counts} onScope={onScope} />);
    expect(trigger().textContent).toContain("进行中");
    fireEvent.click(screen.getByRole("button", { name: "清除筛选" }));
    expect(onScope).toHaveBeenCalledWith("all");
  });
});

describe("the two new scopes", () => {
  const s = (over: Partial<TreeSession> = {}) => ({ path: "/p", name: "n", ...over }) as TreeSession;
  it("lists unread conversations under Unread", () => {
    expect(inScope(s({ unread: true }), "unread", false, false, false)).toBe(true);
    expect(inScope(s(), "unread", false, false, false)).toBe(false);
  });
  it("lists a conversation waiting on the person under Needs you", () => {
    expect(inScope(s(), "waiting", false, false, false, false, true)).toBe(true);
    expect(inScope(s(), "waiting", false, false, false, false, false)).toBe(false);
  });
});
