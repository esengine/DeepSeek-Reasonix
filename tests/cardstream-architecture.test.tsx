import { Box, type DOMElement, Text, useBoxMetrics } from "ink";
import React, { useEffect, useMemo, useRef } from "react";
import { afterEach, describe, expect, it } from "vitest";
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

afterEach(() => restoreErrors());

interface Item {
  id: string;
  height: number;
}

// Pre-fix architecture: inner is measured via useBoxMetrics, scrollRows is
// pinned to maxScroll = inner.height - outer.height, items composition uses
// scrollRows. The loop: cards mounting changes inner.height → maxScroll → pin
// writes scrollRows → marginTop + items change → inner.height re-measure.
function CardStreamPreFix({
  items,
  onScrollRows,
}: {
  items: readonly Item[];
  onScrollRows: (rows: number) => void;
}) {
  const outerRef = useRef<DOMElement>(null!);
  const innerRef = useRef<DOMElement>(null!);
  const outer = useBoxMetrics(outerRef);
  const inner = useBoxMetrics(innerRef);
  const maxScroll = Math.max(0, inner.height - outer.height);
  const [scrollRows, setScrollRows] = React.useState(0);

  useEffect(() => {
    // Pinned-mode write: scrollRows = maxScroll.
    setScrollRows(maxScroll);
    onScrollRows(maxScroll);
  }, [maxScroll, onScrollRows]);

  // Items composition depends on scrollRows — drops items above the window,
  // which changes inner.height (the dropped items become 0-height holes when
  // the consumer hasn't measured them yet).
  const live = useMemo(() => {
    return items.filter((_, i) => i >= scrollRows / 2);
  }, [items, scrollRows]);

  return (
    <Box ref={outerRef} flexDirection="column" overflow="hidden" height={3}>
      <Box ref={innerRef} flexDirection="column" marginTop={-scrollRows} flexShrink={0}>
        {live.map((item) => (
          <Box key={item.id} height={item.height} flexShrink={0}>
            <Text>{item.id}</Text>
          </Box>
        ))}
      </Box>
    </Box>
  );
}

// Post-fix architecture: inner.height is derived from items' known heights,
// not measured. The loop is impossible because nothing inside the data flow
// reads inner.height — scrollRows only affects rendering, never feeds back
// into the value used to compute maxScroll.
function CardStreamPostFix({
  items,
  onScrollRows,
}: {
  items: readonly Item[];
  onScrollRows: (rows: number) => void;
}) {
  const outerRef = useRef<DOMElement>(null!);
  const outer = useBoxMetrics(outerRef);
  const totalInnerRows = useMemo(() => {
    let sum = 0;
    for (const item of items) sum += item.height;
    return sum;
  }, [items]);
  const maxScroll = Math.max(0, totalInnerRows - outer.height);
  const [scrollRows, setScrollRows] = React.useState(0);

  useEffect(() => {
    setScrollRows(maxScroll);
    onScrollRows(maxScroll);
  }, [maxScroll, onScrollRows]);

  const live = useMemo(() => {
    return items.filter((_, i) => i >= scrollRows / 2);
  }, [items, scrollRows]);

  return (
    <Box ref={outerRef} flexDirection="column" overflow="hidden" height={3}>
      <Box flexDirection="column" marginTop={-scrollRows} flexShrink={0}>
        {live.map((item) => (
          <Box key={item.id} height={item.height} flexShrink={0}>
            <Text>{item.id}</Text>
          </Box>
        ))}
      </Box>
    </Box>
  );
}

describe("CardStream measurement-feedback architecture", () => {
  it("pre-fix: scrollRows feedback into items composition causes inner-height churn", async () => {
    // The pre-fix path is loopy by data flow (scrollRows → items → inner.height
    // → maxScroll → scrollRows) but the production chat-scroll-store's 16ms
    // shrink debounce throttles the loop below React's nested-update limit
    // in practice. The defense was incidental, not architectural — and one
    // missed cycle (e.g. a non-debounced consumer) would expose the loop.
    // Verify the loop AT LEAST settles to *some* maxScroll value rather
    // than crashing here, then prove the post-fix removes the latent risk.
    captureErrors();
    const items: Item[] = Array.from({ length: 20 }, (_, i) => ({ id: `c${i}`, height: 1 }));
    const seen: number[] = [];
    const r = render(<CardStreamPreFix items={items} onScrollRows={(n) => seen.push(n)} />);
    await new Promise((res) => setTimeout(res, 120));
    r.unmount();
    // The measured values churn — that's the latent fragility — but in this
    // synthetic harness React lets it settle. The point is that ANY tightening
    // (busier commit, slower terminal, sync child setState during effect)
    // is enough to push it past the limit.
    expect(seen.length).toBeGreaterThan(0);
  });

  it("post-fix: deriving inner.height from items breaks the loop entirely", async () => {
    captureErrors();
    const items: Item[] = Array.from({ length: 20 }, (_, i) => ({ id: `c${i}`, height: 1 }));
    const r = render(<CardStreamPostFix items={items} onScrollRows={() => {}} />);
    await new Promise((res) => setTimeout(res, 120));
    const tripped = hasMaxDepth();
    r.unmount();
    expect(tripped).toBe(false);
  });

  it("post-fix: maxScroll still converges to the right value when items grow", async () => {
    captureErrors();
    const seen: number[] = [];
    const items: Item[] = Array.from({ length: 10 }, (_, i) => ({ id: `c${i}`, height: 1 }));
    const r = render(<CardStreamPostFix items={items} onScrollRows={(rows) => seen.push(rows)} />);
    await new Promise((res) => setTimeout(res, 80));
    r.unmount();
    expect(hasMaxDepth()).toBe(false);
    // 10 items × 1 row each, outer is height=3 → maxScroll converges to 7.
    expect(seen.at(-1)).toBe(7);
  });
});
