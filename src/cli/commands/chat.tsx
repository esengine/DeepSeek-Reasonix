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
import { KeystrokeProvider } from "../ui/keystroke-context.js";
import type { McpServerSummary } from "../ui/slash.js";
import {
  type McpLifecycleNotice,
  type McpLifecycleSink,
  type McpRuntime,
  type ProgressInfo,
  createMcpRuntime,
} from "./mcp-runtime.js";

export type { McpLifecycleNotice, McpLifecycleSink, McpRuntime, ProgressInfo };

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
    <KeystrokeProvider>
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
