import { render } from "ink";
import React, { useState } from "react";
import {
  loadApiKey,
  mcpEnvFor,
  readConfig,
  searchEnabled,
  webSearchEndpoint,
  webSearchEngine,
} from "../../config.js";
import { loadDotenv } from "../../env.js";
import { t } from "../../i18n/index.js";
import type { CacheFirstLoop } from "../../loop.js";
import { McpClient } from "../../mcp/client.js";
import { type InspectionReport, inspectMcpServer } from "../../mcp/inspect.js";
import { preflightStdioSpec } from "../../mcp/preflight.js";
import { type McpClientHost, bridgeMcpTools } from "../../mcp/registry.js";
import { parseMcpSpec } from "../../mcp/spec.js";
import { buildMcpServerSummary } from "../../mcp/summary.js";
import { buildTransportFromSpec } from "../../mcp/transport-from-spec.js";
import {
  deleteSession,
  freshSessionName,
  listSessionsForWorkspace,
  renameSession,
  resolveSession,
} from "../../memory/session.js";
import { ToolRegistry } from "../../tools.js";
import { registerChoiceTool } from "../../tools/choice.js";
import { registerMemoryTools } from "../../tools/memory.js";
import { registerWebTools } from "../../tools/web.js";
import { markPhase } from "../startup-profile.js";
import { App } from "../ui/App.js";
import { SessionPicker } from "../ui/SessionPicker.js";
import { Setup } from "../ui/Setup.js";
import { drainTtyResponses } from "../ui/drain-tty.js";
import { KeystrokeProvider } from "../ui/keystroke-context.js";
import { formatMcpLifecycleEvent } from "../ui/mcp-lifecycle.js";
import { formatMcpSlowToast } from "../ui/mcp-toast.js";
import type { McpServerSummary } from "../ui/slash.js";

export interface ProgressInfo {
  toolName: string;
  progress: number;
  total?: number;
  message?: string;
}

interface SpecRecord {
  spec: string;
  client: McpClient;
  summary: McpServerSummary;
  /** Names of bridged tools — used for hot-unbridge. */
  registeredNames: string[];
  /** ToolSpec snapshots captured AFTER bridge — handed to loop.prefix.addTool on hot-add. */
  registeredSpecs: import("../../types.js").ToolSpec[];
}

interface RuntimeContext {
  getTools: () => ToolRegistry | undefined;
  getMcpPrefix: () => string | undefined;
  getRequestedCount: () => number;
  progressSink: { current: ((info: ProgressInfo) => void) | null };
}

export type McpLifecycleNotice =
  | { kind: "handshake"; name: string }
  | {
      kind: "connected";
      name: string;
      tools: number;
      resources: number;
      prompts: number;
      ms: number;
    }
  | { kind: "disabled"; name: string }
  | { kind: "failed"; name: string; reason: string }
  | { kind: "slow"; serverName: string; p95Ms: number; sampleSize: number };

export type McpLifecycleSink = (notice: McpLifecycleNotice) => void;

const stderrLifecycleSink: McpLifecycleSink = (n) => {
  if (n.kind === "slow") {
    process.stderr.write(
      `${formatMcpSlowToast({ name: n.serverName, p95Ms: n.p95Ms, sampleSize: n.sampleSize })}\n`,
    );
    return;
  }
  if (n.kind === "failed") {
    process.stderr.write(
      `${formatMcpLifecycleEvent({ state: "failed", name: n.name, reason: n.reason })}\n  → run \`reasonix setup\` to remove this entry, or fix the underlying issue (missing npm package, network, etc.).\n`,
    );
    return;
  }
  if (n.kind === "connected") {
    process.stderr.write(
      `${formatMcpLifecycleEvent({
        state: "connected",
        name: n.name,
        tools: n.tools,
        resources: n.resources,
        prompts: n.prompts,
        ms: n.ms,
      })}\n`,
    );
    return;
  }
  process.stderr.write(`${formatMcpLifecycleEvent({ state: n.kind, name: n.name })}\n`);
};

export interface McpRuntime {
  size(): number;
  specs(): string[];
  summaries(): McpServerSummary[];
  addSpec(
    raw: string,
    loop?: CacheFirstLoop,
  ): Promise<{ ok: true; summary: McpServerSummary } | { ok: false; reason: string }>;
  removeSpec(raw: string, loop?: CacheFirstLoop): Promise<boolean>;
  reloadFromConfig(loop?: CacheFirstLoop): Promise<{
    added: string[];
    removed: string[];
    failed: Array<{ spec: string; reason: string }>;
    summaries: McpServerSummary[];
  }>;
  closeAll(): Promise<void>;
  /** Replace the sink that lifecycle events flow through — App.tsx swaps this in on mount so toasts land in the alt-screen UI instead of corrupting it via stderr. */
  setLifecycleSink(sink: McpLifecycleSink): void;
}

