// Run: tsx src/__tests__/transcript-live-handoff.test.tsx

import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { JSDOM } from "jsdom";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { LiveTurnRegion } from "../components/LiveTurnRegion";
import type { TranscriptRow } from "../lib/transcriptRows";

console.log("\ntranscript live completion handoff");

const dom = new JSDOM('<div id="root"></div>');
(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.document = dom.window.document;
globalThis.HTMLElement = dom.window.HTMLElement;
globalThis.Node = dom.window.Node;

const root = createRoot(dom.window.document.getElementById("root")!);
const row = { key: "settled-tail", kind: "answer" } as TranscriptRow;
await act(async () => root.render(
  <LiveTurnRegion
    rows={[row]}
    renderRow={() => <span>settled overlay</span>}
    showStatus={false}
    scrollElement={null}
    handoff
  />,
));

const overlay = dom.window.document.querySelector<HTMLElement>("[data-live-handoff='true']")!;
assert.ok(overlay, "completion paints a dedicated handoff overlay");
assert.equal(overlay.getAttribute("aria-hidden"), "true", "the duplicate paint is hidden from accessibility");
assert.equal(overlay.querySelector(".transcript-selection-overlay"), null, "the overlay owns no selection surface");

const css = readFileSync(new URL("../styles.css", import.meta.url), "utf8");
const rule = css.match(/\.transcript__live-region--handoff\s*\{([\s\S]*?)\}/)?.[1] ?? "";
assert.match(rule, /position:\s*absolute/, "handoff paint is outside layout flow");
assert.match(rule, /pointer-events:\s*none/, "handoff paint cannot intercept input");

await act(async () => root.unmount());
console.log("transcript live completion handoff tests passed");
