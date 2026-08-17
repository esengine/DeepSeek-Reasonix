// bridgeBenchFixtures — deterministic heavyweight mock sessions for the
// real-DOM benchmark harness (desktop/frontend/bench), served by the dev mock
// when the page URL carries ?mock=bench. This module is imported lazily from
// bridge.ts so the generators never land in the eager production bundle.

import type { HistoryMessage, HistoryToolCall } from "./types";

// ── Benchmark fixtures (?mock=bench, Phase F) ─────────────────────────────
// Fixed diagnostic sessions for the real-DOM performance harness
// (desktop/frontend/bench). Content is deterministic and generated once per
// page load. Shapes follow the plan's fixtures, mirrored from the Go-side
// history_slice tests: a tool-dense 38-turn session (~3.2k provider
// messages), a markdown-heavy 46-turn session (~600 messages incl. one
// ~500KiB answer with a big table and one oversized code block), a small
// 6-turn session, and a single turn with thousands of messages.
const benchFixtureCache = new Map<string, HistoryMessage[]>();
const benchFixture = (key: string, build: () => HistoryMessage[]): HistoryMessage[] => {
  const cached = benchFixtureCache.get(key);
  if (cached) return cached;
  const built = build();
  benchFixtureCache.set(key, built);
  return built;
};
const benchToolOutput = (turn: number, index: number): string =>
  [
    `ok turn=${turn} call=${index}`,
    "status: success",
    `duration_ms: ${(turn * 7 + index * 13) % 420}`,
    `detail: ${"x".repeat(120)}`,
  ].join("\n");
const benchToolTurn = (turn: number, callCount: number, answer: string): HistoryMessage[] => {
  const toolCalls: HistoryToolCall[] = [];
  const results: HistoryMessage[] = [];
  for (let k = 0; k < callCount; k += 1) {
    const id = `bench-t${turn}-call-${k}`;
    const readOnly = k % 3 !== 0;
    toolCalls.push({
      id,
      name: readOnly ? "read_file" : "bash",
      arguments: JSON.stringify(readOnly ? { path: `internal/bench/pkg-${k}/mod.go` } : { command: `go test ./internal/bench/pkg-${k}` }),
      resolvedReadOnly: readOnly,
      subject: readOnly ? `internal/bench/pkg-${k}/mod.go` : `go test pkg-${k}`,
    });
    results.push({ role: "tool", toolCallId: id, toolName: toolCalls[k].name, content: benchToolOutput(turn, k) });
  }
  return [
    { role: "user", content: `bench turn ${turn}: run the verification batch and summarize per-package results.` },
    { role: "assistant", content: "", reasoning: `planning verification batch ${turn}: ${callCount} checks.`, toolCalls },
    ...results,
    ...(answer ? [{ role: "assistant", content: answer, workDurationMs: 1200 }] : []),
  ];
};
const benchToolDenseHistory = (): HistoryMessage[] => {
  // 38 visible turns × 86 messages = 3268 provider messages (nominal 3255).
  const messages: HistoryMessage[] = [];
  for (let turn = 1; turn <= 38; turn += 1) messages.push(...benchToolTurn(turn, 42, turn % 4 === 0 ? `Batch ${turn} done: all checks green.` : ""));
  return messages;
};
const benchMarkdownSection = (turn: number): string =>
  [
    `## Turn ${turn} summary`,
    "",
    "The verification sweep completed with all packages green. Key observations:",
    "",
    "- display-index hits stayed high across paged reads",
    "- long tasks remained under the main-thread budget",
    "- cache weights stayed within their declared byte budgets",
    "",
    "```ts",
    "export function digest(values: number[]): number {",
    "  return values.reduce((acc, v) => (acc * 31 + v) | 0, 7);",
    "}",
    "```",
    "",
    "| package | tests | duration ms |",
    "| --- | ---: | ---: |",
    ...Array.from({ length: 12 }, (_, k) => `| pkg-${k} | ${20 + k * 3} | ${40 + ((turn * 17 + k * 29) % 300)} |`),
    "",
  ].join("\n");
