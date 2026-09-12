// Deterministic regression for React error #185 ("Maximum update depth
// exceeded"). Production crash reports show the fuse tripping while the
// backend streams and the reader scrolls: the window adapter's measurement
// protocol legitimately closes every publication batch with a layout-effect
// state update, so a long materialization cascade produced 50+ consecutive
// commits that each left interactive-lane work pending. React counts exactly
// that pattern (pendingLanes & {Sync|InputContinuous|Default}) and cannot
// distinguish it from a runaway render loop. The geometry path now dispatches
// commit-phase updates on a transition lane, so the fuse sees only transition
// lanes remaining after each commit and resets.
//
// The pump fires geometryChanged() from a no-deps layout effect once per
// commit — the same dispatch shape MarkdownHistory produces while parses land
// during streaming. Without the lane change the 51st spawned commit throws
// inside act; with it, all 60 settle cleanly and the window keeps working.
import React, { act, useLayoutEffect, useState, type ComponentType, type ReactNode } from "react";
import { createRoot } from "react-dom/client";
import { createTranscriptHarness } from "./transcript-dom-harness";
import { TranscriptKernel } from "../lib/transcriptKernel";
import { TranscriptViewportWriter } from "../lib/transcriptViewportWriter";
import type { TimelineProjection } from "../lib/transcriptTimeline";
import type { ProjectionViewProps } from "../components/TranscriptProjectionView";

const PUMP_COMMITS = 60; // React's NESTED_UPDATE_LIMIT is 50.
let pumpRemaining = PUMP_COMMITS;
let pumpFires = 0;

const harness = await createTranscriptHarness({ deterministic: true, viewportHeight: 600, rowHeight: 24 });
const kernel = new TranscriptKernel({ clock: harness.clock });
const writer = new TranscriptViewportWriter();
kernel.replaceSurface("nested-update-fuse");
kernel.connectWriter(writer.write);
const projection: TimelineProjection = { hasOlderHistory: false,
  completedBlocks: Array.from({ length: 160 }, (_, index) => ({ key: `fuse-${index}`, rows: [],
    phase: "completed", contentRevision: 1, measurementRevision: "1" })) };
const { default: Window } = await harness.loadModule<{ default: ComponentType<Record<string, unknown>> }>("/src/components/TranscriptWindow.tsx");
// Load through the same SSR module graph as TranscriptWindow — importing the
// context directly here would resolve a second instance and read defaults.
const { useTranscriptPresentation } = await harness.loadModule<{
  useTranscriptPresentation: () => { gestureActive: boolean; windowed: boolean; geometryChanged: () => void };
}>("/src/components/TranscriptPresentationContext.tsx");

function GeometryPump() {
  const presentation = useTranscriptPresentation();
  useLayoutEffect(() => {
    if (pumpRemaining > 0) {
      pumpRemaining -= 1;
      pumpFires += 1;
      presentation.geometryChanged();
    }
  });
  return null;
}

const host = document.createElement("div");
document.body.append(host);
const root = createRoot(host);
let scroller: HTMLDivElement;
let failed = 0;
function check(value: boolean, label: string) { console.log(`${value ? "PASS" : "FAIL"} ${label}`); if (!value) failed++; }

function Fixture() {
  const [element, setElement] = useState<HTMLDivElement | null>(null);
  scroller = element!;
  return <div className="transcript" ref={setElement}>
    <Window projection={projection} scrollElement={element} kernel={kernel} protectedBlockKeys={new Set<string>()}
      forceFull={false} estimateBlock={() => 96} onPinnedJumpVisible={() => {}}
      onGeometryWillChange={() => null}
      onGeometryChange={() => kernel.advanceGeometry()}
      renderProjection={(layout: ProjectionViewProps): ReactNode => <div ref={layout.tailRef} className="transcript__resident-tail">
        <GeometryPump />
        <div ref={layout.spacerRef} className="transcript__window" style={{ height: layout.extent }} />
        {layout.blocks.map(block => {
          const place = layout.placements?.get(block.key);
          return <div key={block.key} className={`transcript__block${place ? " transcript__window-item" : ""}`}
            data-index={place?.index} data-transcript-block-key={block.key}
            style={place ? { position: "absolute", top: place.top } : undefined}>
            <div className="transcript__row" />
          </div>;
        })}
      </div>} />
  </div>;
}

let thrown: unknown = null;
try {
  await act(async () => root.render(<Fixture />));
  writer.attach(scroller!, kernel.generation);
  await harness.settle();
} catch (error) {
  thrown = error;
} finally {
  const message = thrown instanceof Error ? thrown.message : String(thrown ?? "");
  check(thrown == null, `60-commit geometry cascade settles (threw: ${message.slice(0, 120) || "none"})`);
  check(pumpFires === PUMP_COMMITS, `every commit-phase geometry dispatch landed (fires=${pumpFires})`);
  check(scroller != null && scroller.querySelectorAll("[data-transcript-block-key]").length > 0,
    "window still mounts blocks after the cascade");
  try { await act(async () => root.unmount()); } catch { /* root may already be dead after a fuse trip */ }
  host.remove();
  kernel.detachSurface();
  await harness.unmount();
  await harness.close();
}
if (failed) process.exit(1);
console.log("nested-update fuse: all checks passed");
