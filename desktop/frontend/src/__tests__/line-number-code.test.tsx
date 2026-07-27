// Run: tsx src/__tests__/line-number-code.test.tsx

import { JSDOM } from "jsdom";
import React, { act } from "react";
import { CodeViewer } from "../components/CodeViewer";
import LineNumberCode, {
  findCodeMatches,
  highlightLineMatches,
  MAX_SEARCH_MATCHES,
  splitHighlightedCodeLines,
} from "../components/editors/LineNumberCode";
import {
  findRegexCodeMatches,
  MAX_REGEX_PATTERN_LENGTH,
  MAX_REGEX_SOURCE_LENGTH,
  type RegexSearchRequest,
  type RegexSearchResponse,
} from "../components/editors/codeSearch";
import {
  startRegexSearch,
  type RegexSearchWorker,
} from "../components/editors/regexSearchClient";
import {
  highlightToHtml,
  MAX_HIGHLIGHT_BYTES,
  MAX_HIGHLIGHT_LINES,
  shouldHighlightCode,
  shouldHighlightSource,
} from "../lib/highlight";
import { LocaleProvider } from "../lib/i18n";

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

function flush(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

function wait(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

async function waitForSelector(container: ParentNode, selector: string, timeoutMs = 1_000): Promise<Element> {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    const element = container.querySelector(selector);
    if (element) return element;
    await act(async () => wait(10));
  }
  throw new Error(`Timed out waiting for ${selector}`);
}

console.log("\nline-number code viewer");

const repeatedMatches = findCodeMatches("x\nx\nx", "x").matches;
ok(repeatedMatches.length === 3, "finds matches on consecutive lines");
ok(
  repeatedMatches.map((match) => match.lineIndex).join(",") === "0,1,2",
  "does not carry regex state between lines",
);
ok(findCodeMatches("x x", "x").matches.length === 2, "counts occurrences rather than matching lines");
ok(
  findCodeMatches("猫 猫咪 猫", "猫", false, true).matches.length === 2,
  "whole-word matching respects Unicode word characters",
);
ok(
  findCodeMatches("a+b aab a+b", "a+b").matches.length === 2,
  "treats regex metacharacters as literal search text",
);

const regexRequest = (overrides: Partial<RegexSearchRequest> = {}): RegexSearchRequest => ({
  requestId: 1,
  source: "foo1 foo22\nbar3",
  pattern: String.raw`foo\d+`,
  caseSensitive: false,
  wholeWord: false,
  maxMatches: MAX_SEARCH_MATCHES,
  ...overrides,
});
const regexMatches = findRegexCodeMatches(regexRequest());
ok(regexMatches.ok && regexMatches.result.matches.length === 2, "regex mode evaluates explicit patterns");
ok(
  regexMatches.ok && regexMatches.result.matches.map((match) => match.start).join(",") === "0,5",
  "regex mode returns JavaScript-compatible UTF-16 offsets",
);
const lineAnchoredRegex = findRegexCodeMatches(regexRequest({
  source: "foo\nfoo",
  pattern: "^foo",
}));
ok(
  lineAnchoredRegex.ok
    && lineAnchoredRegex.result.matches.length === 2
    && lineAnchoredRegex.result.matches.map((match) => match.lineIndex).join(",") === "0,1",
  "regex anchors continue to address individual source lines",
);
const multilineRegex = findRegexCodeMatches(regexRequest({
  source: "foo\nbar",
  pattern: String.raw`foo\nbar`,
}));
ok(!multilineRegex.ok && multilineRegex.error === "multiline_unsupported", "reports unsupported multiline regex matches");
const regexLiteralDifference = findRegexCodeMatches(regexRequest({
  source: "a+b aab aaab",
  pattern: "a+b",
}));
ok(
  regexLiteralDifference.ok && regexLiteralDifference.result.matches.length === 2,
  "regex mode keeps metacharacter semantics separate from literal search",
);
const invalidRegex = findRegexCodeMatches(regexRequest({ pattern: "(" }));
ok(!invalidRegex.ok && invalidRegex.error === "invalid_pattern", "reports invalid regular expressions");
const zeroLengthRegex = findRegexCodeMatches(regexRequest({ pattern: "^" }));
ok(
  !zeroLengthRegex.ok && zeroLengthRegex.error === "zero_length_unsupported",
  "rejects zero-length regex matches that cannot be highlighted safely",
);
const mixedLengthRegex = findRegexCodeMatches(regexRequest({
  source: "abc",
  pattern: ".*",
}));
ok(
  mixedLengthRegex.ok
    && mixedLengthRegex.result.matches.length === 1
    && mixedLengthRegex.result.matches[0].start === 0
    && mixedLengthRegex.result.matches[0].end === 3,
  "keeps non-empty regex results when the expression also produces an empty match",
);
const longRegex = findRegexCodeMatches(regexRequest({ pattern: "x".repeat(MAX_REGEX_PATTERN_LENGTH + 1) }));
ok(!longRegex.ok && longRegex.error === "pattern_too_long", "caps regular-expression length");
const oversizedRegexSource = findRegexCodeMatches(regexRequest({
  source: "x".repeat(MAX_REGEX_SOURCE_LENGTH + 1),
}));
ok(
  !oversizedRegexSource.ok && oversizedRegexSource.error === "source_too_large",
  "caps source copied into the regex worker",
);
const cappedRegex = findRegexCodeMatches(regexRequest({
  source: "x x x",
  pattern: "x",
  maxMatches: 2,
}));
ok(
  cappedRegex.ok && cappedRegex.result.matches.length === 2 && cappedRegex.result.truncated,
  "caps regex result sets before returning them to the UI",
);

class FakeRegexWorker implements RegexSearchWorker {
  onmessage: ((event: MessageEvent<RegexSearchResponse>) => void) | null = null;
  onerror: ((event: ErrorEvent) => void) | null = null;
  posted: RegexSearchRequest[] = [];
  terminated = false;

  postMessage(request: RegexSearchRequest) {
    this.posted.push(request);
  }

  terminate() {
    this.terminated = true;
  }

  respond(response: RegexSearchResponse) {
    this.onmessage?.({ data: response } as MessageEvent<RegexSearchResponse>);
  }
}

const timedOutWorker = new FakeRegexWorker();
let timeoutCallback: (() => void) | null = null;
let timeoutResponse: RegexSearchResponse | null = null;
startRegexSearch(
  regexRequest({ requestId: 2 }),
  { onResponse: (response) => { timeoutResponse = response; } },
  {
    createWorker: async () => timedOutWorker,
    setTimer: (callback) => {
      timeoutCallback = callback;
      return 1;
    },
    clearTimer: () => {},
  },
);
await Promise.resolve();
timeoutCallback?.();
ok(timedOutWorker.terminated, "hard timeout terminates a stuck regex worker");
ok(
  timeoutResponse != null && !timeoutResponse.ok && timeoutResponse.error === "timeout",
  "hard timeout reports a bounded search failure",
);

let creationTimeoutCallback: (() => void) | null = null;
let creationTimeoutResponse: RegexSearchResponse | null = null;
startRegexSearch(
  regexRequest({ requestId: 5 }),
  { onResponse: (response) => { creationTimeoutResponse = response; } },
  {
    createWorker: () => new Promise<RegexSearchWorker>(() => {}),
    setTimer: (callback) => {
      creationTimeoutCallback = callback;
      return 5;
    },
    clearTimer: () => {},
  },
);
creationTimeoutCallback?.();
ok(
  creationTimeoutResponse != null
    && !creationTimeoutResponse.ok
    && creationTimeoutResponse.error === "timeout",
  "hard timeout covers a worker that never finishes initializing",
);

const staleWorker = new FakeRegexWorker();
let staleResponseAccepted = false;
const cancelStaleSearch = startRegexSearch(
  regexRequest({ requestId: 3 }),
  { onResponse: () => { staleResponseAccepted = true; } },
  {
    createWorker: async () => staleWorker,
    setTimer: () => 2,
    clearTimer: () => {},
  },
);
await Promise.resolve();
cancelStaleSearch();
staleWorker.respond({
  requestId: 3,
  ok: true,
  result: { matches: [], truncated: false },
});
ok(staleWorker.terminated, "cancelling a stale request terminates its worker");
ok(!staleResponseAccepted, "discarded requests cannot update search results");

const completedWorker = new FakeRegexWorker();
let completedResponse: RegexSearchResponse | null = null;
let completedTimerCleared = false;
startRegexSearch(
  regexRequest({ requestId: 4 }),
  { onResponse: (response) => { completedResponse = response; } },
  {
    createWorker: async () => completedWorker,
    setTimer: () => 3,
    clearTimer: () => { completedTimerCleared = true; },
  },
);
await Promise.resolve();
completedWorker.respond({
  requestId: 4,
  ok: true,
  result: { matches: [], truncated: false },
});
ok(completedResponse?.requestId === 4, "accepts the current worker response");
ok(completedWorker.terminated && completedTimerCleared, "successful searches release worker and timer resources");

const cappedMatches = findCodeMatches("x".repeat(MAX_SEARCH_MATCHES + 1), "x");
ok(cappedMatches.matches.length === MAX_SEARCH_MATCHES, "caps pathological result sets");
ok(cappedMatches.truncated, "reports capped result sets to the viewer");
ok(shouldHighlightCode(MAX_HIGHLIGHT_BYTES, MAX_HIGHLIGHT_LINES), "keeps syntax highlighting within its budget");
ok(!shouldHighlightCode(MAX_HIGHLIGHT_BYTES + 1, 1), "falls back to plain text above the byte budget");
ok(!shouldHighlightCode(1, MAX_HIGHLIGHT_LINES + 1), "falls back to plain text above the line budget");
ok(shouldHighlightSource("const value = 1;"), "keeps ordinary chat code within the shared budget");
ok(
  !shouldHighlightSource("é".repeat(Math.floor(MAX_HIGHLIGHT_BYTES / 2) + 1)),
  "measures the shared byte budget as UTF-8 rather than UTF-16 code units",
);
ok(
  !shouldHighlightSource("const value = 1;\n".repeat(MAX_HIGHLIGHT_LINES)),
  "applies the shared line budget without requiring a workspace preview",
);

const entitySource = "const x = \"<&\";";
for (const query of ["<", "&"]) {
  const matches = findCodeMatches(entitySource, query).matches;
  const markedHtml = highlightLineMatches(
    splitHighlightedCodeLines(highlightToHtml(entitySource, "typescript"))[0],
    matches,
    matches[0],
  );
  const entityDom = new JSDOM(`<code>${markedHtml}</code>`);
  const code = entityDom.window.document.querySelector("code");
  ok(code?.textContent === entitySource, `searching ${query} preserves rendered source text`);
  ok(code?.querySelectorAll("mark").length === 1, `searching ${query} highlights the exact entity`);
}

const multilineSource = "const value = `first\nsecond`;";
const multilineHtml = splitHighlightedCodeLines(highlightToHtml(multilineSource, "typescript"));
ok(multilineHtml.length === 2, "splits highlighted multiline source into rows");
ok(multilineHtml[1].includes("hljs-string"), "preserves lexer state on the second line");

const dom = new JSDOM("<!doctype html><html><body><div id=\"root\"></div></body></html>", {
  pretendToBeVisual: true,
  url: "http://localhost/",
});
(dom.window.HTMLElement.prototype as unknown as { attachEvent: () => void }).attachEvent = () => {};
(dom.window.HTMLElement.prototype as unknown as { detachEvent: () => void }).detachEvent = () => {};
(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.document = dom.window.document;
globalThis.Node = dom.window.Node;
globalThis.HTMLElement = dom.window.HTMLElement;
globalThis.HTMLInputElement = dom.window.HTMLInputElement;
globalThis.Event = dom.window.Event;
globalThis.KeyboardEvent = dom.window.KeyboardEvent;
Object.defineProperty(globalThis, "navigator", { configurable: true, value: dom.window.navigator });
const { createRoot } = await import("react-dom/client");

const scrolledLines: number[] = [];
Object.defineProperty(dom.window.HTMLElement.prototype, "scrollIntoView", {
  configurable: true,
  value(this: HTMLElement) {
    const lineIndex = this.dataset.lineIndex;
    if (lineIndex != null) scrolledLines.push(Number(lineIndex));
  },
});
Object.defineProperty(dom.window.HTMLElement.prototype, "scrollTo", {
  configurable: true,
  value() {},
});

const container = document.getElementById("root")!;
const root = createRoot(container);
const searchableValue = Array.from(
  { length: 80 },
  (_, index) => index === 39 || index === 59 ? `needle ${index + 1}` : `line ${index + 1}`,
).join("\n");
await act(async () => {
  root.render(
    <LocaleProvider>
      <LineNumberCode value={searchableValue} showLineNumbers />
      <LineNumberCode value="beta" showLineNumbers />
    </LocaleProvider>,
  );
});

ok(container.querySelectorAll(".code-block__copy").length === 2, "keeps copy controls on line-number viewers");
const viewers = container.querySelectorAll<HTMLElement>(".code--lines");
await act(async () => {
  viewers[0].focus();
  viewers[0].dispatchEvent(new KeyboardEvent("keydown", { key: "f", ctrlKey: true, bubbles: true }));
  await flush();
});
ok(container.querySelectorAll(".code-search").length === 1, "opens search only for the focused viewer");
ok(
  container.querySelectorAll(".code-block__wrap")[0].querySelector(".code-search") != null,
  "keeps the search shortcut scoped to its owning viewer",
);
ok(
  container.querySelectorAll(".code-block__wrap")[1].querySelector(".code-search") == null,
  "does not fan the shortcut out to sibling viewers",
);
const firstViewer = container.querySelectorAll<HTMLElement>(".code-block__wrap")[0];
ok(
  firstViewer.querySelector(".code-block__copy") == null,
  "removes the covered floating copy control while search is open",
);
ok(
  firstViewer.querySelector('.code-search .code-search__copy[aria-label="Copy"]') != null,
  "keeps copy available as a visible search-toolbar action",
);

const pendingRequestContainer = document.createElement("div");
document.body.appendChild(pendingRequestContainer);
const pendingRequestRoot = createRoot(pendingRequestContainer);
let pendingRequestConsumed = false;
await act(async () => {
  pendingRequestRoot.render(
    <LocaleProvider>
      <LineNumberCode
        value="pending search"
        showLineNumbers
        searchRequestPending
        onSearchRequestConsumed={() => {
          pendingRequestConsumed = true;
        }}
      />
    </LocaleProvider>,
  );
  await flush();
});
ok(
  pendingRequestContainer.querySelector(".code-search") != null,
  "consumes a search request that arrived before the editor mounted",
);
ok(pendingRequestConsumed, "acknowledges a consumed search request");
await act(async () => pendingRequestRoot.unmount());

const searchInput = container.querySelector<HTMLInputElement>(".code-search__input")!;
await act(async () => {
  const setter = Object.getOwnPropertyDescriptor(dom.window.HTMLInputElement.prototype, "value")?.set;
  setter?.call(searchInput, "needle");
  searchInput.dispatchEvent(new dom.window.Event("input", { bubbles: true }));
  searchInput.dispatchEvent(new dom.window.Event("change", { bubbles: true }));
  await wait(130);
});
await act(async () => flush());
ok(container.querySelector(".code-search__count")?.textContent === "1 of 2", "activates the first match after a query settles");
ok(scrolledLines.at(-1) === 39, "reveals the first match instead of leaving the viewport behind");
ok(container.querySelector(".code-line-row--current .code-line-ln")?.textContent === "40", "marks the first matching line as current");

await act(async () => {
  searchInput.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true }));
  await flush();
});
ok(container.querySelector(".code-search__count")?.textContent === "2 of 2", "Enter advances from the visible first match");
ok(scrolledLines.at(-1) === 59, "Enter reveals the next matching line");

