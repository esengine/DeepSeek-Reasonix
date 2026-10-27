// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { createRef } from "react";
import { TooltipButton } from "./TooltipButton";
import { ViewSwitch } from "./ViewSwitch";

afterEach(() => { cleanup(); vi.useRealTimers(); });

it("waits 400ms for hover and cancels both pending and visible hints on leave", () => {
  vi.useFakeTimers();
  render(<TooltipButton title="复制回复">Copy</TooltipButton>);
  const button = screen.getByRole("button");
  expect(button.hasAttribute("title")).toBe(false);
  fireEvent.pointerEnter(button);
  act(() => vi.advanceTimersByTime(399));
  expect(screen.queryByRole("tooltip")).toBeNull();
  fireEvent.pointerLeave(button);
  act(() => vi.advanceTimersByTime(400));
  expect(screen.queryByRole("tooltip")).toBeNull();
  fireEvent.pointerEnter(button);
  act(() => vi.advanceTimersByTime(400));
  expect(screen.getByRole("tooltip").textContent).toBe("复制回复");
  expect(screen.getByRole("tooltip").parentElement).toBe(document.body);
  expect(button.getAttribute("aria-describedby")).toBe(screen.getByRole("tooltip").id);
  fireEvent.pointerLeave(button);
  expect(screen.queryByRole("tooltip")).toBeNull();
});

it("supports keyboard focus, Escape, current-state labels and the existing button ref", () => {
  const ref = createRef<HTMLButtonElement>();
  const clicked = vi.fn();
  const view = render(<TooltipButton ref={ref} title="Expand" shortcut="Shortcut" onClick={clicked}>Toggle</TooltipButton>);
  const button = screen.getByRole("button");
  expect(ref.current).toBe(button);
  fireEvent.focus(button);
  expect(screen.getByRole("tooltip").textContent).toContain("Shortcut");
  view.rerender(<TooltipButton ref={ref} title="Restore" onClick={clicked}>Toggle</TooltipButton>);
  expect(screen.getByRole("tooltip").textContent).toBe("Restore");
  fireEvent.keyDown(button, { key: "Escape" });
  expect(screen.queryByRole("tooltip")).toBeNull();
  fireEvent.blur(button);
  fireEvent.focus(button);
  fireEvent.click(button);
  expect(clicked).toHaveBeenCalledOnce();
  expect(screen.queryByRole("tooltip")).toBeNull();
});

it("cancels delayed work when the button unmounts", () => {
  vi.useFakeTimers();
  const view = render(<TooltipButton title="文件">Files</TooltipButton>);
  fireEvent.pointerEnter(screen.getByRole("button"));
  view.unmount();
  act(() => vi.advanceTimersByTime(400));
  expect(screen.queryByRole("tooltip")).toBeNull();
});

it("shows only the latest hint when hover moves away from a keyboard-focused control", () => {
  vi.useFakeTimers();
  render(<><TooltipButton title="引用">Quote</TooltipButton><TooltipButton title="更多">More</TooltipButton></>);
  fireEvent.focus(screen.getByRole("button", { name: "Quote" }));
  fireEvent.pointerEnter(screen.getByRole("button", { name: "More" }));
  act(() => vi.advanceTimersByTime(400));
  expect(screen.getAllByRole("tooltip")).toHaveLength(1);
  expect(screen.getByRole("tooltip").textContent).toBe("更多");
});

it("describes opening and closing the top-bar workbench without changing its control identity", () => {
  const onView = vi.fn();
  const view = render(<ViewSwitch view="flow" rows={1} surfaces={2} onView={onView} />);
  const button = screen.getByRole("button", { name: "工作台" });
  fireEvent.focus(button);
  expect(screen.getByRole("tooltip").textContent).toContain("打开工作台");
  expect(screen.getByRole("tooltip").textContent).toContain("3");
  view.rerender(<ViewSwitch view="browser" rows={1} surfaces={2} onView={onView} />);
  expect(screen.getByRole("button", { name: "工作台" })).toBe(button);
  expect(screen.getByRole("tooltip").textContent).toContain("关闭工作台");
  fireEvent.click(button);
  expect(onView).toHaveBeenCalledWith("flow");
});