const benchBigMarkdownAnswer = (): string => {
  // ~500KiB answer: a giant table plus repeated prose/code sections.
  const parts: string[] = ["# Full verification report", ""];
  parts.push("| row | package | tests | duration ms | status |", "| ---: | --- | ---: | ---: | --- |");
  for (let row = 0; row < 4000; row += 1) {
    parts.push(`| ${row} | pkg-${row % 64} | ${(row * 7) % 90} | ${(row * 13) % 800} | ${row % 11 === 0 ? "flaky" : "green"} |`);
  }
  let body = parts.join("\n");
  let section = 0;
  while (body.length < 500 * 1024) {
    section += 1;
    body += `\n\n${benchMarkdownSection(1000 + section)}`;
  }
  return body;
};
const benchOversizedCodeBlock = (): string => {
  // Single >64KiB code block (a content-ref candidate on the real backend).
  const line = "const row = await db.query('select id, payload from bench where shard = $1', [shard]); // ";
  const lines = Math.ceil((300 * 1024) / line.length);
  return ["Here is the full generated migration:", "", "```sql", ...Array.from({ length: lines }, (_, k) => `-- ${k} ${line}`), "```"].join("\n");
};
const benchMarkdownHeavyHistory = (): HistoryMessage[] => {
  // 46 visible turns; 13 messages per normal turn (10 tool pairs), plus the
  // newest turn carrying the ~500KiB report and the oversized code block.
  const messages: HistoryMessage[] = [];
  for (let turn = 1; turn <= 45; turn += 1) messages.push(...benchToolTurn(turn, 5, benchMarkdownSection(turn)));
  messages.push(
    { role: "user", content: "bench final turn: produce the full verification report with the big table, then the migration SQL." },
    { role: "assistant", content: benchBigMarkdownAnswer(), workDurationMs: 2400 },
    { role: "assistant", content: benchOversizedCodeBlock(), workDurationMs: 800 },
  );
  return messages;
};
const benchSmallHistory = (): HistoryMessage[] => {
  // 6 visible turns × 78 messages = 468 provider messages (nominal 473).
  const messages: HistoryMessage[] = [];
  for (let turn = 1; turn <= 6; turn += 1) {
    const answer = turn === 6
      ? [
          "# Asynchronously hydrated verification appendix",
          ...Array.from({ length: 1_200 }, (_, row) => `- package-${row % 42}: verified row ${row} with stable virtual measurements`),
          "ASYNC LAYOUT EXPANSION COMPLETE",
        ].join("\n")
      : `Batch ${turn} summary.`;
    messages.push(...benchToolTurn(turn, 38, answer));
  }
  return messages;
};
const benchGiantTurnHistory = (): HistoryMessage[] => {
  // A single turn with thousands of messages (1000 tool pairs).
  return benchToolTurn(1, 1000, "Single-turn sweep complete.");
};

// Ref-resolution storm fixture (#8657): the newest page of a long session
// carries many ref-replaced fields, so opening the session fires a paced
// stream of history_items_patch invalidations — the exact load that used to
// remount the virtual list on every scroll idle and strand the view at
// estimate-based restore landings. The marker sits past the 4KiB preview
// cut, so it only appears in the DOM once the ref has resolved.
export const BENCH_STORM_MARKER = "BENCH STORM HYDRATION RESOLVED";
const benchStormAnswer = (turn: number): string => {
  let body = [
    `## Storm turn ${turn} verification report`,
    "",
    "Inline preview summary: the batch completed and per-package details follow.",
    "",
  ].join("\n");
  let row = 0;
  while (body.length < 9 * 1024) {
    row += 1;
    body += `\n- storm-${turn}-${row}: resolved payload ${"y".repeat(90)}`;
  }
  body += `\n- storm-${turn}-FINAL ${BENCH_STORM_MARKER}`;
  return body;
};
const benchStormReasoning = (turn: number): string => {
  let body = `planning storm turn ${turn}: gather results, then tabulate.\n`;
  while (body.length < 6 * 1024) body += `reasoning fragment ${turn} ${"z".repeat(90)}\n`;
  return `${body}${BENCH_STORM_MARKER}`;
};
const benchStormHistory = (): HistoryMessage[] => {
  // 40 visible turns. The newest 12 turns (exactly the page a session opens
  // with) carry ref-replaced answer+reasoning fields; turns 1-28 are small
  // and eager. Opening the session resolves ~24 refs at a paced interval.
  const messages: HistoryMessage[] = [];
  for (let turn = 1; turn <= 40; turn += 1) {
    if (turn <= 28) {
      messages.push(...benchToolTurn(turn, 3, `Batch ${turn} summary.`));
    } else {
      messages.push(
        { role: "user", content: `storm turn ${turn}: produce the verification report.` },
        { role: "assistant", content: benchStormAnswer(turn), reasoning: benchStormReasoning(turn), workDurationMs: 900 },
      );
    }
  }
  return messages;
};

/** The bench session for a mock topic, or undefined for non-bench topics. */
export function benchTopicHistory(topicId: string): HistoryMessage[] | undefined {
  switch (topicId) {
    case "topic_bench_tools":
      return benchFixture("tools", benchToolDenseHistory);
    case "topic_bench_markdown":
      return benchFixture("markdown", benchMarkdownHeavyHistory);
    case "topic_bench_small":
      return benchFixture("small", benchSmallHistory);
    case "topic_bench_giant_turn":
      return benchFixture("giant", benchGiantTurnHistory);
    case "topic_bench_storm":
      return benchFixture("storm", benchStormHistory);
    default:
      return undefined;
  }
}
