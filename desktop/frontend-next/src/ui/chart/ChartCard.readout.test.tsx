// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render } from "@testing-library/react";
import "../testkit";
import { ChartCard } from "./ChartCard";
import { SALES, withSpec } from "./fixtures";
import type { ChartSpec } from "./spec";

afterEach(() => { cleanup(); vi.restoreAllMocks(); });

const hint = "悬停或聚焦图表以查看数值";
const draw = (spec: ChartSpec) => render(<ChartCard spec={spec} />).container;
const readout = (box: HTMLElement) => box.querySelector(".chart-read")?.textContent;

function move(path: Element, x: number) {
  const svg = path.closest("svg")!;
  vi.spyOn(svg, "getBoundingClientRect").mockReturnValue({ left: -100, width: 1280 } as DOMRect);
  fireEvent.mouseMove(path, { clientX: -100 + x * 2 });
}

describe("chart value discovery", () => {
  it("offers a visible hint, reads hovered bars, and restores the hint on leave", () => {
    const box = draw(SALES);
    expect(readout(box)).toBe(hint);
    const bar = box.querySelector(".bar")!;
    fireEvent.mouseEnter(bar);
    expect(readout(box)).toBe("revenue · Jan · 10");
    fireEvent.mouseLeave(bar);
    expect(readout(box)).toBe(hint);
  });

  it("gives bars, line points and pie slices native titles matching their accessible values", () => {
    for (const type of ["bar", "line", "pie"] as const) {
      const box = draw(withSpec({ marks: [{ type, x: "month", y: ["revenue"] }], y_axis: { unit: "USD" } }));
      const marks = box.querySelectorAll(type === "bar" ? ".bar" : type === "line" ? ".pt" : ".slice");
      expect(marks).toHaveLength(3);
      for (const mark of marks) {
        expect(mark.querySelector("title")?.textContent).toBe(mark.getAttribute("aria-label"));
        expect(mark.querySelector("title")?.textContent).toContain("USD");
      }
      cleanup();
    }
  });

  it("reads dense lines along the path even when point markers are omitted", () => {
    const rows = Array.from({ length: 61 }, (_, i) => [`day ${i}`, i]);
    const box = draw(withSpec({ data: { ...SALES.data, rows }, marks: [{ type: "line", x: "month", y: ["revenue"] }] }));
    expect(box.querySelector(".pt")).toBeNull();
    const path = box.querySelector(".line-hit")!;
    expect(path).not.toBeNull();
    move(path, 330);
    expect(readout(box)).toBe("revenue · day 30 · 30");
    move(path, 20);
    expect(readout(box)).toBe("revenue · day 0 · 0");
    move(path, 630);
    expect(readout(box)).toBe("revenue · day 60 · 60");
    fireEvent.mouseLeave(path);
    expect(readout(box)).toBe(hint);
  });

  it("uses numeric positions, the hovered series and only points in that continuous run", () => {
    const box = draw(withSpec({
      data: { columns: [{ name: "x", type: "number" }, { name: "a", type: "number" }, { name: "b", type: "number" }], rows: [[100, 10, 100], [0, 0, 0], [80, 8, 80], [20, null, 20]] },
      marks: [{ type: "line", x: "x", y: ["a", "b"] }], y_axis: { unit: "kg" },
    }));
    const paths = box.querySelectorAll(".line-hit");
    expect(paths).toHaveLength(3);
    move(paths[1], 160);
    expect(readout(box)).toBe("a · 80 · 8 kg");
    move(paths[2], 160);
    expect(readout(box)).toBe("b · 20 · 20 kg");
    expect(box.querySelectorAll(".line path:not(.line-hit)")).toHaveLength(3);
    fireEvent.focus(box.querySelector('.pt[aria-label="a · 100 · 10 kg"]')!);
    expect(readout(box)).toBe("a · 100 · 10 kg");
    fireEvent.blur(box.querySelector('.pt[aria-label="a · 100 · 10 kg"]')!);
    expect(readout(box)).toBe(hint);
  });

  it("keeps an isolated point readable when a dense series has gaps", () => {
    const rows = Array.from({ length: 61 }, (_, i) => [`day ${i}`, i === 30 ? 42 : null]);
    const box = draw(withSpec({ data: { ...SALES.data, rows }, marks: [{ type: "line", x: "month", y: ["revenue"] }] }));
    const path = box.querySelector(".line-hit")!;
    expect(path).not.toBeNull();
    move(path, 330);
    expect(readout(box)).toBe("revenue · day 30 · 42");
  });

  it("keeps hostile title text inert and tolerates an empty line", () => {
    const evil = "<img src=x onerror=alert(1)>";
    const box = draw(withSpec({ data: { ...SALES.data, rows: [[evil, 10]] } }));
    expect(box.querySelector(".bar title")?.textContent).toBe(`revenue · ${evil} · 10`);
    expect(box.querySelector("img, script, [onerror]")).toBeNull();
    cleanup();
    const empty = draw(withSpec({ data: { ...SALES.data, rows: [] }, marks: [{ type: "line", x: "month", y: ["revenue"] }] }));
    expect(empty.querySelector(".line-hit")).toBeNull();
    expect(readout(empty)).toBe(hint);
  });
});
