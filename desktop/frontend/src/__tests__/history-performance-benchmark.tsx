// Run:
//   corepack pnpm --dir desktop/frontend exec tsx src/__tests__/history-performance-benchmark.tsx
//
// Synthetic, privacy-safe benchmark for long restored histories. It logs counts,
// byte lengths, and elapsed times only; it never uses real conversation content.

import { historyMessagesToItems, initialState, reducer, type Item } from "../lib/useController";
import { buildTurnGroups, compactQuestionText, scrollVersion, type TurnGroup } from "../lib/transcriptGrouping";
import type { HistoryMessage } from "../lib/types";
import { buildTranscriptRowBlocks, buildTurnModels, EMPTY_FOLDS, NO_LIVE } from "../lib/transcriptRows";
import { projectTranscriptTimeline } from "../lib/transcriptTimeline";
import { commitTranscriptWindowGeometry } from "../lib/transcriptWindowGeometry";

type BenchCase = {
  name: string;
  turns: number;
  toolsPerTurn: number;
  outputSize: number;
  archived: boolean;
};

type BenchResult = {
  name: string;
  messages: number;
  items: number;
  jsonBytes: number;
  itemStringBytes: number;
  convertMs: number;
  reducerMs: number;
  transcriptComputeMs: number;
  turnGroups: number;
  projectionMs: number;
  rangeMs: number;
  mountedCompleted: number;
};

const cases: BenchCase[] = [
  { name: "200-turns-full-10KB", turns: 200, toolsPerTurn: 1, outputSize: 10 * 1024, archived: false },
  { name: "200-turns-archived-10KB", turns: 200, toolsPerTurn: 1, outputSize: 10 * 1024, archived: true },
  { name: "1000-turns-full-1KB", turns: 1000, toolsPerTurn: 1, outputSize: 1024, archived: false },
  { name: "1000-turns-archived-1KB", turns: 1000, toolsPerTurn: 1, outputSize: 1024, archived: true },
  { name: "1000-turns-archived-3-tools", turns: 1000, toolsPerTurn: 3, outputSize: 1024, archived: true },
  { name: "5000-turns-archived-1-tool", turns: 5000, toolsPerTurn: 1, outputSize: 1024, archived: true },
  { name: "10000-turns-archived-1-tool", turns: 10000, toolsPerTurn: 1, outputSize: 1024, archived: true },
];

function syntheticHistory(c: BenchCase): HistoryMessage[] {
  const messages: any[] = [];
  const output = "x".repeat(c.outputSize);
  for (let turn = 0; turn < c.turns; turn += 1) {
    messages.push({ role: "user", content: `prompt ${turn}` });
    const toolCalls: any[] = [];
    for (let tool = 0; tool < c.toolsPerTurn; tool += 1) {
      const id = `call_${turn}_${tool}`;
      toolCalls.push({
        id,
        name: "bash",
        arguments: c.archived ? "" : `{"command":"synthetic ${turn} ${tool}"}`,
        argumentsArchived: c.archived || undefined,
        subject: c.archived ? `synthetic ${turn} ${tool}` : undefined,
        summary: c.archived ? "1 line" : undefined,
      });
    }
    messages.push({ role: "assistant", content: `answer ${turn}`, toolCalls });
    for (let tool = 0; tool < c.toolsPerTurn; tool += 1) {
      const id = `call_${turn}_${tool}`;
      messages.push({
        role: "tool",
        toolCallId: id,
        toolName: "bash",
        content: c.archived ? "" : output,
        toolResultArchived: c.archived || undefined,
      });
    }
  }
  return messages as HistoryMessage[];
}

function itemStringBytes(items: Item[]): number {
  let total = 0;
  for (const item of items) {
    if (item.kind === "user") total += item.text.length;
    if (item.kind === "assistant") total += item.text.length + item.reasoning.length;
    if (item.kind === "tool") total += item.args.length + (item.output?.length ?? 0) + (item.error?.length ?? 0);
  }
  return total;
}

function time<T>(fn: () => T): { value: T; ms: number } {
  const start = performance.now();
  const value = fn();
  return { value, ms: performance.now() - start };
}

function buildQuestions(items: Item[]): number {
  let anchors = 0;
  for (const item of items) {
    if (item.kind !== "user") continue;
    compactQuestionText(item.text);
    anchors += 1;
  }
  return anchors;
}

function buildSubcallsByParent(items: Item[]): Map<string, Extract<Item, { kind: "tool" }>[]> {
  const map = new Map<string, Extract<Item, { kind: "tool" }>[]>();
  for (const item of items) {
    if (item.kind === "tool" && item.parentId) {
      const arr = map.get(item.parentId) ?? [];
      arr.push(item);
      map.set(item.parentId, arr);
    }
  }
  return map;
}

