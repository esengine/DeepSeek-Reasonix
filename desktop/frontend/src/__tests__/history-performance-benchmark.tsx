// Run:
//   corepack pnpm --dir desktop/frontend exec tsx src/__tests__/history-performance-benchmark.tsx
//
// Synthetic, privacy-safe benchmark for long restored histories. It logs counts,
// byte lengths, and elapsed times only; it never uses real conversation content.

import {
  noteActivationRequested, noteActivationSettled, noteActivationStarted,
  noteResumeHistoryPage, noteTranscriptRowCounts, resetSessionDiagnostics, sessionPipelineDiagnostics,
} from "../lib/sessionDiagnostics";
import { historyMessagesToItems, initialState, reducer, type Item } from "../lib/useController";
import type { HistoryMessage } from "../lib/types";
import { ChatSource } from "../lib/chatViewSource";

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
  readMs: number;
  projectedNodes: number;
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

function runCase(c: BenchCase): BenchResult {
  const messages = syntheticHistory(c);
  const jsonBytes = JSON.stringify(messages).length;
  const converted = time(() => historyMessagesToItems(messages, "perf"));
  const reduced = time(() => reducer(initialState, { type: "history", messages }));
  const items = converted.value.items;
  const source = new ChatSource(c.name);
  const projection = time(() => source.update({ items, running: false, hydrating: false, hasOlder: false, loadingOlder: false }));
  const transcript = time(() => source.getOrderSnapshot().filter(key => source.getNodeSnapshot(key)?.kind === "user"));
  const range = time(() => source.getOrderSnapshot().map(key => source.getNodeSnapshot(key)));
  const nodeCount = range.value.length;
  source.dispose();
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
    readMs: range.ms,
    projectedNodes: nodeCount,
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
    `readMs=${r.readMs.toFixed(2)}`,
    `projectedNodes=${r.projectedNodes}`,
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
  if (result.convertMs > 1_000 || result.reducerMs > 1_000 || result.transcriptComputeMs > 1_000 || result.projectionMs > 1_000 || result.readMs > 1_000) {
    failures.push(`${result.name}: exceeded 1s responsiveness ceiling`);
  }
  if (result.projectedNodes < input.turns * 3) failures.push(`${result.name}: missing loaded history nodes`);
}

const full10KB = results.find((result) => result.name === "200-turns-full-10KB");
const archived10KB = results.find((result) => result.name === "200-turns-archived-10KB");
if (!full10KB || full10KB.itemStringBytes * 10 >= full10KB.jsonBytes) {
  failures.push("restored full tool results retained too much source text");
}
if (!archived10KB || archived10KB.itemStringBytes * 5 >= archived10KB.jsonBytes) {
  failures.push("restored archived tool results retained too much source text");
}

// ── Session-switch diagnostics ───────────────────────────────────────────────
// These fixtures verify diagnostic interpretation, not physical disk reads.
// The desktop switch tests exercise the real load and snapshot entry points.
// Missing evidence must remain unknown instead of passing a zero-repeat gate.
const switchMessages = syntheticHistory(cases[1]);
const switchPhases = {
  resolveMs: 1, loadMs: 12, rebindMs: 30, historyMs: 9, totalMs: 52,
  loadedMessages: switchMessages.length, loadedBytes: 65_536,
  historyEntries: switchMessages.length, durableReads: 1, outcome: "ok",
};

resetSessionDiagnostics();
noteActivationRequested("switch-ticket");
noteActivationStarted("switch-ticket", "tab-switch");
noteActivationSettled("switch-ticket", "ready");
noteResumeHistoryPage({ messages: switchMessages, switch: switchPhases }, switchPhases.totalMs);
noteTranscriptRowCounts(40, switchMessages.length);
const pipelined = sessionPipelineDiagnostics();
process.stdout.write(`\n${JSON.stringify({
  activation: pipelined.activation,
  history: pipelined.history,
  mountedRows: pipelined.mountedRows,
  duplicateLoadCount: pipelined.duplicateLoadCount,
}, null, 2)}\n`);

if (pipelined.duplicateLoadCount !== 0) {
  failures.push(`switch performed duplicate durable loads: ${pipelined.duplicateLoadCount}`);
}
if (pipelined.history?.source !== "resume-loaded") {
  failures.push(`switch history source = ${pipelined.history?.source}, want resume-loaded`);
}
if (pipelined.history?.entries !== switchMessages.length) {
  failures.push(`switch history entries = ${pipelined.history?.entries}, want ${switchMessages.length}`);
}
if (pipelined.activation?.totalMs === undefined || pipelined.activation.startingToReadyMs === undefined) {
  failures.push("activation phases were not derived from the ticket");
}
if (pipelined.mountedRows?.mounted !== 40 || pipelined.mountedRows.total !== switchMessages.length) {
  failures.push("mounted row counts were not reported");
}
// The gate has to be able to fail, or it proves nothing: a switch that rebuilt
// its first screen from a second read reports two.
noteResumeHistoryPage({ messages: switchMessages, switch: { ...switchPhases, durableReads: 2 } }, switchPhases.totalMs);
if (sessionPipelineDiagnostics().duplicateLoadCount !== 1) {
  failures.push("duplicate-load gate did not observe a second durable read");
}
resetSessionDiagnostics();
if (sessionPipelineDiagnostics().duplicateLoadCount !== null) {
  failures.push("missing switch evidence must remain unknown");
}
noteResumeHistoryPage({ messages: switchMessages, switch: switchPhases }, 60, 8);
if (sessionPipelineDiagnostics().resumeHistory?.source !== "transcript-snapshot" || sessionPipelineDiagnostics().resumeSnapshotMs !== 8) failures.push("modern snapshot timing is missing");
noteResumeHistoryPage({ messages: [] }, 1, 1);
if (sessionPipelineDiagnostics().duplicateLoadCount !== null || sessionPipelineDiagnostics().resumeSwitch) failures.push("an uninstrumented response retained old switch evidence");

if (failures.length > 0) {
  for (const failure of failures) process.stderr.write(`FAIL ${failure}\n`);
  process.exit(1);
}
process.stdout.write("PASS long-history performance contracts\n");
