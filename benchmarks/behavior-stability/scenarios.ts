/** Behavior-stability scenario for PR #1462 — constraint persistence across context folds. */

import { AppendOnlyLog, SessionStats } from "../../src/index.js";
import { ContextManager } from "../../src/context-manager.js";
import { Usage } from "../../src/client.js";
import type { EvalResult, EvalScenario } from "./types.js";

/* ------------------------------------------------------------------ */
/*  Helpers                                                            */
/* ------------------------------------------------------------------ */

function withTiming(fn: () => Promise<EvalResult>): Promise<EvalResult> {
  const t0 = Date.now();
  return fn().then((r) => ({ ...r, durationMs: Date.now() - t0 }));
}

function pass(id: string, details: string): EvalResult {
  return { scenarioId: id, pass: true, durationMs: 0, details };
}

function fail(id: string, details: string): EvalResult {
  return { scenarioId: id, pass: false, durationMs: 0, details };
}

/* ------------------------------------------------------------------ */
/*  Mock client — returns a fixed summary for fold testing             */
/* ------------------------------------------------------------------ */

class MockDeepSeekClient {
  private _replyContent: string;
  constructor(replyContent: string) {
    this._replyContent = replyContent;
  }
  async chat(): Promise<{
    content: string;
    reasoningContent: string;
    usage: Usage;
  }> {
    return {
      content: this._replyContent,
      reasoningContent: "",
      usage: new Usage(100, 50, 150, 0, 100),
    };
  }
}

/* ------------------------------------------------------------------ */
/*  #1462 — Constraint persistence across folds (local)                */
/* ------------------------------------------------------------------ */

const constraintPersistence: EvalScenario = {
  id: "constraint-persistence",
  name: "Context fold preserves pinned constraints from system prompt",
  category: "context",
  requiresApi: false,
  run: () =>
    withTiming(async () => {
      const systemPrompt = `You are a coding assistant.

# HIGH PRIORITY constraints

- DO NOT use npm install without asking.
- Never delete files without confirmation.

# User memory

The user prefers TypeScript over JavaScript.

# Project memory

This project uses pnpm instead of npm.`;

      const log = new AppendOnlyLog();
      // Fill log with synthetic long history to trigger a fold boundary > 0.
      for (let i = 0; i < 40; i++) {
        log.append({
          role: "user",
          content: `Turn ${i}: Please review the codebase and tell me what you found.`,
        });
        log.append({
          role: "assistant",
          content: `Turn ${i} summary: I reviewed the codebase and found several issues including unused imports, missing type annotations, and inconsistent formatting.`,
        });
      }

      const client = new MockDeepSeekClient("Summary of all prior turns.");
      const stats = new SessionStats();
      const ctx = new ContextManager({
        client: client as any,
        log,
        stats,
        sessionName: null,
        getAbortSignal: () => new AbortController().signal,
        getCurrentTurn: () => 1,
        getSystemPrompt: () => systemPrompt,
      });

      // Use a tiny tail budget so almost everything becomes head and gets summarized.
      const result = await ctx.fold("deepseek-v4-flash", { keepRecentTokens: 200 });

      if (!result.folded) {
        return fail(
          "constraint-persistence",
          `Fold did not happen (before=${result.beforeMessages}, after=${result.afterMessages}).`,
        );
      }

      const foldedSystem = log.toMessages()[0];
      if (foldedSystem?.role !== "assistant") {
        return fail(
          "constraint-persistence",
          `Expected first folded message to be assistant summary, got ${foldedSystem?.role}.`,
        );
      }

      const content = foldedSystem.content as string;
      const checks = [
        { text: "DO NOT use npm install without asking", label: "HIGH PRIORITY constraint" },
        { text: "Never delete files without confirmation", label: "HIGH PRIORITY constraint 2" },
        { text: "TypeScript over JavaScript", label: "User memory" },
        { text: "pnpm instead of npm", label: "Project memory" },
      ];

      const missing = checks.filter((c) => !content.includes(c.text));
      if (missing.length > 0) {
        return fail(
          "constraint-persistence",
          `Folded summary missing ${missing.length} constraint(s): ${missing.map((m) => m.label).join(", ")}.`,
        );
      }

      return pass(
        "constraint-persistence",
        `Fold reduced ${result.beforeMessages} → ${result.afterMessages} messages and preserved all ${checks.length} pinned constraints.`,
      );
    }),
};

/* ------------------------------------------------------------------ */
/*  Exports                                                            */
/* ------------------------------------------------------------------ */

export const LOCAL_SCENARIOS: EvalScenario[] = [constraintPersistence];

export const API_SCENARIOS: EvalScenario[] = [];

export const ALL_SCENARIOS: EvalScenario[] = [...LOCAL_SCENARIOS, ...API_SCENARIOS];
