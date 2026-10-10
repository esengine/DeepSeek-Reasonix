// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "../testkit";
import { ChartCard } from "./ChartCard";
import { SALES, withSpec } from "./fixtures";
import type { ChartSpec } from "./spec";

afterEach(cleanup);

const draw = (spec: ChartSpec) => render(<ChartCard spec={spec} callId="call-1" />).container;

const series = (): ChartSpec => withSpec({
  data: { columns: [{ name: "d", type: "date" }, { name: "v", type: "number" }], rows: [["2024-01-01", 1], ["2024-02-01", null], ["2024-03-01", 3], ["2024-04-01", 4]] },
  marks: [{ type: "line", x: "d", y: ["v"] }],
});

describe("a chart card", () => {
  it("names itself, its source call and its content for assistive tech", () => {
    const box = draw(SALES);
    expect(box.querySelector(".call")?.getAttribute("data-call")).toBe("call-1");
    expect(box.querySelector(".hl .nm")?.textContent).toBe("图表");
    const label = box.querySelector("figure")?.getAttribute("aria-label") ?? "";
    expect(label).toContain("Sales");
    expect(label).toContain("3");
  });

  it("draws one bar per row, each reachable by keyboard", async () => {
    const box = draw(SALES);
    const bars = box.querySelectorAll(".bar");
    expect(bars).toHaveLength(3);
    await userEvent.tab();
    await userEvent.tab();
    expect(document.activeElement?.getAttribute("aria-label")).toBe("revenue · Jan · 10");
    expect(box.querySelector(".chart-read")?.textContent).toBe("revenue · Jan · 10");
  });

  it("never hides a focusable point from assistive tech and announces the readout", () => {
    for (const spec of [SALES, series(), withSpec({ marks: [{ type: "pie", x: "month", y: ["revenue"] }] })]) {
      const box = draw(spec);
      expect(box.querySelectorAll("[tabindex]").length).toBeGreaterThan(0);
      expect(box.querySelectorAll('[aria-hidden="true"] [tabindex], [aria-hidden="true"][tabindex]')).toHaveLength(0);
      expect(box.querySelector(".chart-read")?.getAttribute("aria-live")).toBe("polite");
      cleanup();
    }
  });

  it("says so when a pie has nothing to draw", () => {
    const box = draw(withSpec({ data: { ...SALES.data, rows: [["a", 0], ["b", null]] }, marks: [{ type: "pie", x: "month", y: ["revenue"] }] }));
    expect(box.querySelector(".chart-empty")?.textContent).toBe("没有可画的数据");
  });

  it("keeps a plot at its drawn size and lets it scroll by keyboard instead of shrinking", () => {
    const box = draw(SALES);
    const mark = box.querySelector(".chart-pane")!;
    expect(mark.getAttribute("tabindex")).toBe("0");
    expect(mark.getAttribute("role")).toBe("region");
    expect(mark.getAttribute("aria-label")).toBe("图表：Sales（可横向滚动）");
    expect((box.querySelector(".chart-svg") as SVGElement).style.minWidth).toBe("640px");
  });

  it("shows when there is more to scroll to, and from which side", () => {
    const size = (w: number, c: number) => {
      Object.defineProperty(HTMLElement.prototype, "scrollWidth", { configurable: true, get: () => w });
      Object.defineProperty(HTMLElement.prototype, "clientWidth", { configurable: true, get: () => c });
    };
    size(640, 320);
    const box = draw(SALES);
    const wrap = box.querySelector(".chart-scroll")!;
    expect(wrap.hasAttribute("data-more-right")).toBe(true);
    expect(wrap.hasAttribute("data-more-left")).toBe(false);
    const mark = box.querySelector(".chart-pane") as HTMLElement;
    mark.scrollLeft = 320;
    fireEvent.scroll(mark);
    expect(wrap.hasAttribute("data-more-left")).toBe(true);
    expect(wrap.hasAttribute("data-more-right")).toBe(false);
    cleanup();
    size(640, 640);
    expect(draw(SALES).querySelector(".chart-scroll")!.hasAttribute("data-more-right")).toBe(false);
    delete (HTMLElement.prototype as unknown as Record<string, unknown>).scrollWidth;
    delete (HTMLElement.prototype as unknown as Record<string, unknown>).clientWidth;
  });

  it("breaks a line at a null instead of bridging it", () => {
    const box = draw(series());
    expect(box.querySelectorAll(".line path:not(.line-hit)")).toHaveLength(2);
  });

  it("shows the data as a real table on request and hides it again", async () => {
    const box = draw(SALES);
    expect(box.querySelector("table")).toBeNull();
    const toggle = screen.getByRole("button", { name: "查看数据" });
    expect(toggle.getAttribute("aria-expanded")).toBe("false");
    await userEvent.click(toggle);
    expect(toggle.getAttribute("aria-expanded")).toBe("true");
    expect([...box.querySelectorAll("th")].map((h) => h.textContent)).toEqual(["month", "revenue"]);
    expect(box.querySelectorAll("tbody tr")).toHaveLength(3);
    await userEvent.click(screen.getByRole("button", { name: "收起数据" }));
    expect(box.querySelector("table")).toBeNull();
  });

  it("caps the table and says how many rows it left out", async () => {
    const rows = Array.from({ length: 250 }, (_, i) => [`c${i}`, i]);
    const box = draw(withSpec({ data: { ...SALES.data, rows } }));
    await userEvent.click(screen.getByRole("button", { name: "查看数据" }));
    expect(box.querySelectorAll("tbody tr")).toHaveLength(200);
    expect(box.querySelector(".chart-more")?.textContent).toBe("另有 50 行");
  });

  it("draws hostile strings as inert text", async () => {
    const evil = '<img src=x onerror=alert(1)><script>alert(1)</script>';
    const box = draw(withSpec({
      title: evil,
      data: { columns: [{ name: "m", type: "string" }, { name: "v", type: "number" }], rows: [[evil, 1]] },
      marks: [{ type: "bar", x: "m", y: ["v"] }],
      y_axis: { title: evil, unit: evil },
    }));
    await userEvent.click(screen.getByRole("button", { name: "查看数据" }));
    expect(box.querySelector("script, img, [onerror]")).toBeNull();
    expect(box.textContent).toContain("<script>");
  });

  it("draws a pie's slices with their share and a donut with a hole", () => {
    const pie = (donut: boolean) => draw(withSpec({ marks: [{ type: "pie", x: "month", y: ["revenue"], donut }] }));
    const solid = pie(false);
    expect(solid.querySelectorAll(".slice")).toHaveLength(3);
    expect(solid.querySelector(".chart-key .v")?.textContent).toBe("16.7%");
    cleanup();
    expect(pie(true).querySelector(".slice")?.getAttribute("d")).not.toBe(solid.querySelector(".slice")?.getAttribute("d"));
  });

  it("gives every series a name in the legend and a second channel past the fifth", () => {
    const groups = Array.from({ length: 7 }, (_, i) => `g${i}`);
    const rows = groups.map((g) => ["a", g, 1]);
    const box = draw(withSpec({
      data: { columns: [{ name: "x", type: "string" }, { name: "g", type: "string" }, { name: "v", type: "number" }], rows },
      marks: [{ type: "line", x: "x", y: ["v"], color: "g" }],
    }));
    expect(box.querySelectorAll(".chart-key li")).toHaveLength(7);
    expect(box.querySelectorAll(".line.sv1")).toHaveLength(2);
  });

  it("hatches bars from the sixth series on", () => {
    const groups = Array.from({ length: 7 }, (_, i) => `g${i}`);
    const box = draw(withSpec({
      data: { columns: [{ name: "x", type: "string" }, { name: "g", type: "string" }, { name: "v", type: "number" }], rows: groups.map((g) => ["a", g, 1]) },
      marks: [{ type: "bar", x: "x", y: ["v"], color: "g" }],
    }));
    expect(box.querySelectorAll(".bar .hatch")).toHaveLength(2);
  });

  it("stacks bars and keeps a spec with several marks as several plots", () => {
    const box = draw(withSpec({ marks: [{ type: "bar", x: "month", y: ["revenue"], stacked: true }, { type: "line", x: "month", y: ["revenue"] }] }));
    expect(box.querySelectorAll(".chart-mark")).toHaveLength(2);
  });

  it("carries no animation of its own", () => {
    const box = draw(SALES);
    expect(box.querySelector("animate, animateTransform, [style*='animation']")).toBeNull();
  });

  it("handles an empty data set and an all-zero pie without throwing", () => {
    expect(() => draw(withSpec({ data: { ...SALES.data, rows: [] } }))).not.toThrow();
    cleanup();
    expect(() => draw(withSpec({ data: { ...SALES.data, rows: [["a", 0]] }, marks: [{ type: "pie", x: "month", y: ["revenue"] }] }))).not.toThrow();
  });
});
