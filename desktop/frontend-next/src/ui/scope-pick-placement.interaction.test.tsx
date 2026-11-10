// @vitest-environment jsdom
import "./testkit";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ScopePill } from "./CapabilityScope";
import type { CapabilityScope } from "../port/port";

afterEach(() => { cleanup(); vi.restoreAllMocks(); });

const scope = (over: Partial<CapabilityScope> = {}): CapabilityScope => ({
  key: "a", root: "/work/app", name: "app", label: "", repo: true, trees: 1, current: true, overrides: 0, branch: "main", ...over,
}) as CapabilityScope;
const scopes = [scope(), scope({ key: "b", root: "/work/site", name: "site", current: false })];

type Box = { left: number; top: number; width: number; height: number };
const rect = (b: Box): DOMRect => ({ x: b.left, y: b.top, left: b.left, top: b.top, width: b.width, height: b.height, right: b.left + b.width, bottom: b.top + b.height, toJSON: () => ({}) }) as DOMRect;

// The settings column is the box that clips the list: it starts right of the sidebar.
const COLUMN: Box = { left: 240, top: 48, width: 1040, height: 752 };
const POPUP = { width: 320, height: 241 };

function drawAt(anchor: Box) {
  vi.spyOn(Element.prototype, "getBoundingClientRect").mockImplementation(function (this: Element) {
    if (this.classList.contains("scope-pick")) return rect({ left: 0, top: 0, ...POPUP });
    if (this.classList.contains("scopeanchor")) return rect(anchor);
    if (this.classList.contains("prefs-main")) return rect(COLUMN);
    return rect({ left: 0, top: 0, width: 0, height: 0 });
  });
  vi.stubGlobal("innerWidth", 1280);
  vi.stubGlobal("innerHeight", 800);
  render(<div className="prefs-main"><ScopePill scope={scope()} scopes={scopes} onPick={() => {}} /></div>);
  return userEvent.click(screen.getByRole("button", { name: /app/ })).then(() => document.querySelector<HTMLElement>(".scope-pick")!);
}

describe("where the project list opens", () => {
  beforeEach(() => { vi.stubGlobal("innerWidth", 1280); });

  it("aligns to the pill's left edge when the pill sits at the left of the column", async () => {
    const pick = await drawAt({ left: 285, top: 300, width: 94, height: 24 });
    expect(pick.style.left).toBe("0px");
    expect(pick.style.right).toBe("auto");
  });

  it("aligns to the pill's right edge when it would run past the column's right side", async () => {
    const pick = await drawAt({ left: 1100, top: 300, width: 94, height: 24 });
    expect(pick.style.right).toBe("0px");
    expect(pick.style.left).toBe("auto");
  });

  it("opens below when there is room", async () => {
    const pick = await drawAt({ left: 285, top: 300, width: 94, height: 24 });
    expect(pick.style.top).toBe("calc(100% + 4px)");
    expect(pick.style.bottom).toBe("auto");
  });

  it("opens above when the column ends before the list does", async () => {
    const pick = await drawAt({ left: 285, top: 648, width: 94, height: 24 });
    expect(pick.style.bottom).toBe("calc(100% + 4px)");
    expect(pick.style.top).toBe("auto");
  });
});
