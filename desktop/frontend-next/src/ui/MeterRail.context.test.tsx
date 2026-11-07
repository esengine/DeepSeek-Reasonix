// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render } from "@testing-library/react";
import "./testkit";
import { MeterRail } from "./MeterRail";

afterEach(cleanup);

const rail = (used: number) => render(
  <MeterRail
    tps={0} trail={[]} running={false} speed={{ output: 0, modelSeconds: 0, average: 0, firstToken: 0 }}
    metrics={{ hit: 0, miss: 0 }} ctx={{ used, window: 100, items: [] } as never} mcp={[]} compact={{ phase: "idle" } as never} cost={[]} wallet={{} as never}
    hideAmounts={false} tasks={[]} jobs={[]} onSettings={vi.fn()} onCancelJob={vi.fn(async () => {})}
  />,
);
const level = (c: HTMLElement) => c.querySelector(".studio-meter-context")?.getAttribute("data-level");

describe("the context reading", () => {
  it("stays plain while there is room", () => {
    expect(level(rail(19).container)).toBeNull();
    cleanup();
    expect(level(rail(79).container)).toBeNull();
  });

  it("turns warm from 80% so the limit is seen coming, with no dialog", () => {
    const { container } = rail(86);
    expect(level(container)).toBe("high");
    expect(container.querySelector('[data-open] [role="dialog"]')).toBeNull();
  });
});
