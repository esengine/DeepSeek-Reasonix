import { type DeepSeekClient, Usage } from "./client.js";
import type { PauseGate } from "./core/pause-gate.js";
import { pauseGate as defaultPauseGate } from "./core/pause-gate.js";
import { type HookPayload, type ResolvedHook, runHooks } from "./hooks.js";
import {
  DEFAULT_MAX_RESULT_CHARS,
  DEFAULT_MAX_RESULT_TOKENS,
  truncateForModel,
  truncateForModelByTokens,
} from "./mcp/registry.js";

import { ContextManager } from "./context-manager.js";
import { InflightSet } from "./core/inflight.js";
import { t } from "./i18n/index.js";
import { formatLoopError, is5xxError, probeDeepSeekReachable } from "./loop/errors.js";
import { type ForceSummaryContext, forceSummaryAfterIterLimit } from "./loop/force-summary.js";
import {
  fixToolCallPairing,
  healLoadedMessages,
  healLoadedMessagesByTokens,
  stampMissingReasoningForThinkingMode,
} from "./loop/healing.js";
import { hookWarnings, safeParseToolArgs } from "./loop/hook-events.js";
import { buildAssistantMessage, buildSyntheticAssistantMessage } from "./loop/messages.js";
import {
  looksLikeCompleteJson,
  shrinkOversizedToolCallArgsByTokens,
  shrinkOversizedToolResults,
  shrinkOversizedToolResultsByTokens,
} from "./loop/shrink.js";
import {
  isThinkingModeModel,
  stripHallucinatedToolMarkup,
  thinkingModeForModel,
} from "./loop/thinking.js";
import type { LoopEvent } from "./loop/types.js";
import { AppendOnlyLog, type ImmutablePrefix, VolatileScratch } from "./memory/runtime.js";
import {
  appendSessionMessage,
  archiveSession,
  loadSessionMessages,
  loadSessionMeta,
  rewriteSession,
} from "./memory/session.js";
import { type RepairReport, ToolCallRepair } from "./repair/index.js";
import { SessionStats, type TurnStats } from "./telemetry/stats.js";
import { ToolRegistry } from "./tools.js";
import { parseRateLimitedToolResult } from "./tools/rate-limit.js";
import { ReadTracker } from "./tools/read-tracker.js";
import type { ChatMessage, ToolCall } from "./types.js";

export const MID_TURN_STEER_WRAPPER =
  "[Mid-turn steer queued by the user. Do not treat this as a new task; use it only as additional guidance for the current task after completing the current step.]";

function formatSteerUserMessage(content: string): string {
  return [MID_TURN_STEER_WRAPPER, content].join("\n");
}

export {
  fixToolCallPairing,
  formatLoopError,
  healLoadedMessages,
  healLoadedMessagesByTokens,
  isThinkingModeModel,
  looksLikeCompleteJson,
  shrinkOversizedToolCallArgsByTokens,
  shrinkOversizedToolResults,
  shrinkOversizedToolResultsByTokens,
  stampMissingReasoningForThinkingMode,
  stripHallucinatedToolMarkup,
  thinkingModeForModel,
};
export type { EventRole, LoopEvent } from "./loop/types.js";

export interface CacheFirstLoopOptions {
  client: DeepSeekClient;
  prefix: ImmutablePrefix;
  tools?: ToolRegistry;
  model?: string;
  stream?: boolean;
  reasoningEffort?: "high" | "max";
  /** Soft USD cap — warns at 80%, refuses next turn at 100%. Opt-in (default no cap). */
  budgetUsd?: number;
  session?: string;
  /** PreToolUse + PostToolUse only — UserPromptSubmit / Stop live at the App boundary. */
  hooks?: ResolvedHook[];
  /** `cwd` reported to hooks; `reasonix code` sets this to the sandbox root, not shell home. */
  hookCwd?: string;
  /** PauseGate bridge — defaults to singleton, injectable for tests. */
  confirmationGate?: PauseGate;
  /** Re-runs the prompt builder (applyMemoryStack / codeSystemPrompt) on /new so REASONIX.md edits take effect without a restart. Accepting a cache miss is the price. */
  rebuildSystem?: () => string;
}

export interface ReconfigurableOptions {
  model?: string;
  stream?: boolean;
  /** V4 thinking mode only; deepseek-chat ignores. */
  reasoningEffort?: "high" | "max";
}

export class CacheFirstLoop {
  readonly client: DeepSeekClient;
  readonly prefix: ImmutablePrefix;
  readonly tools: ToolRegistry;
  readonly log = new AppendOnlyLog();
  readonly scratch = new VolatileScratch();
  readonly stats = new SessionStats();
  readonly repair: ToolCallRepair;
  /** Files the model has read this session; gates edit_file / multi_edit so SEARCH text matches on-disk bytes. Cleared on fold / mechanical truncate (the model's byte-level view of the elided history is gone). In-memory only — naturally empty on resume. */
  readonly readTracker = new ReadTracker();

  // Mutable via configure() — slash commands in the TUI / library callers tweak
  // these mid-session so users don't have to restart.
  model: string;
  stream: boolean;
  reasoningEffort: "high" | "max";
  budgetUsd: number | null;
  /** One-shot 80% warning latch — cleared by setBudget so a bump re-arms at the new boundary. */
  private _budgetWarned = false;
  sessionName: string | null;

