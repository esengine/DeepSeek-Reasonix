// @vitest-environment jsdom
import "./testkit";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { Picker } from "./Menu";
import { refresh } from "./viewport";

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  delete document.documentElement.dataset.fold;
  Reflect.deleteProperty(document.body, "clientWidth");
  Reflect.deleteProperty(document.body, "clientHeight");
});

it("repositions an open menu after the body observer publishes a responsive fold", async () => {
  document.documentElement.dataset.fold = "rail side scene";
  Object.defineProperty(document.body, "clientWidth", { configurable: true, value: 1440 });
  Object.defineProperty(document.body, "clientHeight", { configurable: true, value: 1000 });
  const real = HTMLElement.prototype.getBoundingClientRect;
  let widePosition = 250;
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(function (this: HTMLElement) {
    if (this.getAttribute("aria-haspopup") !== "menu") return real.call(this);
    const x = document.documentElement.dataset.fold ? 85 : widePosition;
    return { x, y: 600, left: x, right: x + 112, top: 600, bottom: 628, width: 112, height: 28, toJSON: () => ({}) };
  });
  render(<Picker label="Choose" items={[{ value: "a", label: "A" }]} onPick={() => {}} place="bottom" />);
  await userEvent.click(screen.getByRole("button", { name: "Choose" }));
  const menu = screen.getByRole("menu");
  expect(menu.style.left).toBe("85px");
  fireEvent.resize(window);
  expect(menu.style.left).toBe("85px");
  act(() => refresh());
  expect(menu.style.left).toBe("250px");
  widePosition = 570;
  fireEvent.transitionEnd(document.body);
  expect(menu.style.left).toBe("570px");
  await userEvent.keyboard("{Escape}");
  const closedPosition = menu.style.left;
  Object.defineProperty(document.body, "clientWidth", { configurable: true, value: 390 });
  act(() => refresh());
  expect(menu.style.left).toBe(closedPosition);
});
