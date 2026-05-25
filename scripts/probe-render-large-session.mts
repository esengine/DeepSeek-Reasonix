/**
 * Two-scenario render probe — answers the App.tsx-split debate with numbers.
 *
 *   PROBE_CARDS=500 PROBE_TICKS=200 node --import tsx scripts/probe-render-large-session.mts
 *
 * (a) Ink mount wall-time for N cards — proxies session-restore paint cost.
 * (b) Sibling-tick render counts via react-test-renderer — proves memo holds
 *     (or doesn't) under sustained parent re-render.
 */

process.env.REASONIX_TRACE_RENDERS = "1";
// Silences react-test-renderer's act() warning; harmless outside a test framework.
(globalThis as Record<string, unknown>).IS_REACT_ACT_ENVIRONMENT = true;

import { render as inkRender } from "ink-testing-library";
import React, { forwardRef, useImperativeHandle, useState } from "react";
import TestRenderer from "react-test-renderer";
import { StaticCardStream } from "../src/cli/ui/layout/StaticCardStream.js";
import {
  readRenderTraceStats,
  renderTraceEnabled,
  resetRenderTraceStats,
} from "../src/cli/ui/render-trace.js";
import type { Card } from "../src/cli/ui/state/cards.js";
import { AgentStoreProvider } from "../src/cli/ui/state/provider.js";
import type { SessionInfo } from "../src/cli/ui/state/state.js";

const CARDS = Number(process.env.PROBE_CARDS ?? 500);
const TICKS = Number(process.env.PROBE_TICKS ?? 200);

if (!renderTraceEnabled) {
  console.error(
    "[probe] REASONIX_TRACE_RENDERS did not activate before module-load. Re-run with the env var set in the shell.",
  );
  process.exit(1);
}

const SESSION: SessionInfo = {
  id: "probe",
  branch: "main",
  workspace: process.cwd(),
  model: "deepseek-chat",
};

function makeCards(n: number): Card[] {
  const out: Card[] = [];
  const body = "lorem ipsum dolor sit amet ".repeat(40);
  for (let i = 0; i < n; i++) {
    const mod = i % 3;
    if (mod === 0) {
      out.push({ id: `u-${i}`, ts: i, kind: "user", text: `turn ${i} user prompt` });
    } else if (mod === 1) {
      out.push({
        id: `a-${i}`,
        ts: i,
        kind: "streaming",
        text: body,
        done: true,
        model: "deepseek-chat",
        endedAt: i + 1,
      });
    } else {
      out.push({
        id: `t-${i}`,
        ts: i,
        kind: "tool",
        name: "read_file",
        args: { path: `f${i}.txt` },
        output: body,
        done: true,
        elapsedMs: 12,
      });
    }
  }
  return out;
}

interface TickHandle {
  tick(): void;
}
const TickHarness = forwardRef<TickHandle>(function TickHarness(_, ref): React.ReactElement {
  const [tick, setTick] = useState(0);
  useImperativeHandle(ref, () => ({ tick: () => setTick((v) => v + 1) }), []);
  return React.createElement(
    React.Fragment,
    null,
    React.createElement(StaticCardStream, { suppressLive: false }),
    React.createElement("text", null, `tick=${tick}`),
  );
});

async function scenarioInkMount(cards: Card[]): Promise<number> {
  const start = performance.now();
  const { unmount } = inkRender(
    React.createElement(
      AgentStoreProvider,
      { session: SESSION, initialCards: cards },
      React.createElement(StaticCardStream, { suppressLive: false }),
    ),
  );
  await new Promise((r) => setTimeout(r, 0));
  const ms = performance.now() - start;
  unmount();
  return ms;
}

async function scenarioTickCounts(cards: Card[]): Promise<{ wallMs: number }> {
  resetRenderTraceStats();
  const ref = React.createRef<TickHandle>();
  const root = TestRenderer.create(
    React.createElement(
      AgentStoreProvider,
      { session: SESSION, initialCards: cards },
      React.createElement(TickHarness, { ref }),
    ),
  );
  await new Promise((r) => setTimeout(r, 0));

  const start = performance.now();
  for (let i = 0; i < TICKS; i++) {
    TestRenderer.act(() => {
      ref.current?.tick();
    });
  }
  await new Promise((r) => setTimeout(r, 0));
  const ms = performance.now() - start;
  root.unmount();
  return { wallMs: ms };
}

function printStats(
  label: string,
  stats: Map<string, { count: number; totalMs: number; maxMs: number }>,
): void {
  process.stdout.write(`  ${label}:\n`);
  if (stats.size === 0) {
    process.stdout.write(`    (no traced components fired)\n`);
    return;
  }
  for (const [name, s] of [...stats.entries()].sort((a, b) => b[1].count - a[1].count)) {
    const avg = s.count > 0 ? s.totalMs / s.count : 0;
    process.stdout.write(
      `    ${name.padEnd(24)} ${String(s.count).padStart(4)} renders · avg ${avg.toFixed(2)} ms · max ${s.maxMs.toFixed(1)} ms\n`,
    );
  }
}

async function main(): Promise<void> {
  const cards = makeCards(CARDS);
  process.stdout.write(
    `\n== probe-render-large-session ==\n  cards loaded: ${CARDS}\n  ticks driven: ${TICKS}\n\n`,
  );

  // (a) Ink real-layout mount — what session-restore actually pays
  const mountMs = await scenarioInkMount(cards);
  process.stdout.write(
    `(a) ink mount of ${CARDS} cards: ${mountMs.toFixed(0)} ms  (${(mountMs / CARDS).toFixed(2)} ms/card)\n\n`,
  );

  // (b) Sibling-tick re-renders via react-test-renderer — does memo hold?
  const tick = await scenarioTickCounts(cards);
  process.stdout.write(
    `(b) ${TICKS} sibling-state ticks (parent re-render, memo'd children should skip):\n  wall: ${tick.wallMs.toFixed(1)} ms  (avg ${(tick.wallMs / TICKS).toFixed(2)} ms/tick)\n\n`,
  );
  printStats("per-component render counts", readRenderTraceStats());
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