  hooks: ResolvedHook[];
  hookCwd: string;

  /** PauseGate bridge — defaults to singleton, injectable for tests. */
  readonly confirmationGate: PauseGate;

  /** Number of messages that were pre-loaded from the session file. */
  readonly resumedMessageCount: number;

  private readonly _rebuildSystem: (() => string) | null;

  private _turn = 0;
  private _streamPreference: boolean;
  /** Threaded through HTTP + every tool dispatch so Esc cancels in-flight work, not after. */
  private _turnAbort: AbortController = new AbortController();
  /** Authoritative running-id set — UI cards consult this instead of trusting end-event delivery. Insert at dispatch entry, delete in finally. */
  private readonly _inflight = new InflightSet();

  /** Typeahead steer messages set by the UI; step() consumes one at each iter boundary. */
  private readonly _steerQueue: string[] = [];

  /** Set true when a steer was consumed this turn; cleared on next step() entry. */
  private _steerConsumed = false;

  /** UI calls this to inject a mid-turn steer message without aborting the current turn.
   *  New text resets steerConsumed because a fresh steer is queued. */
  steer(text: string | null): void {
    if (text === null) {
      this._steerQueue.length = 0;
      return;
    }
    this._steerQueue.push(text);
    this._steerConsumed = false;
  }

  /** True when a steer was consumed this turn (UI gate to avoid double-submit). */
  get steerConsumed(): boolean {
    return this._steerConsumed;
  }

  private _turnSelfCorrected = false;
  private _foldedThisTurn = false;
  private context!: ContextManager;

  /** Subscribe API so UI hooks can derive `running` from finally-guaranteed insertions. */
  get inflight(): InflightSet {
    return this._inflight;
  }

  get currentTurn(): number {
    return this._turn;
  }

  constructor(opts: CacheFirstLoopOptions) {
    this.client = opts.client;
    this.prefix = opts.prefix;
    this.tools = opts.tools ?? new ToolRegistry();
    this.model = opts.model ?? "deepseek-v4-flash";
    this.reasoningEffort = opts.reasoningEffort ?? "max";
    this.budgetUsd =
      typeof opts.budgetUsd === "number" && opts.budgetUsd > 0 ? opts.budgetUsd : null;

    this.hooks = opts.hooks ?? [];
    this.hookCwd = opts.hookCwd ?? process.cwd();
    this.confirmationGate = opts.confirmationGate ?? defaultPauseGate;
    this._rebuildSystem = opts.rebuildSystem ?? null;

    this._streamPreference = opts.stream ?? true;
    this.stream = this._streamPreference;

    const allowedNames = new Set([...this.prefix.toolSpecs.map((s) => s.function.name)]);
    // Storm breaker clears its window on mutating calls so read → edit → verify isn't a storm.
    const registry = this.tools;
    const isStormExempt = (call: ToolCall): boolean => {
      const name = call.function?.name;
      if (!name) return false;
      return registry.get(name)?.stormExempt === true;
    };
    this.repair = new ToolCallRepair({
      allowedToolNames: allowedNames,
      isMutating: (call) => this.isMutating(call),
      isStormExempt,
      stormThreshold: parsePositiveIntEnv(process.env.REASONIX_STORM_THRESHOLD),
      stormWindow: parsePositiveIntEnv(process.env.REASONIX_STORM_WINDOW),
    });

    // Heal-on-load: oversized tool results would 400 the next call before the user types.
    this.sessionName = opts.session ?? null;
    if (this.sessionName) {
      const prior = loadSessionMessages(this.sessionName);
      const shrunk = healLoadedMessagesByTokens(prior, DEFAULT_MAX_RESULT_TOKENS);
      // Thinking-mode sessions: API 400s if any historical assistant turn lacks reasoning_content.
      const stamped = stampMissingReasoningForThinkingMode(shrunk.messages, this.model);
      const messages = stamped.messages;
      const healedCount = shrunk.healedCount + stamped.stampedCount;
      const tokensSaved = shrunk.tokensSaved;
      for (const msg of messages) this.log.append(msg);
      this.resumedMessageCount = messages.length;
      this._turn = messages.reduce((n, m) => (m.role === "assistant" ? n + 1 : n), 0);
      // Carry forward cumulative cost / turn count so the TUI's session
      // total continues across resumes; otherwise each restart resets to $0.
      if (messages.length > 0) {
        const meta = loadSessionMeta(this.sessionName);
        this.stats.seedCarryover({
          totalCostUsd: meta.totalCostUsd,
          turnCount: meta.turnCount,
          cacheHitTokens: meta.cacheHitTokens,
          cacheMissTokens: meta.cacheMissTokens,
          lastPromptTokens: meta.lastPromptTokens,
        });
      }
      if (healedCount > 0) {
        // Persist healed log so the same break isn't re-noticed every restart.
        try {
          rewriteSession(this.sessionName, messages);
        } catch {
          /* disk full / perms — skip, in-memory heal still applies */
        }
        process.stderr.write(
          `▸ session "${this.sessionName}": healed ${healedCount} entr${healedCount === 1 ? "y" : "ies"}${tokensSaved > 0 ? ` (shrunk ${tokensSaved.toLocaleString()} tokens of oversized tool output)` : " (dropped dangling tool_calls tail)"}. Rewrote session file.\n`,
        );
      }
    } else {
      this.resumedMessageCount = 0;
    }

    this.context = new ContextManager({
      client: this.client,
      log: this.log,
      stats: this.stats,
      sessionName: this.sessionName,
      getAbortSignal: () => this._turnAbort.signal,
      getCurrentTurn: () => this._turn,
      getSystemPrompt: () => this.prefix.system,
      getToolSpecs: () => this.prefix.toolSpecs,
      getFewShots: () => this.prefix.fewShots,
      onLogRewrite: () => this.readTracker.reset(),
    });
  }

