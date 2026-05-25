import { type DeepSeekClient, Usage } from "./client.js";
import type { ReasoningEffort } from "./config.js";
import type { PauseGate } from "./core/pause-gate.js";
import { pauseGate as defaultPauseGate } from "./core/pause-gate.js";
import { type HookPayload, type ResolvedHook, runHooks } from "./hooks.js";
import {
  DEFAULT_MAX_RESULT_CHARS,
  DEFAULT_MAX_RESULT_TOKENS,
  truncateForModel,
  truncateForModelByTokens,
} from "./mcp/registry.js";

import { ContextManager, TURN_START_FOLD_THRESHOLD } from "./context-manager.js";
import { InflightSet } from "./core/inflight.js";
import { t } from "./i18n/index.js";
import { dispatchToolCallsChunked } from "./loop/dispatch.js";
import {
  formatLoopError,
  is5xxError,
  isDeepSeekHost,
  probeDeepSeekReachable,
} from "./loop/errors.js";
import { type ForceSummaryContext, forceSummaryAfterIterLimit } from "./loop/force-summary.js";
import {
  fixToolCallPairing,
  healLoadedMessages,
  healLoadedMessagesByTokens,
  stampMissingReasoningForThinkingMode,
} from "./loop/healing.js";
import { hookWarnings, safeParseToolArgs } from "./loop/hook-events.js";
import { buildAssistantMessage, buildSyntheticAssistantMessage } from "./loop/messages.js";
import { stripDroppableReasoningContent } from "./loop/reasoning-retention.js";
import {
  looksLikeCompleteJson,
  shrinkOversizedToolCallArgsByTokens,
  shrinkOversizedToolResults,
  shrinkOversizedToolResultsByTokens,
} from "./loop/shrink.js";
import { streamModelResponse } from "./loop/streaming.js";
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
  patchSessionMeta,
  rewriteSession,
} from "./memory/session.js";
import { type RepairReport, ToolCallRepair } from "./repair/index.js";
import { SessionStats, type TurnStats } from "./telemetry/stats.js";
import { ToolRegistry } from "./tools.js";
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
  reasoningEffort?: ReasoningEffort;
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
  reasoningEffort?: ReasoningEffort;
}

export interface LoopAbortOptions {
  /** Explicit user interrupts can discard the unfinished turn so the next prompt starts clean. */
  discardCurrentTurn?: boolean;
}

