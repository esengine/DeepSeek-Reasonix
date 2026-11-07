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

describe("the slider's drawing", () => {
  it("is drawn in layers the reader never meets: decoration, hidden from the accessibility tree", () => {
    document.documentElement.dataset.fold = PHONE;
    draw([heading, ...levels]);
    const fill = document.querySelector(".effort-slider .es-fill");
    expect(fill).not.toBeNull();
    expect(fill?.getAttribute("aria-hidden")).toBe("true");
    expect(document.querySelector(".effort-slider .es-line")?.getAttribute("aria-hidden")).toBe("true");
  });

  it("wears the product's own mark in its thumb", () => {
    document.documentElement.dataset.fold = PHONE;
    draw([heading, ...levels]);
    expect(document.querySelector(".effort-slider .es-thumb svg.es-r")).not.toBeNull();
  });

  it("keeps the track's reach in step with the thumb", () => {
    document.documentElement.dataset.fold = PHONE;
    draw([heading, ...levels], "high");
    const track = document.querySelector<HTMLElement>(".effort-slider .es-track")!;
    expect(track.style.getPropertyValue("--p")).toBe("1");
    fireEvent.change(screen.getByRole("slider"), { target: { value: "1" } });
    expect(Number(track.style.getPropertyValue("--p"))).toBeCloseTo(1 / 3, 5);
  });
});

describe("dragging the slider", () => {
  beforeEach(() => { document.documentElement.dataset.fold = PHONE; });
  const track = () => document.querySelector<HTMLElement>(".effort-slider .es-track")!;

  it("moves continuously between levels, naming the nearest one", () => {
    const onPick = draw([heading, ...levels]);
    const slider = screen.getByRole("slider") as HTMLInputElement;
    fireEvent.change(slider, { target: { value: "2.4" } });
    expect(Number(track().style.getPropertyValue("--p"))).toBeCloseTo(0.8, 5);
    expect(document.querySelector(".es-name")?.textContent).toBe("平衡");
    fireEvent.change(slider, { target: { value: "2.6" } });
    expect(document.querySelector(".es-name")?.textContent).toBe("深入");
    expect(onPick).not.toHaveBeenCalled();
  });

  it("lets go onto the nearest level, and chooses that one", () => {
    const onPick = draw([heading, ...levels]);
    const slider = screen.getByRole("slider") as HTMLInputElement;
    fireEvent.change(slider, { target: { value: "2.4" } });
    fireEvent.pointerUp(slider);
    expect(onPick).toHaveBeenCalledTimes(1);
    expect(onPick).toHaveBeenCalledWith("medium");
    expect(Number(track().style.getPropertyValue("--p"))).toBeCloseTo(2 / 3, 5);
  });

  it("marks the track while the thumb is held, so it follows the finger without lag", () => {
    draw([heading, ...levels]);
    const slider = screen.getByRole("slider");
    expect(track().hasAttribute("data-drag")).toBe(false);
    fireEvent.pointerDown(slider);
    expect(track().hasAttribute("data-drag")).toBe(true);
    fireEvent.pointerUp(slider);
    expect(track().hasAttribute("data-drag")).toBe(false);
  });

  it("steps a level at a time from the keyboard, and chooses when the key is released", () => {
    const onPick = draw([heading, ...levels], "low");
    const slider = screen.getByRole("slider");
    fireEvent.keyDown(slider, { key: "ArrowRight" });
    expect(document.querySelector(".es-name")?.textContent).toBe("平衡");
    fireEvent.keyUp(slider, { key: "ArrowRight" });
    expect(onPick).toHaveBeenCalledWith("medium");
  });

  it("jumps to either end with Home and End", () => {
    const onPick = draw([heading, ...levels], "low");
    const slider = screen.getByRole("slider");
    fireEvent.keyDown(slider, { key: "End" });
    fireEvent.keyUp(slider, { key: "End" });
    expect(onPick).toHaveBeenLastCalledWith("high");
  });

  it("draws its own thumb, so the settle onto a level can be animated", () => {
    draw([heading, ...levels]);
    const thumb = document.querySelector(".effort-slider .es-thumb");
    expect(thumb).not.toBeNull();
    expect(thumb?.getAttribute("aria-hidden")).toBe("true");
  });
});

describe("the slider's steps", () => {
  beforeEach(() => { document.documentElement.dataset.fold = PHONE; });

  it("shows a dot on the track for every level, lit up to the current one", () => {
    draw([heading, ...levels], "medium");
    const dots = [...document.querySelectorAll(".effort-slider .es-tick")];
    expect(dots).toHaveLength(4);
    expect(dots.map((d) => d.hasAttribute("data-on"))).toEqual([true, true, true, false]);
    expect(dots.every((d) => d.getAttribute("aria-hidden") === "true")).toBe(true);
  });

  it("names every level under its dot, and marks the one it is standing on", () => {
    draw([heading, ...levels], "low");
    const marks = [...document.querySelectorAll(".effort-slider .es-mark")];
    expect(marks.map((m) => m.textContent)).toEqual(["自动", "快速", "平衡", "深入"]);
    expect(marks.map((m) => m.hasAttribute("data-on"))).toEqual([false, true, false, false]);
  });

  it("moves the lit dots and the marked name with the thumb while it is held", () => {
    draw([heading, ...levels], "low");
    fireEvent.change(screen.getByRole("slider"), { target: { value: "2.9" } });
    expect([...document.querySelectorAll(".es-tick")].map((d) => d.hasAttribute("data-on"))).toEqual([true, true, true, true]);
    expect([...document.querySelectorAll(".es-mark")].map((m) => m.hasAttribute("data-on"))).toEqual([false, false, false, true]);
  });
});