  /** Replace older turns with one summary message; keep tail within keepRecentTokens budget. */
  async compactHistory(opts?: { keepRecentTokens?: number }): Promise<{
    folded: boolean;
    beforeMessages: number;
    afterMessages: number;
    summaryChars: number;
  }> {
    return this.context.fold(this.model, opts);
  }

  /** Real-time token count of the current log — forwarded to Desktop for meter refresh. */
  getCurrentLogTokens(): number {
    return this.context.getLogTokens();
  }

  appendAndPersist(message: ChatMessage): void {
    this.log.append(message);
    if (this.sessionName) {
      try {
        appendSessionMessage(this.sessionName, message);
      } catch {
        /* disk full or permission denied shouldn't kill the chat */
      }
    }
  }

  /** Swap the just-appended assistant entry — used by self-correction to restore the original tool_calls without dropping reasoning_content. */
  private replaceTailAssistantMessage(message: ChatMessage): void {
    const entries = this.log.entries;
    const tail = entries[entries.length - 1];
    if (!tail || tail.role !== "assistant") return;
    const kept = entries.slice(0, -1);
    kept.push(message);
    this.log.compactInPlace(kept);
    if (this.sessionName) {
      try {
        rewriteSession(this.sessionName, kept);
      } catch {
        /* disk issue shouldn't block the in-memory swap */
      }
    }
  }

  /** "New chat" — drops in-memory messages, archives the on-disk transcript so it survives in Sessions, keeps sessionName so the prefix cache stays warm. Re-runs the system-prompt builder if one was wired (issue #778: REASONIX.md edits otherwise need a restart). */
  clearLog(): { dropped: number; archived: string | null; systemRebuilt: boolean } {
    const dropped = this.log.length;
    this.log.compactInPlace([]);
    let archived: string | null = null;
    if (this.sessionName) {
      try {
        archived = archiveSession(this.sessionName);
        if (archived === null) rewriteSession(this.sessionName, []);
      } catch {
        /* disk issue shouldn't block the in-memory clear */
      }
    }
    this.scratch.reset();
    this._inflight.clear();
    this.stats.reset();
    this._turn = 0;
    this._budgetWarned = false;
    let systemRebuilt = false;
    if (this._rebuildSystem) {
      try {
        systemRebuilt = this.prefix.replaceSystem(this._rebuildSystem());
      } catch {
        /* builder threw — keep prior system rather than crash /new */
      }
    }
    return { dropped, archived, systemRebuilt };
  }

  /** `/cwd` follow-through — archives the previous session, drops in-memory state, repoints sessionName, and rebuilds the system prompt against whatever the rebuilder closure now resolves (the caller is expected to have already updated the root the closure reads). */
  switchWorkspace(opts: { sessionName: string }): { dropped: number; archived: string | null } {
    const dropped = this.log.length;
    let archived: string | null = null;
    if (this.sessionName) {
      try {
        archived = archiveSession(this.sessionName);
        if (archived === null) rewriteSession(this.sessionName, []);
      } catch {
        /* disk issue shouldn't block the in-memory swap */
      }
    }
    this.log.compactInPlace([]);
    this.scratch.reset();
    this._inflight.clear();
    this.sessionName = opts.sessionName;
    if (this._rebuildSystem) {
      try {
        this.prefix.replaceSystem(this._rebuildSystem());
      } catch {
        /* builder threw — keep prior system rather than crash /cwd */
      }
    }
    return { dropped, archived };
  }

  configure(opts: ReconfigurableOptions): void {
    if (opts.model !== undefined) this.model = opts.model;
    if (opts.stream !== undefined) {
      this._streamPreference = opts.stream;
      this.stream = opts.stream;
    }
    if (opts.reasoningEffort !== undefined) this.reasoningEffort = opts.reasoningEffort;
  }

  /** `null` disables the cap; any change re-arms the 80% warning. */
  setBudget(usd: number | null): void {
    this.budgetUsd = typeof usd === "number" && usd > 0 ? usd : null;
    this._budgetWarned = false;
  }

  /** UI surface — model id of the call about to run (or running) right now. */
  get currentCallModel(): string {
    return this.model;
  }