function computeTranscriptInputs(items: Item[]): TurnGroup[] {
  buildQuestions(items);
  scrollVersion(items);
  buildSubcallsByParent(items);
  let needed = 30;
  for (let i = items.length - 1; i >= 0; i -= 1) {
    if (items[i].kind === "user") {
      needed -= 1;
      if (needed <= 0) break;
    }
  }
  return buildTurnGroups(items);
}

function runCase(c: BenchCase): BenchResult {
  const messages = syntheticHistory(c);
  const jsonBytes = JSON.stringify(messages).length;
  const converted = time(() => historyMessagesToItems(messages, "perf"));
  const reduced = time(() => reducer(initialState, { type: "history", messages }));
  const items = converted.value.items;
  const transcript = time(() => computeTranscriptInputs(items));
  const projection = time(() => projectTranscriptTimeline(buildTranscriptRowBlocks(buildTurnModels(items, NO_LIVE, false), {
    folds: EMPTY_FOLDS, sessionExperience: "standard", hasOlderHistory: true, creationMode: false,
    turnForUser: (item) => (item.historyTurn ?? 1) - 1,
  }), true));
  const measurements = projection.value.completedBlocks.slice(0, -2).map((block, index) => ({
    key: block.key, index, start: index * 200, end: (index + 1) * 200, size: 200,
  }));
  const range = time(() => commitTranscriptWindowGeometry({
    candidate: [], measurements, retainedIndexes: new Set<number>(), structureRevision: c.name,
    scrollTop: measurements.length * 100, clientHeight: 800, scrollMargin: 0, totalSize: measurements.length * 200,
    maxItems: 38, direction: "forward", gestureActive: false, residentCount: 2, forceFull: false,
  }));
  if (!range.value.covered) throw new Error(`${c.name}: benchmark has no window coverage`);
  return {
    name: c.name,
    messages: messages.length,
    items: items.length,
    jsonBytes,
    itemStringBytes: itemStringBytes(reduced.value.items),
    convertMs: converted.ms,
    reducerMs: reduced.ms,
    transcriptComputeMs: transcript.ms,
    turnGroups: transcript.value.length,
    projectionMs: projection.ms,
    rangeMs: range.ms,
    mountedCompleted: range.value.range.items.length + 2,
  };
}

function printResult(r: BenchResult): void {
  process.stdout.write([
    r.name,
    `messages=${r.messages}`,
    `items=${r.items}`,
    `jsonBytes=${r.jsonBytes}`,
    `itemStringBytes=${r.itemStringBytes}`,
    `convertMs=${r.convertMs.toFixed(2)}`,
    `reducerMs=${r.reducerMs.toFixed(2)}`,
    `transcriptComputeMs=${r.transcriptComputeMs.toFixed(2)}`,
    `turnGroups=${r.turnGroups}`,
    `projectionMs=${r.projectionMs.toFixed(2)}`,
    `rangeMs=${r.rangeMs.toFixed(2)}`,
    `mountedCompleted=${r.mountedCompleted}`,
  ].join(" ") + "\n");
}

console.log("\nhistory performance benchmark");
const results = cases.map(runCase);
for (const result of results) {
  printResult(result);
}

const failures: string[] = [];
for (let index = 0; index < results.length; index += 1) {
  const result = results[index];
  const input = cases[index];
  const expectedMessages = input.turns * (2 + input.toolsPerTurn);
  if (result.messages !== expectedMessages) failures.push(`${result.name}: unexpected message count`);
  if (result.turnGroups !== input.turns) failures.push(`${result.name}: unexpected turn-group count`);
  if (result.convertMs > 1_000 || result.reducerMs > 1_000 || result.transcriptComputeMs > 1_000 || result.projectionMs > 1_000 || result.rangeMs > 1_000) {
    failures.push(`${result.name}: exceeded 1s responsiveness ceiling`);
  }
  if (result.mountedCompleted > 40) failures.push(`${result.name}: window exceeded completed mount budget`);
}

const full10KB = results.find((result) => result.name === "200-turns-full-10KB");
const archived10KB = results.find((result) => result.name === "200-turns-archived-10KB");
if (!full10KB || full10KB.itemStringBytes * 10 >= full10KB.jsonBytes) {
  failures.push("restored full tool results retained too much source text");
}
if (!archived10KB || archived10KB.itemStringBytes * 5 >= archived10KB.jsonBytes) {
  failures.push("restored archived tool results retained too much source text");
}

if (failures.length > 0) {
  for (const failure of failures) process.stderr.write(`FAIL ${failure}\n`);
  process.exit(1);
}
process.stdout.write("PASS long-history performance contracts\n");
