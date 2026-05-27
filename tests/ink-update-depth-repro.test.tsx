import { Box, Text, useAnimationFrame, useBoxMetrics } from "ink";
import React from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { render } from "./helpers/ink-test.js";

const originalError = console.error;
const captured: string[] = [];

function captureErrors() {
  captured.length = 0;
  console.error = (...args: unknown[]) => {
    captured.push(args.map((a) => (a instanceof Error ? a.message : String(a))).join(" "));
  };
}
function restoreErrors() {
  console.error = originalError;
}

function hasMaxDepth(): boolean {
  return captured.some((m) => /Maximum update depth/.test(m));
}

afterEach(() => {
  restoreErrors();
  vi.useRealTimers();
});

describe("Ink update-depth repro candidates", () => {
  it("useBoxMetrics: stable layout does not loop", async () => {
    captureErrors();
    function Probe() {
      const ref = React.useRef(null!);
      const m = useBoxMetrics(ref);
      return (
        <Box ref={ref} flexDirection="column">
          <Text>{`h=${m.height}`}</Text>
          <Text>line a</Text>
          <Text>line b</Text>
        </Box>
      );
    }
    const r = render(<Probe />);
    await new Promise((res) => setTimeout(res, 80));
    expect(hasMaxDepth()).toBe(false);
    r.unmount();
  });

  it("useBoxMetrics: oscillating call sites no longer crash React's depth guard", async () => {
    // useBoxMetrics defers each measure off the React commit batch
    // (setTimeout 0), so a Box that renders from its own measurement
    // still oscillates between heights but never trips "Maximum update
    // depth exceeded". The property under test is the absence of a
    // crash + the presence of the underlying anti-pattern (heights
    // alternate, proving it's not silently converging).
    captureErrors();
    let stableRenders = 0;
    const stableHeights = new Set<number>();
    function Stable() {
      const ref = React.useRef(null!);
      const m = useBoxMetrics(ref);
      stableRenders++;
      stableHeights.add(m.height);
      return (
        <Box ref={ref} flexDirection="column">
          <Text>a</Text>
          <Text>b</Text>
        </Box>
      );
    }
    const oscHeights = new Set<number>();
    function Oscillator() {
      const ref = React.useRef(null!);
      const m = useBoxMetrics(ref);
      oscHeights.add(m.height);
      const extra = m.height % 2 === 1;
      return (
        <Box ref={ref} flexDirection="column">
          <Text>a</Text>
          {extra ? <Text>b</Text> : null}
        </Box>
      );
    }
    const a = render(<Stable />);
    await new Promise((res) => setTimeout(res, 80));
    a.unmount();
    const b = render(<Oscillator />);
    await new Promise((res) => setTimeout(res, 80));
    b.unmount();
    expect(hasMaxDepth()).toBe(false);
    expect(stableRenders).toBeLessThan(10);
    expect(stableHeights.size).toBeLessThanOrEqual(2);
    expect(oscHeights.size).toBeGreaterThanOrEqual(2);
  });

  it("useAnimationFrame: many subscribers with short interval does not loop alone", async () => {
    captureErrors();
    function Pulse() {
      const [ref, t] = useAnimationFrame(16);
      return (
        <Box ref={ref}>
          <Text>{`${t % 10}`}</Text>
        </Box>
      );
    }
    function Many() {
      return (
        <Box flexDirection="column">
          {Array.from({ length: 50 }, (_, i) => `p-${i}`).map((id) => (
            <Pulse key={id} />
          ))}
        </Box>
      );
    }
    const r = render(<Many />);
    await new Promise((res) => setTimeout(res, 250));
    expect(hasMaxDepth()).toBe(false);
    r.unmount();
  });

  it("useAnimationFrame + parent measures child: drives many setStates per tick but each tick still yields", async () => {
    // Empirically the tick + measure combination does NOT directly trigger
    // the nested-update limit — each tick is a fresh macrotask that lets
    // React drain its work loop before the next tick fires. Documents the
    // real boundary so future regressions don't reopen this rabbit hole.
    captureErrors();
    function FlipPulse() {
      const [ref, t] = useAnimationFrame(16);
      const cols = t % 60 > 30 ? 3 : 1;
      return (
        <Box ref={ref} flexDirection="column">
          {Array.from({ length: cols }, (_, i) => `r-${i}`).map((id) => (
            <Text key={id}>row</Text>
          ))}
        </Box>
      );
    }
    function ParentMeasures() {
      const ref = React.useRef(null!);
      const m = useBoxMetrics(ref);
      const pad = m.height > 2 ? 1 : 0;
      return (
        <Box ref={ref} flexDirection="column" paddingTop={pad}>
          <FlipPulse />
        </Box>
      );
    }
    const r = render(<ParentMeasures />);
    await new Promise((res) => setTimeout(res, 250));
    const tripped = hasMaxDepth();
    r.unmount();
    expect(tripped).toBe(false);
  });
});
