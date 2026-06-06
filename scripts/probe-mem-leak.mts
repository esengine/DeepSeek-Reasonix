/**
 * Long-running memory probe — drives CacheFirstLoop through N turns with a
 * fake fetch (no API key, no network) and samples process memory + key
 * data-structure sizes every K turns. Pinpoints which container is growing
 * unboundedly.
 *
 * Run:
 *   node --expose-gc --import tsx scripts/probe-mem-leak.mts
 *
 * Tuning:
 *   PROBE_TURNS=400 PROBE_SAMPLE=10 PROBE_TOOL_BYTES=8000 \
 *     node --expose-gc --import tsx scripts/probe-mem-leak.mts
 *
 * Output: CSV-ish table — turn, RSS MB, heap MB, log entries,
 * SessionStats.turns, prefix tokens. A clean run has RSS/heap rising
 * during warmup then plateauing; a leak shows monotonic growth.
 */

import { writeHeapSnapshot } from "node:v8";
import { DeepSeekClient } from "../src/client.js";
import { CacheFirstLoop } from "../src/loop.js";
import { ImmutablePrefix } from "../src/memory/runtime.js";
import { ToolRegistry } from "../src/tools.js";

const TURNS = Number(process.env.PROBE_TURNS ?? 200);
const SAMPLE_EVERY = Number(process.env.PROBE_SAMPLE ?? 10);
const TOOL_PAYLOAD_BYTES = Number(process.env.PROBE_TOOL_BYTES ?? 8000);
const SNAPSHOT_DIR = process.env.PROBE_SNAPSHOTS;
// Empty → simulate context growth (1200 + i*4); a number → fixed small prompt
// so the fold threshold is never tripped (the "YOLO with tiny turns" pattern).
const FIXED_PROMPT_TOKENS = process.env.PROBE_PROMPT_TOKENS
  ? Number(process.env.PROBE_PROMPT_TOKENS)
  : null;
// Fraction of prompt_tokens that count as cache hits. Default 0.83 matches
// a healthy cache; 0 defeats the cache entirely and stresses fold.
const CACHE_HIT_RATIO = Number(process.env.PROBE_CACHE_HIT_RATIO ?? 0.83);
// When set, after each turn replace `content` of all tool messages older than
// the most-recent N with a short stub. Simulates "swap old tool result to
// disk" without actual IO — isolates whether dropping in-memory strings is
// enough to let V8 release pages.
const STRIP_OLD_TOOL_KEEP = process.env.PROBE_STRIP_OLD_TOOL_KEEP
  ? Number(process.env.PROBE_STRIP_OLD_TOOL_KEEP)
  : null;

const fillBlock = (size: number, marker: string) => {
  // Deterministic-ish payload so the LLM "responses" look like real tool
  // output — and so heap-snapshot string-deduplication doesn't flatten N
  // identical strings into one entry that hides the leak.
  const chunk = `[${marker}] ${"x".repeat(40)}\n`;
  const out: string[] = [];
  let len = 0;
  while (len < size) {
    out.push(chunk);
    len += chunk.length;
  }
  return out.join("");
};

interface FakeChoice {
  message: {
    role: "assistant";
    content: string;
    reasoning_content?: string | null;
    tool_calls?: Array<{
      id: string;
      type: "function";
      function: { name: string; arguments: string };
    }>;
  };
  finish_reason: "stop" | "tool_calls";
  index: 0;
}

let callCounter = 0;

function fakeFetch(): typeof fetch {
  // Cycle: each turn first responds with one tool_call → tool result feeds
  // back → second response is a short text wrap-up. Matches the
  // "tool-heavy YOLO" pattern that user reports leaks fastest.
  return (async (_url: string, init: RequestInit) => {
    callCounter += 1;
    const body = init.body ? JSON.parse(init.body as string) : {};
    const messages = body.messages as Array<{ role: string }>;
    const lastRole = messages[messages.length - 1]?.role;
    let choice: FakeChoice;
    if (lastRole === "tool") {
      choice = {
        index: 0,
        finish_reason: "stop",
        message: { role: "assistant", content: `ok #${callCounter}` },
      };
    } else {
      const callId = `call_${callCounter.toString(36)}`;
      choice = {
        index: 0,
        finish_reason: "tool_calls",
        message: {
          role: "assistant",
          content: "",
          tool_calls: [
            {
              id: callId,
              type: "function",
              function: { name: "read_block", arguments: `{"id":${callCounter}}` },
            },
          ],
        },
      };
    }
    const promptTokens = FIXED_PROMPT_TOKENS ?? 1200 + callCounter * 4;
    const hit = Math.floor(promptTokens * CACHE_HIT_RATIO);
    const miss = promptTokens - hit;
    return new Response(
      JSON.stringify({
        choices: [choice],
        usage: {
          prompt_tokens: promptTokens,
          completion_tokens: 30,
          total_tokens: promptTokens + 30,
          prompt_cache_hit_tokens: hit,
          prompt_cache_miss_tokens: miss,
        },
      }),
      { status: 200, headers: { "Content-Type": "application/json" } },
    );
  }) as unknown as typeof fetch;
}

