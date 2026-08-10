// Run: tsx src/__tests__/transcript-logical-selection.test.ts

import { JSDOM } from "jsdom";
import {
  domPointToTranscriptOffset,
  domRangeForTranscriptOffsets,
  projectTranscriptSelectableDom,
  transcriptSelectionPointFromClient,
  transcriptSelectionProjectionReadyForNode,
} from "../lib/transcriptSelectionDom";
import {
  TranscriptSelectionStore,
  type TranscriptSelectableRow,
  type TranscriptSelectionPoint,
} from "../lib/transcriptSelectionStore";
import {
  mergeTranscriptSelectableRows,
  transcriptLiveSelectableRows,
  transcriptSelectableRows,
  userMessageSelectionText,
} from "../lib/transcriptSelectionText";
import type { TranscriptRow } from "../lib/transcriptRows";
import { TranscriptMarkdownCache, type ParsedMarkdownValue } from "../lib/transcriptMarkdownCache";
import { formatSelectedTextContext, formatSelectionLabels } from "../lib/selectedTextContext";

let passed = 0;
let failed = 0;

function ok(value: unknown, label: string) {
  if (value) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

function eq(actual: unknown, expected: unknown, label: string) {
  if (actual === expected) ok(true, label);
  else ok(false, `${label}: expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}`);
}

const point = (rowKey: string, textOffset: number): TranscriptSelectionPoint => ({
  rowKey,
  textOffset,
  affinity: "forward",
});

const row = (rowKey: string, text: string, revision = 1, pin?: () => () => void): TranscriptSelectableRow => ({
  rowKey,
  sourceText: text,
  contentRevision: revision,
  resolveText: async () => text,
  pin,
});

console.log("\nlogical transcript selection store");

{
  const store = new TranscriptSelectionStore();
  const rows = [row("a", "alpha"), row("b", "bravo"), row("c", "charlie")];
  const id = store.beginNative("tab-a");
  store.updateNativeRange(point("a", 2), point("c", 3));
  eq(store.promoteToLogical("tab-a", point("a", 2), point("c", 3), rows), id, "cross-row selection promotes in the source tab");
  let redundantUpdates = 0;
  const unsubscribe = store.subscribe(() => { redundantUpdates += 1; });
  store.updateLogicalFocus(point("c", 3));
  unsubscribe();
  eq(redundantUpdates, 0, "unchanged caret reconciliation does not republish the selection snapshot");
  store.settleLogical();
  eq(await store.resolveText(id), "pha\n\nbravo\n\ncha", "forward selection resolves partial endpoints and full middle rows");
  eq(store.getSnapshot().mode, "logical-settled", "pointer release settles logical selection");
}

{
  const store = new TranscriptSelectionStore();
  const rows = [
    row("answer-a", "alpha"),
    { ...row("offscreen-reasoning", "expanded thought"), kind: "reasoning" as const },
    row("answer-b", "bravo"),
  ];
  const id = store.beginNative("tab-offscreen-reasoning");
  store.promoteToLogical(
    "tab-offscreen-reasoning",
    point("answer-a", 0),
    point("answer-b", 5),
    rows,
  );
  eq(
    await store.resolveText(id),
    "alpha\n\nexpanded thought\n\nbravo",
    "expanded offscreen reasoning remains in logical selection even when its DOM is not mounted",
  );
}

{
  const store = new TranscriptSelectionStore();
  const rows = [row("a", "A😀B"), row("b", "second")];
  const id = store.beginNative("tab-r");
  store.promoteToLogical("tab-r", point("b", 4), point("a", 3), rows);
  eq(store.getSnapshot().direction, "backward", "reverse drag records direction");
  eq(await store.resolveText(id), "B\n\nseco", "reverse drag still copies document order using UTF-16 offsets");
}

{
  let pins = 0;
  const store = new TranscriptSelectionStore();
  const rows = [row("a", "one", 1, () => { pins += 1; return () => { pins -= 1; }; }), row("b", "two")];
  store.beginNative("tab-p");
  store.promoteToLogical("tab-p", point("a", 0), point("b", 3), rows);
  eq(pins, 1, "promotion pins active cached projections");
  ok(store.validateRowChanges([row("a", "one more", 2)]), "append-only live content keeps the frozen selected prefix");
  eq(store.getSnapshot().mode, "logical-dragging", "incremental live validation does not clear an append-only selection");
  ok(!store.validateRowChanges([row("a", "replaced", 3)]), "non-append live replacement invalidates selected content");
  eq(store.getSnapshot().mode, "none", "invalid content clears the logical selection");
  eq(pins, 0, "selection cleanup releases projection pins");
}

{
  const cache = new TranscriptMarkdownCache(16);
  const store = new TranscriptSelectionStore();
  const rows = ["a", "b", "c", "d"].map((rowKey) => row(
    rowKey,
    rowKey,
    1,
    () => cache.pin(rowKey, 1),
  ));
  const value = (source: string): ParsedMarkdownValue => ({
    source,
    blocks: [],
    selectionText: source,
    selectionRevision: 1,
    bytes: 8,
  });
  store.beginNative("tab-budget");
  store.promoteToLogical("tab-budget", point("b", 0), point("c", 1), rows);
  for (const entry of rows) cache.set(entry.rowKey, 1, value(entry.sourceText));
  eq(cache.bytes, 32, "dragging may temporarily pin every prospective selection row");
  store.settleLogical();
  eq(cache.bytes, 16, "settling releases unselected projections back to the cache budget");
  eq(cache.size(), 2, "settled selection retains only its final row interval");
  eq([...store.getSnapshot().contentRevisions.keys()].join(","), "b,c", "settled snapshot drops revisions outside its final interval");
  store.clear("done");
}

{
  let resolve!: (text: string) => void;
  const deferred = new Promise<string>((done) => { resolve = done; });
  const store = new TranscriptSelectionStore();
  const deferredRow: TranscriptSelectableRow = {
    rowKey: "a",
    sourceText: "fallback",
    contentRevision: 1,
    resolveText: () => deferred,
  };
  const id = store.beginNative("tab-stale");
  store.promoteToLogical("tab-stale", point("a", 0), point("a", 8), [deferredRow]);
  const pending = store.resolveText(id);
  store.clear("tab-switch");
  resolve("resolved");
  eq(await pending, "", "late async projection cannot resolve a cleared snapshot");
  ok(!store.isCurrent(id, "tab-stale"), "cleared snapshot id cannot be reused by async consumers");
}

console.log("\nlogical transcript DOM adapter");

{
  const dom = new JSDOM(`<!doctype html><body>
    <div class="transcript__row" data-row-key="a">
      <div id="root" data-transcript-selectable="message">
        <p>Hello <strong>world</strong><br>next</p>
        <p><strong>bold</strong> <em>italic</em></p>
        <span class="katex" data-latex-source="x^2"><span aria-hidden="true">rendered</span></span>
        <button>Copy</button>
        <table><tbody><tr><td>A</td><td>1</td></tr><tr><td>B</td><td>2</td></tr></tbody></table>
      </div>
      <div data-transcript-selectable="reasoning">visible thought</div>
    </div>
  </body>`);
  globalThis.window = dom.window as unknown as Window & typeof globalThis;
  globalThis.document = dom.window.document;
  globalThis.Node = dom.window.Node;
  globalThis.Element = dom.window.Element;
  globalThis.HTMLElement = dom.window.HTMLElement;
  globalThis.Range = dom.window.Range;
  const root = document.getElementById("root") as HTMLElement;
  ok(transcriptSelectionProjectionReadyForNode(root.firstChild), "rendered Markdown DOM is eligible for logical promotion");
  const projection = projectTranscriptSelectableDom(root);
  eq(projection.text, "Hello world\nnext\nbold italic\n$x^2$\nA\t1\nB\t2", "DOM projection filters controls and restores formula/table structure");
  const hello = root.querySelector("p")?.firstChild as Text;
  eq(domPointToTranscriptOffset(root, hello, 3), 3, "DOM text boundary maps to a UTF-16 projection offset");
  const range = domRangeForTranscriptOffsets(root, 6, 11);
  eq(range?.toString(), "world", "logical offsets map back to a DOM highlight range");
}

{
  const dom = new JSDOM(`<!doctype html><body>
    <div class="transcript__row" data-row-key="stale"><div id="stale" data-transcript-selectable="message">stale</div></div>
    <div class="transcript__row" data-row-key="visible"><div id="visible" data-transcript-selectable="message">visible</div></div>
  </body>`);
  globalThis.window = dom.window as unknown as Window & typeof globalThis;
  globalThis.document = dom.window.document;
  globalThis.Node = dom.window.Node;
  globalThis.Element = dom.window.Element;
  globalThis.HTMLElement = dom.window.HTMLElement;
  globalThis.Range = dom.window.Range;
  const stale = document.getElementById("stale") as HTMLElement;
  const visible = document.getElementById("visible") as HTMLElement;
  Object.defineProperty(document, "elementFromPoint", { configurable: true, value: () => visible });
  Object.defineProperty(document, "caretPositionFromPoint", {
    configurable: true,
    value: () => ({ offsetNode: stale.firstChild, offset: 2 }),
  });
  Object.defineProperty(visible, "getBoundingClientRect", {
    configurable: true,
    value: () => ({ left: 0, right: 100, top: 0, bottom: 20, width: 100, height: 20 }),
  });
  eq(
    transcriptSelectionPointFromClient(document, 10, 10)?.rowKey,
    "visible",
    "client caret rejects a stale virtual row outside the geometric hit target",
  );
  dom.window.close();
}

{
  const dom = new JSDOM(`<!doctype html><body>
    <div class="transcript__row" data-row-key="fallback">
      <div data-transcript-selectable="message"><div data-transcript-selection-source-fallback>**raw**</div></div>
    </div>
  </body>`);
  globalThis.window = dom.window as unknown as Window & typeof globalThis;
  globalThis.document = dom.window.document;
  globalThis.Node = dom.window.Node;
  globalThis.Element = dom.window.Element;
  globalThis.HTMLElement = dom.window.HTMLElement;
  const raw = document.querySelector("[data-transcript-selection-source-fallback]")?.firstChild ?? null;
  ok(!transcriptSelectionProjectionReadyForNode(raw), "plain Markdown source fallback stays in native selection mode");
  dom.window.close();
}

console.log("\nuser transcript selection projection");

{
  const selected = [{ id: "s1", text: "quoted context" }];
  const labels = formatSelectionLabels(selected);
  const submit = `question @/tmp/report.md\n\n${formatSelectedTextContext(selected)}`;
  eq(
    userMessageSelectionText(`question @/tmp/report.md ${labels}`, submit),
    "question\n\nquoted context",
    "user projection keeps body and selected context while filtering attachment/card metadata",
  );
}

console.log("\nstreaming transcript selection projection");

{
  const rows: TranscriptRow[] = [
    {
      kind: "answer",
      key: "a:history",
      item: { kind: "assistant", id: "history", text: "stable history", reasoning: "", streaming: false },
    },
    {
      kind: "answer",
      key: "a:live",
      item: { kind: "assistant", id: "live", text: "", reasoning: "", streaming: true },
    },
  ];
  const base = transcriptSelectableRows(rows);
  const byKey = new Map(base.map((entry) => [entry.rowKey, entry]));
  const firstLive = transcriptLiveSelectableRows(byKey, {
    id: "live",
    text: "one",
    reasoning: "",
    reasoningComplete: true,
  });
  const secondLive = transcriptLiveSelectableRows(byKey, {
    id: "live",
    text: "one two",
    reasoning: "",
    reasoningComplete: true,
  });
  eq(firstLive.length, 1, "a streaming token projects only the active selectable row");
  const firstMerged = mergeTranscriptSelectableRows(base, firstLive);
  const secondMerged = mergeTranscriptSelectableRows(base, secondLive);
  ok(firstMerged[0] === base[0] && secondMerged[0] === base[0], "successive stream updates reuse every historical projection object");
  eq(secondMerged[1]?.sourceText, "one two", "the live override tracks the newest streamed answer text");
}

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
