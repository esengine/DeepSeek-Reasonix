// UI performance scenarios: the workloads the streaming pipeline must stay
// smooth under — not just "a normal chat answer". Deterministic generators let
// the simulated harness (ui-perf-scenarios.test.ts) assert count budgets in
// CI, and the same specs are the contract for the future real-browser
// (Playwright) driver, which owns the wall-clock/fps side.

export type ScenarioDriver = "simulated" | "browser";

export interface UIPerfScenario {
  id: string;
  title: string;
  chunksPerSec: number;
  reasoningChars: number;
  reasoningVisible: boolean;
  textChars: number;
  paragraphs: number;
  codeFences: number;
  // browser-driver scenarios need a real input pipeline (typing, wheel,
  // selection, tool-card interaction) or a real DOM/session to be meaningful.
  driver: ScenarioDriver;
  notes?: string;
}

export const UI_PERF_SCENARIOS: UIPerfScenario[] = [
  {
    id: "UI-PERF-01",
    title: "normal answer: 2KB markdown prose",
    chunksPerSec: 80,
    reasoningChars: 0,
    reasoningVisible: false,
    textChars: 2_000,
    paragraphs: 8,
    codeFences: 0,
    driver: "simulated",
  },
  {
    id: "UI-PERF-02",
    title: "DeepSeek reasoning: 16KB reasoning + 8KB answer",
    chunksPerSec: 150,
    reasoningChars: 16_000,
    reasoningVisible: true,
    textChars: 8_000,
    paragraphs: 16,
    codeFences: 0,
    driver: "simulated",
  },
  {
    id: "UI-PERF-03",
    title: "code heavy: 10 fences in 20KB markdown",
    chunksPerSec: 120,
    reasoningChars: 0,
    reasoningVisible: false,
    textChars: 20_000,
    paragraphs: 10,
    codeFences: 10,
    driver: "simulated",
  },
  {
    id: "UI-PERF-04",
    title: "interaction under streaming: typing, wheel, selection, tool cards",
    chunksPerSec: 150,
    reasoningChars: 8_000,
    reasoningVisible: true,
    textChars: 8_000,
    paragraphs: 12,
    codeFences: 2,
    driver: "browser",
    notes: "The point is input latency while streaming, not stream fps; needs the real input pipeline.",
  },
  {
    id: "UI-PERF-05",
    title: "long session: 100 turns, 30 tool calls, diffs and code",
    chunksPerSec: 100,
    reasoningChars: 0,
    reasoningVisible: false,
    textChars: 4_000,
    paragraphs: 10,
    codeFences: 2,
    driver: "simulated",
    notes: "Per-delta cost must not scale with transcript length; DOM/heap steady state belongs to the browser driver.",
  },
  {
    id: "UI-PERF-06",
    title: "background agent: tab A streams while tab B is active",
    chunksPerSec: 150,
    reasoningChars: 4_000,
    reasoningVisible: false,
    textChars: 4_000,
    paragraphs: 8,
    codeFences: 0,
    driver: "simulated",
  },
];

export interface ScenarioChunk {
  kind: "text" | "reasoning";
  delta: string;
}

function fill(prefix: string, target: number, sentence: string): string {
  let out = prefix;
  while (out.length < target) out += sentence;
  return out.slice(0, target);
}

// generateScenarioChunks produces the scenario's full stream as fixed-size
// deltas: reasoning first, then answer text with the requested paragraph and
// fence structure. Content is deterministic filler — budgets are about shape
// (blocks, fences, rates), never about wording.
export function generateScenarioChunks(spec: UIPerfScenario, chunkChars = 12): ScenarioChunk[] {
  const chunks: ScenarioChunk[] = [];
  const emit = (kind: "text" | "reasoning", content: string) => {
    for (let i = 0; i < content.length; i += chunkChars) {
      chunks.push({ kind, delta: content.slice(i, i + chunkChars) });
    }
  };
  if (spec.reasoningChars > 0) {
    emit("reasoning", fill("thinking: ", spec.reasoningChars, "considering the next step carefully. "));
  }
  const blocks: string[] = [];
  const proseTarget = Math.max(1, Math.floor((spec.textChars * (spec.codeFences > 0 ? 0.5 : 1)) / spec.paragraphs));
  for (let p = 0; p < spec.paragraphs; p += 1) {
    blocks.push(fill(`Paragraph ${p} with **bold** and \`code\`. `, proseTarget, "More explanatory prose follows here. "));
  }
  if (spec.codeFences > 0) {
    const codeTarget = Math.max(24, Math.floor((spec.textChars * 0.5) / spec.codeFences));
    for (let f = 0; f < spec.codeFences; f += 1) {
      const body = fill(`function block${f}() {\n`, codeTarget, `  compute(${f});\n`);
      blocks.push(`\`\`\`js\n${body}\n\`\`\``);
    }
  }
  emit("text", blocks.join("\n\n") + "\n\n");
  return chunks;
}