function fmtMB(bytes: number): string {
  return (bytes / (1024 * 1024)).toFixed(1).padStart(7);
}

async function main() {
  const reg = new ToolRegistry();
  reg.register({
    name: "read_block",
    description: "Read a block of synthetic content.",
    parameters: {
      type: "object",
      properties: { id: { type: "number" } },
      required: ["id"],
    },
    fn: async (args: Record<string, unknown>) => {
      const id = Number(args.id ?? 0);
      return fillBlock(TOOL_PAYLOAD_BYTES, `block-${id}`);
    },
  });

  const client = new DeepSeekClient({ apiKey: "sk-test", fetch: fakeFetch() });
  const loop = new CacheFirstLoop({
    client,
    prefix: new ImmutablePrefix({
      system: "test agent — call read_block once per turn, then summarize.",
      toolSpecs: reg.specs(),
    }),
    tools: reg,
    stream: false,
    model: "deepseek-chat",
    maxToolIters: 2,
  });

  // Snapshot baseline before any turn.
  if (global.gc) global.gc();
  const baseline = process.memoryUsage();
  console.log(
    `\nbaseline: rss=${fmtMB(baseline.rss)} MB · heap=${fmtMB(baseline.heapUsed)} MB · payload=${TOOL_PAYLOAD_BYTES}B/turn · turns=${TURNS}`,
  );
  console.log(
    `params:   prompt=${FIXED_PROMPT_TOKENS ?? "linear(1200+i*4)"} · cacheHit=${(CACHE_HIT_RATIO * 100).toFixed(0)}%\n`,
  );
  if (SNAPSHOT_DIR) {
    const p = `${SNAPSHOT_DIR}/heap-baseline.heapsnapshot`;
    writeHeapSnapshot(p);
    console.log(`heap snapshot → ${p}`);
  }

  console.log(
    `turn | rss MB  | heap MB | ext MB  | abuf MB | log.len | rss-heap-ext (off-tracked)`,
  );
  console.log(
    `-----+---------+---------+---------+---------+---------+---------------------------`,
  );

  for (let turn = 1; turn <= TURNS; turn++) {
    for await (const _ev of loop.step(`turn ${turn} please`)) {
      /* drain */
    }

    if (STRIP_OLD_TOOL_KEEP !== null) {
      const entries = loop.log.entries as Array<{ role: string; content?: unknown }>;
      const toolIdx: number[] = [];
      for (let i = 0; i < entries.length; i++) {
        if (entries[i]!.role === "tool") toolIdx.push(i);
      }
      const cutoff = toolIdx.length - STRIP_OLD_TOOL_KEEP;
      for (let k = 0; k < cutoff; k++) {
        const e = entries[toolIdx[k]!]!;
        if (typeof e.content === "string" && e.content.length > 100) {
          e.content = `[stub:${e.content.length}b]`;
        }
      }
    }

    if (turn % SAMPLE_EVERY === 0 || turn === 1) {
      if (global.gc) global.gc();
      const m = process.memoryUsage();
      const untracked = m.rss - m.heapUsed - m.external;
      console.log(
        `${String(turn).padStart(4)} | ${fmtMB(m.rss)} | ${fmtMB(m.heapUsed)} | ${fmtMB(m.external)} | ${fmtMB(m.arrayBuffers)} | ${String(loop.log.length).padStart(7)} | ${fmtMB(untracked)} MB`,
      );
    }
  }

  if (global.gc) global.gc();
  const end = process.memoryUsage();
  console.log(
    `\nfinal:    rss=${fmtMB(end.rss)} MB · heap=${fmtMB(end.heapUsed)} MB · log=${loop.log.length} · stats=${loop.stats.summary().turns}`,
  );
  console.log(
    `delta:    rss=${fmtMB(end.rss - baseline.rss)} MB · heap=${fmtMB(end.heapUsed - baseline.heapUsed)} MB`,
  );
  if (SNAPSHOT_DIR) {
    const p = `${SNAPSHOT_DIR}/heap-final.heapsnapshot`;
    writeHeapSnapshot(p);
    console.log(`heap snapshot → ${p}  (diff against heap-baseline.heapsnapshot in Chrome DevTools)`);
  }
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