await act(async () => {
  searchInput.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", shiftKey: true, bubbles: true }));
  await flush();
});
ok(container.querySelector(".code-search__count")?.textContent === "1 of 2", "Shift+Enter navigates to the previous match");

await act(async () => {
  searchInput.dispatchEvent(new KeyboardEvent("keydown", { key: "f", ctrlKey: true, bubbles: true }));
  await flush();
});
ok(document.activeElement === searchInput, "repeated find shortcuts stay scoped while the search input is focused");

await act(async () => {
  const setter = Object.getOwnPropertyDescriptor(dom.window.HTMLInputElement.prototype, "value")?.set;
  setter?.call(searchInput, "line");
  searchInput.dispatchEvent(new dom.window.Event("input", { bubbles: true }));
  searchInput.dispatchEvent(new dom.window.Event("change", { bubbles: true }));
  await flush();
});
await act(async () => {
  searchInput.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true }));
  await flush();
});
await act(async () => flush());
ok(container.querySelector(".code-search__count")?.textContent === "1 of 78", "immediate Enter commits a pending query at its first match");
ok(scrolledLines.at(-1) === 0, "immediate Enter reveals the first result instead of skipping ahead");

const caseToggle = container.querySelector<HTMLButtonElement>('[aria-label="Match case"]')!;
ok(caseToggle.getAttribute("aria-pressed") === "false", "exposes the inactive match-case state");
await act(async () => {
  caseToggle.click();
  await flush();
});
ok(caseToggle.getAttribute("aria-pressed") === "true", "exposes the active match-case state");

