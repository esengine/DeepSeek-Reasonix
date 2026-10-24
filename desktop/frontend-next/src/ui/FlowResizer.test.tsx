// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";
import { act, cleanup, fireEvent, render, renderHook } from "@testing-library/react";
import "./testkit";
import { barsFit, flowCeiling, flowFloor, FlowGrips, useColumn, type Column } from "./FlowResizer";

const RAIL_MIN = 232;
const DOCK_MIN = 320;

afterEach(cleanup);

describe("the reading column's two ends", () => {
  it("holds the readable measure as the floor and the window as the ceiling", () => {
    // 1600px window, both sidebars at their floors: 1048px is left for the text,
    // which is more than the readable measure, so 760 holds the floor.
    expect(flowCeiling(1600, RAIL_MIN, DOCK_MIN)).toBe(1048);
    expect(flowFloor(1600, RAIL_MIN, DOCK_MIN)).toBe(760);

    // A wide window leaves more still; the ceiling follows it, the floor does not.
    expect(flowCeiling(2400, RAIL_MIN, DOCK_MIN)).toBe(1848);
    expect(flowFloor(2400, RAIL_MIN, DOCK_MIN)).toBe(760);

    // On a window too small to hold the readable measure, the window becomes the
    // floor rather than a promise of room the layout cannot hand over.
    expect(flowFloor(1200, RAIL_MIN, DOCK_MIN)).toBe(648);
  });

  it("never reports a floor above its ceiling, however narrow the window", () => {
    for (const room of [900, 1200, 1600, 2400, 3440]) {
      expect(flowFloor(room, RAIL_MIN, DOCK_MIN)).toBeLessThanOrEqual(flowCeiling(room, RAIL_MIN, DOCK_MIN));
    }
  });

  it("never reports a ceiling below zero", () => {
    expect(flowCeiling(400, RAIL_MIN, DOCK_MIN)).toBe(0);
    expect(flowFloor(400, RAIL_MIN, DOCK_MIN)).toBe(0);
  });
});

describe("the two sidebars making room for the column", () => {
  it("leaves them alone while there is room for both", () => {
    expect(barsFit(2400, 760, 316, 316, RAIL_MIN, DOCK_MIN)).toEqual({ rail: 316, dock: 316 });
  });

  it("pins the rail at its floor rather than growing the other past its setting", () => {
    // 1300 - 760 leaves 540, under the 552 the two floors need. The rail is
    // pinned at its own floor; the workbench keeps the 316 it was set to rather
    // than growing into the room the rail gave up.
    const { rail, dock } = barsFit(1300, 760, 316, 316, RAIL_MIN, DOCK_MIN);
    expect(rail).toBe(RAIL_MIN);
    expect(dock).toBe(316);
    expect(rail + dock).toBeLessThan(316 + 316);
  });

  it("never narrows a sidebar past its own floor, and never past its setting", () => {
    for (const room of [1200, 1300, 1600, 2000, 2400]) {
      for (const ask of [760, 1000, 1400, 1800]) {
        const { rail, dock } = barsFit(room, ask, 400, 340, RAIL_MIN, DOCK_MIN);
        expect(rail).toBeGreaterThanOrEqual(RAIL_MIN);
        expect(dock).toBeGreaterThanOrEqual(DOCK_MIN);
        expect(rail).toBeLessThanOrEqual(400);
        expect(dock).toBeLessThanOrEqual(340);
      }
    }
  });

  it("recomputes from the settings, so narrowing hands the same room back", () => {
    // Derived from the settings on every call, never from the previous result:
    // that is what makes this reversible and keeps either sidebar from creeping
    // wider than the width it was set to.
    const squeezed = barsFit(1350, 760, 316, 316, RAIL_MIN, DOCK_MIN);
    const released = barsFit(2400, 760, 316, 316, RAIL_MIN, DOCK_MIN);
    expect(squeezed.rail + squeezed.dock).toBeLessThan(632);
    expect(released).toEqual({ rail: 316, dock: 316 });
    expect(barsFit(2400, 760, 316, 316, RAIL_MIN, DOCK_MIN)).toEqual(released);
  });

  it("answers for a width a hand is still holding, not only a committed one", () => {
    // A drag asks this on every frame. Committing on release only is what let
    // the column grow under sidebars that had not moved yet, so the answer has
    // to exist before anything is stored.
    localStorage.clear();
    const { result } = renderHook(() => useColumn(316, 400, RAIL_MIN, DOCK_MIN));
    const bars = result.current.fit(result.current.ceiling);
    expect(bars.rail).toBeGreaterThanOrEqual(RAIL_MIN);
    expect(bars.dock).toBeGreaterThanOrEqual(DOCK_MIN);
    expect(bars.rail + bars.dock).toBeLessThan(316 + 400);
    // The same rule the committed numbers obey, so committing cannot jump.
    expect(bars).toEqual(barsFit(document.body.clientWidth, result.current.ceiling, 316, 400, RAIL_MIN, DOCK_MIN));
  });
});

