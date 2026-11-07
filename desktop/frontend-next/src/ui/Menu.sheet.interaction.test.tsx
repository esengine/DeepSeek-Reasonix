// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import "./testkit";
import { Picker, type MenuItem } from "./Menu";

afterEach(() => {
  cleanup();
  delete document.documentElement.dataset.fold;
});

const models: MenuItem[] = [{ value: "a", label: "alpha" }, { value: "b", label: "beta" }];

function open(props: Partial<Parameters<typeof Picker>[0]> = {}) {
  render(<Picker label="trigger" place="bottom" current="a" items={models} onPick={() => {}} {...props} />);
  fireEvent.click(screen.getByRole("button", { name: "trigger" }));
  return document.querySelector<HTMLElement>(".portmenu")!;
}

// On a phone a menu that asks for it is a sheet along the bottom edge: the
// stylesheet places it, so the script that measures an anchor must not.
describe("a menu that asks to be a sheet on a phone", () => {
  it("is marked, and left unplaced by the script, on a phone", () => {
    document.documentElement.dataset.fold = "rail side scene phone";
    const menu = open({ sheet: true });
    expect(menu.classList.contains("menu-sheet")).toBe(true);
    expect(menu.style.left).toBe("");
    expect(menu.style.top).toBe("");
  });

  it("is placed beside its button everywhere else", () => {
    document.documentElement.dataset.fold = "rail side";
    const menu = open({ sheet: true });
    expect(menu.style.left).not.toBe("");
    expect(menu.style.top).not.toBe("");
  });

  it("leaves a menu that did not ask exactly as it was, on a phone too", () => {
    document.documentElement.dataset.fold = "rail side scene phone";
    const menu = open();
    expect(menu.classList.contains("menu-sheet")).toBe(false);
    expect(menu.style.left).not.toBe("");
  });
});