await act(async () => {
  const setter = Object.getOwnPropertyDescriptor(dom.window.HTMLInputElement.prototype, "value")?.set;
  setter?.call(searchInput, "");
  searchInput.dispatchEvent(new dom.window.Event("input", { bubbles: true }));
  searchInput.dispatchEvent(new dom.window.Event("change", { bubbles: true }));
  await wait(130);
});
const regexToggle = container.querySelector<HTMLButtonElement>('[aria-label="Use regular expression"]')!;
ok(regexToggle.getAttribute("aria-pressed") === "false", "keeps regex mode disabled by default");
await act(async () => {
  regexToggle.click();
  await flush();
});
ok(regexToggle.getAttribute("aria-pressed") === "true", "exposes regex mode as an explicit advanced toggle");
await act(async () => {
  const setter = Object.getOwnPropertyDescriptor(dom.window.HTMLInputElement.prototype, "value")?.set;
  setter?.call(searchInput, "x".repeat(MAX_REGEX_PATTERN_LENGTH + 1));
  searchInput.dispatchEvent(new dom.window.Event("input", { bubbles: true }));
  searchInput.dispatchEvent(new dom.window.Event("change", { bubbles: true }));
  await wait(130);
});
ok(
  container.querySelector(".code-search__count--error")?.textContent === "Expression too long",
  "shows a localized error before unsafe regex work starts",
);

