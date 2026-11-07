// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import "./testkit";
import { EffortColumn } from "./EffortColumn";
import type { MenuItem } from "./Menu";
import { boot, STORAGE } from "../i18n";

const PHONE = "rail side scene phone";

const levels: MenuItem[] = [
  { value: "auto", label: "自动", strength: 1, recommended: true, desc: "按任务调整" },
  { value: "low", label: "快速", strength: 1, desc: "轻量" },
  { value: "medium", label: "平衡", strength: 2, desc: "适中" },
  { value: "high", label: "深入", strength: 3, desc: "更深" },
];
const heading: MenuItem = { value: "__h", label: "推理强度", right: "deepseek-flash", header: true };
const mode: MenuItem = { value: "mode:pro", label: "Pro 模式", toggle: false };

beforeEach(() => {
  localStorage.setItem(STORAGE, "zh");
  boot();
});
afterEach(() => {
  cleanup();
  localStorage.clear();
  delete document.documentElement.dataset.fold;
});

function draw(items: MenuItem[], current = "low") {
  const onPick = vi.fn();
  render(<div><EffortColumn items={items} current={current} onPick={onPick} /></div>);
  return onPick;
}

describe("the reasoning levels on a phone", () => {
  beforeEach(() => { document.documentElement.dataset.fold = PHONE; });

  it("are one slider with a step per level, standing on the current one", () => {
    draw([heading, ...levels]);
    const slider = screen.getByRole("slider", { name: "推理强度" }) as HTMLInputElement;
    expect(slider.min).toBe("0");
    expect(slider.max).toBe("3");
    expect(slider.value).toBe("1");
    expect(slider.getAttribute("aria-valuetext")).toBe("快速");
    expect(screen.queryAllByRole("menuitem")).toHaveLength(0);
  });

  it("names the level in large type, and says which model it is for", () => {
    const view = draw([heading, ...levels], "high");
    expect(view === undefined).toBe(false);
    expect(document.querySelector(".effort-slider .es-name")?.textContent).toBe("深入");
    expect(document.querySelector(".effort-slider .es-model")?.textContent).toBe("deepseek-flash");
  });

  it("follows the thumb while it moves and chooses nothing until it is let go", () => {
    const onPick = draw([heading, ...levels]);
    const slider = screen.getByRole("slider") as HTMLInputElement;
    fireEvent.change(slider, { target: { value: "2" } });
    expect(document.querySelector(".es-name")?.textContent).toBe("平衡");
    expect(onPick).not.toHaveBeenCalled();
    fireEvent.pointerUp(slider);
    expect(onPick).toHaveBeenCalledTimes(1);
    expect(onPick).toHaveBeenCalledWith("medium");
  });

  it("chooses on a key press that moves it, once the key is released", () => {
    const onPick = draw([heading, ...levels]);
    const slider = screen.getByRole("slider") as HTMLInputElement;
    fireEvent.change(slider, { target: { value: "3" } });
    fireEvent.keyUp(slider, { key: "ArrowRight" });
    expect(onPick).toHaveBeenCalledWith("high");
  });

  it("does not choose again when it is let go where it already was", () => {
    const onPick = draw([heading, ...levels]);
    fireEvent.pointerUp(screen.getByRole("slider"));
    expect(onPick).not.toHaveBeenCalled();
  });

  it("leaves a mode switch as the row it was", () => {
    const onPick = draw([heading, ...levels, mode]);
    fireEvent.click(screen.getByRole("menuitemcheckbox", { name: /Pro 模式/ }));
    expect(onPick).toHaveBeenCalledWith("mode:pro");
  });

  it("has nothing to slide between with one level, and keeps the buttons", () => {
    draw([heading, levels[0]]);
    expect(screen.queryByRole("slider")).toBeNull();
    expect(screen.getAllByRole("menuitem")).toHaveLength(1);
  });
});

describe("the same levels on a wider screen", () => {
  it("are the rows they were, with no slider", () => {
    draw([heading, ...levels]);
    expect(screen.queryByRole("slider")).toBeNull();
    expect(screen.getAllByRole("menuitem")).toHaveLength(4);
  });
});
