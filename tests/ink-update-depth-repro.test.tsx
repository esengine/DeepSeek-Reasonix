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

  it("useBoxMetrics: rendering from own measurement never converges", async () => {
    // Whether React's nested-update guard fires depends on event-loop timing
    // (a Linux runner doesn't trip what Windows trips in the same wall clock).
    // The deterministic property is render count: stable layouts converge in
    // 2–3 renders, the broken pattern compounds without bound. Counting is
    // the loop-detection signal; the React error is just one possible
    // downstream symptom.
    captureErrors();
    let stableRenders = 0;
    function Stable() {
      const ref = React.useRef(null!);
      useBoxMetrics(ref);
      stableRenders++;
      return (
        <Box ref={ref} flexDirection="column">
          <Text>a</Text>
          <Text>b</Text>
        </Box>
      );
    }
    let oscRenders = 0;
    function Oscillator() {
      const ref = React.useRef(null!);
      const m = useBoxMetrics(ref);
      oscRenders++;
      // height 0/even → 1 child → measure=1 (odd); odd → 2 children → measure=2 (even).
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
    // Stable converges in a handful of renders. The broken pattern compounds
    // — even a slow CI runner clears ~20 cycles in 80ms; local Node does
    // hundreds. The thresholds are deliberately loose to stay robust across
    // runner speeds; the property under test is "doesn't converge", not a
    // specific count.
    expect(stableRenders).toBeLessThan(10);
    expect(oscRenders).toBeGreaterThan(15);
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