function createMcpRuntime(ctx: RuntimeContext): McpRuntime {
  const records = new Map<string, SpecRecord>();
  const insertionOrder: string[] = [];
  let sink: McpLifecycleSink = stderrLifecycleSink;

  async function addSpec(
    raw: string,
    loop?: CacheFirstLoop,
  ): Promise<{ ok: true; summary: McpServerSummary } | { ok: false; reason: string }> {
    if (records.has(raw)) {
      return { ok: true, summary: records.get(raw)!.summary };
    }
    const tools = ctx.getTools();
    if (!tools) return { ok: false, reason: "no tool registry available" };
    const disabledNames = new Set(readConfig().mcpDisabled ?? []);
    let label = "anon";
    let mcp: McpClient | undefined;
    // Per-server readiness gate — tool dispatches via the bridge await
    // this before calling into `live.callTool`. Resolved on `connected`,
    // rejected on `failed`, so a tool invoked mid-handshake waits
    // (capped by `bridgeMcpTools`'s `readyTimeoutMs`) instead of
    // surfacing a transport error.
    let resolveReady!: () => void;
    let rejectReady!: (err: Error) => void;
    const ready = new Promise<void>((resolve, reject) => {
      resolveReady = resolve;
      rejectReady = reject;
    });
    // Avoid unhandledRejection if no consumer awaits `ready` yet.
    ready.catch(() => undefined);
    try {
      const spec = parseMcpSpec(raw);
      label = spec.name ?? "anon";
      if (spec.name && disabledNames.has(spec.name)) {
        sink({ kind: "disabled", name: label });
        rejectReady(new Error(`MCP server "${label}" is disabled`));
        return { ok: false, reason: "disabled by user" };
      }
      sink({ kind: "handshake", name: label });
      const t0 = Date.now();
      const namePrefix = spec.name
        ? `${spec.name}_`
        : ctx.getRequestedCount() === 1 && ctx.getMcpPrefix()
          ? (ctx.getMcpPrefix() as string)
          : "";
      if (spec.transport === "stdio") preflightStdioSpec(spec);
      const transport = buildTransportFromSpec(spec, { env: mcpEnvFor(spec.name, readConfig()) });
      mcp = new McpClient({ transport });
      await mcp.initialize();
      const host: McpClientHost = { client: mcp };
      const bridge = await bridgeMcpTools(mcp, {
        registry: tools,
        namePrefix,
        serverName: label,
        host,
        ready,
        onProgress: (info) => ctx.progressSink.current?.(info),
        onSlow: (info) =>
          sink({
            kind: "slow",
            serverName: info.serverName,
            p95Ms: info.p95Ms,
            sampleSize: info.sampleSize,
          }),
      });
      let report: InspectionReport;
      try {
        report = await inspectMcpServer(mcp);
      } catch {
        report = {
          protocolVersion: mcp.protocolVersion,
          serverInfo: mcp.serverInfo,
          capabilities: mcp.serverCapabilities ?? {},
          tools: { supported: true, items: [] },
          resources: { supported: false, reason: "inspect failed" },
          prompts: { supported: false, reason: "inspect failed" },
          elapsedMs: 0,
        };
      }
      const ms = Date.now() - t0;
      const resourceCount = report.resources.supported ? report.resources.items.length : 0;
      const promptCount = report.prompts.supported ? report.prompts.items.length : 0;
      sink({
        kind: "connected",
        name: label,
        tools: bridge.registeredNames.length,
        resources: resourceCount,
        prompts: promptCount,
        ms,
      });
      resolveReady();
      const summary = buildMcpServerSummary({
        label,
        spec: raw,
        toolCount: bridge.registeredNames.length,
        report,
        host,
        bridgeEnv: bridge.env,
      });
      // Snapshot tool specs AFTER bridge so hot-add can replay them into loop.prefix.
      const allSpecs = tools.specs();
      const registeredSpecs = allSpecs.filter((s) =>
        bridge.registeredNames.includes(s.function.name),
      );
      records.set(raw, {
        spec: raw,
        client: mcp,
        summary,
        registeredNames: bridge.registeredNames,
        registeredSpecs,
      });
      insertionOrder.push(raw);
      // Hot-add: shift the prefix so the live loop sees the new tools
      // on the very next turn. Each addTool is one cache-miss turn.
      if (loop) for (const s of registeredSpecs) loop.prefix.addTool(s);
      return { ok: true, summary };
    } catch (err) {
      await mcp?.close().catch(() => undefined);
      const reason = (err as Error).message;
      sink({ kind: "failed", name: label, reason });
      rejectReady(new Error(`MCP server "${label}" failed to start: ${reason}`));
      return { ok: false, reason };
    }
  }

  async function removeSpec(raw: string, loop?: CacheFirstLoop): Promise<boolean> {
    const record = records.get(raw);
    if (!record) return false;
    await record.client.close().catch(() => undefined);
    const tools = ctx.getTools();
    for (const name of record.registeredNames) {
      tools?.unregister(name);
      loop?.prefix.removeTool(name);
    }
    records.delete(raw);
    const idx = insertionOrder.indexOf(raw);
    if (idx >= 0) insertionOrder.splice(idx, 1);
    return true;
  }

  async function reloadFromConfig(loop?: CacheFirstLoop): Promise<{
    added: string[];
    removed: string[];
    failed: Array<{ spec: string; reason: string }>;
    summaries: McpServerSummary[];
  }> {
    const desired = readConfig().mcp ?? [];
    const desiredSet = new Set(desired);
    const currentSet = new Set(records.keys());
    const added: string[] = [];
    const removed: string[] = [];
    const failed: Array<{ spec: string; reason: string }> = [];

    for (const spec of [...currentSet]) {
      if (!desiredSet.has(spec)) {
        await removeSpec(spec, loop);
        removed.push(spec);
      }
    }
    for (const spec of desired) {
      if (currentSet.has(spec)) continue;
      const result = await addSpec(spec, loop);
      if (result.ok) added.push(spec);
      else failed.push({ spec, reason: result.reason });
    }
    return { added, removed, failed, summaries: summaries() };
  }

  function specs(): string[] {
    return [...insertionOrder];
  }
  function summaries(): McpServerSummary[] {
    return insertionOrder
      .map((s) => records.get(s)?.summary)
      .filter((s): s is McpServerSummary => Boolean(s));
  }
  async function closeAll(): Promise<void> {
    for (const r of records.values()) await r.client.close().catch(() => undefined);
    records.clear();
    insertionOrder.length = 0;
  }
  function setLifecycleSink(s: McpLifecycleSink): void {
    sink = s;
  }
  return {
    size: () => records.size,
    specs,
    summaries,
    addSpec,
    removeSpec,
    reloadFromConfig,
    closeAll,
    setLifecycleSink,
  };
}

