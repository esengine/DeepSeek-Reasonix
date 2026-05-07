import { type DeepSeekClient, Usage } from "./client.js";
import {
  type BranchOptions,
  type BranchSample,
  aggregateBranchUsage,
  runBranches,
} from "./consistency.js";
import type { PauseGate } from "./core/pause-gate.js";
import { pauseGate as defaultPauseGate } from "./core/pause-gate.js";
import { type HarvestOptions, type TypedPlanState, emptyPlanState, harvest } from "./harvest.js";
import { type HookPayload, type ResolvedHook, runHooks } from "./hooks.js";
import {
  DEFAULT_MAX_RESULT_CHARS,
  DEFAULT_MAX_RESULT_TOKENS,
  truncateForModel,
  truncateForModelByTokens,
} from "./mcp/registry.js";

import { ContextManager } from "./context-manager.js";
import { summarizeBranch } from "./loop/branch.js";
import { formatLoopError } from "./loop/errors.js";
import {
  NEEDS_PRO_BUFFER_CHARS,
  isEscalationRequest,
  looksLikePartialEscalationMarker,
  parseEscalationMarker,
} from "./loop/escalation.js";
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
import { TurnFailureTracker } from "./loop/turn-failure-tracker.js";
import type { BranchSummary, LoopEvent } from "./loop/types.js";
import { AppendOnlyLog, type ImmutablePrefix, VolatileScratch } from "./memory/runtime.js";
import {
  appendSessionMessage,
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
export type { BranchProgress, BranchSummary, EventRole, LoopEvent } from "./loop/types.js";

export interface CacheFirstLoopOptions {
  client: DeepSeekClient;
  prefix: ImmutablePrefix;
  tools?: ToolRegistry;
  model?: string;
  maxToolIters?: number;
  stream?: boolean;
  harvest?: boolean | HarvestOptions;
  /** Branching disables streaming (need all samples) and force-enables harvest (selector input). */
  branch?: number | BranchOptions;
  reasoningEffort?: "high" | "max";
  autoEscalate?: boolean;
  /** Soft USD cap — warns at 80%, refuses next turn at 100%. Opt-in (default no cap). */
  budgetUsd?: number;
  session?: string;
  /** PreToolUse + PostToolUse only — UserPromptSubmit / Stop live at the App boundary. */
  hooks?: ResolvedHook[];
  /** `cwd` reported to hooks; `reasonix code` sets this to the sandbox root, not shell home. */
  hookCwd?: string;
  /** PauseGate bridge — defaults to singleton, injectable for tests. */
  confirmationGate?: PauseGate;
}

export interface ReconfigurableOptions {
  model?: string;
  harvest?: boolean | HarvestOptions;
  branch?: number | BranchOptions;
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
  // these mid-session so users don't have to restart to try harvest or branch.
  model: string;
  stream: boolean;
  harvestEnabled: boolean;
  harvestOptions: HarvestOptions;
  branchEnabled: boolean;
  branchOptions: BranchOptions;
  reasoningEffort: "high" | "max";
  autoEscalate = true;
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

  private _turn = 0;
  private _streamPreference: boolean;
  /** Threaded through HTTP + every tool dispatch so Esc cancels in-flight work, not after. */
  private _turnAbort: AbortController = new AbortController();

  private _proArmedForNextTurn = false;
  private _escalateThisTurn = false;
  private readonly _turnFailures = new TurnFailureTracker();
  private _turnSelfCorrected = false;
  private _foldedThisTurn = false;
  private context!: ContextManager;

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
    // Last-resort backstop — primary stop is the token-context guard inside step().
    this.maxToolIters = opts.maxToolIters ?? 64;
    this.hooks = opts.hooks ?? [];
    this.hookCwd = opts.hookCwd ?? process.cwd();
    this.confirmationGate = opts.confirmationGate ?? defaultPauseGate;

    // Resolve branch config first (since it forces harvest on).
    if (typeof opts.branch === "number") {
      this.branchOptions = { budget: opts.branch };
    } else if (opts.branch && typeof opts.branch === "object") {
      this.branchOptions = opts.branch;
    } else {
      this.branchOptions = {};
    }
    this.branchEnabled = (this.branchOptions.budget ?? 1) > 1;

    // Branching requires harvest for its default selector to work.
    const harvestForced = this.branchEnabled;
    this.harvestEnabled =
      harvestForced ||
      opts.harvest === true ||
      (typeof opts.harvest === "object" && opts.harvest !== null);
    this.harvestOptions =
      typeof opts.harvest === "object" && opts.harvest !== null
        ? opts.harvest
        : (this.branchOptions.harvestOptions ?? {});

    // Streaming is incompatible with branching (need all samples to select).
    this._streamPreference = opts.stream ?? true;
    this.stream = this.branchEnabled ? false : this._streamPreference;

    const allowedNames = new Set([...this.prefix.toolSpecs.map((s) => s.function.name)]);
    // Storm breaker clears its window on mutating calls so read → edit → verify isn't a storm.
    const registry = this.tools;
    const isMutating = (call: ToolCall): boolean => {
      const name = call.function?.name;
      if (!name) return false;
      const def = registry.get(name);
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
        } catch {
          /* ignore — fall through */
        }
      }
      return def.readOnly !== true;
    };
    const isStormExempt = (call: ToolCall): boolean => {
      const name = call.function?.name;
      if (!name) return false;
      return registry.get(name)?.stormExempt === true;
    };
    this.repair = new ToolCallRepair({
      allowedToolNames: allowedNames,
      isMutating,
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

  /** "New chat" — drops messages but keeps session + immutable prefix (cache-first invariant). */
  clearLog(): { dropped: number } {
    const dropped = this.log.length;
    this.log.compactInPlace([]);
    if (this.sessionName) {
      try {
        rewriteSession(this.sessionName, []);
      } catch {
        /* disk issue shouldn't block the in-memory clear */
      }
    }
    this.scratch.reset();
    return { dropped };
  }

  configure(opts: ReconfigurableOptions): void {
    if (opts.model !== undefined) this.model = opts.model;
    if (opts.stream !== undefined) this._streamPreference = opts.stream;
    if (opts.reasoningEffort !== undefined) this.reasoningEffort = opts.reasoningEffort;
    if (opts.autoEscalate !== undefined) this.autoEscalate = opts.autoEscalate;

    if (opts.branch !== undefined) {
      if (typeof opts.branch === "number") {
        this.branchOptions = { budget: opts.branch };
      } else if (opts.branch && typeof opts.branch === "object") {
        this.branchOptions = opts.branch;
      } else {
        this.branchOptions = {};
      }
      this.branchEnabled = (this.branchOptions.budget ?? 1) > 1;
    }

    if (opts.harvest !== undefined) {
      const want =
        opts.harvest === true || (typeof opts.harvest === "object" && opts.harvest !== null);
      this.harvestEnabled = want || this.branchEnabled;
      if (typeof opts.harvest === "object" && opts.harvest !== null) {
        this.harvestOptions = opts.harvest;
      }
    } else if (this.branchEnabled) {
      // branch turned on without explicit harvest → force it on
      this.harvestEnabled = true;
    }

    // Branching always forces non-streaming; otherwise honor preference.
    this.stream = this.branchEnabled ? false : this._streamPreference;
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

  private async runOneToolCall(
    call: ToolCall,
    signal: AbortSignal,
  ): Promise<{ preWarnings: LoopEvent[]; postWarnings: LoopEvent[]; result: string }> {
    const name = call.function?.name ?? "";
    const args = call.function?.arguments ?? "{}";
    const parsedArgs = safeParseToolArgs(args);

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
      const reason = (blocking?.stderr || blocking?.stdout || "blocked by PreToolUse hook").trim();
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
  }

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
          error: `session budget exhausted — spent $${spent.toFixed(4)} ≥ cap $${this.budgetUsd.toFixed(2)}. Bump the cap with /budget <usd>, clear it with /budget off, or end the session.`,
        };
        return;
      }
      if (!this._budgetWarned && spent >= this.budgetUsd * 0.8) {
        this._budgetWarned = true;
        yield {
          turn: this._turn,
          role: "warning",
          content: `▲ budget 80% used — $${spent.toFixed(4)} of $${this.budgetUsd.toFixed(2)}. Next turn or two likely trips the cap.`,
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
        content: "⇧ /pro armed — this turn runs on deepseek-v4-pro (one-shot · disarms after turn)",
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
          content: `aborted at iter ${iter}/${this.maxToolIters} — stopped without producing a summary (press ↑ + Enter or /retry to resume)`,
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
          content: "tool result uploaded · model thinking before next response…",
        };
      }
      if (!warnedForIterBudget && iter >= warnAt) {
        warnedForIterBudget = true;
        yield {
          turn: this._turn,
          role: "warning",
          content: `${iter}/${this.maxToolIters} tool calls used — approaching budget. Press Esc to force a summary now.`,
        };
      }
      let messages = this.buildMessages(pendingUser);

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
            content: "preflight: context near full, attempting fold…",
          };
          const result = await this.context.fold(this.model);
          if (result.folded) {
            yield {
              turn: this._turn,
              role: "warning",
              content: `preflight: request ~${estimate.toLocaleString()}/${ctxMax.toLocaleString()} tokens (${Math.round(
                (estimate / ctxMax) * 100,
              )}%) — folded ${result.beforeMessages} messages → ${result.afterMessages} (summary ${result.summaryChars} chars). Sending.`,
            };
            // Rebuild with the folded log so we send the smaller payload.
            messages = this.buildMessages(pendingUser);
          } else {
            yield {
              turn: this._turn,
              role: "warning",
              content: `preflight: request ~${estimate.toLocaleString()}/${ctxMax.toLocaleString()} tokens (${Math.round(
                (estimate / ctxMax) * 100,
              )}%) and nothing left to fold — DeepSeek will likely 400. Run /clear or /new to start fresh.`,
            };
          }
        }
      }

      let assistantContent = "";
      let reasoningContent = "";
      let toolCalls: ToolCall[] = [];
      let usage: TurnStats["usage"] | null = null;

      let branchSummary: BranchSummary | undefined;
      let preHarvestedPlanState: TypedPlanState | undefined;

      try {
        if (this.branchEnabled) {
          const budget = this.branchOptions.budget ?? 1;
          yield {
            turn: this._turn,
            role: "branch_start",
            content: "",
            branchProgress: {
              completed: 0,
              total: budget,
              latestIndex: -1,
              latestTemperature: -1,
              latestUncertainties: -1,
            },
          };

          // Queue samples as they complete so we can yield progress events
          // in resolution order (not launch order).
          const queue: BranchSample[] = [];
          let waiter: ((s: BranchSample) => void) | null = null;

          const onSampleDone = (sample: BranchSample) => {
            if (waiter) {
              const w = waiter;
              waiter = null;
              w(sample);
            } else {
              queue.push(sample);
            }
          };

          const callModel = this.modelForCurrentCall();
          const branchPromise = runBranches(
            this.client,
            {
              model: callModel,
              messages,
              tools: toolSpecs.length ? toolSpecs : undefined,
              signal,
              thinking: thinkingModeForModel(callModel),
              reasoningEffort: this.reasoningEffort,
            },
            {
              ...this.branchOptions,
              harvestOptions: this.harvestOptions,
              onSampleDone,
            },
          );

          for (let k = 0; k < budget; k++) {
            const sample: BranchSample =
              queue.shift() ??
              (await new Promise<BranchSample>((resolve) => {
                waiter = resolve;
              }));
            yield {
              turn: this._turn,
              role: "branch_progress",
              content: "",
              branchProgress: {
                completed: k + 1,
                total: budget,
                latestIndex: sample.index,
                latestTemperature: sample.temperature,
                latestUncertainties: sample.planState.uncertainties.length,
              },
            };
          }

          const result = await branchPromise;
          assistantContent = result.chosen.response.content;
          reasoningContent = result.chosen.response.reasoningContent ?? "";
          toolCalls = result.chosen.response.toolCalls;

          // Cost accounting: sum usage across ALL samples, not just the winner.
          // (We paid for all three.) Harvest-call tokens are not tracked; they
          // amount to rounding error compared to the main R1 calls.
          const agg = aggregateBranchUsage(result.samples);
          usage = new Usage(
            agg.promptTokens,
            agg.completionTokens,
            agg.totalTokens,
            agg.promptCacheHitTokens,
            agg.promptCacheMissTokens,
          );
          preHarvestedPlanState = result.chosen.planState;
          branchSummary = summarizeBranch(result.chosen, result.samples);
          yield {
            turn: this._turn,
            role: "branch_done",
            content: "",
            branch: branchSummary,
          };
        } else if (this.stream) {
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
            if (chunk.reasoningDelta) {
              reasoningContent += chunk.reasoningDelta;
              yield {
                turn: this._turn,
                role: "assistant_delta",
                content: "",
                reasoningDelta: chunk.reasoningDelta,
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
        yield {
          turn: this._turn,
          role: "error",
          content: "",
          error: formatLoopError(err as Error),
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
          content: `⇧ flash requested escalation — retrying this turn on ${ESCALATION_MODEL}${reasonSuffix}`,
        };
        // Reset per-iter state. We don't record stats for the rejected
        // flash call (cost is small — a ~20-token lead-in that we broke
        // out of early on streaming) — recording would attribute a
        // phantom call to the session total.
        assistantContent = "";
        reasoningContent = "";
        toolCalls = [];
        usage = null;
        branchSummary = undefined;
        preHarvestedPlanState = undefined;
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
      // Harvest is a second API round-trip (cheap model, but still
      // 1-10s) that was previously silent. Bridge the gap with a
      // status indicator so the TUI shows *something* instead of
      // "reasoning finished, now staring at the wall."
      if (
        !preHarvestedPlanState &&
        this.harvestEnabled &&
        (reasoningContent?.trim().length ?? 0) >= 40
      ) {
        yield {
          turn: this._turn,
          role: "status",
          content: "extracting plan state from reasoning…",
        };
      }
      const planState = preHarvestedPlanState
        ? preHarvestedPlanState
        : this.harvestEnabled
          ? await harvest(reasoningContent || null, this.client, this.harvestOptions, signal)
          : emptyPlanState();

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
        planState,
        repair: report,
        branch: branchSummary,
      };

      // Cost-aware escalation: repair fires (scavenge / truncation /
      // storm) are visible "model struggled" signals. Feed them into
      // the turn failure counter — if we hit the threshold, the
      // remainder of this turn's model calls use pro.
      if (this.noteToolFailureSignal("", report)) {
        yield {
          turn: this._turn,
          role: "warning",
          content: `⇧ auto-escalating to ${ESCALATION_MODEL} for the rest of this turn — flash hit ${this._turnFailures.formatBreakdown()}. Next turn falls back to ${this.model} unless /pro is armed.`,
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
          content:
            "Caught a repeated tool call — let the model see the issue and retry with a different approach.",
        };
        continue;
      }

      if (report.stormsBroken > 0) {
        const noteTail = report.notes.length ? ` — ${report.notes[report.notes.length - 1]}` : "";
        const phrase = allSuppressed
          ? "Stopped a stuck retry loop — the model kept calling the same tool with identical args after a self-correction nudge. Try /retry, rephrase, or rule out the underlying blocker."
          : `Suppressed ${report.stormsBroken} repeated tool call(s) — same name + args fired 3+ times.`;
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
        const aggressiveTag = decision.aggressive ? " (aggressive)" : "";
        yield {
          turn: this._turn,
          role: "status",
          content: `compacting history${aggressiveTag}…`,
        };
        const result = await this.compactHistory({ keepRecentTokens: decision.tailBudget });
        if (result.folded) {
          yield {
            turn: this._turn,
            role: "warning",
            content: `context ${before.toLocaleString()}/${ctxMax.toLocaleString()} (${Math.round(
              (before / ctxMax) * 100,
            )}%) — ${decision.aggressive ? "aggressively folded" : "folded"} ${result.beforeMessages} messages → ${result.afterMessages} (summary ${result.summaryChars} chars). Continuing.`,
          };
        }
      } else if (decision.kind === "exit-with-summary") {
        const before = decision.promptTokens;
        const ctxMax = decision.ctxMax;
        yield {
          turn: this._turn,
          role: "warning",
          content: `context ${before.toLocaleString()}/${ctxMax.toLocaleString()} (${Math.round(
            (before / ctxMax) * 100,
          )}%) — forcing summary from what was gathered. Run /compact, /clear, or /new to reset.`,
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
        // never silent.
        for (const call of chunk) {
          yield {
            turn: this._turn,
            role: "tool_start",
            content: "",
            toolName: call.function?.name ?? "",
            toolArgs: call.function?.arguments ?? "{}",
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
              content: `⇧ auto-escalating to ${ESCALATION_MODEL} for the rest of this turn — flash hit ${this._turnFailures.formatBreakdown()}. Next turn falls back to ${this.model} unless /pro is armed.`,
            };
          }

          yield {
            turn: this._turn,
            role: "tool",
            content: result,
            toolName: name,
            toolArgs: args,
          };
        }
      }
    }

    // We exhausted the tool-call budget while the model still wanted to
    // call more tools. Rather than stopping silently (which leaves the
    // user staring at a blank prompt), force one final no-tools call so
    // the model must produce a text summary from everything it has
    // already seen.
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
