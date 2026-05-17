import { closeSync, mkdirSync, openSync, writeSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";
import { render } from "ink";
import React, { useState } from "react";
import {
  loadApiKey,
  readConfig,
  searchEnabled,
  webSearchEndpoint,
  webSearchEngine,
} from "../../config.js";
import { loadDotenv } from "../../env.js";
import { t } from "../../i18n/index.js";
import {
  deleteSession,
  freshSessionName,
  listSessionsForWorkspace,
  renameSession,
  resolveSession,
} from "../../memory/session.js";
import { QQChannel } from "../../qq/channel.js";
import { ToolRegistry } from "../../tools.js";
import { registerChoiceTool } from "../../tools/choice.js";
import { registerMemoryTools } from "../../tools/memory.js";
import { registerWebTools } from "../../tools/web.js";
import { stopAndSaveCpuProfile } from "../cpu-prof.js";
import { markPhase } from "../startup-profile.js";
import { App } from "../ui/App.js";
import { SessionPicker } from "../ui/SessionPicker.js";
import { Setup } from "../ui/Setup.js";
import { drainTtyResponses } from "../ui/drain-tty.js";
import { KeystrokeProvider, type KeystrokeReader } from "../ui/keystroke-context.js";
import {
  type RustKeystrokeReader,
  createRustKeystrokeReader,
  nullKeystrokeReader,
} from "../ui/scene/input-adapter.js";
import { cancelAllListPickers, resolveListPicker } from "../ui/scene/list-picker-store.js";
import { makeNullStdin } from "../ui/scene/null-stdin.js";
import { makeNullStdout } from "../ui/scene/null-stdout.js";
import { cancelAllPromptInputs, resolvePromptInput } from "../ui/scene/prompt-input-store.js";
import { isIntegratedRendererRequested, setIntegratedEventHandler } from "../ui/scene/trace.js";
import type { McpServerSummary } from "../ui/slash.js";
import {
  type McpLifecycleNotice,
  type McpLifecycleSink,
  type McpRuntime,
  type ProgressInfo,
  createMcpRuntime,
} from "./mcp-runtime.js";

export type { McpLifecycleNotice, McpLifecycleSink, McpRuntime, ProgressInfo };

function parseInputCmd(raw: string | undefined): readonly string[] | undefined {
  if (!raw || raw.length === 0) return undefined;
  try {
    const parsed = JSON.parse(raw);
    if (Array.isArray(parsed) && parsed.every((p) => typeof p === "string")) return parsed;
  } catch {
    // fall through
  }
  return undefined;
}

// Under REASONIX_RENDERER=rust the alt-screen is owned by the Rust child. Any
// stderr write from the Node parent overwrites whatever ratatui just drew at
// the same cells, and stays visible until the next frame redraws — so Node's
// own warnings (MaxListeners etc.) and any stray library logs all corrupt the
// view. Redirect stderr to a log file for the duration of the session.
function redirectStderrToLogFile(): () => void {
  const dir = join(homedir(), ".reasonix");
  mkdirSync(dir, { recursive: true });
  const logPath = join(dir, "rust-render-stderr.log");
  const fd = openSync(logPath, "a");
  const origWrite = process.stderr.write.bind(process.stderr);
  (process.stderr.write as unknown as (chunk: string | Uint8Array) => boolean) = (
    chunk: string | Uint8Array,
  ): boolean => {
    const buf = typeof chunk === "string" ? Buffer.from(chunk, "utf8") : Buffer.from(chunk);
    writeSync(fd, buf);
    return true;
  };
  return () => {
    process.stderr.write = origWrite;
    try {
      closeSync(fd);
    } catch {
      // already closed — ignore
    }
  };
}

export interface ChatOptions {
  model: string;
  system: string;
  /** Re-runs the prompt builder on /new so REASONIX.md edits don't need a restart. Should produce the same string `system` was built from. */
  rebuildSystem?: () => string;
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
    /** Notify the launcher that the workspace root just changed — lets the rebuildSystem closure see the new dir. */
    onRootChange?: (newRoot: string) => void;
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
  /** When true and the dashboard is enabled, open its URL in the system default browser as soon as the server is ready. */
  openDashboard?: boolean;
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
  /** Pre-created QQ channel (started before TUI mounts). */
  qqChannel?: QQChannel;
  /** App fills this ref on mount so QQ messages flow into the TUI input queue. */
  qqSubmitRef: { current: ((text: string) => void) | null };
  /** Set by App on mount so Rust approval-response events route to the right handler ref. */
  approvalDispatchRef?: { current: ((kind: string, choice: unknown) => void) | null };
  /** Set by App on mount; chat.tsx calls it with the Rust child's composer text so Ink pickers (@ / slash) recompute. */
  rustComposerRef?: { current: ((text: string) => void) | null };
  /** Apply edit-mode value when Rust emits mode-set (Shift+Tab cycle or picker selection). */
  modeSetRef?: {
    current: ((value: "review" | "auto" | "yolo") => void) | null;
  };
  /** Apply preset value when Rust emits preset-set (picker selection). */
  presetSetRef?: {
    current: ((value: "auto" | "flash" | "pro") => void) | null;
  };
  /** App fills this ref on mount so QQ errors appear in the TUI log. */
  qqErrorRef: { current: ((msg: string) => void) | null };
  /** Custom keystroke source — populated when REASONIX_RENDERER=rust so keys flow from the spawned input child (or a no-op reader in integrated mode) instead of process.stdin. */
  keystrokeReader?: KeystrokeReader;
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
  keystrokeReader,
  ...appProps
}: RootProps) {
  const [key, setKey] = useState<string | undefined>(initialKey);
  const [pickerOpen, setPickerOpen] = useState(showPicker);
  const [activeSession, setActiveSession] = useState<string | undefined>(appProps.session);
  const workspaceRoot = appProps.codeMode?.rootDir ?? process.cwd();
  const [sessions, setSessions] = useState(() => listSessionsForWorkspace(workspaceRoot));

  if (!key) {
    return (
      <KeystrokeProvider reader={keystrokeReader}>
        <Setup
          onReady={(k) => {
            process.env.DEEPSEEK_API_KEY = k;
            setKey(k);
          }}
        />
      </KeystrokeProvider>
    );
  }
  process.env.DEEPSEEK_API_KEY = key;

  if (pickerOpen) {
    return (
      <KeystrokeProvider reader={keystrokeReader}>
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
              void (async () => {
                await stopAndSaveCpuProfile();
                process.exit(0);
              })();
            }
          }}
        />
      </KeystrokeProvider>
    );
  }

  return (
    <KeystrokeProvider reader={keystrokeReader}>
      <App
        // key forces a full remount (and fresh transcript / scrollback / cards) on switch.
        key={activeSession ?? "__new__"}
        model={appProps.model}
        system={appProps.system}
        rebuildSystem={appProps.rebuildSystem}
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
        openDashboard={appProps.openDashboard}
        dashboardPort={appProps.dashboardPort}
        mouse={appProps.mouse}
        qqChannel={appProps.qqChannel}
        qqSubmitRef={appProps.qqSubmitRef}
        qqErrorRef={appProps.qqErrorRef}
        approvalDispatchRef={appProps.approvalDispatchRef}
        rustComposerRef={appProps.rustComposerRef}
        modeSetRef={appProps.modeSetRef}
        presetSetRef={appProps.presetSetRef}
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

  // Create QQ channel before the TUI mounts so connection setup stays
  // outside React lifecycle timing and the WebSocket handshake remains
  // deterministic.
  const qqSubmitRef: { current: ((text: string) => void) | null } = { current: null };
  const qqErrorRef: { current: ((msg: string) => void) | null } = { current: null };
  const approvalDispatchRef: {
    current: ((kind: string, choice: unknown) => void) | null;
  } = { current: null };
  const rustComposerRef: { current: ((text: string) => void) | null } = { current: null };
  const modeSetRef: {
    current: ((value: "review" | "auto" | "yolo") => void) | null;
  } = { current: null };
  const presetSetRef: {
    current: ((value: "auto" | "flash" | "pro") => void) | null;
  } = { current: null };
  const qqRequested = cfg.qq?.enabled === true;
  let qqChannel: QQChannel | undefined;
  if (qqRequested) {
    const channel = new QQChannel({
      onSubmitMessage: (text) => qqSubmitRef.current?.(text),
      onError: (msg) => qqErrorRef.current?.(msg),
    });
    process.stderr.write("Connecting QQ bot...\n");
    try {
      await channel.start();
      qqChannel = channel;
      process.stderr.write("QQ bot connected\n");
    } catch (err) {
      process.stderr.write(`QQ bot failed: ${(err as Error).message}\n`);
    }
  }

  const rustRendererRequested = process.env.REASONIX_RENDERER === "rust";
  // If REASONIX_RENDERER=rust is set but no API key is saved, the first screen
  // is Setup — which renders to Ink's stdout and reads stdin directly. Under
  // the Rust path both would be the null streams, leaving the user typing their
  // key blind. Fall back to the Ink renderer for this launch; the next launch
  // (with the key saved) gets the Rust path.
  const rustRendererActive = rustRendererRequested && initialKey !== undefined;
  if (rustRendererRequested && !rustRendererActive) {
    process.stderr.write(
      "REASONIX_RENDERER=rust ignored for this launch: no saved API key. " +
        "Complete Setup once, then re-launch with the flag.\n",
    );
  }
  const rustIntegrated = rustRendererActive && isIntegratedRendererRequested();
  const inkStdout = rustRendererActive ? makeNullStdout() : undefined;
  const inkStdin = rustRendererActive ? makeNullStdin() : undefined;
  const inputCmdOverride = parseInputCmd(process.env.REASONIX_INPUT_CMD);
  const rustInputChild: RustKeystrokeReader | undefined =
    rustRendererActive && !rustIntegrated
      ? createRustKeystrokeReader(inputCmdOverride ? { command: inputCmdOverride } : {})
      : undefined;
  const keystrokeReader: KeystrokeReader | undefined = rustIntegrated
    ? nullKeystrokeReader
    : rustInputChild;
  const stderrRestore = rustRendererActive ? redirectStderrToLogFile() : undefined;

  if (rustIntegrated) {
    setIntegratedEventHandler((event) => {
      if (event.event === "submit") {
        qqSubmitRef.current?.(event.text);
      } else if (event.event === "exit") {
        void (async () => {
          cancelAllPromptInputs();
          cancelAllListPickers();
          await stopAndSaveCpuProfile();
          process.exit(0);
        })();
      } else if (event.event === "approval-response") {
        approvalDispatchRef.current?.(event.kind, event.choice);
      } else if (event.event === "composer") {
        rustComposerRef.current?.(event.text);
      } else if (event.event === "mode-set") {
        modeSetRef.current?.(event.value);
      } else if (event.event === "preset-set") {
        presetSetRef.current?.(event.value);
      } else if (event.event === "prompt-response") {
        resolvePromptInput(event.id, event.cancelled ? null : (event.text ?? ""));
      } else if (event.event === "list-picker-response") {
        resolveListPicker(event.id, event.cancelled ? null : (event.key ?? null));
      }
      // interrupt: no-op for now; terminal SIGINT already reaches Node.
    });
  }

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
      keystrokeReader={keystrokeReader}
      {...opts}
      session={resolvedSession}
      qqChannel={qqChannel}
      qqSubmitRef={qqSubmitRef}
      approvalDispatchRef={approvalDispatchRef}
      rustComposerRef={rustComposerRef}
      modeSetRef={modeSetRef}
      presetSetRef={presetSetRef}
      qqErrorRef={qqErrorRef}
    />,
    {
      ...(rustRendererActive ? { stdout: inkStdout, stdin: inkStdin } : {}),
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
      // Off when the Rust child owns the terminal — it runs its own alt-screen.
      alternateScreen: !rustRendererActive && opts.altScreen !== false,
    },
  );
  try {
    await waitUntilExit();
  } finally {
    await runtime.closeAll();
    qqChannel?.stop();
    if (rustInputChild) await rustInputChild.close();
    if (stderrRestore) stderrRestore();
    // Eat any pending terminal-feature-detection responses (#365) so the
    // parent shell doesn't print them as junk after exit.
    await drainTtyResponses();
  }
}