function shrinkMessageForRetention(message: ChatMessage): ChatMessage {
  if (message.role !== "assistant" || !Array.isArray(message.tool_calls)) return message;
  return (
    shrinkOversizedToolCallArgsByTokens([message], DEFAULT_MAX_RESULT_TOKENS).messages[0] ?? message
  );
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
  reasoningEffort: ReasoningEffort;
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
  private _discardAbortRequested = false;
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
    this.reasoningEffort = opts.reasoningEffort ?? "high";
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
      // Thinking-mode sessions still need tool-call reasoning_content, while stale
      // plain-turn reasoning can be dropped before it bloats long-session requests.
      const stamped = stampMissingReasoningForThinkingMode(shrunk.messages, this.model);
      const pruned = stripDroppableReasoningContent(stamped.messages);
      const messages = pruned.messages;
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
          totalCompletionTokens: meta.totalCompletionTokens,
          lastPromptTokens: meta.lastPromptTokens,
        });
      }
      if (healedCount > 0 || pruned.prunedCount > 0) {
        // Persist healed log so the same break isn't re-noticed every restart.
        try {
          rewriteSession(this.sessionName, messages);
        } catch {
          /* disk full / perms — skip, in-memory heal still applies */
        }
        if (healedCount > 0) {
          process.stderr.write(
            `▸ session "${this.sessionName}": healed ${healedCount} entr${healedCount === 1 ? "y" : "ies"}${tokensSaved > 0 ? ` (shrunk ${tokensSaved.toLocaleString()} tokens of oversized tool output/arguments)` : " (dropped dangling tool_calls tail)"}. Rewrote session file.\n`,
          );
        }
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
    const retained = shrinkMessageForRetention(message);
    this.log.append(retained);
    if (this.sessionName) {
      try {
        appendSessionMessage(this.sessionName, retained);
      } catch {
        /* disk full or permission denied shouldn't kill the chat */
      }
    }
  }

  /** Swap the just-appended assistant entry — used by self-correction to restore the original tool_calls without dropping reasoning_content. */
  private replaceTailAssistantMessage(message: ChatMessage): void {
    const retained = shrinkMessageForRetention(message);
    const entries = this.log.entries;
    const tail = entries[entries.length - 1];
    if (!tail || tail.role !== "assistant") return;
    const kept = entries.slice(0, -1);
    kept.push(retained);
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
    // Drain leftover steer text — otherwise the first step() after /new
    // injects it as a user message and the next turn leaks prior intent.
    this._steerQueue.length = 0;
    this._steerConsumed = false;
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
    this._steerQueue.length = 0;
    this._steerConsumed = false;
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
        rootDir: this.hookCwd,
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
    const argsShrunk = shrinkOversizedToolCallArgsByTokens(
      healed.messages,
      DEFAULT_MAX_RESULT_TOKENS,
    );
    const pruned = stripDroppableReasoningContent(argsShrunk.messages);
    if (healed.healedCount === 0 && argsShrunk.healedCount === 0 && pruned.prunedCount === 0) {
      return current;
    }
    this.log.compactInPlace(pruned.messages);
    if (this.sessionName) {
      try {
        rewriteSession(this.sessionName, pruned.messages);
      } catch {
        /* disk issue shouldn't block the in-memory heal */
      }
    }
    return pruned.messages;
  }

  abort(opts: LoopAbortOptions = {}): void {
    if (opts.discardCurrentTurn) this._discardAbortRequested = true;
    this._turnAbort.abort();
  }

  private resetAbortState(): void {
    this._turnAbort = new AbortController();
    this._discardAbortRequested = false;
  }

  private discardLogFrom(index: number): void {
    const preserved = this.log.entries.slice(0, index).map((m) => ({ ...m }));
    this.log.compactInPlace(preserved);
    if (this.sessionName) {
      try {
        rewriteSession(this.sessionName, preserved);
      } catch {
        /* disk-full / perms — in-memory compaction still applies */
      }
    }
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
    const turnStartLogIndex = this.log.length;
    this.appendAndPersist({ role: "user", content: userInput });
    const toolSpecs = this.prefix.tools();
    const rateLimitState = { shown: false };

    // Turn-start fold: covers cases the post-response check can't see — terminal
    // prior turn (no tool_calls → no decideAfterUsage), session restore from
    // disk, huge user paste. Fires only above TURN_START_FOLD_THRESHOLD; the
    // post-response 75% trigger handles routine growth.
    {
      const turnStart = this.context.estimateTurnStart(
        this.buildMessages(),
        this.prefix.toolSpecs,
        this.model,
      );
      if (turnStart.ratio > TURN_START_FOLD_THRESHOLD) {
        yield {
          turn: this._turn,
          role: "status",
          content: t("loop.turnStartFoldStatus"),
        };
        const result = await this.context.fold(this.model, {
          requireTailBoundary: true,
        });
        if (result.folded) {
          this._foldedThisTurn = true;
          yield {
            turn: this._turn,
            role: "warning",
            content: t("loop.turnStartFolded", {
              estimate: turnStart.estimateTokens.toLocaleString(),
              ctxMax: turnStart.ctxMax.toLocaleString(),
              pct: Math.round(turnStart.ratio * 100),
              beforeMessages: result.beforeMessages,
              afterMessages: result.afterMessages,
            }),
          };
        }
      }
    }

    for (let iter = 0; ; iter++) {
      if (signal.aborted) {
        // Reset in finally — the consumer (desktop runTurn) breaks the
        // for-await on its own aborter between our yields, which calls
        // generator.return() and skips post-yield straight-line code.
        // Without finally the reset is lost and carryAbort locks every
        // future step() at iter 0.
        try {
          const discardTurn = this._discardAbortRequested;
          const stoppedMsg = discardTurn
            ? "[aborted by user (Esc) — interrupted turn discarded. Ask again when ready.]"
            : "[aborted by user (Esc) — no summary produced. Ask again or /retry when ready; prior tool output is still in the log.]";
          if (discardTurn) {
            this.discardLogFrom(turnStartLogIndex);
          } else {
            this.appendAndPersist(buildSyntheticAssistantMessage(stoppedMsg, this.model));
          }
          yield {
            turn: this._turn,
            role: "assistant_final",
            content: stoppedMsg,
            forcedSummary: true,
          };
          yield { turn: this._turn, role: "done", content: stoppedMsg };
        } finally {
          this.resetAbortState();
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

      let assistantContent = "";
      let reasoningContent = "";
      let toolCalls: ToolCall[] = [];
      let usage: TurnStats["usage"] | null = null;

      try {
        if (this.stream) {
          const result = yield* streamModelResponse({
            client: this.client,
            model: this.model,
            messages,
            toolSpecs,
            signal,
            reasoningEffort: this.reasoningEffort,
            turn: this._turn,
          });
          assistantContent = result.assistantContent;
          reasoningContent = result.reasoningContent;
          toolCalls = result.toolCalls;
          usage = result.usage;
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
          if (this._discardAbortRequested) this.discardLogFrom(turnStartLogIndex);
          try {
            yield { turn: this._turn, role: "done", content: "" };
          } finally {
            this.resetAbortState();
          }
          this._steerQueue.length = 0;
          return;
        }
        const upstreamHost = this.client.baseUrl;
        const dsHost = isDeepSeekHost(upstreamHost);
        const probe =
          is5xxError(err) && dsHost ? await probeDeepSeekReachable(this.client) : undefined;
        yield {
          turn: this._turn,
          role: "error",
          content: "",
          error: formatLoopError(err as Error, probe, { upstreamHost }),
        };
        this._steerQueue.length = 0;
        return;
      }

      // Attribute under the actual model used (escalated → pro, else
      // this.model) so cost/usage logs reflect reality.
      const turnStats = this.stats.record(this._turn, this.model, usage ?? new Usage());

      // Carry cumulative stats across app restarts.
      if (this.sessionName) {
        try {
          const last =
            this.stats.turns.length > 0 ? this.stats.turns[this.stats.turns.length - 1] : null;
          patchSessionMeta(this.sessionName, {
            totalCostUsd: this.stats.totalCost,
            cacheHitTokens: this.stats.cumulativeCacheHitTokens,
            cacheMissTokens: this.stats.cumulativeCacheMissTokens,
            totalCompletionTokens: this.stats.cumulativeCompletionTokens,
            lastPromptTokens: last?.usage.promptTokens,
          });
        } catch {
          // Best-effort; don't crash the turn loop on a write failure.
        }
      }

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
          severity: "low",
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
          severity: allSuppressed ? "high" : "low",
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

      yield* dispatchToolCallsChunked(repairedCalls, {
        turn: this._turn,
        signal,
        isParallelSafe: (name) => this.tools.isParallelSafe(name),
        inflightIdFor: (call) => this.inflightIdFor(call),
        inflightAdd: (id) => this._inflight.add(id),
        runOne: (call, sig) => this.runOneToolCall(call, sig),
        appendAndPersist: (m) => this.appendAndPersist(m),
        rateLimitState,
      });
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
