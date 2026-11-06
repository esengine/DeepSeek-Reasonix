// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render } from "@testing-library/react";
import "./testkit";
import { DOCK, Gutter, RAIL } from "./Gutter";

const pointer = (type: string, x: number) => new MouseEvent(type, { bubbles: true, cancelable: true, clientX: x, button: 0 }) as MouseEvent & { pointerId: number };
const send = (el: Element, type: string, x: number) => {
  const ev = pointer(type, x);
  Object.defineProperty(ev, "pointerId", { value: 1 });
  fireEvent(el, ev);
};

function mount(open: boolean, edge: "l" | "r" = "r") {
  const span = edge === "l" ? RAIL : DOCK;
  const onOpen = vi.fn();
  const onWidth = vi.fn();
  const view = render(
    <div className="app">
      <section className="pane">
        <Gutter edge={edge} span={span} width={span.def} label="resize" open={open} onWidth={onWidth} onOpen={onOpen} />
      </section>
    </div>,
  );
  const bar = view.container.querySelector(".gutter") as HTMLElement;
  return { onOpen, onWidth, bar, app: view.container.querySelector(".app") as HTMLElement };
}

beforeEach(() => {
  HTMLElement.prototype.setPointerCapture = vi.fn();
  HTMLElement.prototype.releasePointerCapture = vi.fn();
});
afterEach(cleanup);

describe.each(["l", "r"] as const)("the %s column's drag handle", (edge) => {
  const renderHandle = (open: boolean) => mount(open, edge);
  const widening = edge === "l" ? 1 : -1;
  it("draws no handle: the divider is the strip itself", () => {
    const { bar } = renderHandle(true);
    expect(bar.querySelector("button, .grip, .knurl")).toBeNull();
  });

  it("opens a shut column when its edge is dragged out", () => {
    const { bar, app, onOpen } = renderHandle(false);
    send(bar, "pointerdown", 500);
    expect(app.dataset.drag).toBe("pointer");
    send(bar, "pointermove", 500 + widening * 130);
    send(bar, "pointerup", 500 + widening * 130);
    expect(app.dataset.drag).toBeUndefined();
    expect(onOpen).toHaveBeenCalledWith(true);
  });

  it("leaves a shut column shut for a press that goes nowhere", () => {
    const { bar, onOpen } = renderHandle(false);
    send(bar, "pointerdown", 500);
    send(bar, "pointerup", 500);
    expect(onOpen).not.toHaveBeenCalled();
  });

  it("still drags from the strip beside the edge", () => {
    const { bar, app, onWidth } = renderHandle(true);
    send(bar, "pointerdown", 500);
    expect(app.dataset.drag).toBe("pointer");
    send(bar, "pointermove", 500 + widening * 50);
    send(bar, "pointerup", 500 + widening * 50);
    expect(onWidth).toHaveBeenCalledWith((edge === "l" ? RAIL : DOCK).def + 50);
  });

  it("toggles from the keyboard", () => {
    const { bar, onOpen } = renderHandle(true);
    fireEvent.keyDown(bar, { key: "Enter" });
    expect(onOpen).toHaveBeenCalledWith(false);
  });

  it("supports keyboard sizing and restoring the default width", () => {
    const { bar, onWidth } = renderHandle(true);
    const span = edge === "l" ? RAIL : DOCK;
    fireEvent.keyDown(bar, { key: widening === 1 ? "ArrowRight" : "ArrowLeft" });
    expect(onWidth).toHaveBeenLastCalledWith(span.def + 12);
    fireEvent.keyDown(bar, { key: "Home" });
    expect(onWidth).toHaveBeenLastCalledWith(span.def);
  });
});
