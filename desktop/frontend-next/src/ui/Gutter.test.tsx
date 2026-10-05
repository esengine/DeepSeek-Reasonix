// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render } from "@testing-library/react";
import { DOCK, Gutter, RAIL, dockMax, widthOf } from "./Gutter";

afterEach(cleanup);

describe("workbench width on wide displays", () => {
  it("keeps the old limit on ordinary windows and leaves half a wide window for the other columns", () => {
    expect(dockMax(1600)).toBe(880);
    expect(dockMax(3440)).toBe(1600);
  });

  it("lets the keyboard widen the workbench past 880 only when there is room", () => {
    let width = 880;
    const onWidth = vi.fn((next: number) => { width = next; });
    const view = render(
      <Gutter edge="r" span={DOCK} width={width} max={dockMax(3440)} label="Workbench width"
        open onWidth={onWidth} onOpen={() => {}} />,
    );
    const separator = view.getByRole("separator");
    fireEvent.keyDown(separator, { key: "ArrowLeft", shiftKey: true });
    expect(width).toBe(928);
    expect(separator.getAttribute("aria-valuemax")).toBe("1600");
    onWidth.mockClear();

    view.rerender(
      <Gutter edge="r" span={DOCK} width={880} max={dockMax(1600)} label="Workbench width"
        open onWidth={onWidth} onOpen={() => {}} />,
    );
    fireEvent.keyDown(separator, { key: "ArrowLeft", shiftKey: true });
    expect(onWidth).not.toHaveBeenCalled();
    expect(separator.getAttribute("aria-valuemax")).toBe("880");
  });
});

describe("workspace column floor", () => {
  it("raises a width saved below the floor, where the session filters no longer fit", () => {
    localStorage.setItem(RAIL.key, "176");
    expect(widthOf(RAIL)).toBe(RAIL.min);
    expect(RAIL.min).toBeGreaterThanOrEqual(232);
  });
});
