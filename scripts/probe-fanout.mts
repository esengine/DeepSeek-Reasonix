/**
 * Reproduces issue #675: count `run_skill` parallel fan-out when the user
 * asks an open question. Mounts the same registry + skills + subagent
 * runner that `reasonix code` uses, but headless — emits a structured
 * log of tool calls instead of rendering a TUI.
 *
 * Run: tsx scripts/probe-fanout.mts
 * Reads DEEPSEEK_API_KEY / DEEPSEEK_BASE_URL from ./.env.
 */

import { readFileSync } from "node:fs";
import { CacheFirstLoop, DeepSeekClient, ImmutablePrefix, ToolRegistry } from "../src/index.js";
import { codeSystemPrompt } from "../src/code/prompt.js";
import { applySkillsIndex } from "../src/skills.js";
import { registerFilesystemTools } from "../src/tools/filesystem.js";
import { JobRegistry } from "../src/tools/jobs.js";
import { registerShellTools } from "../src/tools/shell.js";
import { registerSkillTools } from "../src/tools/skills.js";
import { formatSubagentResult, spawnSubagent } from "../src/tools/subagent.js";

function loadDotenv(path: string): void {
  const txt = readFileSync(path, "utf8");
  for (const line of txt.split(/\r?\n/)) {
    const m = line.match(/^([A-Z_][A-Z0-9_]*)=(.*)$/);
    if (m) process.env[m[1]] ??= m[2];
  }
}
loadDotenv("./.env");

if (!process.env.DEEPSEEK_API_KEY) throw new Error("DEEPSEEK_API_KEY missing");

const rootDir = process.cwd();
const tools = new ToolRegistry();
const jobs = new JobRegistry();
registerFilesystemTools(tools, { rootDir });
registerShellTools(tools, { rootDir, jobs });

const client = new DeepSeekClient({ baseUrl: process.env.DEEPSEEK_BASE_URL });

const spawnedTasks: Array<{ skill: string; task: string; depth: number }> = [];
let activeDepth = 0;
let maxDepth = 0;

registerSkillTools(tools, {
  projectRoot: rootDir,
  subagentRunner: async (skill, task, signal) => {
    activeDepth++;
    if (activeDepth > maxDepth) maxDepth = activeDepth;
    spawnedTasks.push({ skill: skill.name, task: task.slice(0, 140), depth: activeDepth });
    try {
      const result = await spawnSubagent({
        client,
        parentRegistry: tools,
        parentSignal: signal,
        system: skill.body,
        task,
        model: skill.model,
        allowedTools: skill.allowedTools,
        skillName: skill.name,
      });
      return formatSubagentResult(result);
    } finally {
      activeDepth--;
    }
  },
});

const systemPrompt = applySkillsIndex(codeSystemPrompt(rootDir), { projectRoot: rootDir });
const prefix = new ImmutablePrefix({ system: systemPrompt, toolSpecs: tools.specs() });
const loop = new CacheFirstLoop({ client, prefix, tools });

const USER_PROMPT = process.argv[2] ?? "what kind of project is this?";

const toolCallsPerTurn: Array<Record<string, number>> = [];
let currentTurn: Record<string, number> = {};
let runSkillFirstBatch: string[] = [];
let firstBatchCaptured = false;
let assistantText = "";

console.log(`▸ probe-fanout: sending "${USER_PROMPT}"`);
console.log(`▸ registered tools: ${tools.size}`);
console.log("");

const t0 = Date.now();
try {
  for await (const ev of loop.step(USER_PROMPT)) {
    if (ev.role === "assistant_delta" && ev.content) assistantText += ev.content;
    if (ev.role === "assistant_final") {
      if (Object.keys(currentTurn).length > 0) {
        toolCallsPerTurn.push(currentTurn);
        currentTurn = {};
      }
    }
    if (ev.role === "tool") {
      const name = (ev as { toolName?: string }).toolName ?? "?";
      currentTurn[name] = (currentTurn[name] ?? 0) + 1;
      if (!firstBatchCaptured && name === "run_skill") {
        runSkillFirstBatch.push(String((ev as { args?: unknown }).args ?? "").slice(0, 200));
      }
    }
    if (ev.role === "done") {
      if (runSkillFirstBatch.length > 0) firstBatchCaptured = true;
    }
    if (ev.role === "error") {
      console.error(`[error] ${(ev as { error?: string }).error}`);
    }
  }
} catch (err) {
  console.error(`probe aborted: ${(err as Error).message}`);
}
const elapsed = ((Date.now() - t0) / 1000).toFixed(1);

console.log("───────────────────────────────────────────────");
console.log(`elapsed: ${elapsed}s`);
console.log("");
console.log(`tool calls per turn:`);
toolCallsPerTurn.forEach((t, i) => {
  const summary = Object.entries(t)
    .sort((a, b) => b[1] - a[1])
    .map(([n, c]) => `${n}×${c}`)
    .join(" ");
  console.log(`  turn ${i + 1}: ${summary}`);
});
console.log("");
console.log(`run_skill subagent spawns: ${spawnedTasks.length} (max depth: ${maxDepth})`);
for (const [i, s] of spawnedTasks.entries()) {
  console.log(`  [${i + 1}] depth=${s.depth} ${s.skill.padEnd(20)} → ${s.task}`);
}
console.log("");
if (runSkillFirstBatch.length > 0) {
  console.log(`first-turn run_skill arg payloads (${runSkillFirstBatch.length}):`);
  runSkillFirstBatch.forEach((a, i) => console.log(`  [${i + 1}] ${a}`));
}
console.log("");
console.log(`assistant final text (first 360 chars):`);
console.log(`  ${assistantText.slice(0, 360).replace(/\n/g, " ")}`);
