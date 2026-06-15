// Run:
//   corepack pnpm --dir desktop/frontend exec tsx src/__tests__/history-performance-benchmark.tsx
//
// Synthetic, privacy-safe benchmark for long restored histories. It logs counts,
// byte lengths, and elapsed times only; it never uses real conversation content.

import { historyMessagesToItems, initialState, reducer, type Item } from "../lib/useController";
import { buildTurnGroups, compactQuestionText, scrollVersion, type TurnGroup } from "../lib/transcriptGrouping";
import type { HistoryMessage } from "../lib/types";

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
  ].join(" ") + "\n");
}

console.log("\nhistory performance benchmark");
for (const c of cases) {
  printResult(runCase(c));
}