await act(async () => {
  container.querySelector<HTMLButtonElement>('[aria-label="Close search"]')?.click();
  await flush();
});
ok(firstViewer.querySelector(".code-search") == null, "closes the viewer-scoped search toolbar");
ok(firstViewer.querySelector(".code-block__copy") != null, "restores the floating copy control after search closes");

await act(async () => root.unmount());

const largeContainer = document.createElement("div");
document.body.appendChild(largeContainer);
const largeRoot = createRoot(largeContainer);
await act(async () => {
  largeRoot.render(
    <LocaleProvider>
      <LineNumberCode
        value={'const text = "<&";'}
        language="typescript"
        sourceSize={MAX_HIGHLIGHT_BYTES + 1}
        showLineNumbers
      />
    </LocaleProvider>,
  );
});
ok(largeContainer.querySelector(".code--lines")?.getAttribute("data-highlight-mode") === "plain", "uses the plain-text path above the syntax budget");
ok(largeContainer.querySelector(".code-line-text")?.textContent === 'const text = "<&";', "plain-text fallback preserves escaped source text");
ok(largeContainer.querySelector(".hljs-keyword") == null, "plain-text fallback skips syntax token markup");
await act(async () => largeRoot.unmount());

const defaultContainer = document.createElement("div");
document.body.appendChild(defaultContainer);
const defaultRoot = createRoot(defaultContainer);
await act(async () => {
  defaultRoot.render(
    <LocaleProvider>
      <CodeViewer value="const unchanged = true;" language="typescript" />
    </LocaleProvider>,
  );
});
// React.lazy may resolve after more than one task under React 19. Wait for the
// real viewer instead of asserting against the transient Suspense fallback.
await waitForSelector(defaultContainer, "pre.code.hljs");
ok(defaultContainer.querySelector("pre.code.hljs") != null, "keeps the established viewer as the default seam");
ok(defaultContainer.querySelector(".code--lines") == null, "requires an explicit line-number opt-in");
ok(defaultContainer.querySelector(".code-block__copy") != null, "keeps copy available on default code blocks");
await act(async () => defaultRoot.unmount());

const largeChatContainer = document.createElement("div");
document.body.appendChild(largeChatContainer);
const largeChatRoot = createRoot(largeChatContainer);
const largeChatValue = "const value = 1;\n".repeat(MAX_HIGHLIGHT_LINES);
await act(async () => {
  largeChatRoot.render(
    <LocaleProvider>
      <CodeViewer value={largeChatValue} language="typescript" />
    </LocaleProvider>,
  );
});
await waitForSelector(largeChatContainer, 'pre[data-highlight-mode="plain"]');
ok(
  largeChatContainer.querySelector('pre[data-highlight-mode="plain"]') != null,
  "ordinary chat blocks reuse the shared syntax budget",
);
ok(
  largeChatContainer.querySelector(".hljs-keyword") == null,
  "oversized chat blocks skip syntax token generation",
);
ok(
  largeChatContainer.querySelector("code")?.textContent === largeChatValue,
  "oversized chat blocks preserve their plain-text content",
);
await act(async () => largeChatRoot.unmount());

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
