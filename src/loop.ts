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
import {
  NEEDS_PRO_BUFFER_CHARS,
  isEscalationRequest,
  looksLikePartialEscalationMarker,
  parseEscalationMarker,
} from "./loop/escalation.js";
import {
  type ForceSummaryContext,
  forceSummaryAfterIterLimit,
  summarizePartialProgress,
} from "./loop/force-summary.js";
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
import { FAILURE_ESCALATION_THRESHOLD, TurnFailureTracker } from "./loop/turn-failure-tracker.js";
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
import { countTokens } from "./tokenizer.js";
import { ToolRegistry } from "./tools.js";
import type { ChatMessage, ToolCall } from "./types.js";

const ESCALATION_MODEL = "deepseek-v4-pro";
/** Iters-from-cap at which the parent loop starts injecting a remaining-budget tail into tool results. Subagent uses 3 against a 16-cap; parent's default 64-cap means this fires only at iter ≥ 60. */
const PARENT_BUDGET_WARN_THRESHOLD = 5;

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
  maxToolIters?: number;
  stream?: boolean;
  reasoningEffort?: "high" | "max";
  autoEscalate?: boolean;
  /** Soft USD cap — warns at 80%, refuses next turn at 100%. Opt-in (default no cap). */
  budgetUsd?: number;
  /** Per-turn repair/error signal count required to escalate flash→pro. Defaults to FAILURE_ESCALATION_THRESHOLD. Out-of-range values warn + fall back. */
  failureThreshold?: number;
  session?: string;
  /** PreToolUse + PostToolUse only — UserPromptSubmit / Stop live at the App boundary. */
  hooks?: ResolvedHook[];
  /** `cwd` reported to hooks; `reasonix code` sets this to the sandbox root, not shell home. */
  hookCwd?: string;
  /** PauseGate bridge — defaults to singleton, injectable for tests. */
  confirmationGate?: PauseGate;
  /** Re-runs the prompt builder (applyMemoryStack / codeSystemPrompt) on /new so REASONIX.md edits take effect without a restart. Accepting a cache miss is the price. */
  rebuildSystem?: () => string;
  /** What to do when the per-step iter budget is exhausted. "summarize" (default) fires a no-tools call so the user sees an answer — right for top-level chat. "pause" yields a `paused` event leaving the session intact — right for subagents whose parent can decide to resume or accept partial state. */
  onIterBudgetExhausted?: "summarize" | "pause";
}

export interface ReconfigurableOptions {
  model?: string;
  stream?: boolean;
  /** V4 thinking mode only; deepseek-chat ignores. */
  reasoningEffort?: "high" | "max";
  /** `false` pins to `model` — kills both NEEDS_PRO marker scavenge and failure-count threshold. */
  autoEscalate?: boolean;
}

export class CacheFirstLoop {
  readonly client: DeepSeekClient;
  readonly prefix: ImmutablePrefix;
  readonly tools: ToolRegistry;
  readonly maxToolIters: number;
  readonly log = new AppendOnlyLog();
  readonly scratch = new VolatileScratch();
  readonly stats = new SessionStats();
  readonly repair: ToolCallRepair;

  // Mutable via configure() — slash commands in the TUI / library callers tweak
  // these mid-session so users don't have to restart.
  model: string;
  stream: boolean;
  reasoningEffort: "high" | "max";
  autoEscalate = true;
  budgetUsd: number | null;
  /** One-shot 80% warning latch — cleared by setBudget so a bump re-arms at the new boundary. */
  private _budgetWarned = false;
  sessionName: string | null;
  readonly onIterBudgetExhausted: "summarize" | "pause";

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

  /** Typeahead steer message set by the UI; step() consumes it at the next iter boundary. */
  private _steer: string | null = null;

  /** Set true when a steer was consumed this turn; cleared on next step() entry. */
  private _steerConsumed = false;

  /** UI calls this to inject a mid-turn steer message without aborting the current turn.
   *  New text resets steerConsumed — a fresh steer hasn't been consumed yet. */
  steer(text: string | null): void {
    this._steer = text;
    if (text !== null) this._steerConsumed = false;
  }

  /** True when a steer was consumed this turn (UI gate to avoid double-submit). */
  get steerConsumed(): boolean {
    return this._steerConsumed;
  }

