// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render } from "@testing-library/react";
import "./testkit";
import { SettingsNav } from "./SettingsNav";
import { NAV } from "./prefsnav";

afterEach(() => { cleanup(); delete document.documentElement.dataset.fold; });
beforeEach(() => { document.documentElement.dataset.fold = "rail side scene phone"; });

const first = NAV.flatMap(([, items]) => items)[0][0];
const draw = (at = first) => {
  const values = new Proxy({}, { get: () => "" }) as never;
  return render(<SettingsNav at={at} values={values} shown={() => true} danger={() => false} disabled={false} onSelect={() => {}} />);
};

it("turns a mouse wheel over the category strip into a sideways scroll", () => {
  draw();
  const nav = document.querySelector<HTMLElement>(".prefs-nav")!;
  nav.scrollLeft = 0;
  const wheel = new WheelEvent("wheel", { deltaY: 90, bubbles: true, cancelable: true });
  nav.dispatchEvent(wheel);
  expect(nav.scrollLeft).toBe(90);
  expect(wheel.defaultPrevented).toBe(true);
});

it("leaves a sideways wheel alone", () => {
  draw();
  const nav = document.querySelector<HTMLElement>(".prefs-nav")!;
  const wheel = new WheelEvent("wheel", { deltaX: 40, deltaY: 0, bubbles: true, cancelable: true });
  nav.dispatchEvent(wheel);
  expect(wheel.defaultPrevented).toBe(false);
});

it("brings the chosen category into view in the strip", () => {
  const into = vi.fn();
  Element.prototype.scrollIntoView = into;
  const last = NAV.flatMap(([, items]) => items).at(-1)![0];
  const { rerender } = draw();
  into.mockClear();
  const values = new Proxy({}, { get: () => "" }) as never;
  rerender(<SettingsNav at={last} values={values} shown={() => true} danger={() => false} disabled={false} onSelect={() => {}} />);
  expect(into).toHaveBeenCalledWith(expect.objectContaining({ inline: "nearest", block: "nearest" }));
  void fireEvent;
});

it("does nothing to the wheel in the desktop's vertical list", () => {
  delete document.documentElement.dataset.fold;
  draw();
  const wheel = new WheelEvent("wheel", { deltaY: 90, bubbles: true, cancelable: true });
  document.querySelector(".prefs-nav")!.dispatchEvent(wheel);
  expect(wheel.defaultPrevented).toBe(false);
});
