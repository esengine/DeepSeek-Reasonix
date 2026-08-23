// Run: tsx src/__tests__/search-sources.test.ts

import {
  formatSearchFootnotesMarkdown,
  mergeSearchSources,
  parseSearchSources,
  searchSourcesFromHistory,
} from "../lib/searchSources";
import { normalizeSearchSources } from "../lib/searchSourcesPresentation";
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
eq(parseSearchSources("新闻本文\r\nhttps://example.com/a\r\n")[0]?.url, "https://example.com/a", "output parser tolerates CRLF line endings");
const degraded = parseSearchSources("- **新闻本文**\n  <https://example.com/a>");
eq(degraded.length, 1, "degraded footnote-markdown dump still resolves to one source");
eq(degraded[0]?.title, "新闻本文", "degraded dump strips the bullet and bold markers from the title");
eq(degraded[0]?.url, "https://example.com/a", "degraded dump unwraps the autolink URL");
eq(parseSearchSources("<https://example.com/a>")[0]?.url, "https://example.com/a", "autolink-only lines parse as URL sources");
eq(mergeSearchSources([{ title: "A", url: "https://a.example" }], [{ title: "A", url: "https://a.example" }]).length, 1, "duplicate hits collapse");

const normalizedInput = [
  { title: "Tracked", url: "https://example.com/article?utm_source=search&gclid=abc&part=1#section" },
  { title: "Same canonical URL", url: "https://example.com/article?part=1" },
  { title: "Google redirect", url: "https://www.google.com/url?q=https%3A%2F%2Fdocs.example.com%2Fguide%3Futm_medium%3Dcpc%26x%3D1" },
  { title: "Broken redirect", url: "https://page.sm.cn/blm/midpage-317/index?id=11" },
  { title: "Unsafe", url: "javascript:alert(1)" },
  { title: "   ", url: "https://empty-title.example" },
];
const normalized = normalizeSearchSources(normalizedInput);
eq(normalized.visible.length, 2, "normalization keeps only valid unique HTTP(S) sources");
eq(normalized.visible[0]?.href, "https://example.com/article?part=1", "tracking parameters and fragments are removed");
eq(normalized.visible[1]?.href, "https://docs.example.com/guide?x=1", "google redirect target is unwrapped and cleaned");
eq(normalized.visible[0]?.hostname, "example.com", "source projection exposes the hostname");
eq(normalized.visible[0]?.displayUrl, "example.com/article?part=1", "source projection exposes a compact URL");
eq(normalized.hiddenCount, 4, "invalid, duplicate, and missing-title sources are counted as hidden");
eq(normalizedInput[0]?.url, "https://example.com/article?utm_source=search&gclid=abc&part=1#section", "normalization does not mutate raw search data");

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