describe("the column's remembered width", () => {
  it("starts at the floor when nothing has been dragged", () => {
    localStorage.clear();
    const { result } = renderHook(() => useColumn(316, 316, RAIL_MIN, DOCK_MIN));
    expect(result.current.dragged).toBe(0);
    expect(result.current.width).toBe(result.current.floor);
    // Untouched means untouched: no inline variable outranks the measure setting.
    expect(result.current.rail).toBe(316);
    expect(result.current.dock).toBe(316);
  });

  it("remembers what a hand set, and holds it between the two ends", () => {
    localStorage.clear();
    const { result } = renderHook(() => useColumn(316, 316, RAIL_MIN, DOCK_MIN));
    act(() => result.current.commit(5000));
    // The number is kept as it was asked for...
    expect(result.current.dragged).toBe(5000);
    expect(localStorage.getItem("rx-flow-w")).toBe("5000");
    // ...while what is drawn stays inside the range this window allows.
    expect(result.current.width).toBeGreaterThanOrEqual(result.current.floor);
    expect(result.current.width).toBeLessThanOrEqual(result.current.ceiling);
  });

  it("keeps a remembered width below the floor from shrinking the column", () => {
    localStorage.clear();
    localStorage.setItem("rx-flow-w", "100");
    const { result } = renderHook(() => useColumn(316, 316, RAIL_MIN, DOCK_MIN));
    expect(result.current.width).toBe(result.current.floor);
    // Asked for, still remembered — the clamp is about what is drawn.
    expect(result.current.dragged).toBe(100);
  });
});

describe("a drag that ends without a release", () => {
  const into = (over: Partial<Column> = {}) => {
    const box = document.createElement("div");
    box.className = "app";
    document.body.appendChild(box);
    const column: Column = {
      width: 760, floor: 760, ceiling: 1048, rail: 316, dock: 316, dragged: 0,
      commit: () => {},
      fit: (w) => barsFit(2400, w, 316, 316, RAIL_MIN, DOCK_MIN),
      ...over,
    };
    render(<FlowGrips column={column} />, { container: box });
    return box;
  };

  const grip = (box: HTMLElement) => {
    const el = box.querySelector(".flowgrip-l") as HTMLElement;
    el.setPointerCapture = () => {};
    fireEvent.pointerDown(el, { button: 0, clientX: 400, pointerId: 1 });
    return el;
  };

  it("leaves no width behind when a first drag is cancelled", () => {
    // `App` writes `--flow-w` on `.app` only once a width has been committed.
    // A cancelled first drag must therefore take the value away again: setting
    // it back would leave a pixel width nobody owns, and the column would stay
    // pinned to it while the window changed around it.
    const box = into({ dragged: 0 });
    const el = grip(box);
    box.style.setProperty("--flow-w", "900px");
    fireEvent.pointerCancel(el, { pointerId: 1 });
    expect(box.style.getPropertyValue("--flow-w")).toBe("");
    // The sidebars are owned by `App` from the start, so those come back.
    expect(box.style.getPropertyValue("--rail-open")).toBe("316px");
  });

  it("puts a committed width back when a later drag is cancelled", () => {
    const box = into({ dragged: 900, width: 900 });
    const el = grip(box);
    box.style.setProperty("--flow-w", "980px");
    fireEvent.pointerCancel(el, { pointerId: 1 });
    expect(box.style.getPropertyValue("--flow-w")).toBe("900px");
  });
});

describe("the keys walk the same path the hand does", () => {
  const column = (over: Partial<Column> = {}): Column => ({
    width: 900, floor: 760, ceiling: 1048, rail: 300, dock: 400, dragged: 900,
    commit: () => {},
    fit: (w) => barsFit(2400, w, 300, 400, RAIL_MIN, DOCK_MIN),
    ...over,
  });

  const press = (edge: "l" | "r", key: string, shiftKey = false, from = 900) => {
    const seen: number[] = [];
    const { container, unmount } = render(
      <FlowGrips column={column({ width: from, commit: (w) => seen.push(w) })} />,
    );
    fireEvent.keyDown(container.querySelector(`.flowgrip-${edge}`)!, { key, shiftKey });
    unmount();
    return seen;
  };

  it("widens towards its own side on both handles", () => {
    // Left handle, ArrowLeft: the edge travels away from the text, so it grows.
    expect(press("l", "ArrowLeft")).toEqual([912]);
    expect(press("l", "ArrowRight")).toEqual([888]);
    // The right handle mirrors it — the same outward direction, the same result.
    expect(press("r", "ArrowRight")).toEqual([912]);
    expect(press("r", "ArrowLeft")).toEqual([888]);
  });

  it("takes a longer step with Shift and jumps to the floor with Home", () => {
    expect(press("l", "ArrowLeft", true)).toEqual([948]);
    expect(press("l", "Home")).toEqual([760]);
  });

  it("asks for nothing at either end rather than committing what it cannot reach", () => {
    // Already as wide as the window allows: widening again commits nothing.
    expect(press("l", "ArrowLeft", false, 1048)).toEqual([]);
    // Already at the readable measure: narrowing again commits nothing.
    expect(press("l", "ArrowRight", false, 760)).toEqual([]);
  });
});