export interface ChatOptions {
  model: string;
  system: string;
  transcript?: string;
  /**
   * Soft USD cap on session spend. Undefined → no cap (default).
   * The loop warns once at 80% and refuses to start a new turn at
   * 100%. Users can bump or clear via `/budget <usd>` / `/budget off`
   * mid-session.
   */
  budgetUsd?: number;
  /** Per-turn repair-signal count required to escalate flash→pro. Undefined → loop default (3). */
  failureThreshold?: number;
  session?: string;
  /** Zero or more MCP server specs. Each: `"name=cmd args..."` or `"cmd args..."`. */
  mcp?: string[];
  /** Global prefix — only used when a single anonymous server is given. */
  mcpPrefix?: string;
  /**
   * Pre-built ToolRegistry used as a seed. MCP bridges (if any) are
   * layered on top of whatever's already registered. Used by
   * `reasonix code` to register native filesystem tools in place of
   * the old `npx -y @modelcontextprotocol/server-filesystem` subprocess.
   */
  seedTools?: ToolRegistry;
  /**
   * Enable SEARCH/REPLACE edit-block processing after each assistant turn.
   * Set by `reasonix code`; plain `reasonix chat` leaves this off.
   */
  codeMode?: {
    rootDir: string;
    jobs?: import("../../tools/jobs.js").JobRegistry;
    /**
     * `/cwd <path>` callback — re-registers every rootDir-dependent
     * native tool against the new path. Optional so embedders that
     * don't want live cwd switching can omit it (the slash command
     * then falls back to non-tool updates only).
     */
    reregisterTools?: (rootDir: string) => void;
    /** Async tail of `/cwd` — re-probe the new dir for a semantic index. */
    reBootstrapSemantic?: (rootDir: string) => Promise<{ enabled: boolean }>;
  };
  /** Skip the session picker — assume "Resume" (backwards-compatible auto-continue). */
  forceResume?: boolean;
  /** Skip the session picker — assume "New" (wipe the session file and start fresh). */
  forceNew?: boolean;
  /**
   * When true, suppress auto-launch of the embedded web dashboard.
   * Default behavior (false/undefined) is to boot it on mount so the
   * URL is visible in the status bar.
   */
  noDashboard?: boolean;
  /** Pin the dashboard to a fixed port. `undefined` keeps ephemeral assignment. */
  dashboardPort?: number;
  /**
   * Render into the terminal's alternate screen buffer. Default true —
   * alt-screen avoids the scrollback-mode resize/wrap ghost class. Pass
   * false (CLI: `--no-alt-screen`) when the chat output needs to remain
   * in shell scrollback after exit.
   */
  altScreen?: boolean;
  /**
   * Enable DECSET 1007 (alternate-scroll) so the wheel scrolls chat on
   * web/cloud/SSH terminals — terminal translates wheel events to ↑/↓
   * key sequences in alt-screen, no full mouse tracking, native
   * drag-select + right-click unaffected. Default true. Pass false
   * (CLI: `--no-mouse`) to suppress entirely.
   */
  mouse?: boolean;
}