  /** A call counts as mutating when its definition reports `readOnly !== true` and any dynamic `readOnlyCheck` doesn't override that for these args. */
  private isMutating(call: ToolCall): boolean {
    const name = call.function?.name;
    if (!name) return false;
    const def = this.tools.get(name);
    if (!def) return false;
    if (def.readOnlyCheck) {
      let args: Record<string, unknown> = {};
      try {
        args = JSON.parse(call.function?.arguments ?? "{}") ?? {};
      } catch {
        // Malformed args → fall through to the static flag below; the
        // dynamic check would've thrown anyway.
      }
      try {
        if (def.readOnlyCheck(args as never)) return false;
      } catch (err) {
        // Mirror tools.ts: surface buggy readOnlyCheck instead of silently
        // falling through to the static flag.
        process.stderr.write(`readOnlyCheck for ${name} threw: ${(err as Error).message}\n`);
      }
    }
    return def.readOnly !== true;
  }

  private async runOneToolCall(
    call: ToolCall,
    signal: AbortSignal,
  ): Promise<{ preWarnings: LoopEvent[]; postWarnings: LoopEvent[]; result: string }> {
    const name = call.function?.name ?? "";
    const args = call.function?.arguments ?? "{}";
    const parsedArgs = safeParseToolArgs(args);
    this._inflight.add(this.inflightIdFor(call));
    try {
      const preReport = await runHooks({
        hooks: this.hooks,
        payload: {
          event: "PreToolUse",
          cwd: this.hookCwd,
          toolName: name,
          toolArgs: parsedArgs,
        },
      });
      const preWarnings = [...hookWarnings(preReport.outcomes, this._turn)];

      if (preReport.blocked) {
        const blocking = preReport.outcomes[preReport.outcomes.length - 1];
        const reason = (
          blocking?.stderr ||
          blocking?.stdout ||
          "blocked by PreToolUse hook"
        ).trim();
        return {
          preWarnings,
          postWarnings: [],
          result: `[hook block] ${blocking?.hook.command ?? "<unknown>"}\n${reason}`,
        };
      }

      const result = await this.tools.dispatch(name, args, {
        signal,
        maxResultTokens: DEFAULT_MAX_RESULT_TOKENS,
        confirmationGate: this.confirmationGate,
        readTracker: this.readTracker,
      });

      const postReport = await runHooks({
        hooks: this.hooks,
        payload: {
          event: "PostToolUse",
          cwd: this.hookCwd,
          toolName: name,
          toolArgs: parsedArgs,
          toolResult: result,
        },
      });
      const postWarnings = [...hookWarnings(postReport.outcomes, this._turn)];

      return { preWarnings, postWarnings, result };
    } finally {
      this._inflight.delete(this.inflightIdFor(call));
    }
  }

  /** Stable per-call id used as the inflight key AND threaded into tool_start / tool events so the UI matches them up. */
  private inflightIdFor(call: ToolCall): string {
    if (call.id) return call.id;
    const fallback = (call as { _inflightFallback?: string })._inflightFallback;
    if (fallback) return fallback;
    const generated = `inflight-${++this._inflightCounter}`;
    (call as { _inflightFallback?: string })._inflightFallback = generated;
    return generated;
  }
  private _inflightCounter = 0;

  private buildMessages(): ChatMessage[] {
    const healedMessages = this.healActiveLogBeforeSend();
    return [...this.prefix.toMessages(), ...healedMessages];
  }

  private healActiveLogBeforeSend(): ChatMessage[] {
    const current = this.log.toMessages();
    const healed = healLoadedMessages(current, DEFAULT_MAX_RESULT_CHARS);
    if (healed.healedCount === 0) return current;
    this.log.compactInPlace(healed.messages);
    if (this.sessionName) {
      try {
        rewriteSession(this.sessionName, healed.messages);
      } catch {
        /* disk issue shouldn't block the in-memory heal */
      }
    }
    return healed.messages;
  }

  abort(): void {
    this._turnAbort.abort();
  }

  /** Drop the last user message + everything after; caller re-sends. Persists to session file. */
  retryLastUser(): string | null {
    const entries = this.log.entries;
    let lastUserIdx = -1;
    for (let i = entries.length - 1; i >= 0; i--) {
      if (entries[i]!.role === "user") {
        lastUserIdx = i;
        break;
      }
    }
    if (lastUserIdx < 0) return null;
    const raw = entries[lastUserIdx]!.content;
    const userText = typeof raw === "string" ? raw : "";
    const preserved = entries.slice(0, lastUserIdx).map((m) => ({ ...m }));
    this.log.compactInPlace(preserved);
    if (this.sessionName) {
      try {
        rewriteSession(this.sessionName, preserved);
      } catch {
        /* disk-full / perms — in-memory compaction still applies */
      }
    }
    return userText;
  }

  /** Rewind to the N-th user turn (0-indexed). Drops that turn + everything after. */
  rewindToUserTurn(userTurnIndex: number): string | null {
    const entries = this.log.entries;
    let count = 0;
    let targetIdx = -1;
    for (let i = 0; i < entries.length; i++) {
      if (entries[i]!.role !== "user") continue;
      if (count === userTurnIndex) {
        targetIdx = i;
        break;
      }
      count++;
    }
    if (targetIdx < 0) return null;
    const raw = entries[targetIdx]!.content;
    const userText = typeof raw === "string" ? raw : "";
    const preserved = entries.slice(0, targetIdx).map((m) => ({ ...m }));
    this.log.compactInPlace(preserved);
    if (this.sessionName) {
      try {
        rewriteSession(this.sessionName, preserved);
      } catch {
        /* disk-full / perms — in-memory compaction still applies */
      }
    }
    return userText;
  }