  private _proArmedForNextTurn = false;
  private _escalateThisTurn = false;
  private readonly _turnFailures: TurnFailureTracker;
  private _turnSelfCorrected = false;
  private _foldedThisTurn = false;
  private _toolDispatchesThisStep = 0;
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
    if (opts.autoEscalate !== undefined) this.autoEscalate = opts.autoEscalate;
    this.budgetUsd =
      typeof opts.budgetUsd === "number" && opts.budgetUsd > 0 ? opts.budgetUsd : null;
    this._turnFailures = new TurnFailureTracker(
      resolveFailureThreshold(opts.failureThreshold, FAILURE_ESCALATION_THRESHOLD),
    );

    // Last-resort backstop — primary stop is the token-context guard inside step().
    this.maxToolIters = opts.maxToolIters ?? 64;
    this.onIterBudgetExhausted = opts.onIterBudgetExhausted ?? "summarize";
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

    // Inject a remaining-iter hint into tool results when closing in on the per-turn cap. Subagent's child registry pre-installs its own augmenter before constructing the child loop — preserve it instead of clobbering.
    if (!this.tools.hasResultAugmenter) {
      this.tools.setResultAugmenter((_name, _args, result) => {
        this._toolDispatchesThisStep++;
        const remaining = this.maxToolIters - this._toolDispatchesThisStep;
        if (remaining <= 0) {
          return `${result}\n\n[budget: 0 of ${this.maxToolIters} tool calls left this turn — finalize NOW; the next iter forces a summary]`;
        }
        if (remaining <= PARENT_BUDGET_WARN_THRESHOLD) {
          return `${result}\n\n[budget: ${remaining} of ${this.maxToolIters} tool calls left this turn — wrap up soon]`;
        }
        return result;
      });
    }

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
    this._turnFailures.reset();
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
    if (opts.autoEscalate !== undefined) this.autoEscalate = opts.autoEscalate;
  }

  /** `null` disables the cap; any change re-arms the 80% warning. */
  setBudget(usd: number | null): void {
    this.budgetUsd = typeof usd === "number" && usd > 0 ? usd : null;
    this._budgetWarned = false;
  }

  /** Single-turn upgrade consumed at next step() — distinct from `/preset max` (persistent). */
  armProForNextTurn(): void {
    this._proArmedForNextTurn = true;
  }
  /** Cancel `/pro` arming before the next turn starts. */
  disarmPro(): void {
    this._proArmedForNextTurn = false;
  }
  /** UI surface — true while `/pro` is queued but hasn't fired yet. */
  get proArmed(): boolean {
    return this._proArmedForNextTurn;
  }
  /** UI surface — true while the current turn is running on pro (armed or auto-escalated). */
  get escalatedThisTurn(): boolean {
    return this._escalateThisTurn;
  }

  /** UI surface — model id of the call about to run (or running) right now, including escalation. */
  get currentCallModel(): string {
    return this.modelForCurrentCall();
  }

  private modelForCurrentCall(): string {
    return this._escalateThisTurn ? ESCALATION_MODEL : this.model;
  }

  /** Returns true ONLY on the tipping call — caller surfaces a one-shot warning. */
  private noteToolFailureSignal(resultJson: string, repair?: RepairReport): boolean {
    if (!this._turnFailures.noteAndCrossedThreshold(resultJson, repair)) return false;
    if (this._escalateThisTurn || !this.autoEscalate) return false;
    this._escalateThisTurn = true;
    return true;
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

  private buildMessages(pendingUser: string | null): ChatMessage[] {
    // DeepSeek 400s on either unpaired tool_calls or stray tool entries — heal before sending.
    const healed = healLoadedMessages(this.log.toMessages(), DEFAULT_MAX_RESULT_CHARS);
    const msgs: ChatMessage[] = [...this.prefix.toMessages(), ...healed.messages];
    if (pendingUser !== null) msgs.push({ role: "user", content: pendingUser });
    return msgs;
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
    // Per-turn escalation state: reset both flags at turn start, then
    // consume the /pro armed flag into `_escalateThisTurn` (so the
    // armed intent is one-shot — next turn starts fresh on flash
    // unless the user re-arms or mid-turn escalation triggers).
    this._turnFailures.reset();
    this._turnSelfCorrected = false;
    this._escalateThisTurn = false;
    this._foldedThisTurn = false;
    this._toolDispatchesThisStep = 0;
    let armedConsumed = false;
    if (this._proArmedForNextTurn) {
      this._escalateThisTurn = true;
      this._proArmedForNextTurn = false;
      armedConsumed = true;
    }
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
    if (armedConsumed) {
      yield {
        turn: this._turn,
        role: "warning",
        content: t("loop.proArmed"),
      };
    }
    let pendingUser: string | null = userInput;
    const toolSpecs = this.prefix.tools();
    // 70% of the iter budget is the "you're getting close" threshold. We
    // only warn once per step so the user sees a single signal, not a
    // string of identical yellow lines stacked up.
    const warnAt = Math.max(1, Math.floor(this.maxToolIters * 0.7));
    let warnedForIterBudget = false;

    for (let iter = 0; iter < this.maxToolIters; iter++) {
      if (signal.aborted) {
        // Esc means "stop now" — not "stop and force another 30-90s
        // reasoner call to produce a summary I didn't ask for". The
        // user's mental model of cancel is immediate. We emit a
        // synthetic assistant_final (tagged forcedSummary so the
        // code-mode applier ignores it) with a short stopped
        // message, then done. The prior tool outputs are still in
        // the log if the user wants to continue — asking again
        // will hit a warm cache and be cheap.
        //
        // Budget / context-guard still call forceSummaryAfterIterLimit
        // because there the USER didn't choose to stop — we did —
        // and leaving them staring at nothing is worse than one extra
        // call.
        yield {
          turn: this._turn,
          role: "warning",
          content: t("loop.abortedAtIter", { iter, cap: this.maxToolIters }),
        };
        const stoppedMsg =
          "[aborted by user (Esc) — no summary produced. Ask again or /retry when ready; prior tool output is still in the log.]";
        // Synthetic assistant turn — no real model output exists. For
        // reasoner sessions R1 still demands `reasoning_content` on
        // every assistant message, so we attach an empty-string
        // placeholder to satisfy the validator without inventing
        // reasoning we don't have. V3 gets a plain message as before.
        this.appendAndPersist(buildSyntheticAssistantMessage(stoppedMsg, this.model));
        yield {
          turn: this._turn,
          role: "assistant_final",
          content: stoppedMsg,
          forcedSummary: true,
        };
        yield { turn: this._turn, role: "done", content: stoppedMsg };
        // Reset to a fresh, non-aborted controller before returning.
        // Without this the carry-abort logic above sees the still-
        // aborted controller on the NEXT step() entry and immediately
        // re-aborts at iter 0, locking the session: every subsequent
        // user message produces "stopped without producing a summary"
        // before any work happens. A user-initiated Esc is a discrete
        // event tied to ONE turn; it must not bleed into the next.
        // (The race scenario the carry-abort handles — abort fired in
        // the async window before step() entry — still works: a fresh
        // abort() between turns aborts the new controller below.)
        this._turnAbort = new AbortController();
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
      if (!warnedForIterBudget && iter >= warnAt) {
        warnedForIterBudget = true;
        yield {
          turn: this._turn,
          role: "warning",
          content: t("loop.toolBudgetWarning", { iter, cap: this.maxToolIters }),
        };
      }
      let messages = this.buildMessages(pendingUser);

      // Consume a typeahead steer if the UI wrote one between iters.
      // Injecting as a user message via appendAndPersist means the
      // next buildMessages() (or the fold rebuild below) will include it.
      if (this._steer !== null) {
        const steer = this._steer;
        this._steer = null;
        this._steerConsumed = true;
        this.appendAndPersist({ role: "user", content: steer });
        messages = this.buildMessages(pendingUser);
        // Treat the steer as a fresh user utterance — reset pendingUser
        // since it's already in the log now.
        pendingUser = null;
        yield {
          turn: this._turn,
          role: "steer",
          content: steer,
        };
      }

      // Preflight context check. Local estimate of the outgoing payload
      // catches cases where prior usage didn't warn us (fresh resume, one
      // huge tool result). Above 95% we attempt a fold as a last resort —
      // it costs one summary call but stays cache-friendly. If the fold
      // can't shrink anything, we surface a warning and let the request
      // go (and likely 400) so the user knows to /clear.
      {
        const decision = this.context.decidePreflight(messages, this.prefix.toolSpecs, this.model);
        if (decision.needsAction) {
          const { estimateTokens: estimate, ctxMax } = decision;
          yield {
            turn: this._turn,
            role: "status",
            content: t("loop.preflightFoldStatus"),
          };
          const result = await this.context.fold(this.model);
          if (result.folded) {
            yield {
              turn: this._turn,
              role: "warning",
              content: t("loop.preflightFolded", {
                estimate: estimate.toLocaleString(),
                ctxMax: ctxMax.toLocaleString(),
                pct: Math.round((estimate / ctxMax) * 100),
                beforeMessages: result.beforeMessages,
                afterMessages: result.afterMessages,
                summaryChars: result.summaryChars,
              }),
            };
            // Rebuild with the folded log so we send the smaller payload.
            messages = this.buildMessages(pendingUser);
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
          const callModel = this.modelForCurrentCall();
          // Escalation-marker buffer: delay the first few assistant_delta
          // yields so a "<<<NEEDS_PRO>>>" lead-in never flashes on-screen
          // before we abort + retry. Only active on flash AND when the
          // user hasn't disabled auto-escalation (the `flash` preset
          // turns this off — model output flows through verbatim, no
          // marker handling). pro never requests its own escalation.
          const bufferForEscalation = this.autoEscalate && callModel !== ESCALATION_MODEL;
          let escalationBuf = "";
          let escalationBufFlushed = false;
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
              if (bufferForEscalation && !escalationBufFlushed) {
                escalationBuf += chunk.contentDelta;
                // Early exit: marker matches — break and let the
                // post-call retry path take over. No delta was yielded
                // so the user sees nothing flicker.
                if (isEscalationRequest(escalationBuf)) {
                  break;
                }
                // Flush once we have enough content to rule out the
                // marker (clearly not a partial match anymore, or past
                // the look-ahead window).
                if (
                  escalationBuf.length >= NEEDS_PRO_BUFFER_CHARS ||
                  !looksLikePartialEscalationMarker(escalationBuf)
                ) {
                  escalationBufFlushed = true;
                  yield {
                    turn: this._turn,
                    role: "assistant_delta",
                    content: escalationBuf,
                  };
                  escalationBuf = "";
                }
              } else {
                yield {
                  turn: this._turn,
                  role: "assistant_delta",
                  content: chunk.contentDelta,
                };
              }
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
          // Stream ended before the escalation buffer got flushed —
          // either a short response or a partial marker match. If the
          // buffer ISN'T the marker, flush it as the final delta so
          // the user sees it. Marker-match is handled post-call.
          if (bufferForEscalation && !escalationBufFlushed && escalationBuf.length > 0) {
            if (!isEscalationRequest(escalationBuf)) {
              yield {
                turn: this._turn,
                role: "assistant_delta",
                content: escalationBuf,
              };
            }
          }
        } else {
          const callModel = this.modelForCurrentCall();
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
          yield { turn: this._turn, role: "done", content: "" };
          // Reset the controller so the carry-abort check at the top of
          // the NEXT step() doesn't inherit this turn's aborted state.
          // Without this, a queued-submit triggered by App.tsx (e.g.
          // ShellConfirm "run once" → loop.abort() + setQueuedSubmit)
          // produces a spurious "aborted at iter 0/64" the moment the
          // synthetic message starts processing, locking the session.
          this._turnAbort = new AbortController();
          return;
        }
        const probe = is5xxError(err) ? await probeDeepSeekReachable(this.client) : undefined;
        yield {
          turn: this._turn,
          role: "error",
          content: "",
          error: formatLoopError(err as Error, probe),
        };
        return;
      }

      // Self-reported escalation: the model (flash) emitted the
      // NEEDS_PRO marker as its lead-in. Abort this call's accounting,
      // flip the turn to pro, and re-enter the iter without advancing
      // the counter — next attempt runs on v4-pro with the same
      // messages. Only triggers when the call was on a model OTHER
      // than the escalation model; if the user already configured
      // v4-pro (via /preset max etc.), the marker is taken as a
      // no-op content and passed through verbatim, so there's no
      // infinite-retry loop.
      if (
        this.autoEscalate &&
        this.modelForCurrentCall() !== ESCALATION_MODEL &&
        isEscalationRequest(assistantContent)
      ) {
        const { reason } = parseEscalationMarker(assistantContent);
        this._escalateThisTurn = true;
        const reasonSuffix = reason ? ` — ${reason}` : "";
        yield {
          turn: this._turn,
          role: "warning",
          content: t("loop.flashEscalation", { model: ESCALATION_MODEL, reasonSuffix }),
        };
        // Reset per-iter state. We don't record stats for the rejected
        // flash call (cost is small — a ~20-token lead-in that we broke
        // out of early on streaming) — recording would attribute a
        // phantom call to the session total.
        assistantContent = "";
        reasoningContent = "";
        toolCalls = [];
        usage = null;
        // Redo this iter on pro — `iter--` cancels the `iter++` the
        // for loop runs on `continue`.
        iter--;
        continue;
      }

      // Attribute under the actual model used (escalated → pro, else
      // this.model) so cost/usage logs reflect reality.
      const turnStats = this.stats.record(
        this._turn,
        this.modelForCurrentCall(),
        usage ?? new Usage(),
      );

      // Commit the user turn to the log only on success of the first round-trip.
      if (pendingUser !== null) {
        this.appendAndPersist({ role: "user", content: pendingUser });
        pendingUser = null;
      }

      this.scratch.reasoning = reasoningContent || null;

      const { calls: repairedCalls, report } = this.repair.process(
        toolCalls,
        reasoningContent || null,
        assistantContent || null,
      );

      this.appendAndPersist(
        buildAssistantMessage(
          assistantContent,
          repairedCalls,
          this.modelForCurrentCall(),
          reasoningContent,
        ),
      );

      yield {
        turn: this._turn,
        role: "assistant_final",
        content: assistantContent,
        stats: turnStats,
        repair: report,
      };

      // Cost-aware escalation: repair fires (scavenge / truncation /
      // storm) are visible "model struggled" signals. Feed them into
      // the turn failure counter — if we hit the threshold, the
      // remainder of this turn's model calls use pro.
      if (this.noteToolFailureSignal("", report)) {
        yield {
          turn: this._turn,
          role: "warning",
          content: t("loop.autoEscalation", {
            model: ESCALATION_MODEL,
            breakdown: this._turnFailures.formatBreakdown(),
            fallback: this.model,
          }),
        };
      }

      const allSuppressed =
        report.stormsBroken > 0 && repairedCalls.length === 0 && toolCalls.length > 0;

      // First all-suppressed storm: rewrite tail with the original tool_calls
      // (so the next prompt shows what was attempted), stub tool responses to
      // keep the API contract, and continue the iter — model gets one shot to
      // self-correct before the loud-warning path takes over.
      if (allSuppressed && !this._turnSelfCorrected) {
        this._turnSelfCorrected = true;
        this.replaceTailAssistantMessage(
          buildAssistantMessage(
            assistantContent,
            toolCalls,
            this.modelForCurrentCall(),
            reasoningContent,
          ),
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
        if (allSuppressed) {
          yield* forceSummaryAfterIterLimit(this.summaryContext(), { reason: "stuck" });
          return;
        }
        yield { turn: this._turn, role: "done", content: assistantContent };
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

          this.appendAndPersist({
            role: "tool",
            tool_call_id: call.id ?? "",
            name,
            content: result,
          });

          if (this.noteToolFailureSignal(result)) {
            yield {
              turn: this._turn,
              role: "warning",
              content: t("loop.autoEscalation", {
                model: ESCALATION_MODEL,
                breakdown: this._turnFailures.formatBreakdown(),
                fallback: this.model,
              }),
            };
          }
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

    // Iter budget exhausted while the model still wanted more tools.
    // Top-level chat → force a no-tools summary call so the user sees
    // SOMETHING. Subagents → emit `paused`, leaving session messages
    // intact so the parent can resume by passing the session name back.
    if (this.onIterBudgetExhausted === "pause") {
      const partial = await summarizePartialProgress(this.summaryContext());
      yield {
        turn: this._turn,
        role: "paused",
        content: "",
        sessionName: this.sessionName ?? undefined,
        pausedAtIter: this.maxToolIters,
        partialSummary: partial?.summary,
      };
      yield { turn: this._turn, role: "done", content: "" };
      return;
    }
    yield* forceSummaryAfterIterLimit(this.summaryContext(), { reason: "budget" });
  }

  private summaryContext(): ForceSummaryContext {
    return {
      client: this.client,
      signal: this._turnAbort.signal,
      buildMessages: () => this.buildMessages(null),
      appendAndPersist: (m) => this.appendAndPersist(m),
      recordStats: (model, usage) => this.stats.record(this._turn, model, usage),
      turn: this._turn,
      maxToolIters: this.maxToolIters,
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

/** Sane-range bounds for the flash→pro escalation threshold. */
const FAILURE_THRESHOLD_MIN = 1;
const FAILURE_THRESHOLD_MAX = 20;

function resolveFailureThreshold(raw: number | undefined, fallback: number): number {
  if (raw === undefined) return fallback;
  if (!Number.isInteger(raw) || raw < FAILURE_THRESHOLD_MIN || raw > FAILURE_THRESHOLD_MAX) {
    process.stderr.write(
      `▲ ignoring escalation failureThreshold=${raw} (must be an integer in [${FAILURE_THRESHOLD_MIN},${FAILURE_THRESHOLD_MAX}]) — using default ${fallback}\n`,
    );
    return fallback;
  }
  return raw;
}
