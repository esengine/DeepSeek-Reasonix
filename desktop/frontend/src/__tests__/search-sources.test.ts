// Run: tsx src/__tests__/search-sources.test.ts

import {
  formatSearchFootnotesMarkdown,
  mergeSearchSources,
  parseSearchSources,
  searchSourcesFromHistory,
} from "../lib/searchSources";
import { historyMessagesToItems, initialState, reducer } from "../lib/useController";
import type { HistoryMessage, WireEvent } from "../lib/types";

let passed = 0;
let failed = 0;

function eq(a: unknown, b: unknown, label: string) {
  if (a === b) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(b)}, got ${JSON.stringify(a)}\n`);
    failed += 1;
  }
}

console.log("\nsearch footnotes");

eq(
  formatSearchFootnotesMarkdown([{ title: "Change Log", url: "https://api-docs.deepseek.com/updates/" }, { title: "No URL" }]),
  "\n- **Change Log**\n  <https://api-docs.deepseek.com/updates/>\n- **No URL**\n",
  "footnotes reuse the title-and-autolink list",
);
eq(formatSearchFootnotesMarkdown([{ title: "bad", url: "javascript:alert(1)" }]), "\n- **bad**\n", "unsafe URLs are dropped");
eq(parseSearchSources("新闻本文\nhttps://example.com/a\nNo URL").length, 2, "output parser keeps title-only hits");
eq(parseSearchSources("新闻本文\nhttps://example.com/a")[0]?.title, "新闻本文", "output parser keeps the title");
eq(mergeSearchSources([{ title: "A", url: "https://a.example" }], [{ title: "A", url: "https://a.example" }]).length, 1, "duplicate hits collapse");

const history = historyMessagesToItems([{
  role: "assistant",
  content: "answer only",
  serverSearch: [{
    id: "s1",
    query: "bitcoin",
    results: [{ title: "新闻本文", url: "https://example.com/a" }],
  }],
}] as HistoryMessage[], "h-").items;
const answer = history.find((item) => item.kind === "assistant");
eq(answer?.kind === "assistant" ? answer.text : "", "answer only", "answer text stays model-only");
eq(answer?.kind === "assistant" ? answer.searchSources?.[0]?.title : "", "新闻本文", "history hydrates footnotes on the answer");

function ev(s: typeof initialState, e: WireEvent) {
  return reducer(s, { type: "event", e });
}

let live = ev(initialState, { kind: "turn_started" });
live = ev(live, {
  kind: "tool_result",
  tool: { id: "s1", name: "web_search", readOnly: true, output: "新闻本文\nhttps://example.com/a" },
} as WireEvent);
live = ev(live, { kind: "text", text: "answer only" });
live = ev(live, { kind: "message", text: "answer only" });
const liveAnswer = live.items.find((item) => item.kind === "assistant");
eq(liveAnswer?.kind === "assistant" ? liveAnswer.text : "", "answer only", "live answer stays model-only");
eq(liveAnswer?.kind === "assistant" ? liveAnswer.searchSources?.[0]?.title : "", "新闻本文", "live tool result attaches footnotes to the answer");
eq(searchSourcesFromHistory([{ results: [{ title: "A" }] }])[0]?.title, "A", "history helper reads structured hits");

if (failed) {
  process.stdout.write(`\n${failed} failed, ${passed} passed\n`);
  process.exit(1);
}
process.stdout.write(`\n${passed} passed\n`);