interface RootProps extends ChatOptions {
  initialKey: string | undefined;
  tools: ToolRegistry | undefined;
  mcpSpecs: string[];
  mcpServers: McpServerSummary[];
  /** App.tsx writes its progress handler here on mount so MCP frames flow into OngoingToolRow. */
  progressSink: { current: ((info: ProgressInfo) => void) | null };
  /** Show the SessionPicker (full list) when no --session was specified and saved sessions exist. */
  showPicker: boolean;
  /** Hot-reload runtime — passed through to App so /mcp browse + dashboard can bridge after install. */
  mcpRuntime: McpRuntime;
  /** One-time startup info rows shown after App mounts. */
  startupInfoHints: string[];
}

function Root({
  initialKey,
  tools,
  mcpSpecs,
  mcpServers,
  progressSink,
  showPicker,
  mcpRuntime,
  startupInfoHints,
  ...appProps
}: RootProps) {
  const [key, setKey] = useState<string | undefined>(initialKey);
  const [pickerOpen, setPickerOpen] = useState(showPicker);
  const [activeSession, setActiveSession] = useState<string | undefined>(appProps.session);
  const workspaceRoot = appProps.codeMode?.rootDir ?? process.cwd();
  const [sessions, setSessions] = useState(() => listSessionsForWorkspace(workspaceRoot));

  if (!key) {
    return (
      <Setup
        onReady={(k) => {
          process.env.DEEPSEEK_API_KEY = k;
          setKey(k);
        }}
      />
    );
  }
  process.env.DEEPSEEK_API_KEY = key;

  if (pickerOpen) {
    return (
      <KeystrokeProvider>
        <SessionPicker
          sessions={sessions}
          workspace={workspaceRoot}
          onChoose={(outcome) => {
            if (outcome.kind === "open") {
              setActiveSession(outcome.name);
              setPickerOpen(false);
              return;
            }
            if (outcome.kind === "new") {
              setActiveSession(freshSessionName(activeSession));
              setPickerOpen(false);
              return;
            }
            if (outcome.kind === "delete") {
              deleteSession(outcome.name);
              setSessions(listSessionsForWorkspace(workspaceRoot));
              return;
            }
            if (outcome.kind === "rename") {
              renameSession(outcome.name, outcome.newName);
              setSessions(listSessionsForWorkspace(workspaceRoot));
              return;
            }
            if (outcome.kind === "quit") {
              process.exit(0);
            }
          }}
        />
      </KeystrokeProvider>
    );
  }

  return (
    <KeystrokeProvider>
      <App
        // key forces a full remount (and fresh transcript / scrollback / cards) on switch.
        key={activeSession ?? "__new__"}
        model={appProps.model}
        system={appProps.system}
        transcript={appProps.transcript}
        budgetUsd={appProps.budgetUsd}
        failureThreshold={appProps.failureThreshold}
        session={activeSession}
        tools={tools}
        mcpSpecs={mcpSpecs}
        mcpServers={mcpServers}
        mcpRuntime={mcpRuntime}
        progressSink={progressSink}
        startupInfoHints={startupInfoHints}
        codeMode={appProps.codeMode}
        noDashboard={appProps.noDashboard}
        dashboardPort={appProps.dashboardPort}
        mouse={appProps.mouse}
        onSwitchSession={setActiveSession}
      />
    </KeystrokeProvider>
  );
}