  async *step(userInput: string): AsyncGenerator<LoopEvent> {
    // Reset per-turn flags.
    this._steerConsumed = false;

    // Budget gate runs FIRST, before any per-turn state mutation, so a
    // refusal leaves the loop unchanged and the user can correct the
    // cap and re-issue. Default `null` short-circuits the whole check
    // so the no-budget path is one comparison, no behavior delta.
    if (this.budgetUsd !== null) {
      const spent = this.stats.totalCost;
      if (spent >= this.budgetUsd) {
        yield {
          turn: this._turn,
          role: "error",
          content: "",
          error: t("loop.budgetExhausted", {
            spent: spent.toFixed(4),
            cap: this.budgetUsd.toFixed(2),
          }),
        };
        this._steerQueue.length = 0;
        return;
      }
      if (!this._budgetWarned && spent >= this.budgetUsd * 0.8) {
        this._budgetWarned = true;
        yield {
          turn: this._turn,
          role: "warning",
          content: t("loop.budget80Pct", {
            spent: spent.toFixed(4),
            cap: this.budgetUsd.toFixed(2),
          }),
        };
      }
    }
    this._turn++;
    this.scratch.reset();
    // A fresh user turn is a new intent — don't let StormBreaker's
    // old sliding window of (name, args) signatures keep blocking
    // calls that are now legitimately on-task. The window repopulates
    // naturally as this turn's tool calls flow through.
    this.repair.resetStorm();
    this._turnSelfCorrected = false;
    this._foldedThisTurn = false;
    // Fresh controller for this turn: the prior step's signal has
    // already fired (or stayed clean); either way we don't want its
    // state to bleed into the new turn.
    //
    // Edge case — `loop.abort()` may have been called BEFORE step()
    // ran (race: caller fires abort during async setup, but step()
    // hadn't been awaited yet). Naively reassigning _turnAbort would
    // silently drop that abort. Forward the prior aborted state into
    // the fresh controller so the iter-0 check still bails out. This
    // is load-bearing for subagents: the parent's onParentAbort
    // listener calls childLoop.abort(), which can fire before
    // childLoop.step() has reached the `for await` line below.
    const carryAbort = this._turnAbort.signal.aborted;
    this._turnAbort = new AbortController();
    if (carryAbort) this._turnAbort.abort();
    const signal = this._turnAbort.signal;
    // Persist the user message before the first API round-trip so a
    // mid-stream abort or a session switch doesn't drop the prompt and
    // leave a new session orphaned without a .jsonl on disk (issue #943
    // — sidebar globs .jsonl files, so an unpersisted new session vanishes
    // when the user navigates away before the model responds). A failed
    // first round-trip still leaves the message in the log; the user can
    // /retry without re-typing.
    this.appendAndPersist({ role: "user", content: userInput });
    const toolSpecs = this.prefix.tools();
    let rateLimitWarningShown = false;

    for (let iter = 0; ; iter++) {
      if (signal.aborted) {
        // Reset in finally — the consumer (desktop runTurn) breaks the
        // for-await on its own aborter between our yields, which calls
        // generator.return() and skips post-yield straight-line code.
        // Without finally the reset is lost and carryAbort locks every
        // future step() at iter 0.
        try {
          yield {
            turn: this._turn,
            role: "warning",
            content: t("loop.abortedAtIter", { iter }),
          };
          const stoppedMsg =
            "[aborted by user (Esc) — no summary produced. Ask again or /retry when ready; prior tool output is still in the log.]";
          this.appendAndPersist(buildSyntheticAssistantMessage(stoppedMsg, this.model));
          yield {
            turn: this._turn,
            role: "assistant_final",
            content: stoppedMsg,
            forcedSummary: true,
          };
          yield { turn: this._turn, role: "done", content: stoppedMsg };
        } finally {
          this._turnAbort = new AbortController();
        }
        this._steerQueue.length = 0;
        return;
      }
      // Bridge the silence between the PREVIOUS iter's tool result and
      // THIS iter's first streaming byte. R1 can spend 20-90s reasoning
      // about tool output before the first delta lands, and prior to
      // this hint the UI had nothing to render. Only emit on iter > 0
      // because iter 0's "thinking" phase is already covered by the
      // streaming row / StreamingAssistant's placeholder.
      //
      // Wording is explicit about the two things happening: the tool
      // result IS being uploaded (it's now part of the next prompt) and
      // the model IS thinking. Users were reading "thinking about the
      // tool result" as the model-only phase, but the wait also covers
      // the upload round-trip.
      if (iter > 0) {
        yield {
          turn: this._turn,
          role: "status",
          content: t("loop.toolUploadStatus"),
        };
      }
      let messages = this.buildMessages();

      if (this._steerQueue.length > 0) {
        const steer = this._steerQueue.shift()!;
        this._steerConsumed = this._steerQueue.length === 0;
        this.appendAndPersist({
          role: "user",
          content: formatSteerUserMessage(steer),
        });
        messages = this.buildMessages();
        yield {
          turn: this._turn,
          role: "steer",
          content: steer,
        };
      }

      // Preflight context check. Local estimate of the outgoing payload
      // catches cases where prior usage didn't warn us (fresh resume, one
      // huge tool result). Above the emergency threshold we try semantic
      // fold first; mechanical drop is the last resort when fold cannot
      // summarize (empty head, savings too small, summarizer failed).
      {
        const decision = this.context.decidePreflight(messages, this.prefix.toolSpecs, this.model);
        if (decision.needsAction) {
          const { estimateTokens: estimate, ctxMax } = decision;
          yield {
            turn: this._turn,
            role: "status",
            content: t("loop.preflightTruncateStatus"),
          };
          let result = await this.context.fold(this.model, { requireTailBoundary: true });
          if (!result.folded) {
            result = this.context.mechanicalTruncate(this.model, { allowEmpty: false });
          }
          if (result.folded) {
            // Block decideAfterUsage from folding again on the same turn — the
            // log was just compacted; another fold would noop and the warning
            // would be misleading.
            this._foldedThisTurn = true;
            messages = this.buildMessages();
            const after = this.context.decidePreflight(messages, this.prefix.toolSpecs, this.model);
            const stillFull = after.needsAction;
            yield {
              turn: this._turn,
              role: "warning",
              content: t(
                stillFull ? "loop.preflightTruncatedStillFull" : "loop.preflightTruncated",
                {
                  estimate: after.estimateTokens.toLocaleString(),
                  ctxMax: after.ctxMax.toLocaleString(),
                  pct: Math.round((after.estimateTokens / after.ctxMax) * 100),
                  beforeMessages: result.beforeMessages,
                  afterMessages: result.afterMessages,
                },
              ),
            };
          } else {
            yield {
              turn: this._turn,
              role: "warning",
              content: t("loop.preflightNoFold", {
                estimate: estimate.toLocaleString(),
                ctxMax: ctxMax.toLocaleString(),
                pct: Math.round((estimate / ctxMax) * 100),
              }),
            };
          }
        }
      }

      let assistantContent = "";
      let reasoningContent = "";
      let toolCalls: ToolCall[] = [];
      let usage: TurnStats["usage"] | null = null;

      try {
        if (this.stream) {
          const callBuf: Map<number, ToolCall> = new Map();
          // Indices whose accumulated args have parsed as valid JSON at
          // least once. Purely informational — we don't dispatch until
          // the stream ends (that's the eager-dispatch feature we
          // intentionally punted) but the UI shows "N ready" so the
          // user sees progress on long multi-tool turns instead of a
          // stagnant "building tool call" spinner.
          const readyIndices = new Set<number>();
          const callModel = this.model;
          for await (const chunk of this.client.stream({
            model: callModel,
            messages,
            tools: toolSpecs.length ? toolSpecs : undefined,
            signal,
            thinking: thinkingModeForModel(callModel),
            reasoningEffort: this.reasoningEffort,
          })) {
            // DeepSeek transition chunks carry both reasoning_content and
            // content; emit reasoning first so consumers can merge
            // consecutive same-kind segments instead of fragmenting.
            if (chunk.reasoningDelta) {
              reasoningContent += chunk.reasoningDelta;
              yield {
                turn: this._turn,
                role: "assistant_delta",
                content: "",
                reasoningDelta: chunk.reasoningDelta,
              };
            }
            if (chunk.contentDelta) {
              assistantContent += chunk.contentDelta;
              yield {
                turn: this._turn,
                role: "assistant_delta",
                content: chunk.contentDelta,
              };
            }
            if (chunk.toolCallDelta) {
              const d = chunk.toolCallDelta;
              const cur = callBuf.get(d.index) ?? {
                id: d.id,
                type: "function" as const,
                function: { name: "", arguments: "" },
              };
              if (d.id) cur.id = d.id;
              if (d.name) cur.function.name = (cur.function.name ?? "") + d.name;
              if (d.argumentsDelta)
                cur.function.arguments = (cur.function.arguments ?? "") + d.argumentsDelta;
              callBuf.set(d.index, cur);

              // Mark this index "ready" once its args first parse as
              // valid JSON. JSON.parse is sub-millisecond on typical
              // tool-call payloads; skip the check once already ready.
              if (
                !readyIndices.has(d.index) &&
                cur.function.name &&
                looksLikeCompleteJson(cur.function.arguments ?? "")
              ) {
                readyIndices.add(d.index);
              }

              // Skip the id-only opener: name is empty until the next chunk.
              if (cur.function.name) {
                yield {
                  turn: this._turn,
                  role: "tool_call_delta",
                  content: "",
                  toolName: cur.function.name,
                  toolCallArgsChars: (cur.function.arguments ?? "").length,
                  toolCallIndex: d.index,
                  toolCallReadyCount: readyIndices.size,
                };
              }
            }
            if (chunk.usage) usage = chunk.usage;
          }
          toolCalls = [...callBuf.values()];
        } else {
          const callModel = this.model;
          const resp = await this.client.chat({
            model: callModel,
            messages,
            tools: toolSpecs.length ? toolSpecs : undefined,
            signal,
            thinking: thinkingModeForModel(callModel),
            reasoningEffort: this.reasoningEffort,
          });
          assistantContent = resp.content;
          reasoningContent = resp.reasoningContent ?? "";
          toolCalls = resp.toolCalls;
          usage = resp.usage;
        }
      } catch (err) {
        // An aborted signal here is almost always our own doing —
        // either Esc, or App.tsx calling `loop.abort()` to switch to a
        // queued synthetic input (ShellConfirm "always allow", PlanConfirm
        // approve, etc.). The DeepSeek client's fetch path translates
        // the abort into a generic `AbortError("This operation was
        // aborted")`, which used to bubble up here and render as a
        // scary red "error" row even though nothing actually broke.
        // Treat it as a clean early-exit instead: the next turn (queued
        // synthetic OR user re-prompt) starts immediately and gets to
        // produce its own answer.
        if (signal.aborted) {
          // Reset in finally — same rationale as the iter-start handler:
          // if the consumer breaks the for-await before draining `done`,
          // generator.return() would skip a bare post-yield reset and
          // leave carryAbort locked on the next step().
          try {
            yield { turn: this._turn, role: "done", content: "" };
          } finally {
            this._turnAbort = new AbortController();
          }
          this._steerQueue.length = 0;
          return;
        }
        const probe = is5xxError(err) ? await probeDeepSeekReachable(this.client) : undefined;
        yield {
          turn: this._turn,
          role: "error",
          content: "",
          error: formatLoopError(err as Error, probe),
        };
        this._steerQueue.length = 0;
        return;
      }

      // Attribute under the actual model used (escalated → pro, else
      // this.model) so cost/usage logs reflect reality.
      const turnStats = this.stats.record(this._turn, this.model, usage ?? new Usage());

      this.scratch.reasoning = reasoningContent || null;

      const { calls: repairedCalls, report } = this.repair.process(
        toolCalls,
        reasoningContent || null,
        assistantContent || null,
      );

      this.appendAndPersist(
        buildAssistantMessage(assistantContent, repairedCalls, this.model, reasoningContent),
      );

      yield {
        turn: this._turn,
        role: "assistant_final",
        content: assistantContent,
        stats: turnStats,
        repair: report,
      };

      const allSuppressed =
        report.stormsBroken > 0 && repairedCalls.length === 0 && toolCalls.length > 0;

      // First all-suppressed storm: rewrite tail with the original tool_calls
      // (so the next prompt shows what was attempted), stub tool responses to
      // keep the API contract, and continue the iter — model gets one shot to
      // self-correct before the loud-warning path takes over.
      if (allSuppressed && !this._turnSelfCorrected) {
        this._turnSelfCorrected = true;
        this.replaceTailAssistantMessage(
          buildAssistantMessage(assistantContent, toolCalls, this.model, reasoningContent),
        );
        for (const call of toolCalls) {
          this.appendAndPersist({
            role: "tool",
            tool_call_id: call.id ?? "",
            name: call.function?.name ?? "",
            content:
              "[repeat-loop guard] this call was suppressed because it was identical to a previous call in this turn. Earlier results for it are above — try a meaningfully different approach, or stop and answer if you have enough.",
          });
        }
        yield {
          turn: this._turn,
          role: "warning",
          content: t("loop.repeatToolCallWarning"),
        };
        continue;
      }

      if (report.stormsBroken > 0) {
        const noteTail = report.notes.length ? ` — ${report.notes[report.notes.length - 1]}` : "";
        const phrase = allSuppressed
          ? t("loop.stormStuck")
          : t("loop.stormSuppressed", { count: report.stormsBroken });
        yield {
          turn: this._turn,
          role: "warning",
          content: `${phrase}${noteTail}`,
        };
      }

      if (repairedCalls.length === 0) {
        if (this._steerQueue.length > 0) {
          continue;
        }
        if (allSuppressed) {
          yield* forceSummaryAfterIterLimit(this.summaryContext(), { reason: "stuck" });
          this._steerQueue.length = 0;
          return;
        }
        yield { turn: this._turn, role: "done", content: assistantContent };
        this._steerQueue.length = 0;
        return;
      }

      // Context-management decision after each turn's response.
      // ContextManager owns the policy; loop renders the events.
      const decision = this.context.decideAfterUsage(usage, this.model, this._foldedThisTurn);
      if (decision.kind === "fold") {
        this._foldedThisTurn = true;
        const before = decision.promptTokens;
        const ctxMax = decision.ctxMax;
        const aggressiveTag = decision.aggressive ? t("loop.aggressiveTag") : "";
        yield {
          turn: this._turn,
          role: "status",
          content: t("loop.compactingHistoryStatus", { aggressiveTag }),
        };
        const result = await this.compactHistory({ keepRecentTokens: decision.tailBudget });
        if (result.folded) {
          yield {
            turn: this._turn,
            role: "warning",
            content: t(
              decision.aggressive ? "loop.aggressivelyFoldedHistory" : "loop.foldedHistory",
              {
                before: before.toLocaleString(),
                ctxMax: ctxMax.toLocaleString(),
                pct: Math.round((before / ctxMax) * 100),
                beforeMessages: result.beforeMessages,
                afterMessages: result.afterMessages,
                summaryChars: result.summaryChars,
              },
            ),
          };
        }
      } else if (decision.kind === "exit-with-summary") {
        const before = decision.promptTokens;
        const ctxMax = decision.ctxMax;
        yield {
          turn: this._turn,
          role: "warning",
          content: t("loop.forcingSummary", {
            before: before.toLocaleString(),
            ctxMax: ctxMax.toLocaleString(),
            pct: Math.round((before / ctxMax) * 100),
          }),
        };
        this.context.trimTrailingToolCalls();
        yield* forceSummaryAfterIterLimit(this.summaryContext(), { reason: "context-guard" });
        this._steerQueue.length = 0;
        return;
      }

      const dispatchSerial =
        (process.env.REASONIX_TOOL_DISPATCH ?? "auto").toLowerCase() === "serial";
      const parallelMaxParsed = Number.parseInt(process.env.REASONIX_PARALLEL_MAX ?? "", 10);
      const parallelMax =
        Number.isFinite(parallelMaxParsed) && parallelMaxParsed >= 1
          ? Math.min(parallelMaxParsed, 16)
          : 3;

      let callIdx = 0;
      while (callIdx < repairedCalls.length) {
        // Group consecutive parallel-safe calls; an unsafe call breaks
        // the chunk and runs alone (serial barrier).
        const chunk: ToolCall[] = [];
        if (!dispatchSerial) {
          while (
            callIdx < repairedCalls.length &&
            chunk.length < parallelMax &&
            this.tools.isParallelSafe(repairedCalls[callIdx]?.function?.name ?? "")
          ) {
            chunk.push(repairedCalls[callIdx++]!);
          }
        }
        if (chunk.length === 0) {
          chunk.push(repairedCalls[callIdx++]!);
        }

        // tool_start announces every call in the chunk BEFORE any
        // dispatch awaits — TUI shows live indicators for each, and the
        // gap between assistant_final and the first tool_result yield is
        // never silent. Pre-add to the inflight set so the spinner is
        // already correct on the very first card render — runOneToolCall's
        // own add is then idempotent and its finally is the cleanup contract.
        for (const call of chunk) {
          const callId = this.inflightIdFor(call);
          this._inflight.add(callId);
          yield {
            turn: this._turn,
            role: "tool_start",
            content: "",
            toolName: call.function?.name ?? "",
            toolArgs: call.function?.arguments ?? "{}",
            callId,
          };
        }

        // Race the chunk; collect outcomes in declared order so history
        // append + tool yields are deterministic regardless of which
        // call settles first.
        const settled = await Promise.allSettled(chunk.map((c) => this.runOneToolCall(c, signal)));

        for (let k = 0; k < chunk.length; k++) {
          const call = chunk[k]!;
          const name = call.function?.name ?? "";
          const args = call.function?.arguments ?? "{}";
          const s = settled[k]!;

          let result: string;
          let preWarnings: LoopEvent[] = [];
          let postWarnings: LoopEvent[] = [];
          if (s.status === "fulfilled") {
            preWarnings = s.value.preWarnings;
            postWarnings = s.value.postWarnings;
            result = s.value.result;
          } else {
            const err = s.reason instanceof Error ? s.reason : new Error(String(s.reason));
            result = JSON.stringify({ error: `${err.name}: ${err.message}` });
          }

          for (const w of preWarnings) yield w;
          for (const w of postWarnings) yield w;

          // Keep the structured result in history; the warning is only host-side visibility.
          const rateLimited = parseRateLimitedToolResult(result);
          if (rateLimited && !rateLimitWarningShown) {
            rateLimitWarningShown = true;
            yield {
              turn: this._turn,
              role: "warning",
              content: rateLimited.message,
            };
          }

          this.appendAndPersist({
            role: "tool",
            tool_call_id: call.id ?? "",
            name,
            content: result,
          });

          yield {
            turn: this._turn,
            role: "tool",
            content: result,
            toolName: name,
            toolArgs: args,
            callId: this.inflightIdFor(call),
          };
        }
      }
    }
    // Unreachable — the for-loop above is unbounded. The model exits the
    // loop via return statements when it produces no more tool calls,
    // when the context guard fires, when an abort fires, or when a fatal
    // error escapes the inner try blocks.
  }

  private summaryContext(): ForceSummaryContext {
    return {
      client: this.client,
      signal: this._turnAbort.signal,
      buildMessages: () => this.buildMessages(),
      appendAndPersist: (m) => this.appendAndPersist(m),
      recordStats: (model, usage) => this.stats.record(this._turn, model, usage),
      turn: this._turn,
    };
  }

  async run(userInput: string, onEvent?: (ev: LoopEvent) => void): Promise<string> {
    let final = "";
    for await (const ev of this.step(userInput)) {
      onEvent?.(ev);
      if (ev.role === "assistant_final") final = ev.content;
      if (ev.role === "done") break;
    }
    return final;
  }
}

function parsePositiveIntEnv(raw: string | undefined): number | undefined {
  if (!raw) return undefined;
  const n = Number.parseInt(raw, 10);
  return Number.isFinite(n) && n > 0 ? n : undefined;
}
