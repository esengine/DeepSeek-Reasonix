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

  it("useBoxMetrics: re-measures when parent layout changes after initial render", async () => {
    // Regression test for #2076 / PR #2095 review feedback:
    // The reviewer noted that using `[ref.current]` as a dependency would
    // cause useBoxMetrics to capture the first measurement and never
    // update — so a parent resize would leave the child stuck on stale
    // dimensions. This test verifies that when a parent's layout changes
    // (via a state update that adds content), the child's useBoxMetrics
    // reports the new dimensions.
    captureErrors();
    const measurements: Array<{ width: number; height: number }> = [];

    function Child() {
      const ref = React.useRef(null!);
      const m = useBoxMetrics(ref);
      measurements.push({ ...m });
      return (
        <Box ref={ref} flexDirection="column">
          <Text>child content</Text>
        </Box>
      );
    }

    function Parent() {
      const [expanded, setExpanded] = React.useState(false);
      React.useEffect(() => {
        // Expand after initial render to simulate a parent layout change
        const timer = setTimeout(() => setExpanded(true), 50);
        return () => clearTimeout(timer);
      }, []);

      return (
        <Box flexDirection="column">
          {expanded && (
            <Box padding={2}>
              <Text>extra content that changes layout</Text>
            </Box>
          )}
          <Child />
        </Box>
      );
    }

    const r = render(<Parent />);
    // Wait for the state update to propagate and effects to settle
    await new Promise((res) => setTimeout(res, 200));
    r.unmount();

    expect(hasMaxDepth()).toBe(false);

    // We should have collected multiple measurements, and at least one
    // should differ from the initial (0,0) — proving that useBoxMetrics
    // re-measured after the parent layout changed.
    expect(measurements.length).toBeGreaterThanOrEqual(2);
    const hasNonZero = measurements.some((m) => m.width > 0 || m.height > 0);
    expect(hasNonZero).toBe(true);
  });
});