export async function chatCommand(opts: ChatOptions): Promise<void> {
  markPhase("chat_command_enter");
  loadDotenv();
  const initialKey = loadApiKey();
  markPhase("config_loaded");

  const requestedSpecs = opts.mcp ?? [];
  // Shared progress sink: the bridge's onProgress callback writes
  // through `progressSink.current`, which App.tsx sets to its UI
  // updater on mount. Started null so early progress frames (before
  // the App has mounted) are dropped rather than buffered.
  const progressSink: { current: ((info: ProgressInfo) => void) | null } = { current: null };
  // Seed registry from the caller (e.g. reasonix code's native
  // filesystem tools) — MCP bridges layer on top rather than
  // replacing. When no seed AND no MCP, tools stays undefined and
  // the loop runs as a bare chat.
  let tools: ToolRegistry | undefined = opts.seedTools;
  if (requestedSpecs.length > 0 && !tools) tools = new ToolRegistry();

  const runtime = createMcpRuntime({
    getTools: () => tools,
    getMcpPrefix: () => opts.mcpPrefix,
    getRequestedCount: () => requestedSpecs.length,
    progressSink,
  });

  // MCP bridging deferred to App.tsx mount — handshakes are 100ms–2s each
  // and we don't want the alt-screen UI to block on the slowest one.
  const mcpSpecs = [...requestedSpecs];
  const mcpServers: McpServerSummary[] = [];
  const cfg = readConfig();
  const startupInfoHints: string[] = [];
  if (cfg.setupCompleted === true && (cfg.mcp?.length ?? 0) === 0 && mcpSpecs.length === 0) {
    startupInfoHints.push(t("mcpHealth.emptyHint"));
  }

  // Register web search/fetch tools unless explicitly disabled. DDG
  // backs them with no key required; the model invokes them whenever
  // a question needs info fresher than its training data.
  if (searchEnabled()) {
    if (!tools) tools = new ToolRegistry();
    registerWebTools(tools, {
      webSearchEngine: webSearchEngine(),
      webSearchEndpoint: webSearchEndpoint(),
    });
  }

  // Memory tools — available in every session, not just code mode.
  // Chat-mode callers get global scope only; project scope requires
  // the seedTools path from `reasonix code` (which registers its own
  // MemoryStore bound to rootDir before chatCommand runs).
  // `run_skill` is registered later in App.tsx (where the client
  // exists) so it can wire the subagent runner for runAs:subagent
  // skills.
  if (!opts.seedTools) {
    if (!tools) tools = new ToolRegistry();
    registerMemoryTools(tools, {});
    // `ask_choice` — branching primitive, useful in chat too (stylistic
    // preferences, doc language, library picks). Independent of plan
    // mode, which chat doesn't have anyway.
    registerChoiceTool(tools);
  }

  // resolveSession handles --new (timestamped name, old session preserved)
  // and --resume (latest prefixed). Default falls through to the latest
  // prefixed-or-base.
  const { resolved: resolvedSession } = resolveSession(
    opts.session,
    opts.forceNew,
    opts.forceResume,
  );
  const launchWorkspace = opts.codeMode?.rootDir ?? process.cwd();
  const showPicker =
    !opts.session && !opts.forceResume && listSessionsForWorkspace(launchWorkspace).length > 0;

  markPhase("ink_render_call");
  const { waitUntilExit } = render(
    <Root
      initialKey={initialKey}
      tools={tools}
      mcpSpecs={mcpSpecs}
      mcpServers={mcpServers}
      mcpRuntime={runtime}
      progressSink={progressSink}
      startupInfoHints={startupInfoHints}
      showPicker={showPicker}
      {...opts}
      session={resolvedSession}
    />,
    {
      exitOnCtrlC: true,
      // patchConsole:false — winpty/MINTTY redraw-glitch source.
      patchConsole: false,
      // incrementalRendering:false — Ink's diff drifts when stringWidth
      // misjudges CJK / emoji ZWJ width or when async terminal-event
      // bytes interleave mid-render, leaving residual rows. Full-frame
      // redraws cost more stdout bytes per flush but eliminate the
      // ghost class.
      incrementalRendering: false,
      // Default true — alt-screen is the only mode without scrollback-
      // reflow ghosting. `--no-alt-screen` opts back into scrollback mode
      // for users who need chat output preserved in shell history on exit.
      alternateScreen: opts.altScreen !== false,
    },
  );
  try {
    await waitUntilExit();
  } finally {
    await runtime.closeAll();
    // Eat any pending terminal-feature-detection responses (#365) so the
    // parent shell doesn't print them as junk after exit.
    await drainTtyResponses();
  }
}
