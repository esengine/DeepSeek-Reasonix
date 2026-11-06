// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render } from "@testing-library/react";
import "../testkit";
import { SayCard } from "./SayCard";
import type { Item } from "../../state/session";

vi.mock("../LazyMarkdown", () => ({
  LazyMarkdown: () => { throw new Error("typeset failed"); },
}));

afterEach(() => { cleanup(); vi.restoreAllMocks(); });

describe("a reply whose layout cannot be drawn", () => {
  it("falls back to its text with a tag saying so, losing nothing", () => {
    vi.spyOn(console, "warn").mockImplementation(() => {});
    const item = { t: "say", id: "s", text: "| 列 | 说明 |\n|---|---|\n| retry | 退避次数 |", done: true } as Extract<Item, { t: "say" }>;
    const { container } = render(<SayCard item={item} />);
    expect(container.querySelector(".srcfb-tag")?.textContent).toBe("按原文显示");
    expect(container.querySelector(".srcfb-hd")?.textContent).toContain("内容一字不少");
    expect(container.querySelector(".srcfb-src")?.textContent).toBe(item.text);
  });
});
