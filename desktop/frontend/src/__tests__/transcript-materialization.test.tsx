import React, { act, useState, type ComponentType, type ReactNode } from "react";
import { createRoot } from "react-dom/client";
import { createTranscriptHarness } from "./transcript-dom-harness";
import { TranscriptKernel, type LogicalAnchor, type TranscriptViewportSnapshot } from "../lib/transcriptKernel";
import { TranscriptViewportWriter } from "../lib/transcriptViewportWriter";
import type { TimelineProjection } from "../lib/transcriptTimeline";
import type { ProjectionViewProps } from "../components/TranscriptProjectionView";

// Fixed natural boxes isolate first materialization from Markdown parsing,
// font delivery, async hydration, and the estimator's text heuristics.
async function verifyMaterialization(naturalHeight: number): Promise<number> {
  console.log(`\nMaterialization: ${naturalHeight}px DOM / 171px estimate`);
  const harness = await createTranscriptHarness({ deterministic: true, viewportHeight: 600, rowHeight: naturalHeight / 3 });
  const kernel = new TranscriptKernel({ clock: harness.clock });
  const writer = new TranscriptViewportWriter();
  kernel.replaceSurface("materialization");
  kernel.connectWriter(writer.write);
  const projection: TimelineProjection = { hasOlderHistory: false,
    completedBlocks: Array.from({ length: 160 }, (_, index) => ({ key: `fixed-${index}`, rows: [],
      phase: "completed", contentRevision: 1, measurementRevision: "1" })) };
  const { default: Window } = await harness.loadModule<{ default: ComponentType<Record<string, unknown>> }>("/src/components/TranscriptWindow.tsx");
  const host = document.createElement("div");
  document.body.append(host);
  const root = createRoot(host);
  let scroller: HTMLDivElement;
  let failed = 0;
  function check(value: boolean, label: string) { console.log(`${value ? "PASS" : "FAIL"} ${label}`); if (!value) failed++; }
  function snapshot(): TranscriptViewportSnapshot {
    return { scrollTop: scroller.scrollTop, scrollHeight: scroller.scrollHeight, clientHeight: 600,
      visibleBlocks: Array.from(scroller.querySelectorAll<HTMLElement>("[data-transcript-block-key]"))
        .map(element => ({ key: element.dataset.transcriptBlockKey!, top: element.getBoundingClientRect().top + scroller.scrollTop,
          bottom: element.getBoundingClientRect().bottom + scroller.scrollTop })) };
  }
  function Fixture() {
    const [element, setElement] = useState<HTMLDivElement | null>(null);
    scroller = element!;
    return <div className="transcript" ref={setElement}>
      <Window projection={projection} scrollElement={element} kernel={kernel} protectedBlockKeys={new Set<string>()}
        forceFull={false} estimateBlock={() => 171} onPinnedJumpVisible={() => {}}
        onGeometryWillChange={(anchor?: LogicalAnchor) => { if (!kernel.userGestureActive && !kernel.activeTransaction) kernel.begin("restore", anchor); }}
        onGeometryChange={() => {
          kernel.advanceGeometry();
          const transaction = kernel.activeTransaction;
          if (transaction && element) kernel.correctAnchor(transaction, key => snapshot().visibleBlocks.find(block => block.key === key)?.top);
        }}
        renderProjection={(layout: ProjectionViewProps): ReactNode => <div ref={layout.tailRef} className="transcript__resident-tail">
          <div ref={layout.spacerRef} className="transcript__window" style={{ height: layout.extent }} />
          {layout.blocks.map(block => {
            const place = layout.placements?.get(block.key);
            return <div key={block.key} className={`transcript__block${place ? " transcript__window-item" : ""}`}
              data-index={place?.index} data-transcript-block-key={block.key}
              style={place ? { position: "absolute", top: place.top } : undefined}>
              <div className="transcript__row" /><div className="transcript__row" /><div className="transcript__row" />
            </div>;
          })}
        </div>} />
    </div>;
  }
  const visible = () => snapshot().visibleBlocks.filter(block => block.bottom > scroller.scrollTop && block.top < scroller.scrollTop + 600)
    .map(block => ({ ...block, top: block.top - scroller.scrollTop, bottom: block.bottom - scroller.scrollTop }));
  try {
    await act(async () => root.render(<Fixture />));
    writer.attach(scroller!, kernel.generation);
    await act(async () => {
      kernel.beginUserGesture(snapshot());
      scroller.scrollTop = 15_000;
      kernel.observeNativeScroll(snapshot());
      scroller.dispatchEvent(new Event("scroll"));
    });
    const first = visible();
    check(first.length >= 3, "cold jump exposes multiple real blocks");
    check(first.length > 0 && first.every(block => Math.abs(block.bottom - block.top - naturalHeight) < 0.01), "actual DOM height is fixed independently of parsing");
    check(first.length >= 3 && first.slice(1).every((block, index) => Math.abs(block.top - first[index].bottom) < 0.5),
      "first paint commits real sizes without overlaps or estimate gaps during native input");
    // Reverse native travel adds previously unmounted blocks before several
    // existing visible blocks. Every common position must track native input,
    // not the newly discovered prefix size.
    let previousTop = scroller.scrollTop;
    let previous = visible();
    let drift = 0;
    let overlap = 0;
    const writes: string[] = [];
    window.__REASONIX_TRANSCRIPT_SCROLL_WRITE__ = write => {
      if (kernel.userGestureActive && write.outcome === "accepted") writes.push(write.owner ?? "unknown");
    };
    for (let step = 0; step < 32; step++) {
      await act(async () => {
        scroller.scrollTop = Math.max(0, scroller.scrollTop - 180);
        kernel.observeNativeScroll(snapshot());
        scroller.dispatchEvent(new Event("scroll"));
      });
      const current = visible();
      for (const before of previous) {
        const after = current.find(block => block.key === before.key);
        if (after) drift = Math.max(drift, Math.abs(after.top - before.top + scroller.scrollTop - previousTop));
      }
      for (let i = 1; i < current.length; i++) overlap = Math.max(overlap, Math.abs(current[i].top - current[i - 1].bottom));
      previous = current;
      previousTop = scroller.scrollTop;
    }
    check(drift <= 0.5, `reverse materialization preserves every common visible position (${drift}px)`);
    check(overlap <= 0.5, `reverse materialization has no inter-block gap or overlap (${overlap}px)`);
    check(writes.length === 0, "materialization does not write native scroll during input");
    const heldMidway = visible();
    let prepend: ReturnType<TranscriptKernel["begin"]> = null;
    await act(async () => {
      kernel.endUserGesture();
      // A history transaction can already own correction when the window
      // consumes its temporary origin. Measurement must not replace it.
      prepend = kernel.begin("prepend", kernel.anchor);
      root.render(<Fixture />);
    });
    check(prepend != null && prepend.status !== "cancelled", "origin release preserves an existing prepend owner");
    await harness.settle();
    check(prepend?.status === "committed", "prepend settles after origin and materialization geometry commit");
    check(heldMidway.every(before => {
      const after = visible().find(block => block.key === before.key);
      return after != null && Math.abs(after.top - before.top) <= 0.5;
    }), "mid-history origin release preserves the previously painted prefix anchor");
    await act(async () => { kernel.beginUserGesture(snapshot()); root.render(<Fixture />); });

    // Reach the leading edge without releasing the input lease. Prefix-origin
    // calibration must be continuous, including the final one-pixel step.
    let maxEdgeExcess = 0;
    while (scroller.scrollTop > 0) {
      const before = visible();
      const oldTop = scroller.scrollTop;
      const travel = Math.min(oldTop, oldTop <= 3 ? 1 : 180);
      await act(async () => {
        scroller.scrollTop = oldTop - travel;
        kernel.observeNativeScroll(snapshot());
        scroller.dispatchEvent(new Event("scroll"));
      });
      const current = visible();
      for (const prior of before) {
        const next = current.find(block => block.key === prior.key);
        if (next) maxEdgeExcess = Math.max(maxEdgeExcess, Math.abs(next.top - prior.top) - 2 * travel);
      }
    }
    check(maxEdgeExcess <= 0.5, `leading-edge origin is continuous, without a final reset jump (${maxEdgeExcess}px)`);
    const held = visible();
    await act(async () => { kernel.endUserGesture(); root.render(<Fixture />); });
    await harness.settle();
    const after = visible();
    check(held.length >= 3 && held.every(block => {
      const current = after.find(value => value.key === block.key);
      return current != null && Math.abs(current.top - block.top) < 0.5;
    }), "lease release cannot repay first-paint geometry debt inside the visible range");
    await act(async () => {
      kernel.beginUserGesture(snapshot());
      scroller.scrollTop = 0;
      kernel.observeNativeScroll(snapshot());
      scroller.dispatchEvent(new Event("scroll"));
    });
    const leading = scroller.querySelector<HTMLElement>('[data-transcript-block-key="fixed-0"]');
    check(!!leading && Math.abs(leading.getBoundingClientRect().top) <= 0.5, "native top exposes the complete first block");
    // A previously invisible block can grow across the viewport boundary.
    // Its new DOM height must not replace the reader's committed anchor.
    await act(async () => {
      scroller.scrollTop = 6_000;
      kernel.observeNativeScroll(snapshot());
      scroller.dispatchEvent(new Event("scroll"));
    });
    await act(async () => { kernel.endUserGesture(); root.render(<Fixture />); });
    await harness.settle();
    await act(async () => {
      kernel.beginUserGesture(snapshot());
      kernel.endUserGesture();
      root.render(<Fixture />);
    });
    const beforeGrowth = visible()[0];
    const predecessor = Array.from(scroller.querySelectorAll<HTMLElement>(".transcript__window-item"))
      .filter(element => element.getBoundingClientRect().bottom <= 0).at(-1);
    check(!!predecessor && !!beforeGrowth, "offscreen growth fixture owns a visible anchor and predecessor");
    if (predecessor && beforeGrowth) {
      await act(async () => {
        for (let row = 0; row < 9; row++) {
          const extra = document.createElement("div"); extra.className = "transcript__row"; predecessor.append(extra);
        }
        check(predecessor.getBoundingClientRect().bottom > 0, "natural growth enters viewport before its measured prefix commits");
        harness.observers.filter(observer => observer.target === predecessor).forEach(observer => observer.notify());
      });
      await harness.settle();
      const afterGrowth = visible().find(block => block.key === beforeGrowth.key);
      check(afterGrowth != null && Math.abs(afterGrowth.top - beforeGrowth.top) <= 0.5,
        "offscreen growth preserves the committed reader anchor instead of selecting the newly visible predecessor");
    }

  } finally {
    await act(async () => root.unmount());
    host.remove();
    kernel.detachSurface();
    await harness.unmount();
    await harness.close();
  }
  return failed;
}
const failures = await verifyMaterialization(191) + await verifyMaterialization(151);
if (failures) process.exit(1);
