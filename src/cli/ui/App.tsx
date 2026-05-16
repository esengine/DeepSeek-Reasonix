import { type WriteStream, statSync } from "node:fs";
import { resolve } from "node:path";
import { Box, Text, useStdin, useStdout } from "ink";
import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  type JsonlEventSink,
  eventLogPath,
  openEventSink,
} from "../../adapters/event-sink-jsonl.js";
import { type AtUrlExpansion, expandAtMentions, expandAtUrls } from "../../at-mentions.js";
import {
  type CheckpointMeta,
  createCheckpoint,
  deleteCheckpoint,
  fmtAgo,
  listCheckpoints,
  restoreCheckpoint,
} from "../../code/checkpoints.js";
import {
  type EditBlock,
  applyEditBlocks,
  snapshotBeforeEdits,
  toWholeFileEditBlock,
} from "../../code/edit-blocks.js";
import { clearPendingEdits, loadPendingEdits } from "../../code/pending-edits.js";
import {
  clearPlanState,
  loadPlanState,
  relativeTime,
  savePlanState,
} from "../../code/plan-store.js";
import {
  type EditMode,
  type PresetName,
  defaultConfigPath,
  editModeHintShown,
  loadBaseUrl,
  loadReasoningEffort,
  loadTheme,
  markEditModeHintShown,
  markMouseClipboardHintShown,
  mouseClipboardHintShown,
  readConfig,
  resolveThemePreference,
  saveEditMode,
  savePreset,
  saveTheme,
} from "../../config.js";
import { Eventizer } from "../../core/eventize.js";
import { pauseGate } from "../../core/pause-gate.js";
import { autoResolveVerdict, shouldAutoResolveCheckpoint } from "../../core/pause-policy.js";
import { formatHookOutcomeMessage, runHooks } from "../../hooks.js";
import { t, tObj } from "../../i18n/index.js";
import { CacheFirstLoop, DeepSeekClient, ImmutablePrefix } from "../../index.js";
import type { LoopEvent } from "../../loop.js";
import {
  deleteSession,
  detectGitBranch,
  freshSessionName,
  type listSessions,
  listSessionsForWorkspace,
  loadSessionMessages,
  loadSessionMeta,
  patchSessionMeta,
  renameSession,
} from "../../memory/session.js";
import type { QQChannel } from "../../qq/channel.js";
import { useQQChannel } from "../../qq/use-qq-channel.js";
import type {
  ActiveModal,
  DashboardEvent,
  DashboardMessage,
  PickerResolution,
  SubmitResult,
} from "../../server/context.js";
import type { DashboardServerHandle } from "../../server/index.js";
import { loadSlashUsage, recordSlashUse } from "../../slash-usage.js";
import {
  DEEPSEEK_CONTEXT_TOKENS,
  DEFAULT_CONTEXT_TOKENS,
  type SessionSummary,
} from "../../telemetry/stats.js";
import { defaultUsageLogPath } from "../../telemetry/usage.js";
import type { ToolRegistry } from "../../tools.js";
import type { ChoiceOption } from "../../tools/choice.js";
import type { PlanStep } from "../../tools/plan.js";
import { formatCommandResult, runCommand } from "../../tools/shell.js";
import { registerSkillTools } from "../../tools/skills.js";
import { formatSubagentResult, spawnSubagent } from "../../tools/subagent.js";
import { webFetch } from "../../tools/web.js";
import { openTranscriptFile } from "../../transcript/log.js";
import { openInExternalEditor } from "../edit/external-editor.js";
import { dumpStartupProfile, markPhase } from "../startup-profile.js";
import { AtMentionSuggestions } from "./AtMentionSuggestions.js";
import { BootSplash } from "./BootSplash.js";
import { CheckpointPicker } from "./CheckpointPicker.js";
import { ChoiceConfirm, type ChoiceConfirmChoice } from "./ChoiceConfirm.js";
import { EditConfirm, type EditReviewChoice } from "./EditConfirm.js";
import { McpHub } from "./McpHub.js";
import { ModelPicker } from "./ModelPicker.js";
import { PathConfirm } from "./PathConfirm.js";
import { PlanCheckpointConfirm } from "./PlanCheckpointConfirm.js";
import { PlanConfirm, type PlanConfirmChoice } from "./PlanConfirm.js";
import { PlanRefineInput } from "./PlanRefineInput.js";
import { PlanReviseConfirm, type ReviseChoice } from "./PlanReviseConfirm.js";
import { PlanReviseEditor } from "./PlanReviseEditor.js";
import { PromptInput } from "./PromptInput.js";
import { SessionPicker } from "./SessionPicker.js";
import { ShellConfirm, type ShellConfirmChoice, derivePrefix } from "./ShellConfirm.js";
import { SlashArgPicker } from "./SlashArgPicker.js";
import { SlashSuggestions } from "./SlashSuggestions.js";
import { type ThemeChoice, ThemePicker } from "./ThemePicker.js";
import { WelcomeBanner } from "./WelcomeBanner.js";
import { detectBangCommand, formatBangUserMessage } from "./bang.js";
import { CopyMode } from "./copy-mode/CopyMode.js";
import type { PickerSnapshot, ViewerSnapshot } from "./dashboard/use-picker-broadcast.js";
import { useViewerBroadcast } from "./dashboard/use-picker-broadcast.js";
import { formatEditResults } from "./edit-history.js";
import { loopEventToDashboard } from "./effects/loop-to-dashboard.js";
import { appendGlobalMemory, appendProjectMemory, detectHashMemory } from "./hash-memory.js";
import { applySlashResult } from "./hooks/apply-slash-result.js";
import { handleAssistantFinal } from "./hooks/handle-assistant-final.js";
import {
  handleErrorEvent,
  handleToolStart,
  handleWarningEvent,
} from "./hooks/handle-stream-events.js";
import { handleToolEvent } from "./hooks/handle-tool-event.js";
import { useActivityLabel } from "./hooks/useActivityPhase.js";
import { useAgentSession } from "./hooks/useAgentSession.js";
import { useCodeMode } from "./hooks/useCodeMode.js";
import { useEditGate } from "./hooks/useEditGate.js";
import { useHookList } from "./hooks/useHookList.js";
import { useInputRecall } from "./hooks/useInputRecall.js";
import { useLanguageReload } from "./hooks/useLanguageReload.js";
import { useLoopMode } from "./hooks/useLoopMode.js";
import { usePresetMode } from "./hooks/usePresetMode.js";
import { useQuit } from "./hooks/useQuit.js";
import { summarizeCard, useSceneTrace } from "./hooks/useSceneTrace.js";
import { useScrollback } from "./hooks/useScrollback.js";
import { useTerminalSetup } from "./hooks/useTerminalSetup.js";
import { useToolProgressDisplay } from "./hooks/useToolProgressDisplay.js";
import { useTranscriptWriter } from "./hooks/useTranscriptWriter.js";
import { useWorkspaceRoot } from "./hooks/useWorkspaceRoot.js";
import { useKeystroke } from "./keystroke-context.js";
import { CardStream } from "./layout/CardStream.js";
import { LiveExpandContext } from "./layout/LiveExpandContext.js";
import {
  ModeStatusBar,
  OngoingToolRow,
  SubagentLiveStack,
  ThinkingRow,
  UndoBanner,
} from "./layout/LiveRows.js";
import { StatusRow } from "./layout/StatusRow.js";
import type { StatusBarConfig } from "./layout/StatusRow.js";
import { ToastRail } from "./layout/ToastRail.js";
import { PlanLiveRow } from "./layout/plan-live-row.js";
import { ViewportBudgetProvider } from "./layout/viewport-budget.js";
import { formatLoopStatus } from "./loop.js";
import { applyMcpAppend } from "./mcp-append.js";
import { handleMcpBrowseSlash } from "./mcp-browse.js";
import { formatMcpLifecycleEvent } from "./mcp-lifecycle.js";
import { replaceMcpServerSummary } from "./mcp-server-list.js";
import { formatMcpSlowToast } from "./mcp-toast.js";
import { openUrl } from "./open-url.js";
import { formatLongPaste } from "./paste-collapse.js";
import { extractOpenQuestionsSection } from "./plan-open-questions.js";
import { PRESETS, resolvePreset } from "./presets.js";
import { type McpServerSummary, handleSlash, parseSlash, suggestSlashCommands } from "./slash.js";
import { TurnTranslator } from "./state/TurnTranslator.js";
import { cardsToDashboardMessages } from "./state/cards-to-messages.js";
import {
  ChatScrollProvider,
  useChatScrollActions,
  useChatScrollState,
} from "./state/chat-scroll-provider.js";
import { hydrateCardsFromMessages } from "./state/hydrate.js";
import { InflightProvider } from "./state/inflight-context.js";
import { AgentStoreProvider, useAgentState, useAgentStore } from "./state/provider.js";
import { ThemeProvider } from "./theme/context.js";
import { listThemeNames } from "./theme/tokens.js";
import { FG, type ThemeName } from "./theme/tokens.js";
import { TickerProvider } from "./ticker.js";
import { useCompletionPickers } from "./useCompletionPickers.js";
import { useEditHistory } from "./useEditHistory.js";
import { useSessionInfo } from "./useSessionInfo.js";
import { useSubagent } from "./useSubagent.js";

export interface AppProps {
  model: string;
  system: string;
  /** Re-runs the prompt builder on /new so REASONIX.md edits don't need a restart. Must produce the same shape as `system` was built from. */
  rebuildSystem?: () => string;
  transcript?: string;
  /** Soft USD spend cap; undefined 闂?no cap. See CacheFirstLoopOptions.budgetUsd. */
  budgetUsd?: number;
  /** Per-turn repair-signal count required to escalate flash闂傚倸鍊搁崐鎼佸磹閹间礁纾归柟闂寸绾惧綊鏌熼梻瀵割槮缁炬儳缍婇弻鐔兼⒒鐎靛壊妲紒鐐劤缂嶅﹪寮婚悢鍏尖拻閻庨潧澹婂Σ顔剧磼閻愵剙鍔ょ紓宥咃躬瀵鏁愭径濠勵吅闂佹寧绻傞幉娑㈠箻缂佹鍘辨繝鐢靛Т閸婂綊宕戦妷鈺傜厸閻忕偠顕ф慨鍌溾偓娈垮枟閹告娊骞冨▎寰濆湱鈧綆浜欐竟鏇㈡⒑閸涘﹦缂氶柛搴㈠▕閹矂宕卞☉娆戝幈濡炪倖鍔戦崐鏇㈠几閹达附鐓曟繛鍡楃箳缁犳彃菐閸パ嶈含妞ゃ垺绋戦埥澶婎潨閸℃鐤傜紓鍌欒兌閾忓酣宕㈤崗鍏兼珷濞寸姴顑嗛崕澶嬨亜韫囨挾澧曠紒鐘虫皑閹茬顭ㄩ崼鐔蜂簵婵犻潧鍊搁幉锟犳偂濞戙垺鍊堕柣鎰絻閳锋棃鏌嶉挊澶樻█闁哄苯绉归幐濠冨緞濡儵鏋呴柣搴ゎ潐濞测晝鎹㈠┑瀣畺婵炲棙鍨熼崑鎾绘偨閸涘﹥鐝抽梺瑙勭叀缁犳牕顫忔繝姘＜婵炲棙鍩堝Σ顕€姊虹涵鍜佸殝缂佺粯绻堥獮鍐倻閽樺）銊ф喐鎼达紕鏆﹂柛娆忣槹閸欏繑淇婇悙棰濆殭濞存粓绠栭幃妤€鈻撻崹顔界亪濡炪値鍘鹃崗妯侯嚕椤愶箑绠涢柡澶嬪閵囨繃绻涙潏鍓у埌闁圭⒈鍋勯锝夊箮閼恒儮鎷绘繛杈剧到閹诧繝宕悙鐢电＜妞ゆ棁鍋愭晶锕傛煙椤旀儳浠遍柡浣稿暣瀹曟帒顫濇鏍ㄐ? Undefined 闂?loop default (3). */
  failureThreshold?: number;
  session?: string;
  /**
   * Pre-populated tool registry (e.g. from bridgeMcpTools()). When present,
   * its specs are folded into the ImmutablePrefix so the model sees them,
   * and its dispatch is used for tool calls 闂?MCP tools become first-class.
   */
  tools?: ToolRegistry;
  /** Raw `--mcp` / config-derived spec strings, for `/mcp` slash display. */
  mcpSpecs?: string[];
  /**
   * Pre-captured inspection reports for each connected MCP server,
   * collected once at chat startup. Drives the rich `/mcp` slash view
   * (tools + resources + prompts per server).
   */
  mcpServers?: McpServerSummary[];
  /**
   * Hot-reload runtime owned by chatCommand. Lets slash + dashboard
   * trigger an add/remove round-trip after the user installs from the
   * marketplace, without restarting the process.
   */
  mcpRuntime?: import("../commands/chat.js").McpRuntime;
  /**
   * Shared ref the MCP bridge's onProgress callback writes through.
   * We attach our updater to `progressSink.current` on mount so any
   * `notifications/progress` frame from any bridged tool flows into
   * the UI. `null` allowed 闂?chat mode without MCP leaves it unset.
   */
  progressSink?: {
    current:
      | ((info: { toolName: string; progress: number; total?: number; message?: string }) => void)
      | null;
  };
  /**
   * When set, parse SEARCH/REPLACE blocks from assistant responses and
   * apply them to disk under `rootDir`. Set by `reasonix code`. The
   * optional `jobs` registry enables /jobs + /kill slashes in the TUI
   * and the status-bar "N jobs running" indicator.
   */
  codeMode?: {
    rootDir: string;
    jobs?: import("../../tools/jobs.js").JobRegistry;
    /**
     * `/cwd <path>` callback 闂?re-registers every rootDir-dependent
     * native tool against the new path. Optional: when omitted the
     * slash command degrades to updating hook cwd / memory root only,
     * with file/shell tools still pointing at the original root.
     */
    reregisterTools?: (rootDir: string) => void;
    /**
     * Async tail of the `/cwd` swap 闂?re-probes the new directory for a
     * compatible semantic index, registers `semantic_search` against it
     * if found, unregisters the stale binding otherwise. Kept separate
     * from `reregisterTools` so the sync FS/shell/memory re-registration
     * isn't blocked on disk I/O.
     */
    reBootstrapSemantic?: (rootDir: string) => Promise<{ enabled: boolean }>;
  };
  /**
   * When `true`, suppress the auto-launch of the embedded web dashboard
   * server on TUI mount. Default behavior is to boot the dashboard so
   * the URL shows in the status bar (clickable in OSC-8-aware
   * terminals) 闂?most users had no idea `/dashboard` even existed.
   * `--no-dashboard` is the CLI flag that flips this on for CI / users
   * who don't want a localhost listener.
   */
  noDashboard?: boolean;
  /** When true and the dashboard is enabled, open its URL in the system default browser as soon as the auto-start finishes. */
  openDashboard?: boolean;
  /** Pin the dashboard to a fixed port. `undefined` keeps ephemeral assignment. */
  dashboardPort?: number;
  /** Mid-chat session swap 闂?Root remounts App with the new session via key. */
  onSwitchSession?: (name: string | undefined) => void;
  /**
   * Enable DECSET 1007 (alternate-scroll) so the wheel scrolls chat
   * on web/cloud/SSH terminals 闂?terminal translates wheel events to
   * 闂?闂?key sequences in alt-screen, no full mouse tracking, native
   * drag-select + right-click unaffected. Default true. Pass false
   * (CLI: `--no-mouse`) to suppress entirely.
   */
  mouse?: boolean;
  /** One-time startup info rows injected by chatCommand. */
  startupInfoHints?: string[];
  /** Pre-created QQ channel (started before TUI mounts). */
  qqChannel?: QQChannel;
  /** Ref filled by App on mount so QQ messages flow into the TUI input queue. */
  qqSubmitRef?: { current: ((text: string) => void) | null };
  /** Ref filled by App on mount so QQ errors appear in the TUI log. */
  qqErrorRef?: { current: ((msg: string) => void) | null };
}

/**
 * Throttle interval in ms. 50ms 闂?20Hz 闂?slow enough that cursor-up
 * repaints on winpty/MINTTY/ConEmu/tmux don't leave half-drawn frames,
 * fast enough that streaming text still reads as continuous. Override
 * via `REASONIX_FLUSH_MS` if you want 60Hz on a terminal you trust.
 */
const FLUSH_INTERVAL_MS = (() => {
  const raw = process.env.REASONIX_FLUSH_MS;
  if (!raw) return 50;
  const parsed = Number(raw);
  if (!Number.isFinite(parsed) || parsed < 16 || parsed > 1000) return 50;
  return Math.round(parsed);
})();

/**
 * Renders either the input area (pinned) or the "reading history" hint
 * (scrolled up). Reads `pinned` from the chat-scroll store directly so
 * AppInner doesn't subscribe 闂?toggling pinned only re-renders this leaf.
 */
function InputAreaWithHistoryHint({
  inputArea,
}: { inputArea: React.ReactNode }): React.ReactElement {
  const pinned = useChatScrollState((s) => s.pinned);
  if (!pinned) {
    return (
      <Text color={FG.faint}>
        {
          " 濠电姷鏁告慨鐑藉极閸涘﹥鍙忛柣鎴ｆ閺嬩線鏌涘☉姗堟敾闁告瑥绻橀弻锝夊箣濠垫劖缍楅梺閫炲苯澧柛濠傛健楠炴劖绻濋崘顏嗗骄闂佸啿鎼鍥╃矓椤旈敮鍋撶憴鍕８闁告梹鍨甸锝夊醇閺囩偟顓洪梺缁樼懃閹虫劙鐛姀銈嗏拻闁稿本鐟︾粊鐗堛亜椤愩埄妲搁柣锝呭槻铻ｉ悶娑掑墲閻忓啫鈹戦悙鏉戠仸缁炬澘绉归、鏇熺鐎ｎ偆鍘梺鍓插亝缁诲啴宕戦鍡樺枑闁绘鐗嗙粭姘舵煕鐎ｎ偄濮夐柍褜鍓涢幊鎾寸珶婵犲洤绐楅柡宥庡幖缁€鍫澝归悡搴ｆ憼闁抽攱鍨堕幈銊╂偡閻楀牊鎮欓梺閫炲苯鍘甸柛濠冪箓閻ｇ兘寮剁拠鐐瀹曘劑顢橀崶椋庣暤闁哄本鐩鎾Ω閵壯傚摋闂備礁鎲￠崝蹇涘磻閹剧繝绻嗛柣鎰典簻閳ь剚鐗犲畷婵嬫晝閳ь剟鈥﹂崸妤€鐒垫い鎺戝€荤壕鍏笺亜閺冨倸甯舵い锝呯－缁?reading history 闂?End / PgDn to return 闂?闂?to advance one line"
        }
      </Text>
    );
  }
  return <>{inputArea}</>;
}

/**
 * Captures printable keys / backspace / Enter while history is unpinned so the
 * user can type blind and see the buffer when they scroll back. Lives in its
 * own leaf so AppInner doesn't subscribe to `pinned` 闂?same trick as
 * `InputAreaWithHistoryHint` above.
 */
function HistoryTypingCapture({
  input,
  setInput,
  enabled,
  onReturnToBottom,
}: {
  input: string;
  setInput: (next: string) => void;
  enabled: boolean;
  onReturnToBottom: () => void;
}): null {
  const pinned = useChatScrollState((s) => s.pinned);
  useKeystroke((ev) => {
    if (ev.paste) return;
    if (ev.return) {
      onReturnToBottom();
      return;
    }
    if (ev.backspace) {
      setInput(input.slice(0, -1));
      return;
    }
    if (ev.input.length > 0 && ev.input >= " ") {
      setInput(input + ev.input);
    }
  }, enabled && !pinned);
  return null;
}

/**
 * Single-line status pill rendered below the modeline whenever a /loop
 * is active. Re-renders every second so the countdown ticks.
 */
function LoopStatusRow({
  loop,
}: {
  loop: { prompt: string; intervalMs: number; nextFireAt: number; iter: number };
}) {
  const [, setTick] = React.useState(0);
  React.useEffect(() => {
    const id = setInterval(() => setTick((t) => t + 1), 1000);
    return () => clearInterval(id);
  }, []);
  const nextFireMs = Math.max(0, loop.nextFireAt - Date.now());
  return (
    <Box>
      <Text color="cyan">{`闂?${formatLoopStatus(loop.prompt, nextFireMs, loop.iter)} 闂?/loop stop or type to cancel`}</Text>
    </Box>
  );
}

interface StreamingState {
  id: string;
  text: string;
  reasoning: string;
  toolCallBuild?: { name: string; chars: number };
}

export function App(props: AppProps): React.ReactElement {
  markPhase("app_render_start");
  const session = useAgentSession({
    sessionId: props.session,
    model: props.model,
    workspace: props.codeMode?.rootDir ?? process.cwd(),
  });
  const initialCards = React.useMemo(
    () => (props.session ? hydrateCardsFromMessages(loadSessionMessages(props.session)) : []),
    [props.session],
  );
  const [themeName, setThemeName] = React.useState<ThemeName>(() =>
    resolveThemePreference(loadTheme(), process.env.REASONIX_THEME),
  );
  const statusBar = React.useMemo((): StatusBarConfig => {
    const cfg = readConfig().statusBar ?? {};
    return {
      showBalance: cfg.showBalance !== false,
      showSessionCost: cfg.showSessionCost !== false,
      showTurnCost: cfg.showTurnCost !== false,
      showCacheHit: cfg.showCacheHit !== false,
      showCtxUsage: cfg.showCtxUsage !== false,
      showVersion: cfg.showVersion !== false,
      showFeedbackHint: cfg.showFeedbackHint !== false,
    };
  }, []);
  return (
    <ThemeProvider name={themeName}>
      <AgentStoreProvider session={session} initialCards={initialCards}>
        <ChatScrollProvider>
          <AppInner
            {...props}
            themeName={themeName}
            setThemeName={setThemeName}
            statusBar={statusBar}
          />
        </ChatScrollProvider>
      </AgentStoreProvider>
    </ThemeProvider>
  );
}

type AppInnerProps = AppProps & {
  themeName: ThemeName;
  setThemeName: React.Dispatch<React.SetStateAction<ThemeName>>;
  statusBar: StatusBarConfig;
};

function AppInner({
  model,
  system,
  rebuildSystem,
  transcript,
  budgetUsd,
  failureThreshold,
  session,
  tools,
  mcpSpecs,
  mcpServers,
  mcpRuntime,
  progressSink,
  codeMode,
  noDashboard,
  openDashboard,
  dashboardPort,
  onSwitchSession,
  mouse = true,
  startupInfoHints,
  qqChannel,
  qqSubmitRef,
  qqErrorRef,
  themeName,
  setThemeName,
  statusBar,
}: AppInnerProps) {
  markPhase("app_inner_start");
  const log = useScrollback();
  const agentStore = useAgentStore();
  const hasConversation = useAgentState((s) =>
    s.cards.some((c) => c.kind === "user" || c.kind === "streaming"),
  );
  const isStreaming = useAgentState((s) => s.cards.some((c) => c.kind === "streaming" && !c.done));
  const cardCount = useAgentState((s) => s.cards.length);
  const recentCardsJson = useAgentState((s) =>
    JSON.stringify(
      s.cards.slice(-24).map((c) => ({ kind: c.kind, summary: summarizeCard(c) ?? "" })),
    ),
  );
  const activityLabel = useActivityLabel();
  const chatScroll = useChatScrollActions();
  const [input, setInput] = useState("");
  const [composerCursor, setComposerCursor] = useState(0);
  const [busy, setBusy] = useState(false);
  const [slashUsage, setSlashUsage] = useState<Readonly<Record<string, number>>>(() =>
    loadSlashUsage(),
  );
  // ctrl-o toggles full-tail view on the live streaming card.
  // Auto-resets at the end of every turn so the next reply starts collapsed.
  const [liveExpand, setLiveExpand] = useState(false);
  useEffect(() => {
    if (!isStreaming && liveExpand) setLiveExpand(false);
  }, [isStreaming, liveExpand]);
  const languageVersion = useLanguageReload();
  // Splash holds for one full whale-spout cycle (~1.4s) so the brand
  // mark always lands clean and heavy first-paint cost stays hidden.
  const [bootReady, setBootReady] = useState(false);
  useEffect(() => {
    const t = setTimeout(() => setBootReady(true), 1400);
    return () => clearTimeout(t);
  }, []);
  useEffect(() => {
    markPhase("first_paint");
    dumpStartupProfile();
  }, []);
  // Live MCP server list: initialized from the boot-time prop, then
  // updated immutably when append-drift adds tools mid-session.
  const [liveMcpServers, setLiveMcpServers] = useState<McpServerSummary[]>(() => mcpServers ?? []);
  // Tracks whether the current turn has been aborted via Esc, so the
  // Esc handler only fires once per turn (repeated presses would yield
  // stacked warning events).
  const abortedThisTurn = useRef(false);
  // Mirrors the live `busy` flag for /loop's timer (it has no React
  // closure handle, only refs). Skips the firing when a prior turn is
  // still running rather than queuing a duplicate submit.
  useEffect(() => {
    busyRef.current = busy;
  }, [busy]);
  const {
    ongoingTool,
    setOngoingTool,
    toolProgress,
    setToolProgress,
    statusLine,
    setStatusLine,
    clear: clearToolProgressDisplay,
  } = useToolProgressDisplay(progressSink);
  const { stdout } = useStdout();
  useTerminalSetup(mouse);

  // Subagent UI wiring: live activity row + sink ref the loop closure
  // captures. Must be declared BEFORE loop construction so the
  // subagentRunner closure can read the ref. The wallet-currency thunk
  // reads from a ref populated AFTER useSessionInfo loads balance, so the
  // subagent-end cost suffix renders in the live wallet's symbol.
  const walletCurrencyRef = useRef<string | undefined>(undefined);
  const { activities: subagentActivities, sinkRef: subagentSinkRef } = useSubagent({
    session,
    log,
    getWalletCurrency: () => walletCurrencyRef.current,
  });
  const { currentRootDir, setCurrentRootDir, currentRootDirRef } = useWorkspaceRoot(
    codeMode?.rootDir,
  );
  const { hookList, reloadHooks } = useHookList(codeMode?.rootDir);
  // Session-scoped edit history + undo banner + /undo, /history, /show
  // handlers. Kept in a custom hook so App.tsx only sees the small API
  // it needs 闂?append an edit, arm the banner, answer the slash
  // callbacks, seal the turn entry, check whether anything's undoable.
  const {
    undoBanner,
    recordEdit,
    armUndoBanner,
    toggleUndoPause,
    codeUndo,
    codeHistory,
    codeShowEdit,
    sealCurrentEntry,
    hasUndoable,
    touchedPaths,
  } = useEditHistory(codeMode);
  const {
    pendingEdits,
    pendingCount,
    pendingTick,
    syncPendingCount,
    editMode,
    setEditMode,
    editModeRef,
    modeFlash,
  } = useEditGate(!!codeMode);
  const { preset, setPreset, proArmed, setProArmed, turnOnPro, setTurnOnPro } =
    usePresetMode(model);
  // Refs that mirror state for stable read-callbacks handed to the
  // embedded dashboard server. The server's `getXxx()` closures are
  // captured once at startDashboard time; without ref-mirrors the
  // returned values would freeze at boot. Same pattern as editModeRef.
  const planModeRef = useRef<boolean>(false);
  const latestVersionRef = useRef<string | null>(null);
  // Current per-edit confirmation prompt (review mode, tool-call path).
  // Non-null 闂?EditConfirm modal renders, interceptor is suspended on
  // `editReviewResolveRef.current`, other live rows hide. User picks a
  // choice 闂?handleEditReviewChoose resolves the promise, interceptor
  // resumes and returns the tool result the model will see.
  const [pendingEditReview, setPendingEditReview] = useState<EditBlock | null>(null);
  // /walk active flag 闂?when true the App walks pendingEdits one block
  // at a time through EditConfirm. Distinct from `pendingEditReview`,
  // which is the AUTO-mode tool-call interceptor. Walkthrough is
  // user-initiated against the QUEUED pending list, not mid-stream.
  const [walkthroughActive, setWalkthroughActive] = useState(false);
  /** Result from the EditConfirm modal: choice plus optional deny context. */
  interface EditReviewResult {
    choice: EditReviewChoice;
    denyContext?: string;
  }
  const editReviewResolveRef = useRef<((r: EditReviewResult) => void) | null>(null);
  // Per-turn override: set by "apply-rest-of-turn" so subsequent edits
  // in the SAME turn skip the modal and land like AUTO. Resets to "ask"
  // at handleSubmit entry so the next user turn starts fresh.
  const turnEditPolicyRef = useRef<"ask" | "apply-all">("ask");
  // Shell command the model asked to run that wasn't on the auto-run
  // allowlist. Non-null renders the ShellConfirm modal and disables
  // the prompt input; the user picks Run once / Always allow in this
  // project / Deny and we feed the result back as a synthetic user
  // message so the model sees what happened.
  const [pendingShell, setPendingShell] = useState<{
    id: number;
    command: string;
    kind: "run_command" | "run_background";
    cwd?: string;
    timeoutSec?: number;
    waitSec?: number;
  } | null>(null);
  /** Outside-sandbox file access the model asked for (#684). Non-null renders PathConfirm and blocks the gate behind it. */
  const [pendingPath, setPendingPath] = useState<{
    id: number;
    path: string;
    intent: "read" | "write";
    toolName: string;
    sandboxRoot: string;
    allowPrefix: string;
  } | null>(null);
  // Plan text the model submitted via `submit_plan` while plan mode
  // was active. Non-null renders PlanConfirm; user picks Approve /
  // Refine / Cancel and we drive the loop from there. Separate from
  // `planMode` because a pending plan is a one-shot decision even if
  // plan mode stays on (Refine keeps mode on; Approve/Cancel flip off).
  const [pendingPlan, setPendingPlan] = useState<string | null>(null);
  /** While the user is interactively editing the proposed plan via PlanReviseEditor; null = not editing. */
  const [pendingReviseEditor, setPendingReviseEditor] = useState<string | null>(null);
  /** True while the SessionPicker is open mid-chat (triggered by `/sessions`). */
  const [pendingSessionsPicker, setPendingSessionsPicker] = useState(false);
  const [sessionsPickerList, setSessionsPickerList] = useState<ReturnType<typeof listSessions>>(
    () => listSessionsForWorkspace(currentRootDir),
  );
  const [sessionsPickerFocus, setSessionsPickerFocus] = useState(0);
  /** True while the CheckpointPicker is open mid-chat (triggered by bare `/restore`). */
  const [pendingCheckpointPicker, setPendingCheckpointPicker] = useState(false);
  const [checkpointPickerList, setCheckpointPickerList] = useState<CheckpointMeta[]>([]);
  /** Opens the unified McpHub modal 闂?null when closed. `tab` selects the initial tab. */
  const [pendingMcpHub, setPendingMcpHub] = useState<{ tab: "live" | "marketplace" } | null>(null);
  /** True while the ModelPicker is open mid-chat (triggered by bare `/model`). */
  const [pendingModelPicker, setPendingModelPicker] = useState(false);
  /** True while the ThemePicker is open mid-chat (triggered by bare `/theme`). */
  const [pendingThemePicker, setPendingThemePicker] = useState(false);
  const [pendingCopyMode, setPendingCopyMode] = useState(false);
  // Stashed plan + intent while the user types free-form feedback
  // (refinement or last instructions on approve). When the picker
  // returns "refine" or "approve", we defer the loop-resume and show
  // PlanRefineInput. User types + Enter 闂?we ship it; Esc 闂?restore
  // pendingPlan and re-show the picker. Letting Approve also take
  // input closes the "model left open questions, user had no place
  // to answer them" hole.
  const [stagedInput, setStagedInput] = useState<{
    plan: string;
    mode: "refine" | "approve" | "reject";
    /** Open-questions / risks block extracted from the plan; surfaced in PlanRefineInput on refine. */
    questions?: string;
  } | null>(null);
  // Mid-execution pause from mark_step_complete 闂?model finished a step
  // and the loop waits for user to pick Continue / Revise / Stop.
  const [pendingCheckpoint, setPendingCheckpoint] = useState<{
    stepId: string;
    title?: string;
    completed: number;
    total: number;
  } | null>(null);
  // Staged entry for the Revise feedback input at a checkpoint.
  const [stagedCheckpointRevise, setStagedCheckpointRevise] = useState<{
    stepId: string;
    title?: string;
    completed: number;
    total: number;
  } | null>(null);
  // Plan revision proposal from `revise_plan`. Non-null mounts the
  // PlanReviseConfirm picker showing a step-level diff. Accept replaces
  // remaining steps in planStepsRef; Reject drops the proposal and the
  // model continues with the original plan.
  const [pendingRevision, setPendingRevision] = useState<{
    reason: string;
    remainingSteps: PlanStep[];
    summary?: string;
  } | null>(null);
  // Branching question from `ask_choice`. Non-null mounts ChoiceConfirm;
  // user picks an option (synthetic "user picked <id>"), types a
  // custom answer (synthetic "user answered: <text>"), or cancels.
  // Kept separate from pendingPlan because a branch question is
  // orthogonal to plan state 闂?it can fire in chat mode or mid-plan
  // when the model genuinely needs a decision.
  const [pendingChoice, setPendingChoice] = useState<{
    question: string;
    options: ChoiceOption[];
    allowCustom: boolean;
  } | null>(null);
  // Staged entry for the "Let me type my own answer" path. Same
  // two-step pattern as stagedInput for plan approvals 闂?user picks
  // "custom", we stash the question context, show a free-form input,
  // and Esc restores the picker.
  const [stagedChoiceCustom, setStagedChoiceCustom] = useState<{
    question: string;
    options: ChoiceOption[];
    allowCustom: boolean;
  } | null>(null);
  // Truthy when any pending modal owns the screen 闂?gates global
  // hotkeys (chat-scroll, etc.) so they don't fire behind a picker.
  const modalOpen =
    !!pendingShell ||
    !!pendingPlan ||
    !!pendingReviseEditor ||
    !!pendingSessionsPicker ||
    !!pendingCheckpointPicker ||
    !!pendingMcpHub ||
    pendingModelPicker ||
    pendingThemePicker ||
    pendingCopyMode ||
    !!stagedInput ||
    !!pendingEditReview ||
    walkthroughActive ||
    !!pendingChoice ||
    !!stagedChoiceCustom ||
    !!pendingRevision ||
    !!stagedCheckpointRevise ||
    !!pendingCheckpoint;
  // Truthy when the live activity rows (ongoing tool, subagent stack, thinking
  // row, plan live row) can render. Hidden whenever a take-over modal is up so
  // they don't fight the picker for visual attention. Tighter than `modalOpen` —
  // doesn't include model/theme/copy-mode pickers that overlay without owning
  // the bottom rows.
  const noTakeoverOverlay =
    !pendingShell &&
    !pendingPath &&
    !pendingPlan &&
    !pendingReviseEditor &&
    !pendingSessionsPicker &&
    !pendingCheckpointPicker &&
    !pendingMcpHub &&
    !stagedInput &&
    !pendingEditReview;
  // Plan-mode indicator 闂?displayed in the StatsPanel, mirrored onto
  // the ToolRegistry so dispatch enforces read-only. Toggled via the
  // `/plan` slash and PlanConfirm picker. Ephemeral 闂?not persisted
  // across launches (you explicitly opt in per session).
  const [planMode, setPlanMode] = useState<boolean>(false);
  // Text waiting to be submitted AFTER the current turn finishes.
  // Set by ShellConfirm's onChoose when the user approves faster than
  // the model's "awaiting confirmation" response. We can't call
  // handleSubmit directly because it early-returns on `busy === true`,
  // so we abort the in-flight turn and let the effect below fire the
  // submit once busy clears.
  const [queuedSubmit, setQueuedSubmit] = useState<string | null>(null);
  // Ctrl+P/Ctrl+N recall over a turn-local prompt history. We don't
  // persist to disk 闂?the session log already keeps the messages, and
  // cross-session bash-style recall would need per-project scoping.
  const { recallPrev, recallNext, pushHistory, resetCursor } = useInputRecall(setInput);
  const { setRawMode, isRawModeSupported } = useStdin();
  // Ctrl+X 闂?hand the composer buffer to $EDITOR. Raw-mode flip lets the
  // editor own line-buffered input; result replaces the composer value.
  const handleOpenExternalEditor = useCallback(async () => {
    if (!isRawModeSupported) {
      log.pushWarning(t("composer.editorFailed"), t("composer.editorNoRawMode"));
      return;
    }
    setRawMode(false);
    try {
      const result = await openInExternalEditor(input);
      if (result.kind === "ok") setInput(result.content);
      else if (result.detail) log.pushWarning(t("composer.editorFailed"), result.detail);
    } finally {
      setRawMode(true);
    }
  }, [input, isRawModeSupported, log, setRawMode]);
  // Disambiguates <Static> keys when a single turn yields multiple assistant_final events.
  const assistantIterCounter = useRef<number>(0);
  // Per-session @url fetch cache. Keyed by stripped URL; same URL
  // referenced twice in one session fetches once. Not persisted 闂?  // we deliberately re-fetch on session resume since the page may
  // have changed. Shape mirrors AtUrlExpansion + an optional `body`
  // so the trailing block can be reconstructed from cache alone.
  const atUrlCache = useRef<Map<string, AtUrlExpansion & { body?: string }>>(new Map());
  // handleSubmit is defined far below as a useCallback. The /loop timer
  // needs to call the LATEST closure on each firing (config could have
  // shifted mid-loop), so we mirror it through a ref. The mirror is
  // synced in a useEffect once handleSubmit is defined.
  const handleSubmitRef = useRef<((raw: string) => Promise<void>) | null>(null);
  const busyRef = useRef<boolean>(false);
  const submittingRef = useRef<boolean>(false);
  // Embedded dashboard server handle. Set when /dashboard boots; null
  // otherwise. Mutations to this ref happen inside the start/stop
  // callbacks; the slash handler uses getDashboardUrl() to surface
  // the current state without triggering re-renders on every poll.
  const dashboardRef = useRef<DashboardServerHandle | null>(null);
  // De-dupe concurrent startDashboard() invocations. Without this, when
  // the auto-start useEffect re-fires (because `startDashboard`'s
  // useCallback deps change mid-mount) the early `if (dashboardRef.current)
  // return` check sees null because the first call hasn't returned from
  // its `await startDashboardServer()` yet 闂?so we'd start two listeners
  // on two ports, leak the first handle, and make the chrome pill flicker
  // between two URLs. Hold the in-flight Promise here and reuse it.
  const dashboardStartingRef = useRef<Promise<string> | null>(null);
  // SSE subscribers attached by /api/events. App.tsx fans out one
  // DashboardEvent per loop event so the web Chat tab updates in
  // sync with the TUI. The Set is keyed by the subscriber function
  // itself; subscribeEvents returns an unsubscribe closure.
  const eventSubscribersRef = useRef<Set<(ev: DashboardEvent) => void>>(new Set());
  /** Only one picker mounts at a time; snapshot feeds `getActiveModal` for late SSE clients. */
  const activePickerResolverRef = useRef<((res: PickerResolution) => void) | null>(null);
  const activePickerSnapshotRef = useRef<PickerSnapshot | null>(null);
  /** Active read-only viewer (e.g. /replay plan archive). Same late-SSE concern, simpler resolver (close only). */
  const activeViewerResolverRef = useRef<(() => void) | null>(null);
  const activeViewerSnapshotRef = useRef<ViewerSnapshot | null>(null);
  const [pendingReplayViewer, setPendingReplayViewer] = useState<ViewerSnapshot | null>(null);
  // Structured steps captured from the most recent `submit_plan` call.
  // Populated only when the model supplied `steps`; used by the
  // `mark_step_complete` handler to look up the step title and compute
  // the `N/M` counter. Reset on every new plan submission so a
  // revised plan starts fresh 闂?old completions don't spill over.
  const planStepsRef = useRef<PlanStep[] | null>(null);
  const completedStepIdsRef = useRef<Set<string>>(new Set());
  // Markdown body + human-friendly summary captured from submit_plan.
  // Persisted alongside the structured state so a future Time-Travel
  // replay can show the model's full original proposal without re-
  // reading the JSONL log, and so /plans + the resume banner can
  // identify plans by intent rather than by filename.
  const planBodyRef = useRef<string | null>(null);
  const planSummaryRef = useRef<string | null>(null);
  // Wall-clock when the latest tool_start fired. Cleared when the
  // matching `tool` event arrives (or at turn end). Tools are
  // dispatched serially in the loop, so a single ref is enough 闂?no
  // need for a per-toolName map.
  const toolStartedAtRef = useRef<number | null>(null);
  // Persist the active plan state (steps + completedStepIds) to disk
  // whenever it changes, so closing the terminal doesn't lose
  // structured progress. The on-disk format lives in plan-store.ts;
  // we just thread the session name through and call save/clear at
  // the right points. No-op when session is undefined (e.g.
  // ephemeral runs with --no-session).
  const persistPlanState = useCallback(() => {
    if (!session) return;
    const steps = planStepsRef.current;
    if (!steps || steps.length === 0) {
      clearPlanState(session);
      return;
    }
    const extras: { body?: string; summary?: string } = {};
    if (planBodyRef.current) extras.body = planBodyRef.current;
    if (planSummaryRef.current) extras.summary = planSummaryRef.current;
    savePlanState(session, steps, completedStepIdsRef.current, extras);
  }, [session]);
  const [summary, setSummary] = useState<SessionSummary>({
    turns: 0,
    totalCostUsd: 0,
    totalInputCostUsd: 0,
    totalOutputCostUsd: 0,
    claudeEquivalentUsd: 0,
    savingsVsClaudePct: 0,
    cacheHitRatio: 0,
    lastPromptTokens: 0,
    lastTurnCostUsd: 0,
  });

  const transcriptRef = useRef<WriteStream | null>(null);
  if (transcript && !transcriptRef.current) {
    transcriptRef.current = openTranscriptFile(transcript, {
      version: 1,
      source: "reasonix chat",
      model,
      startedAt: new Date().toISOString(),
    });
  }
  // Kernel event log sidecar 闂?opens iff the session has a name (skip
  // ephemeral sessions). Sink + Eventizer share lifetime with App; the
  // for-await consumer below pipes every LoopEvent through them so a
  // typed Event log accumulates at `~/.reasonix/sessions/<name>.events.jsonl`.
  // Old transcript path is unchanged 闂?this is a parallel artifact, not
  // a replacement. Future replay / projection consumers read from here.
  const eventSinkRef = useRef<JsonlEventSink | null>(null);
  const eventizerRef = useRef<Eventizer | null>(null);
  if (session && !eventSinkRef.current) {
    eventSinkRef.current = openEventSink(eventLogPath(session));
    eventizerRef.current = new Eventizer();
    eventSinkRef.current.append(eventizerRef.current.emitSessionOpened(0, session, 0));
  }
  useEffect(() => {
    return () => {
      transcriptRef.current?.end();
      void eventSinkRef.current?.close();
    };
  }, []);

  const loopRef = useRef<CacheFirstLoop | null>(null);
  // hookList + currentRootDir intentionally NOT in deps 闂?they seed
  // the loop on first construction (loopRef guards a single
  // instantiation), and later edits flow in through the mutable
  // `loop.hooks = hookList` / `loop.hookCwd = currentRootDir` effects
  // below. Putting them in deps would tear down the loop on every
  // reload, wiping the append-only log mid-session.
  // biome-ignore lint/correctness/useExhaustiveDependencies: hookList 闂?see comment above
  // biome-ignore lint/correctness/useExhaustiveDependencies: currentRootDir 闂?see comment above
  const loop = useMemo(() => {
    if (loopRef.current) return loopRef.current;
    const client = new DeepSeekClient({ baseUrl: loadBaseUrl() });
    // Register run_skill HERE (not in code.tsx / chat.tsx) because
    // subagent-runAs skills need the client + parent registry to
    // spawn child loops. Wiring lives in App.tsx so the same code
    // path covers both code mode and chat mode.
    //
    // The closure captures `tools` (parent registry), `client`, and
    // the subagent sink ref by lexical scope 闂?`spawnSubagent` reads
    // them per invocation, so a sink handler attached after this
    // registration still receives events.
    if (tools && !tools.has("run_skill")) {
      registerSkillTools(tools, {
        projectRoot: codeMode?.rootDir,
        subagentRunner: async (skill, task, signal) => {
          const result = await spawnSubagent({
            client,
            parentRegistry: tools,
            parentSignal: signal,
            // Skill body is the subagent's persona/playbook; the user-
            // supplied task is what to actually do inside it.
            system: skill.body,
            task,
            // Per-skill model override (frontmatter `model: ...`),
            // else falls through to spawnSubagent's default.
            model: skill.model,
            allowedTools: skill.allowedTools,
            sink: subagentSinkRef.current,
            // Stamped onto every event so the TUI sink + usage log can
            // attribute the run to a skill without extra bookkeeping.
            skillName: skill.name,
            maxToolIters: skill.maxToolIters,
          });
          return formatSubagentResult(result);
        },
      });
    }
    const prefix = new ImmutablePrefix({
      system,
      toolSpecs: tools?.specs(),
    });
    const l = new CacheFirstLoop({
      client,
      prefix,
      tools,
      model,
      budgetUsd,
      failureThreshold,
      session,
      hooks: hookList,
      hookCwd: currentRootDir,
      // Restore the user's last-chosen effort cap. Without this a
      // `/effort high` silently reverted to `max` on relaunch 闂?the
      // loop's constructor default wins over persisted state.
      reasoningEffort: loadReasoningEffort(),
      rebuildSystem,
    });
    loopRef.current = l;
    return l;
  }, [model, system, rebuildSystem, budgetUsd, failureThreshold, session, tools, codeMode]);

  useEffect(() => {
    if (!session || !tools) return;
    tools.setAuditListener((event) => {
      const sink = eventSinkRef.current;
      const eventizer = eventizerRef.current;
      if (!sink || !eventizer) return;
      sink.append(eventizer.emitToolCall(loop.currentTurn, event.name, event.args));
    });
    pauseGate.setAuditListener((event) => {
      const sink = eventSinkRef.current;
      const eventizer = eventizerRef.current;
      if (!sink || !eventizer) return;
      switch (event.type) {
        case "tool.confirm.allow":
          sink.append(eventizer.emitToolConfirmAllow(loop.currentTurn, event.kind, event.payload));
          break;
        case "tool.confirm.deny":
          sink.append(
            eventizer.emitToolConfirmDeny(
              loop.currentTurn,
              event.kind,
              event.payload,
              event.denyContext,
            ),
          );
          break;
        case "tool.confirm.always_allow":
          sink.append(
            eventizer.emitToolConfirmAlwaysAllow(
              loop.currentTurn,
              event.kind,
              event.payload,
              event.prefix,
            ),
          );
          break;
      }
    });
    return () => {
      tools.setAuditListener(null);
      pauseGate.setAuditListener(null);
    };
  }, [loop, session, tools]);

  // Keep the loop's hook list in sync after a `/hooks reload`. The
  // loop's field is intentionally mutable for exactly this case 闂?  // construction happens once, hook edits are picked up live.
  useEffect(() => {
    loop.hooks = hookList;
  }, [loop, hookList]);

  // Seed status.preset from initial loop state so the StatusRow preset pill
  // renders correctly on first paint 闂?usePresetMode's React-state mirror
  // doesn't propagate to the agent store, so without this dispatch the pill
  // would show the bare model id instead of the resolved preset.
  // biome-ignore lint/correctness/useExhaustiveDependencies: mount-only seed
  useEffect(() => {
    const canonical: "auto" | "flash" | "pro" | null =
      loop.model === "deepseek-v4-pro"
        ? "pro"
        : loop.model === "deepseek-v4-flash"
          ? loop.autoEscalate
            ? "auto"
            : "flash"
          : null;
    agentStore.dispatch({ type: "session.preset.change", preset: canonical });
  }, []);

  // Deferred MCP bridge 闂?fire addSpec for each requested server in the
  // background instead of blocking startup, route lifecycle events to
  // the in-app log so they don't corrupt alt-screen via stderr.
  const mcpBridgeStartedRef = useRef(false);
  useEffect(() => {
    if (mcpBridgeStartedRef.current) return;
    if (!mcpRuntime || !mcpSpecs || mcpSpecs.length === 0) return;
    mcpBridgeStartedRef.current = true;
    const total = mcpSpecs.length;
    let ready = 0;
    agentStore.dispatch({ type: "mcp.loading", ready, total });
    const bumpReady = () => {
      ready = Math.min(ready + 1, total);
      agentStore.dispatch({ type: "mcp.loading", ready, total });
    };
    mcpRuntime.setLifecycleSink((notice) => {
      if (notice.kind === "handshake") {
        log.pushInfo(formatMcpLifecycleEvent({ state: "handshake", name: notice.name }));
      } else if (notice.kind === "connected") {
        log.pushInfo(
          formatMcpLifecycleEvent({
            state: "connected",
            name: notice.name,
            tools: notice.tools,
            resources: notice.resources,
            prompts: notice.prompts,
            ms: notice.ms,
          }),
        );
        bumpReady();
      } else if (notice.kind === "disabled") {
        log.pushInfo(formatMcpLifecycleEvent({ state: "disabled", name: notice.name }));
        bumpReady();
      } else if (notice.kind === "failed") {
        log.pushWarning(
          `MCP 闂?${notice.name} failed`,
          `${notice.reason}\n闂?run \`reasonix setup\` to remove this entry, or fix the underlying issue (missing npm package, network, etc.).`,
        );
        bumpReady();
      } else if (notice.kind === "slow") {
        log.pushInfo(
          formatMcpSlowToast({
            name: notice.serverName,
            p95Ms: notice.p95Ms,
            sampleSize: notice.sampleSize,
          }),
        );
      }
    });
    for (const spec of mcpSpecs) {
      void mcpRuntime.addSpec(spec, loop).then(() => {
        setLiveMcpServers(mcpRuntime.summaries());
      });
    }
  }, [mcpRuntime, mcpSpecs, loop, log, agentStore]);

  // Ambient session info (balance, model catalog, latest published
  // version) 闂?three independent mount-time fetches behind one hook
  // so the refresh callbacks can be wired into handleSubmit's finally
  // (balance) and the slash context (/models, /update).
  const { balance, models, latestVersion, refreshBalance, refreshModels, refreshLatestVersion } =
    useSessionInfo(loop);

  // Keep the dashboard-server ref-mirrors in sync with their state.
  // These four are the load-bearing live reads for the attached
  // dashboard's read APIs; without these mirrors the captured
  // closures inside startDashboardServer freeze at boot time.
  useEffect(() => {
    planModeRef.current = planMode;
  }, [planMode]);

  useEffect(() => {
    latestVersionRef.current = latestVersion ?? null;
  }, [latestVersion]);
  // Ref-mirror so getStats() (frozen at startDashboard time) sees fresh
  // balance. useSessionInfo refreshes balance every few minutes; we
  // forward to the dashboard without re-minting startDashboard.
  const balanceRef = useRef<typeof balance>(null);
  const modelsRef = useRef<string[] | null>(null);
  useEffect(() => {
    modelsRef.current = models;
  }, [models]);
  useEffect(() => {
    balanceRef.current = balance;
    walletCurrencyRef.current = balance?.currency;
    if (balance) {
      agentStore.dispatch({
        type: "session.update",
        patch: { balance: balance.total, balanceCurrency: balance.currency },
      });
    }
  }, [balance, agentStore]);

  // Fan out a DashboardEvent to every web subscriber. No-op when
  // nothing is connected, so the cost of the bridge in the common
  // (no dashboard open) case is one Set.size lookup per event.
  const broadcastDashboardEvent = useCallback((ev: DashboardEvent) => {
    const subs = eventSubscribersRef.current;
    if (subs.size === 0) return;
    for (const h of subs) {
      try {
        h(ev);
      } catch {
        /* one bad subscriber must not stop the others */
      }
    }
  }, []);
  const pickerPorts = useMemo(
    () => ({
      broadcast: broadcastDashboardEvent,
      resolverRef: activePickerResolverRef,
      snapshotRef: activePickerSnapshotRef,
    }),
    [broadcastDashboardEvent],
  );
  const viewerPorts = useMemo(
    () => ({
      broadcast: broadcastDashboardEvent,
      resolverRef: activeViewerResolverRef,
      snapshotRef: activeViewerSnapshotRef,
    }),
    [broadcastDashboardEvent],
  );
  useViewerBroadcast(
    !!pendingReplayViewer,
    pendingReplayViewer ?? { viewerKind: "replay-plan", title: "" },
    () => setPendingReplayViewer(null),
    viewerPorts,
  );

  // Broadcast busy-state changes so the web Chat tab can disable its
  // submit button while a turn is in flight. Mirrors what the TUI's
  // `busy` flag already drives for PromptInput.
  useEffect(() => {
    broadcastDashboardEvent({ kind: "busy-change", busy });
  }, [busy, broadcastDashboardEvent]);

  // ---------- Modal mirroring (web parity for ShellConfirm / ChoiceConfirm /
  // PlanConfirm / EditConfirm) ----------
  //
  // Each pending* state is the source of truth on the TUI side. These
  // effects fan it out to web subscribers as `modal-up` events; the
  // useEffect cleanup fires `modal-down` when the modal closes (the
  // user picked from EITHER surface 闂?once a pending state goes null
  // the cleanup runs and both clients see it disappear).
  //
  // The shell + choice + plan paths are straightforward state闂傚倸鍊搁崐鎼佸磹閹间礁纾归柟闂寸绾惧綊鏌熼梻瀵割槮缁炬儳缍婇弻鐔兼⒒鐎靛壊妲紒鐐劤缂嶅﹪寮婚悢鍏尖拻閻庨潧澹婂Σ顔剧磼閻愵剙鍔ょ紓宥咃躬瀵鏁愭径濠勵吅闂佹寧绻傞幉娑㈠箻缂佹鍘辨繝鐢靛Т閸婂綊宕戦妷鈺傜厸閻忕偠顕ф慨鍌溾偓娈垮枟閹告娊骞冨▎寰濆湱鈧綆浜欐竟鏇㈡⒑閸涘﹦缂氶柛搴㈠▕閹矂宕卞☉娆戝幈濡炪倖鍔戦崐鏇㈠几閹达附鐓曟繛鍡楃箳缁犳彃菐閸パ嶈含妞ゃ垺绋戦埥澶婎潨閸℃鐤傜紓鍌欒兌閾忓酣宕㈤崗鍏兼珷濞寸姴顑嗛崕澶嬨亜韫囨挾澧曠紒鐘虫皑閹茬顭ㄩ崼鐔蜂簵婵犻潧鍊搁幉锟犳偂濞戙垺鍊堕柣鎰絻閳锋棃鏌嶉挊澶樻█闁哄苯绉归幐濠冨緞濡儵鏋呴柣搴ゎ潐濞测晝鎹㈠┑瀣畺婵炲棙鍨熼崑鎾绘偨閸涘﹥鐝抽梺瑙勭叀缁犳牕顫忔繝姘＜婵炲棙鍩堝Σ顕€姊虹涵鍜佸殝缂佺粯绻堥獮鍐倻閽樺）銊ф喐鎼达紕鏆﹂柛娆忣槹閸欏繑淇婇悙棰濆殭濞存粓绠栭幃妤€鈻撻崹顔界仌濡炪倖娉﹂崶鑸垫櫍婵犻潧鍊婚…鍫ユ煁閸ャ劊浜滈柟鎷屾硾瀵攱銇勯幒鎴濐仾闁抽攱甯掗湁闁挎繂娲ら崝瀣煕閵堝倸浜鹃梺璇插椤旀牠宕伴弽顓熷亯濠靛倻顭堢粻姘扁偓鍏夊亾闁告洦鍓欐禍顖涚節?
  // edit-review is different 闂?its source of truth is `editReviewResolveRef`
  // (a promise the dispatch interceptor is awaiting), wired via a
  // separate `pendingEditReview` state that we already broadcast here.

  useEffect(() => {
    if (!pendingShell) return;
    const modal: ActiveModal = {
      kind: "shell",
      command: pendingShell.command,
      allowPrefix: derivePrefix(pendingShell.command),
      shellKind: pendingShell.kind,
    };
    broadcastDashboardEvent({ kind: "modal-up", modal });
    return () => {
      broadcastDashboardEvent({ kind: "modal-down", modalKind: "shell" });
    };
  }, [pendingShell, broadcastDashboardEvent]);

  useEffect(() => {
    if (!pendingChoice) return;
    const modal: ActiveModal = {
      kind: "choice",
      question: pendingChoice.question,
      options: pendingChoice.options,
      allowCustom: pendingChoice.allowCustom,
    };
    broadcastDashboardEvent({ kind: "modal-up", modal });
    return () => {
      broadcastDashboardEvent({ kind: "modal-down", modalKind: "choice" });
    };
  }, [pendingChoice, broadcastDashboardEvent]);

  useEffect(() => {
    if (!pendingPlan) return;
    broadcastDashboardEvent({
      kind: "modal-up",
      modal: { kind: "plan", body: pendingPlan },
    });
    return () => {
      broadcastDashboardEvent({ kind: "modal-down", modalKind: "plan" });
    };
  }, [pendingPlan, broadcastDashboardEvent]);

  useEffect(() => {
    if (!pendingEditReview) return;
    // Trim the preview 闂?older clients only render this string; newer
    // clients use `search`/`replace` directly to render a side-by-side
    // diff with syntax highlighting (full content, no line cap).
    const previewLines = (pendingEditReview.search || pendingEditReview.replace || "")
      .split("\n")
      .slice(0, 12);
    const preview = previewLines.join("\n");
    broadcastDashboardEvent({
      kind: "modal-up",
      modal: {
        kind: "edit-review",
        path: pendingEditReview.path,
        search: pendingEditReview.search ?? "",
        replace: pendingEditReview.replace ?? "",
        preview,
        total: pendingEdits.current.length,
        remaining: pendingEdits.current.length,
      },
    });
    return () => {
      broadcastDashboardEvent({ kind: "modal-down", modalKind: "edit-review" });
    };
  }, [pendingEditReview, broadcastDashboardEvent, pendingEdits]);

  useEffect(() => {
    if (!pendingRevision) return;
    broadcastDashboardEvent({
      kind: "modal-up",
      modal: {
        kind: "revision",
        reason: pendingRevision.reason,
        remainingSteps: pendingRevision.remainingSteps.map((s) => ({
          id: s.id,
          title: s.title,
          action: s.action,
          ...(s.risk ? { risk: s.risk } : {}),
        })),
        ...(pendingRevision.summary ? { summary: pendingRevision.summary } : {}),
      },
    });
    return () => {
      broadcastDashboardEvent({ kind: "modal-down", modalKind: "revision" });
    };
  }, [pendingRevision, broadcastDashboardEvent]);

  useEffect(() => {
    if (!pendingCheckpoint) return;
    broadcastDashboardEvent({
      kind: "modal-up",
      modal: {
        kind: "checkpoint",
        stepId: pendingCheckpoint.stepId,
        ...(pendingCheckpoint.title ? { title: pendingCheckpoint.title } : {}),
        completed: pendingCheckpoint.completed,
        total: pendingCheckpoint.total,
      },
    });
    return () => {
      broadcastDashboardEvent({ kind: "modal-down", modalKind: "checkpoint" });
    };
  }, [pendingCheckpoint, broadcastDashboardEvent]);

  // Three mutually-exclusive input-prefix pickers (slash name, @ file
  // mention, slash argument) 闂?state + memos + commit callbacks live
  // in a dedicated hook so App.tsx only sees the small surface it
  // actually consumes in useInput / handleSubmit / render. Declared
  // after useSessionInfo because the slash-arg picker reads the model
  // catalog for `/model <partial>` completion.
  const {
    slashMatches,
    slashSelected,
    setSlashSelected,
    slashGroupMode,
    slashAdvancedHidden,
    atState,
    atSelected,
    setAtSelected,
    pickAtMention,
    recordRecentFile,
    slashArgContext,
    slashArgMatches,
    slashArgSelected,
    setSlashArgSelected,
    pickSlashArg,
  } = useCompletionPickers({
    input,
    setInput,
    codeMode,
    rootDir: currentRootDir,
    models,
    mcpServers: liveMcpServers,
    slashUsage,
  });

  const slashMatchesJson = useMemo(() => {
    if (!slashMatches || slashMatches.length === 0) return undefined;
    return JSON.stringify(
      slashMatches.map((m) => ({
        cmd: m.cmd,
        summary: m.summary,
        ...(m.argsHint !== undefined ? { argsHint: m.argsHint } : {}),
      })),
    );
  }, [slashMatches]);

  useEffect(() => {
    setSessionsPickerList(listSessionsForWorkspace(currentRootDir));
  }, [currentRootDir]);

  const sessionsJson = useMemo(() => {
    if (!pendingSessionsPicker || sessionsPickerList.length === 0) return undefined;
    return JSON.stringify(
      sessionsPickerList.map((s) => {
        const branch = s.meta.branch ?? "main";
        const turns = s.meta.turnCount ?? Math.ceil(s.messageCount / 2);
        return { title: s.name, meta: `${branch} · ${turns} turns` };
      }),
    );
  }, [pendingSessionsPicker, sessionsPickerList]);

  const sidebarSessionsJson = useMemo(() => {
    if (sessionsPickerList.length === 0) return undefined;
    return JSON.stringify(
      sessionsPickerList.map((s) => ({
        title: s.name,
        meta: s.meta.branch ?? "main",
      })),
    );
  }, [sessionsPickerList]);

  const sceneApproval = useMemo(() => {
    if (pendingShell) return { kind: "shell", prompt: pendingShell.command };
    if (pendingPath) return { kind: "path", prompt: `${pendingPath.intent} ${pendingPath.path}` };
    if (pendingEditReview) return { kind: "edit", prompt: `review ${pendingEditReview.path}` };
    if (pendingChoice) return { kind: "choice", prompt: pendingChoice.question };
    if (pendingPlan) return { kind: "plan", prompt: "approve plan" };
    return null;
  }, [pendingShell, pendingPath, pendingEditReview, pendingChoice, pendingPlan]);

  useSceneTrace({
    cardCount,
    busy,
    activity: activityLabel,
    model,
    recentCardsJson,
    composerText: input,
    composerCursor,
    slashMatchesJson,
    slashSelectedIndex: slashMatchesJson ? slashSelected : undefined,
    approvalKind: sceneApproval?.kind,
    approvalPrompt: sceneApproval?.prompt,
    sessionsJson,
    sessionsFocusedIndex: sessionsJson ? sessionsPickerFocus : undefined,
    walletBalance: balance?.total,
    walletCurrency: balance?.currency,
    sidebarSessionsJson,
    sidebarActiveSession: session ?? undefined,
    mcpServerCount: liveMcpServers.length,
    editMode,
    cwd: currentRootDir,
  });

  // Ctrl+P / Ctrl+N from PromptInput route here. When any input-prefix
  // picker is open (slash / @ / slash-arg), the keys navigate that picker
  // 闂?consistent with 闂?闂? Otherwise they walk prompt history (issue #647).
  const handleHistoryPrev = useCallback(() => {
    if (atState && atState.entries.length > 0) {
      setAtSelected((i) => Math.max(0, i - 1));
      return;
    }
    if (slashArgMatches && slashArgMatches.length > 0) {
      setSlashArgSelected((i) => Math.max(0, i - 1));
      return;
    }
    if (slashMatches && slashMatches.length > 0) {
      setSlashSelected((i) => Math.max(0, i - 1));
      return;
    }
    recallPrev();
  }, [
    atState,
    slashArgMatches,
    slashMatches,
    setAtSelected,
    setSlashArgSelected,
    setSlashSelected,
    recallPrev,
  ]);
  const handleHistoryNext = useCallback(() => {
    if (atState && atState.entries.length > 0) {
      setAtSelected((i) => Math.min(atState.entries.length - 1, i + 1));
      return;
    }
    if (slashArgMatches && slashArgMatches.length > 0) {
      setSlashArgSelected((i) => Math.min(slashArgMatches.length - 1, i + 1));
      return;
    }
    if (slashMatches && slashMatches.length > 0) {
      setSlashSelected((i) => Math.min(slashMatches.length - 1, i + 1));
      return;
    }
    recallNext();
  }, [
    atState,
    slashArgMatches,
    slashMatches,
    setAtSelected,
    setSlashArgSelected,
    setSlashSelected,
    recallNext,
  ]);

  // Surface a one-time banner about session state on first mount.
  const sessionBannerShown = useRef(false);
  useEffect(() => {
    if (sessionBannerShown.current) return;
    sessionBannerShown.current = true;
    if (!session) {
      log.pushInfo(t("ui.ephemeralSession"));
    } else if (loop.resumedMessageCount > 0) {
      log.pushInfo(t("ui.resumedSession", { name: session, count: loop.resumedMessageCount }));
    } else {
      log.pushInfo(t("ui.newSession", { name: session }));
    }
    for (const hint of startupInfoHints ?? []) log.pushInfo(hint);
    // Restore any pending edit queue from a prior run that was
    // interrupted before /apply or /discard. The checkpoint file sits
    // next to the session log; if present, we re-populate pendingEdits
    // and post an info row so the user knows what's waiting.
    if (session && codeMode) {
      const restored = loadPendingEdits(session);
      if (restored && restored.length > 0) {
        pendingEdits.current = restored;
        syncPendingCount();
        log.pushInfo(t("ui.restoredEdits", { count: restored.length }));
      }
    }
    // Restore structured plan state from a prior run. plan.json sits
    // next to the session JSONL; if present, populate planStepsRef +
    // completedStepIdsRef and post an info row showing how far along
    // the plan was. Pure-markdown plans don't persist (nothing to
    // restore), so users see this banner only when there's real
    // structured state to pick back up.
    // Guard: skip restoration when the session has zero prior messages
    // (truly fresh). A stale plan file from a prior wipe that wasn't
    // cleaned up is not a real plan to resume 闂?it's a sidecar orphan.
    if (session && loop.resumedMessageCount > 0) {
      const restoredPlan = loadPlanState(session);
      if (restoredPlan && restoredPlan.steps.length > 0) {
        planStepsRef.current = restoredPlan.steps;
        completedStepIdsRef.current = new Set(restoredPlan.completedStepIds);
        planBodyRef.current = restoredPlan.body ?? null;
        planSummaryRef.current = restoredPlan.summary ?? null;
        const when = relativeTime(restoredPlan.updatedAt);
        const done = new Set(restoredPlan.completedStepIds);
        const summary = restoredPlan.summary ? ` 闂?${restoredPlan.summary}` : "";
        log.showPlan({
          title: t("ui.resumedPlan", { when, summary }),
          steps: restoredPlan.steps.map((s) => ({
            id: s.id,
            title: s.title,
            status: done.has(s.id) ? "done" : "queued",
          })),
          variant: "resumed",
        });
      }
    }
    // One-time onboarding tip for the edit-gate keybindings. New users
    // wouldn't otherwise discover Shift+Tab (it's in /keys and the
    // bottom status bar, but both require looking). Shown exactly once
    // per install; the config flag suppresses re-display on every
    // relaunch. Skips chat mode 闂?those shortcuts don't apply there.
    if (codeMode && !editModeHintShown()) {
      const tip = tObj<{
        topic: string;
        sections: ReadonlyArray<{ rows: ReadonlyArray<{ key: string; text: string }> }>;
        footer: string;
      }>("ui.tipEditBindings");
      log.pushTip({ topic: tip.topic, sections: tip.sections, footer: tip.footer });
      markEditModeHintShown();
    }
    if (!mouseClipboardHintShown()) {
      const tip = tObj<{
        topic: string;
        sections: ReadonlyArray<{ rows: ReadonlyArray<{ key: string; text: string }> }>;
        footer: string;
      }>("ui.tipMouseClipboard");
      log.pushTip({ topic: tip.topic, sections: tip.sections, footer: tip.footer });
      markMouseClipboardHintShown();
    }
  }, [session, loop, codeMode, syncPendingCount, log, pendingEdits, startupInfoHints]);

  // Esc handles "abort the current turn" separately; Ctrl+C is the universal "I'm done" key.
  const quitProcess = useQuit(transcriptRef);

  // 闂?闂?PgUp/PgDn always scroll chat; wheel arrives as 闂?闂?via
  // DECSET 1007 alternate-scroll so it joins the same path. Pickers
  // (slash / @-mention / slash-arg / shell-confirm) own 闂?闂?闂?when
  // any of them is open we skip the arrow path so chat doesn't scroll
  // alongside picker navigation; PgUp/PgDn/End still scroll. Prompt
  // history + multi-line cursor moves live on Ctrl+P / Ctrl+N.
  useKeystroke((ev) => {
    const pickerOwnsArrows =
      (atState?.entries.length ?? 0) > 0 ||
      (slashMatches?.length ?? 0) > 0 ||
      (slashArgMatches?.length ?? 0) > 0 ||
      pendingShell != null ||
      pendingPath != null;
    if (ev.pageUp || ev.mouseScrollUp) chatScroll.scrollPageUp();
    else if (ev.pageDown || ev.mouseScrollDown) chatScroll.scrollPageDown();
    else if (ev.end) chatScroll.jumpToBottom();
    else if (!pickerOwnsArrows && ev.upArrow) chatScroll.scrollUp();
    else if (!pickerOwnsArrows && ev.downArrow) chatScroll.scrollDown();
  }, !modalOpen);

  // Esc during busy 闂?forward to the loop as an abort signal. The loop
  // finishes the tool call in flight (we can't kill subprocess stdio
  // mid-write), then diverts to its no-tools summary path so the user
  // gets an answer instead of a hard stop. Only listens while busy so
  // we don't accidentally hijack Esc in other contexts.
  //
  // Prompt history (Ctrl+P/Ctrl+N) is handed off from PromptInput via
  // recallPrev/recallNext below 闂?parent-level useInput is simpler
  // than ink-text-input's (absent) history support and lets us own
  // the cursor semantics.
  useKeystroke((ev) => {
    // PromptInput consumes its own keystrokes via useKeystroke too,
    // so events fan out to both this handler and PromptInput's. The
    // global hotkeys here only fire when the relevant condition
    // (busy / codeMode / etc.) holds, otherwise they no-op and let
    // PromptInput own the key.
    const chKey = ev.input;
    const key = ev;
    if (ev.paste) {
      // Paste content goes only to PromptInput. Don't run global
      // hotkey logic over it (a `\n` in paste shouldn't fire submit).
      return;
    }
    if (key.ctrl && key.input === "c") {
      quitProcess();
      return;
    }
    if (key.escape && busy) {
      if (abortedThisTurn.current) return;
      abortedThisTurn.current = true;
      // Flush every pending modal + cancel the awaiting tool fn behind
      // it. pauseGate.ask doesn't watch AbortSignal, so without this a
      // plan_checkpoint / plan_proposed / choice / shell modal would
      // strand its tool fn and busy would never clear.
      resetPendingModals();
      // Esc during a busy turn also kills any active /loop 闂?the user
      // is taking over. Loops persist past plain Esc when the system is
      // idle so a long-cadence loop doesn't die from random key noise.
      if (isLoopActive()) stopLoop();
      loop.abort();
      return;
    }
    // Esc when idle ALSO cancels an active loop, since hitting Esc with
    // nothing else going on is a clear "stop whatever's running"
    // gesture. No-op when no loop is active.
    if (key.escape && !busy && isLoopActive()) {
      stopLoop();
      return;
    }
    // Esc dismisses any composer-level picker (slash / @ / slash-arg)
    // by clearing the prefix that triggered it. Picker footers advertise
    // "esc cancel" 闂?this binds it.
    if (key.escape && !busy && (slashMatches || atState || slashArgContext)) {
      setInput("");
      return;
    }
    // Esc inside a /walk session exits the walk WITHOUT applying or
    // discarding the current block 闂?remaining edits stay queued so
    // the user can resume via /walk or commit via /apply later.
    if (key.escape && walkthroughActive) {
      setWalkthroughActive(false);
      const remaining = pendingEdits.current.length;
      log.pushInfo(
        remaining > 0
          ? t("app.walkCancelledRemaining", { count: remaining })
          : t("app.walkCancelled"),
      );
      return;
    }
    // Edit-mode cycle: Shift+Tab flips review 闂?auto. Available any
    // time a modal isn't up 闂?including mid-turn 闂?so the user can
    // switch gears without abandoning the in-flight request. Prefer
    // this to typing `/mode <x>`; one keystroke, no command parsing.
    if (
      codeMode &&
      key.shift &&
      key.tab &&
      !pendingShell &&
      !pendingPath &&
      !pendingPlan &&
      !pendingReviseEditor &&
      !pendingSessionsPicker &&
      !pendingCheckpointPicker &&
      !pendingMcpHub &&
      !stagedInput &&
      !pendingEditReview &&
      !walkthroughActive &&
      !pendingChoice &&
      !stagedChoiceCustom &&
      !pendingRevision
    ) {
      // Three-stop cycle: review 闂?auto 闂?yolo 闂?review. yolo also
      // disables shell confirmations so true zero-prompt iteration takes two Shift+Tabs from default.
      const cur = editModeRef.current;
      const next: EditMode = cur === "review" ? "auto" : cur === "auto" ? "yolo" : "review";
      setEditMode(next);
      const message =
        next === "yolo"
          ? t("app.editModeYolo")
          : next === "auto"
            ? t("app.editModeAuto")
            : t("app.editModeReview");
      log.pushInfo(message);
      return;
    }
    // Undo banner keybind: `u` rolls back the last auto-apply. Gated
    // on an empty prompt buffer so typing "user" into the input doesn't
    // steal from the first keystroke. 5-second window; after that the
    // banner self-dismisses and /undo remains the only path.
    if (
      codeMode &&
      input.length === 0 &&
      (chKey === "u" || chKey === "U") &&
      !pendingShell &&
      !pendingPath &&
      !pendingPlan &&
      !pendingReviseEditor &&
      !pendingSessionsPicker &&
      !pendingCheckpointPicker &&
      !pendingMcpHub &&
      !stagedInput &&
      !pendingEditReview &&
      !walkthroughActive &&
      !pendingChoice &&
      !stagedChoiceCustom &&
      !pendingRevision &&
      // Fire when EITHER the banner is up OR there's any non-undone
      // history entry 闂?the keybind is useful long after the 5-second
      // banner expires, which users rightly want.
      (undoBanner || hasUndoable())
    ) {
      const out = codeUndo([]);
      log.pushInfo(out);
      return;
    }
    // Space toggles pause on the active undo countdown. Same gating as
    // the `u` keybind so typing in the prompt isn't intercepted.
    if (
      codeMode &&
      input.length === 0 &&
      chKey === " " &&
      undoBanner &&
      !pendingShell &&
      !pendingPath &&
      !pendingPlan &&
      !pendingReviseEditor &&
      !pendingSessionsPicker &&
      !pendingCheckpointPicker &&
      !pendingMcpHub &&
      !stagedInput &&
      !pendingEditReview &&
      !walkthroughActive &&
      !pendingChoice &&
      !stagedChoiceCustom &&
      !pendingRevision
    ) {
      toggleUndoPause();
      return;
    }
    // Ctrl-O toggles full-tail view on the live streaming reply so a long
    // plan / todo can be read while it's still being written. Resets at
    // turn end so each new reply starts collapsed.
    if (
      key.ctrl &&
      key.input === "o" &&
      isStreaming &&
      !pendingShell &&
      !pendingPath &&
      !pendingPlan &&
      !pendingReviseEditor &&
      !pendingSessionsPicker &&
      !pendingCheckpointPicker &&
      !pendingMcpHub &&
      !stagedInput &&
      !pendingEditReview &&
      !walkthroughActive &&
      !pendingChoice &&
      !stagedChoiceCustom &&
      !pendingRevision
    ) {
      setLiveExpand((v) => !v);
      return;
    }
    if (busy) return;
    // ShellConfirm owns the full keyboard while it's showing. If we
    // kept handling 闂?闂?/ Tab here they'd race with its SingleSelect
    // 闂?the picker would move AND history recall would fire into the
    // (hidden) prompt buffer. Bail early.
    if (pendingShell || pendingPath) return;

    // @-mention picker takes the same priority tier as slash. 闂?闂?walk
    // the list; Tab on a folder drills into it, Tab on a file commits.
    // Enter is caught in handleSubmit. Right arrow stays cursor-move
    // (would otherwise fight PromptInput's multiline cursor). Must come
    // BEFORE slash so the two pickers don't share arrow keys.
    if (atState && atState.entries.length > 0) {
      const entries = atState.entries;
      if (key.upArrow) {
        setAtSelected((i) => Math.max(0, i - 1));
        return;
      }
      if (key.downArrow) {
        setAtSelected((i) => Math.min(entries.length - 1, i + 1));
        return;
      }
      if (key.tab) {
        const sel = entries[atSelected] ?? entries[0];
        if (sel) pickAtMention(sel, sel.isDir ? "drill" : "commit");
        return;
      }
    }

    // Slash-argument picker. Fires inside `/<cmd> <partial>` 闂?either
    // a file picker (for /edit), enum picker (for /preset, /model,
    // /plan, /branch, /harvest), or hint-only row. Navigation + Tab
    // substitute the highlighted value at the arg's offset.
    if (slashArgMatches && slashArgMatches.length > 0) {
      if (key.upArrow) {
        setSlashArgSelected((i) => Math.max(0, i - 1));
        return;
      }
      if (key.downArrow) {
        setSlashArgSelected((i) => Math.min(slashArgMatches.length - 1, i + 1));
        return;
      }
      if (key.tab) {
        const sel = slashArgMatches[slashArgSelected] ?? slashArgMatches[0];
        if (sel) pickSlashArg(sel);
        return;
      }
    }

    // Slash-suggestion mode takes priority over history recall.
    // When the user is typing a `/闂傚倸鍊搁崐鎼佸磹閹间礁纾归柟闂寸绾惧綊鏌熼梻瀵割槮缁炬儳缍婇弻鐔兼⒒鐎靛壊妲紒鐐劤缂嶅﹪寮婚悢鍏尖拻閻庨潧澹婂Σ顔剧磼閻愵剙鍔ょ紓宥咃躬瀵鏁愭径濠勵吅闂佹寧绻傞幉娑㈠箻缂佹鍘遍梺闈涚墕閹冲酣顢旈銏＄厸閻忕偛澧藉ú瀛樸亜閵忊剝绀嬮柡浣瑰姍瀹曞崬鈻庡Ο鎭嶆氨绱撻崒娆掑厡闁稿鎹囧畷鏇㈠箮鐟欙絺鍋撻敃鍌涘€婚柦妯侯槼閹芥洟姊洪崫鍕窛闁哥姵鎸剧划缁樸偅閸愨晝鍘介梺閫涘嵆濞佳勬櫠椤栫偞鐓熸繝闈涙处閳锋帞绱掓潏銊ユ诞闁诡喗鐟╅、妤呭焵椤掑嫬绀夐柕鍫濇缁犲墽鐥銏╂缂佲檧鍋撻柣搴㈩問閸犳盯顢氳閸┿儲寰勬繛銏㈠枛閺屻劎鈧綆鍋呭鎴︽⒒閸屾瑨鍏岀痪顓炵埣瀹曟粌鈹戠€ｃ劉鍋撻崘顓犵杸闁哄啫鍋嗗ù鍕⒒娓氬洤澧紒澶屾暬閸?prefix and there are matches,
    // 闂?闂?walk the suggestion list and Tab snaps the input to the
    // highlighted command. Enter is handled in `handleSubmit` so
    // TextInput's onSubmit still fires cleanly.
    if (slashMatches && slashMatches.length > 0) {
      if (key.upArrow) {
        setSlashSelected((i) => Math.max(0, i - 1));
        return;
      }
      if (key.downArrow) {
        setSlashSelected((i) => Math.min(slashMatches.length - 1, i + 1));
        return;
      }
      if (key.tab) {
        const sel = slashMatches[slashSelected] ?? slashMatches[0];
        if (sel) setInput(`/${sel.cmd}`);
        return;
      }
    }

    // Prompt history is now Ctrl+P / Ctrl+N (PromptInput 闂?multiline
    // keys 闂?historyHandoff 闂?recallPrev / recallNext below). 闂?闂?are
    // reserved for chat scroll 闂?without that move, native drag-select
    // and right-click paste don't work on most terminals because we'd
    // have to keep xterm mouse tracking on to grab the wheel.
  });

  // Edit-gate interceptor. Reroutes `edit_file` / `write_file` tool
  // calls through the review queue (in `review` mode) or the auto-apply
  // snapshot/banner path (in `auto` mode) so the model's tool usage
  // respects the same gate as its text-form SEARCH/REPLACE output.
  // Without this, edit_file bypasses `/apply` entirely 闂?which was the
  // bug that made the preview flow feel absent pre-0.5.24.
  //
  // `editModeRef` is read inside the closure so mode cycles don't need
  // to reinstall the hook. Cleanup clears the slot on unmount so a
  // follow-up App instance (tests, HMR) starts with a fresh registry.
  //
  // biome-ignore lint/correctness/useExhaustiveDependencies: session / setEditMode / syncPendingCount are intentional closure captures 闂?their updaters are stable and we don't want to tear down and rebuild the interceptor on unrelated state churn
  useEffect(() => {
    if (!tools || !codeMode) return;
    tools.setToolInterceptor(async (name, args) => {
      if (name !== "edit_file" && name !== "write_file") return null;
      const rawPath = typeof args.path === "string" ? args.path : "";
      if (!rawPath) return null;
      // Mirror filesystem.ts safePath's leading-slash tolerance so
      // `/src/foo.ts` doesn't get misrouted through applyEditBlock's
      // rootDir-escape check.
      let relPath = rawPath;
      while (relPath.startsWith("/") || relPath.startsWith("\\")) {
        relPath = relPath.slice(1);
      }
      if (!relPath) return null;

      // Read root via ref so a workspace swap (which runs reregisterTools
      // for read_file/run_command) is also visible to this interceptor 闂?      // otherwise edit_file writes to the OLD root while read_file looks in
      // the NEW one, producing ENOENT on the next read of a just-edited file.
      const rootForEdit = currentRootDirRef.current;
      let block: EditBlock;
      if (name === "edit_file") {
        const search = typeof args.search === "string" ? args.search : "";
        const replace = typeof args.replace === "string" ? args.replace : "";
        if (!search) return null; // let the tool fn surface the "empty search" error
        block = { path: relPath, search, replace, offset: 0 };
      } else {
        // write_file: capture the current content (if any) as SEARCH so
        // the queued block is a literal whole-file overwrite. For new
        // files SEARCH stays empty 闂?applyEditBlock's create-new sentinel.
        const content = typeof args.content === "string" ? args.content : "";
        block = toWholeFileEditBlock(relPath, content, rootForEdit);
      }

      // Helper: apply the current block + record into history + arm
      // undo. Used by auto mode AND by the various "apply" branches
      // of the review modal so we don't duplicate the snapshot /
      // apply / banner logic.
      //
      // Does NOT push an info row to scrollback: the returned string
      // becomes the tool result AND the loop yields a `tool` event right
      // after 闂?ToolCard renders that with the same text. Pushing here
      // would produce "result shown twice".
      const applyNow = (): string => {
        const snaps = snapshotBeforeEdits([block], rootForEdit);
        const results = applyEditBlocks([block], rootForEdit);
        const good = results.some((r) => r.status === "applied" || r.status === "created");
        if (good) {
          recordEdit("auto", [block], results, snaps);
          armUndoBanner(results);
        }
        return formatEditResults(results);
      };

      // yolo behaves like auto for edit application 闂?the only extra
      // power yolo adds is bypassing shell confirmations (handled in
      // shell.ts via the allowAll getter).
      if (editModeRef.current === "auto" || editModeRef.current === "yolo") return applyNow();

      // review mode, tool-call path: suspend the interceptor on the
      // per-edit modal unless the user has already hit "apply-rest-of-
      // turn" earlier in the same turn. Text-form SEARCH/REPLACE blocks
      // in assistant_final still queue for end-of-turn preview 闂?they
      // land all at once with no mid-stream opportunity to prompt.
      if (turnEditPolicyRef.current === "apply-all") return applyNow();

      const { choice, denyContext } = await new Promise<EditReviewResult>((resolveChoice) => {
        editReviewResolveRef.current = resolveChoice;
        setPendingEditReview(block);
      });
      // Clear the pending-review slot synchronously so a rapid-fire next
      // tool call doesn't race the React state settling.
      editReviewResolveRef.current = null;
      setPendingEditReview(null);

      if (choice === "reject") {
        const context = denyContext ? ` because: ${denyContext}` : "";
        log.pushInfo(t("app.rejectedEdit", { path: block.path, context }));
        return `User rejected this edit to ${block.path}${context}. Don't retry the same SEARCH/REPLACE 闂?either try a different approach or ask the user what they want instead.`;
      }
      if (choice === "apply-rest-of-turn") {
        turnEditPolicyRef.current = "apply-all";
        log.pushInfo(t("app.autoApprovingRest"));
        return applyNow();
      }
      if (choice === "flip-to-auto") {
        setEditMode("auto");
        log.pushInfo(t("app.flippedAutoSession"));
        return applyNow();
      }
      // "apply"
      return applyNow();
    });
    return () => {
      tools.setToolInterceptor(null);
    };
  }, [tools, codeMode, session, recordEdit, armUndoBanner, syncPendingCount, setEditMode]);

  const { codeApply, codeDiscard } = useCodeMode({
    codeMode: !!codeMode,
    pendingEdits,
    currentRootDir,
    session: session ?? null,
    syncPendingCount,
    recordEdit,
  });

  const prefixHash = loop.prefix.fingerprint;

  const writeTranscript = useTranscriptWriter(transcriptRef, model, prefixHash);

  /**
   * Toggle plan mode on the local state AND on the ToolRegistry. The
   * registry's copy is what actually gates dispatch; the local state
   * drives the StatsPanel indicator and slash ergonomics. Kept in sync
   * by funneling every toggle through this setter.
   */
  const togglePlanMode = useCallback(
    (on: boolean) => {
      setPlanMode(on);
      tools?.setPlanMode(on);
    },
    [tools],
  );

  const {
    startLoop,
    stopLoop,
    getLoopStatus,
    isLoopActive,
    isLoopFiring,
    clearFiringFlag,
    activeLoop,
  } = useLoopMode({ log, busyRef, handleSubmitRef });

  /**
   * Mount the per-block walkthrough modal against the pending-edits
   * queue. Returns the info text the slash handler should display.
   * No-op (with explanatory message) when nothing is pending or we're
   * not in code mode.
   */
  const startWalkthrough = useCallback((): string => {
    if (!codeMode) {
      return "/walk is only available inside `reasonix code`.";
    }
    if (pendingEdits.current.length === 0) {
      return "nothing pending 闂?nothing to walk through.";
    }
    setWalkthroughActive(true);
    return `闂?walking ${pendingEdits.current.length} edit block(s) 闂?y apply 闂?n reject 闂?a apply rest 闂?A flip to AUTO 闂?Esc cancels (keeps remaining queued).`;
  }, [codeMode, pendingEdits]);

  // Embedded dashboard server lifecycle. Boot is async (server has to
  // bind a port + read static assets); the slash handler kicks this
  // off and reads the URL out of `dashboardRef` once the promise
  // resolves. Tear-down is also async but cheap 闂?close drains
  // in-flight requests within a 1s grace window.
  const startDashboard = useCallback(async (): Promise<string> => {
    if (dashboardRef.current) return dashboardRef.current.url;
    if (dashboardStartingRef.current) return dashboardStartingRef.current;
    const startup = (async () => {
      const { startDashboardServer } = await import("../../server/index.js");
      const handle = await startDashboardServer(
        {
          mode: "attached",
          configPath: defaultConfigPath(),
          usageLogPath: defaultUsageLogPath(),
          loop,
          tools,
          mcpServers: liveMcpServers,
          getCurrentCwd: () => (codeMode ? currentRootDirRef.current : undefined),
          getEditMode: () => (codeMode ? editModeRef.current : undefined),
          getPlanMode: () => planModeRef.current,
          getPendingEditCount: () => pendingEdits.current.length,
          getLatestVersion: () => latestVersionRef.current,
          getSessionName: () => session ?? null,
          setEditMode: (m: EditMode) => {
            setEditMode(m);
            editModeRef.current = m;
            saveEditMode(m);
            return m;
          },
          setPlanMode: (on: boolean) => {
            if (codeMode) togglePlanMode(on);
          },
          applyPresetLive: (name: string) => {
            const settings = resolvePreset(name as PresetName);
            loop.configure({
              model: settings.model,
              autoEscalate: settings.autoEscalate,
              reasoningEffort: settings.reasoningEffort,
            });
            agentStore.dispatch({ type: "session.model.change", model: settings.model });
            const canonical: "auto" | "flash" | "pro" =
              settings.model === "deepseek-v4-pro"
                ? "pro"
                : settings.autoEscalate
                  ? "auto"
                  : "flash";
            setPreset(canonical);
            agentStore.dispatch({ type: "session.preset.change", preset: canonical });
            try {
              savePreset(canonical);
            } catch {
              /* disk full / perms 闂?runtime change still took effect */
            }
          },
          applyEffortLive: (effort) => {
            loop.configure({ reasoningEffort: effort });
          },
          applyModelLive: (model) => {
            loop.configure({ model });
            agentStore.dispatch({ type: "session.model.change", model });
          },
          getModels: () => modelsRef.current,
          setProNextLive: (armed) => {
            if (armed) loop.armProForNextTurn();
            else loop.disarmPro();
          },
          setBudgetUsdLive: (usd) => {
            loop.setBudget(usd);
          },
          getLoopRunStatus: () => getLoopStatus(),
          startAutoLoop: (intervalMs, prompt) => startLoop(intervalMs, prompt),
          stopAutoLoop: () => stopLoop(),
          // ---------- Chat bridge ----------
          getMessages: (): DashboardMessage[] =>
            cardsToDashboardMessages(agentStore.getState().cards),
          subscribeEvents: (handler) => {
            eventSubscribersRef.current.add(handler);
            return () => {
              eventSubscribersRef.current.delete(handler);
            };
          },
          submitPrompt: (text: string): SubmitResult => {
            if (busyRef.current) {
              return { accepted: false, reason: "loop is busy with a turn" };
            }
            const fn = handleSubmitRef.current;
            if (!fn) return { accepted: false, reason: "TUI not ready" };
            // Fire-and-forget 闂?handleSubmit drives the loop event stream
            // which the web sees via SSE. We don't await it here because
            // a turn can take minutes; the HTTP request would time out.
            fn(text).catch(() => undefined);
            return { accepted: true };
          },
          abortTurn: () => {
            if (busyRef.current) loop.abort();
          },
          isBusy: () => busyRef.current,
          getStats: () => {
            // Pull from the loop's live aggregator (same source the TUI's
            // StatsPanel reads). `balance` comes from useSessionInfo via a
            // ref-mirror so this callback stays cheap.
            const s = loop.stats.summary();
            const ctxCap = DEEPSEEK_CONTEXT_TOKENS[loop.model] ?? DEFAULT_CONTEXT_TOKENS;
            return {
              turns: s.turns,
              totalCostUsd: s.totalCostUsd,
              lastTurnCostUsd: s.lastTurnCostUsd,
              totalInputCostUsd: s.totalInputCostUsd,
              totalOutputCostUsd: s.totalOutputCostUsd,
              cacheHitRatio: s.cacheHitRatio,
              lastPromptTokens: s.lastPromptTokens,
              contextCapTokens: ctxCap,
              // useSessionInfo's Balance is a flat { currency, total }; the
              // dashboard wire shape is the richer DeepSeek BalanceInfo
              // array (granted / topped_up split). Convert as a single-
              // entry array so the SPA always reads `balance[0]` shape.
              balance: balanceRef.current
                ? [
                    {
                      currency: balanceRef.current.currency,
                      total_balance: String(balanceRef.current.total),
                    },
                  ]
                : null,
            };
          },
          // ---------- Modal mirroring ----------
          getActiveModal: (): ActiveModal | null => {
            // Probe the live state via refs in priority order 闂?only one
            // modal can be up at a time per App invariant.
            const ps = pendingShell;
            if (ps) {
              return {
                kind: "shell",
                command: ps.command,
                allowPrefix: derivePrefix(ps.command),
                shellKind: ps.kind,
              };
            }
            const pc = pendingChoice;
            if (pc) {
              return {
                kind: "choice",
                question: pc.question,
                options: pc.options,
                allowCustom: pc.allowCustom,
              };
            }
            if (pendingPlanRef.current) {
              return { kind: "plan", body: pendingPlanRef.current };
            }
            const er = pendingEditReview;
            if (er) {
              return {
                kind: "edit-review",
                path: er.path,
                search: er.search ?? "",
                replace: er.replace ?? "",
                preview: (er.search || er.replace || "").split("\n").slice(0, 12).join("\n"),
                total: pendingEdits.current.length,
                remaining: pendingEdits.current.length,
              };
            }
            if (pendingRevision) {
              return {
                kind: "revision",
                reason: pendingRevision.reason,
                remainingSteps: pendingRevision.remainingSteps.map((s) => ({
                  id: s.id,
                  title: s.title,
                  action: s.action,
                  ...(s.risk ? { risk: s.risk } : {}),
                })),
                ...(pendingRevision.summary ? { summary: pendingRevision.summary } : {}),
              };
            }
            if (pendingCheckpoint) {
              return {
                kind: "checkpoint",
                stepId: pendingCheckpoint.stepId,
                ...(pendingCheckpoint.title ? { title: pendingCheckpoint.title } : {}),
                completed: pendingCheckpoint.completed,
                total: pendingCheckpoint.total,
              };
            }
            const picker = activePickerSnapshotRef.current;
            if (picker) {
              return { kind: "picker", ...picker };
            }
            const viewer = activeViewerSnapshotRef.current;
            if (viewer) {
              return { kind: "viewer", ...viewer };
            }
            return null;
          },
          resolveShellConfirm: (choice) => {
            const fn = handleShellConfirmRef.current;
            if (fn) Promise.resolve(fn(choice)).catch(() => undefined);
          },
          resolveChoiceConfirm: (choice) => {
            const fn = handleChoiceConfirmRef.current;
            if (fn) fn(choice).catch(() => undefined);
          },
          resolvePlanConfirm: (choice, text) => {
            if (choice === "cancel") {
              handlePlanConfirmRef.current("cancel").catch(() => undefined);
              return;
            }
            const plan = pendingPlanRef.current ?? "";
            // Bypass the picker 闂?input two-step on web. The override
            // form of handleStagedInputSubmit takes the plan + mode
            // directly; behaviour matches the TUI's "user typed feedback +
            // pressed Enter" path.
            handleStagedInputSubmitRef
              .current(text ?? "", { plan, mode: choice })
              .catch(() => undefined);
          },
          resolveEditReview: (choice) => {
            const resolve = editReviewResolveRef.current;
            if (resolve) {
              editReviewResolveRef.current = null;
              setPendingEditReview(null);
              resolve({ choice, denyContext: undefined });
            }
          },
          resolveCheckpointConfirm: (choice, text) => {
            // Web's "revise" path sends feedback in one shot; we hand the
            // current pending checkpoint to the submit handler directly,
            // skipping the TUI's staged-input two-step. continue/stop fall
            // through to the regular picker handler.
            if (choice === "revise" && typeof text === "string") {
              const snap = pendingCheckpoint;
              setPendingCheckpoint(null);
              if (!snap) return;
              Promise.resolve(handleCheckpointReviseSubmitRef.current(text, snap)).catch(
                () => undefined,
              );
              return;
            }
            Promise.resolve(handleCheckpointConfirmRef.current(choice)).catch(() => undefined);
          },
          resolveReviseConfirm: (choice) => {
            Promise.resolve(handleReviseConfirmRef.current(choice)).catch(() => undefined);
          },
          resolvePicker: (resolution) => {
            const fn = activePickerResolverRef.current;
            if (fn) Promise.resolve(fn(resolution)).catch(() => undefined);
          },
          resolveViewer: () => {
            const fn = activeViewerResolverRef.current;
            if (fn) Promise.resolve(fn()).catch(() => undefined);
          },
          // ---------- v0.14 mutation surface ----------
          reloadHooks: () => reloadHooks(codeMode ? currentRootDirRef.current : undefined),
          addToolToPrefix: (spec) => loop.prefix.addTool(spec),
          reloadMcp: mcpRuntime
            ? async () => {
                const r = await mcpRuntime.reloadFromConfig(loop);
                setLiveMcpServers(r.summaries);
                return r.summaries.length;
              }
            : undefined,
        },
        { port: dashboardPort },
      );
      dashboardRef.current = handle;
      setDashboardUrlState(handle.url);
      return handle.url;
    })();
    dashboardStartingRef.current = startup;
    try {
      return await startup;
    } finally {
      dashboardStartingRef.current = null;
    }
  }, [
    loop,
    tools,
    liveMcpServers,
    codeMode,
    session,
    togglePlanMode,
    pendingShell,
    pendingChoice,
    pendingCheckpoint,
    pendingEditReview,
    pendingRevision,
    agentStore,
    mcpRuntime,
    getLoopStatus,
    startLoop,
    stopLoop,
    pendingEdits,
    editModeRef,
    setEditMode,
    currentRootDirRef,
    reloadHooks,
    setPreset,
    dashboardPort,
  ]);

  const stopDashboard = useCallback(async (): Promise<void> => {
    const h = dashboardRef.current;
    if (!h) return;
    dashboardRef.current = null;
    setDashboardUrlState(null);
    try {
      await h.close();
    } catch {
      /* swallow 闂?server going down is best-effort */
    }
    log.pushInfo(t("app.dashboardStopped"));
  }, [log]);

  const getDashboardUrl = useCallback((): string | null => {
    return dashboardRef.current?.url ?? null;
  }, []);

  // Mirror of the dashboard URL into React state so the StatsPanel
  // header can render a clickable pill the moment the server is up.
  // Updated by both the auto-start effect below and the explicit
  // /dashboard slash path (via startDashboard).
  const [dashboardUrl, setDashboardUrlState] = useState<string | null>(null);

  // Auto-start the dashboard once the TUI is mounted unless the user
  // opted out with --no-dashboard. The whole point is discoverability:
  // most users had no idea /dashboard existed, so the URL needs to be
  // visible from the first render. startDashboard updates the React
  // state itself, so we just fire-and-forget. Failures stay silent 闂?  // a missing dashboard never blocks the TUI.
  useEffect(() => {
    if (noDashboard) return;
    if (dashboardRef.current) return;
    startDashboard()
      .then((url) => {
        if (url && openDashboard) openUrl(url);
      })
      .catch((err) => {
        // Auto-start failure surfaces as a visible warn row. The URL
        // itself is shown on the welcome card (when the server is up),
        // so silence here would leave the user with no way to know the
        // web UI is unreachable 闂?port already in use, permission
        // denied, etc. Don't block the TUI; everything else keeps working.
        const reason = err instanceof Error ? err.message : String(err);
        log.pushInfo(t("ui.dashboardAutoStartFailed", { reason }));
      });
  }, [noDashboard, openDashboard, startDashboard, log]);

  // Tear the dashboard down on unmount so the port doesn't leak when
  // the TUI exits via /exit, Ctrl+C, etc.
  useEffect(() => {
    return () => {
      const h = dashboardRef.current;
      if (h) {
        dashboardRef.current = null;
        h.close().catch(() => undefined);
      }
    };
  }, []);

  /**
   * onChoose for the walkthrough EditConfirm. Each pick mutates
   * pendingEdits via the existing codeApply/codeDiscard helpers, which
   * also bump pendingTick 闂?the modal re-renders with the next block.
   * When no blocks remain, the modal unmounts.
   */
  const handleWalkChoice = useCallback(
    (choice: EditReviewChoice) => {
      if (choice === "apply") {
        log.pushInfo(codeApply([1]));
      } else if (choice === "reject") {
        log.pushInfo(codeDiscard([1]));
      } else if (choice === "apply-rest-of-turn") {
        // "apply rest" inside a walkthrough = commit every remaining
        // block at once, then exit. Same end state as if the user had
        // typed `/apply` outside the walk.
        log.pushInfo(codeApply());
        setWalkthroughActive(false);
        return;
      } else if (choice === "flip-to-auto") {
        // Flip the gate first, then apply the current block, then exit
        // the walk. Remaining blocks stay pending 闂?the user can keep
        // walking via /walk again or commit them with /apply.
        setEditMode("auto");
        saveEditMode("auto");
        log.pushInfo(codeApply([1]));
        log.pushInfo(t("app.flippedAutoWalk"));
        setWalkthroughActive(false);
        return;
      }
      // After a per-block apply/reject, check if the queue is empty
      // (codeApply/codeDiscard updated pendingEdits.current). If so,
      // exit; otherwise stay mounted and EditConfirm re-renders against
      // the new first block thanks to pendingTick.
      if (pendingEdits.current.length === 0) setWalkthroughActive(false);
    },
    [codeApply, codeDiscard, log, pendingEdits, setEditMode],
  );

  const pendingGateIdRef = useRef<number | null>(null);
  const handleShellConfirmRef = useRef<
    (choice: "run_once" | "always_allow" | "deny", denyContext?: string) => void
  >(() => undefined);
  const handlePathConfirmRef = useRef<
    (choice: "run_once" | "always_allow" | "deny", denyContext?: string) => void
  >(() => undefined);
  const handlePlanCancelRef = useRef<() => void | Promise<void>>(() => undefined);
  const handlePlanFeedbackRef = useRef<
    (
      feedback: string,
      override: { plan: string; mode: "refine" | "approve" | "reject" },
    ) => void | Promise<void>
  >(() => undefined);
  const handleCheckpointConfirmRef = useRef<(choice: "continue" | "revise" | "stop") => void>(
    () => undefined,
  );
  const handleCheckpointReviseSubmitRef = useRef<
    (feedback: string, snap: { stepId: string; title?: string }) => void
  >(() => undefined);
  const handleReviseConfirmRef = useRef<(choice: ReviseChoice | "cancel") => void | Promise<void>>(
    () => undefined,
  );
  const handleChoiceResolveRef = useRef<
    (
      resolution:
        | { type: "pick"; optionId: string }
        | { type: "text"; text: string }
        | { type: "cancel" },
    ) => void
  >(() => undefined);

  const handleQQModelPick = useCallback(
    (target: string): string => {
      if (target === "auto" || target === "flash" || target === "pro") {
        const preset = PRESETS[target];
        loop.configure({
          model: preset.model,
          autoEscalate: preset.autoEscalate,
          reasoningEffort: preset.reasoningEffort,
        });
        agentStore.dispatch({ type: "session.model.change", model: preset.model });
        setPreset(target);
        agentStore.dispatch({ type: "session.preset.change", preset: target });
        try {
          savePreset(target);
        } catch {}
        return `preset: ${target} / ${preset.model}`;
      }

      loop.configure({ model: target, autoEscalate: false });
      agentStore.dispatch({ type: "session.model.change", model: target });
      const inferred =
        target === "deepseek-v4-pro" ? "pro" : target === "deepseek-v4-flash" ? "flash" : null;
      setPreset(inferred ?? "flash");
      agentStore.dispatch({ type: "session.preset.change", preset: inferred });
      if (inferred) {
        try {
          savePreset(inferred);
        } catch {}
      }
      return `model: ${target}`;
    },
    [agentStore, loop, setPreset],
  );

  const handleQQThemePick = useCallback(
    (target: ThemeChoice): string => {
      saveTheme(target);
      const active = resolveThemePreference(target, process.env.REASONIX_THEME);
      setThemeName(active);
      return `theme saved: ${target}\nactive now: ${active}`;
    },
    [setThemeName],
  );

  const qq = useQQChannel({
    codeMode: !!codeMode,
    initialChannel: qqChannel,
    log,
    isRawModeSupported,
    setRawMode,
    setQueuedSubmit,
    qqSubmitRef,
    qqErrorRef,
    sessionName: session,
    currentRootDir,
    pendingGateIdRef,
    completedStepIdsRef,
    planStepsRef,
    onCreateSession: onSwitchSession ? (name) => onSwitchSession(name) : undefined,
    onSelectSession: onSwitchSession ? (name) => onSwitchSession(name) : undefined,
    onModelPick: handleQQModelPick,
    onThemePick: handleQQThemePick,
    onShellConfirmRef: handleShellConfirmRef,
    onPathConfirmRef: handlePathConfirmRef,
    onPlanCancelRef: handlePlanCancelRef,
    onPlanFeedbackRef: handlePlanFeedbackRef,
    onCheckpointConfirmRef: handleCheckpointConfirmRef,
    onCheckpointReviseRef: handleCheckpointReviseSubmitRef,
    onPlanRevisionRef: handleReviseConfirmRef,
    onChoiceResolveRef: handleChoiceResolveRef,
  });

  const handleSubmit = useCallback(
    async (raw: string) => {
      const incoming = qq.parseSubmit(raw);
      if (!incoming) return;
      let { text, fromQQ } = incoming;
      if (incoming.handled) {
        return;
      }
      if (busy || submittingRef.current) {
        return;
      }
      // Cancel-on-user-input: any user-typed submit cancels an active
      // /loop, regardless of busy state. Loop-fired submits set the
      // firing flag so the timer's own re-submit doesn't self-cancel.
      if (isLoopActive() && !isLoopFiring()) {
        stopLoop();
      }
      clearFiringFlag();
      // @-mention picker intercept. Enter on either a file or a folder
      // commits the path INTO the buffer (with trailing space) 闂?the
      // user almost always types more after a mention. The trailing
      // space dismisses the picker, so the next Enter submits normally.
      // Folders inline as a directory listing at submit time.
      if (atState && atState.entries.length > 0) {
        const sel = atState.entries[atSelected] ?? atState.entries[0];
        if (sel) {
          pickAtMention(sel, "commit");
          return;
        }
      }

      // Slash-argument picker intercept 闂?same shape as @-picker. For
      // file pickers (/edit) we splice + trailing space so the user
      // keeps typing the instruction. For enum pickers (/preset,
      // /model, /plan, 闂? we splice without trailing space; those
      // commands take no further args, so the user presses Enter a
      // second time to run.
      if (slashArgMatches && slashArgMatches.length > 0 && slashArgContext) {
        const sel = slashArgMatches[slashArgSelected] ?? slashArgMatches[0];
        if (sel) {
          pickSlashArg(sel);
          return;
        }
      }

      // Slash auto-complete on Enter. When the user typed a prefix
      // (e.g. "/he") and the suggestion list is visible, substitute
      // the highlighted match so Enter runs it 闂?same effect as Tab
      // + Enter, one keystroke less. Skip substitution if the user
      // already typed a full, exact command name (respect verbatim
      // input when they know what they want).
      if (text.startsWith("/") && !text.includes(" ")) {
        const typed = text.slice(1).toLowerCase();
        const matches = suggestSlashCommands(typed, !!codeMode, slashUsage);
        const exact = matches.find((m) => m.cmd === typed);
        if (!exact && matches.length > 0) {
          const chosen = matches[slashSelected] ?? matches[0];
          if (chosen) text = `/${chosen.cmd}`;
        }
      }

      setInput("");
      resetCursor();

      // Y/N fast-path when edits are pending. One keystroke is all it
      // takes to commit or drop 闂?matches the muscle memory of `git
      // add -p` / most prompts. Deliberately scoped: only when there
      // ARE pending edits, so "y" as a normal message still works
      // when nothing's waiting.
      if (codeMode && pendingEdits.current.length > 0 && (text === "y" || text === "n")) {
        log.pushInfo(text === "y" ? codeApply() : codeDiscard());
        pushHistory(text);
        return;
      }

      // Hash mode 闂?`#note` (project) and `#g note` (global) append to
      // a REASONIX.md so future sessions pin the note in the immutable
      // prefix. No model round-trip. `\#literal` escape falls through to
      // normal submission with the backslash stripped so the model sees
      // `#literal` verbatim.
      const hashParse = detectHashMemory(text);
      if (hashParse?.kind === "memory" || hashParse?.kind === "memory-global") {
        const isGlobal = hashParse.kind === "memory-global";
        const memRoot = currentRootDir;
        pushHistory(text);
        try {
          const result = isGlobal
            ? appendGlobalMemory(hashParse.note)
            : appendProjectMemory(memRoot, hashParse.note);
          const verb = result.created ? t("app.notedVerbCreated") : t("app.notedVerbAppended");
          const scopeTag = isGlobal ? t("app.notedScopeGlobal") : t("app.notedScopeProject");
          log.pushInfo(t("app.notedMemory", { scope: scopeTag, verb, path: result.path }));
        } catch (err) {
          log.pushWarning(t("app.memoryWriteFailed"), (err as Error).message);
        }
        return;
      }
      if (hashParse?.kind === "escape") {
        // Replace the working buffer with the de-escaped form. We don't
        // recurse into handleSubmit to avoid the "still busy" race 闂?        // just rewrite `text` and let the rest of the pipeline (bang /
        // slash / model) see the literal prompt.
        text = hashParse.text;
      }

      // Bash mode 闂?`!cmd` runs a shell command in the sandbox root
      // immediately (no allowlist gate: user-typed = explicit consent),
      // surfaces the formatted output in the Historical log, and
      // persists a user-role message so the next model turn sees what
      // happened AND the bang exchange survives session resume.
      const bangCmd = detectBangCommand(text);
      if (bangCmd !== null) {
        const bangRoot = currentRootDir;
        pushHistory(text);
        log.pushUser(text);
        setBusy(true);
        try {
          const result = await runCommand(bangCmd, {
            cwd: bangRoot,
            timeoutSec: 60,
            maxOutputChars: 32_000,
          });
          const formatted = formatCommandResult(bangCmd, result);
          log.pushInfo(formatted);
          loop.appendAndPersist({
            role: "user",
            content: formatBangUserMessage(bangCmd, formatted),
          });
        } catch (err) {
          log.pushWarning(t("app.commandFailed"), (err as Error).message);
        } finally {
          setBusy(false);
        }
        return;
      }

      // `/btw <question>` 闂?one-shot side question. Same async-not-fit-
      // handleSlash shape as MCP browse: intercept here, call the client
      // directly with a fresh message list, never append to `loop.messages`
      // so the side exchange leaves the conversation context untouched.
      const btwMatch = /^\/btw(?:\s+([\s\S]+))?$/.exec(text);
      if (btwMatch) {
        const question = btwMatch[1]?.trim() ?? "";
        pushHistory(text);
        log.pushUser(text);
        if (!question) {
          log.pushInfo(t("app.btwUsage"));
          return;
        }
        setBusy(true);
        try {
          const reply = await loop.client.chat({
            model: loop.model,
            messages: [
              {
                role: "system",
                content:
                  "You are answering a side question that is unrelated to the current coding conversation. Answer concisely (1-3 sentences) in plain prose. Do not call tools, do not ask clarifying questions, and do not reference any prior turns.",
              },
              { role: "user", content: question },
            ],
          });
          const answer = reply.content.trim() || "(no answer)";
          log.pushInfo(`${t("app.btwHeader")}\n${answer}`, "brand");
        } catch (err) {
          log.pushWarning(t("app.btwFailed"), (err as Error).message);
        } finally {
          setBusy(false);
        }
        return;
      }

      // MCP resource / prompt browsers 闂?async calls that don't fit the
      // synchronous handleSlash shape, so we intercept the exact command
      // forms here. The slash-command registry still lists them (for
      // /help + argument-level picker completion), but this branch is
      // what actually runs the read/fetch.
      const mcpBrowseMatch = /^\/(resource|prompt)(?:\s+([\s\S]*))?$/.exec(text);
      if (mcpBrowseMatch) {
        const kind = mcpBrowseMatch[1] as "resource" | "prompt";
        const arg = mcpBrowseMatch[2]?.trim() ?? "";
        pushHistory(text);
        log.pushUser(text);
        await handleMcpBrowseSlash(kind, arg, liveMcpServers, log);
        return;
      }

      const slash = parseSlash(text);
      if (slash) {
        const sink = eventSinkRef.current;
        const eventizer = eventizerRef.current;
        if (sink && eventizer) {
          sink.append(
            eventizer.emitSlashInvoked(loop.currentTurn, slash.cmd, slash.args.join(" ")),
          );
        }
        setSlashUsage(recordSlashUse(slash.cmd));
        const result = handleSlash(slash.cmd, slash.args, loop, {
          mcpSpecs,
          mcpServers: liveMcpServers,
          codeUndo: codeMode ? codeUndo : undefined,
          codeApply: codeMode ? codeApply : undefined,
          codeDiscard: codeMode ? codeDiscard : undefined,
          codeHistory: codeMode ? codeHistory : undefined,
          codeShowEdit: codeMode ? codeShowEdit : undefined,
          codeRoot: codeMode ? currentRootDir : undefined,
          pendingEditCount: codeMode ? pendingEdits.current.length : undefined,
          memoryRoot: currentRootDir,
          planMode,
          setPlanMode: codeMode ? togglePlanMode : undefined,
          editMode: codeMode ? editMode : undefined,
          setEditMode: codeMode ? setEditMode : undefined,
          touchedFiles: codeMode
            ? () => {
                // Union of (files in completed/undone edit batches) +
                // (paths queued in pendingEdits awaiting /apply). Both
                // represent surface area the user might want to roll
                // back later.
                const set = new Set<string>(touchedPaths());
                for (const b of pendingEdits.current) set.add(b.path);
                return [...set];
              }
            : undefined,
          armPro: () => {
            loop.armProForNextTurn();
            setProArmed(true);
          },
          disarmPro: () => {
            loop.disarmPro();
            setProArmed(false);
          },
          startLoop,
          stopLoop,
          getLoopStatus,
          startWalkthrough: codeMode ? startWalkthrough : undefined,
          startDashboard,
          stopDashboard,
          getDashboardUrl,
          qq: {
            connect: qq.connect,
            disconnect: qq.disconnect,
            status: qq.status,
          },
          sessionId: session,
          jobs: codeMode?.jobs,
          postInfo: fromQQ ? qq.sendInfo : log.pushInfo,
          postDoctor: (checks) => log.showDoctor(checks),
          postUsage: (args) => log.showUsageVerbose(args),
          postKeys: (args) =>
            log.pushTip({
              topic: args.topic,
              sections: args.sections,
              footer: args.footer,
              oneTime: false,
            }),
          dispatch: agentStore.dispatch,
          markPlanStepDone: (stepId: string) => {
            const steps = planStepsRef.current;
            if (!steps || steps.length === 0) return "no-plan";
            if (!steps.some((s) => s.id === stepId)) return "not-in-plan";
            if (completedStepIdsRef.current.has(stepId)) return "already-done";
            completedStepIdsRef.current.add(stepId);
            persistPlanState();
            log.completePlanStep(stepId);
            return "ok";
          },
          markAllPlanStepsDone: () => {
            const steps = planStepsRef.current;
            if (!steps || steps.length === 0) return 0;
            let added = 0;
            for (const s of steps) {
              if (completedStepIdsRef.current.has(s.id)) continue;
              completedStepIdsRef.current.add(s.id);
              log.completePlanStep(s.id);
              added++;
            }
            if (added > 0) persistPlanState();
            return added;
          },
          reloadHooks: () => reloadHooks(codeMode ? currentRootDir : undefined),
          switchCwd: codeMode?.reregisterTools
            ? (newPath: string) => {
                const resolved = resolve(newPath);
                let stat: ReturnType<typeof statSync>;
                try {
                  stat = statSync(resolved);
                } catch (err) {
                  return { ok: false, info: `/cwd: ${(err as Error).message}` };
                }
                if (!stat.isDirectory()) {
                  return { ok: false, info: `/cwd: ${resolved} is not a directory` };
                }
                codeMode.reregisterTools?.(resolved);
                setCurrentRootDir(resolved);
                reloadHooks(resolved);
                const reBootstrap = codeMode.reBootstrapSemantic;
                if (reBootstrap) {
                  void reBootstrap(resolved).then(
                    (r) => {
                      log.pushInfo(
                        r.enabled
                          ? `闂?semantic_search re-pointed at ${resolved}`
                          : `闂?semantic_search disabled (no compatible index in ${resolved})`,
                      );
                    },
                    (err) => {
                      log.pushInfo(
                        `闂?semantic_search re-bootstrap failed: ${(err as Error).message}`,
                      );
                    },
                  );
                }
                return { ok: true, info: `闂?workspace switched to ${resolved}` };
              }
            : undefined,
          reloadMcp: mcpRuntime
            ? async () => {
                const r = await mcpRuntime.reloadFromConfig(loop);
                setLiveMcpServers(r.summaries);
                return r;
              }
            : undefined,
          latestVersion,
          refreshLatestVersion,
          models,
          refreshModels,
        });
        if (
          fromQQ &&
          qq.handleRemoteSlashResult({
            result,
            codeMode: !!codeMode,
            sessions: listSessionsForWorkspace(currentRootDir),
            checkpoints: codeMode ? [...listCheckpoints(currentRootDir)].reverse() : [],
            models,
            restoreCodeOnlyMessage: t("app.restoreCodeOnly"),
          })
        ) {
          pushHistory(text);
          return;
        }
        if (result.openSessionsPicker) {
          const sessions = listSessionsForWorkspace(currentRootDir);
          setSessionsPickerList(sessions);
          setPendingSessionsPicker(true);
          pushHistory(text);
          return;
        }
        if (result.openCheckpointPicker) {
          if (!codeMode) {
            log.pushInfo(t("app.restoreCodeOnly"));
            pushHistory(text);
            return;
          }
          const checkpoints = [...listCheckpoints(currentRootDir)].reverse();
          setCheckpointPickerList(checkpoints);
          setPendingCheckpointPicker(true);
          pushHistory(text);
          return;
        }
        if (result.openMcpHub) {
          setPendingMcpHub({ tab: result.openMcpHub.tab });
          pushHistory(text);
          return;
        }
        if (result.openModelPicker) {
          setPendingModelPicker(true);
          pushHistory(text);
          return;
        }
        if (result.openThemePicker) {
          setPendingThemePicker(true);
          pushHistory(text);
          return;
        }
        if (result.openCopyMode) {
          setPendingCopyMode(true);
          pushHistory(text);
          return;
        }
        if (result.openArgPickerFor) {
          pushHistory(text);
          setInput(`/${result.openArgPickerFor} `);
          return;
        }
        if (result.replayPlan) {
          const rp = result.replayPlan;
          const titleSuffix = rp.summary ? ` 闂?${rp.summary}` : "";
          const done = new Set(rp.completedStepIds);
          setPendingReplayViewer({
            viewerKind: "replay-plan",
            title: `Replay #${rp.index}/${rp.total} 闂?${rp.relativeTime}${titleSuffix}`,
            body: rp.body,
            steps: rp.steps.map((s) => ({
              id: s.id,
              title: s.title,
              status: done.has(s.id) ? "done" : "queued",
            })),
            meta: rp.archiveBasename,
          });
        }
        const outcome = applySlashResult(result, {
          log,
          stdoutWrite: (chunk) => stdout?.write(chunk),
          pendingEdits,
          syncPendingCount,
          session: session ?? null,
          codeModeOn: !!codeMode,
          isLoopActive,
          stopLoop,
          quitProcess,
          pushHistory,
          resetPendingModals,
          text,
        });
        if (fromQQ && result.info) qq.sendText(result.info);
        if (outcome.kind === "resubmit") {
          text = outcome.text;
        } else {
          return;
        }
      }

      // UserPromptSubmit hooks. Exit code 2 from any matching hook
      // drops the message entirely (the user's text never reaches
      // the model). Other non-zero exits surface as warning rows but
      // the prompt still goes through. We render every non-pass
      // outcome's stderr inline so a "blocked" choice has a visible
      // explanation.
      if (hookList.some((h) => h.event === "UserPromptSubmit")) {
        const promptReport = await runHooks({
          hooks: hookList,
          payload: { event: "UserPromptSubmit", cwd: currentRootDir, prompt: text },
        });
        for (const o of promptReport.outcomes) {
          if (o.decision === "pass") continue;
          log.pushWarning(t("app.hookUserPromptSubmit"), formatHookOutcomeMessage(o));
        }
        if (promptReport.blocked) return;
      }

      // Large pastes (stack traces, log dumps, file contents) get a
      // collapsed preview in scrollback; the model still receives the full
      // text below via modelInput.
      pushHistory(text);
      const pasteDisplay = formatLongPaste(text);
      const userId = log.pushUser(pasteDisplay.displayText);
      broadcastDashboardEvent({ kind: "user", id: userId, text });
      if (session) {
        const existing = loadSessionMeta(session);
        const patch: Parameters<typeof patchSessionMeta>[1] = {};
        if (!existing.summary) patch.summary = text.replace(/\s+/g, " ").slice(0, 80);
        if (!existing.branch) patch.branch = detectGitBranch(currentRootDir);
        if (!existing.workspace) patch.workspace = currentRootDir;
        if (Object.keys(patch).length > 0) patchSessionMeta(session, patch);
      }

      const assistantId = `a-${Date.now()}`;
      const streamRef: StreamingState = { id: assistantId, text: "", reasoning: "" };
      const contentBuf = { current: "" };
      const reasoningBuf = { current: "" };
      const translator = new TurnTranslator(log);
      // Coalesces tool_call_delta events into one re-render per flush tick.
      const toolCallBuildBuf: {
        current: {
          name: string;
          chars: number;
          index?: number;
          readyCount?: number;
        } | null;
      } = {
        current: null,
      };

      submittingRef.current = true;
      setBusy(true);
      qq.noteTurnFromQQ(fromQQ);
      abortedThisTurn.current = false;
      // Seal the in-progress history entry so this turn's edits open
      // a new one 闂?prior turns are preserved intact for /history and
      // `/undo` to walk back through independently.
      if (codeMode) sealCurrentEntry();
      // Reset per-turn edit policy so "apply-rest-of-turn" from the
      // previous turn doesn't carry over silently. User expects each
      // new prompt to start with the normal review gate re-armed.
      turnEditPolicyRef.current = "ask";
      // Pro badge state: if /pro was armed, this turn consumes it; the
      // loop emits a "闂?/pro armed" warning we'll catch below. Clear
      // the armed mirror so the badge flips to "escalated" (via the
      // warning handler) rather than staying at "armed" during the
      // actual run.
      if (proArmed) {
        setProArmed(false);
        setTurnOnPro(true);
      } else {
        setTurnOnPro(false);
      }

      const flush = () => {
        if (!contentBuf.current && !reasoningBuf.current && !toolCallBuildBuf.current) return;
        translator.flushBuffers(reasoningBuf.current, contentBuf.current, loop.currentCallModel);
        streamRef.text += contentBuf.current;
        streamRef.reasoning += reasoningBuf.current;
        if (toolCallBuildBuf.current) {
          streamRef.toolCallBuild = toolCallBuildBuf.current;
        }
        contentBuf.current = "";
        reasoningBuf.current = "";
        toolCallBuildBuf.current = null;
      };
      const timer = setInterval(flush, FLUSH_INTERVAL_MS);

      // Expand `@path/to/file.ts` mentions in code mode: the model
      // gets the inlined content appended under a "Referenced files"
      // block; the Historical row above keeps the user's verbatim text
      // so the display doesn't balloon.
      let modelInput = text;
      if (codeMode) {
        const expanded = expandAtMentions(text, currentRootDir);
        if (expanded.expansions.length > 0) {
          modelInput = expanded.text;
          const inlined = expanded.expansions
            .filter((ex) => ex.ok)
            .map((ex) => {
              if (ex.isDirectory) {
                const trunc = ex.truncated ? "+" : "";
                return `${ex.path}/ (${ex.entries ?? 0}${trunc} entries)`;
              }
              return `${ex.path} (${(ex.bytes ?? 0).toLocaleString()} bytes)`;
            });
          const skipped = expanded.expansions
            .filter((ex) => !ex.ok)
            .map((ex) => `${ex.path} (${ex.skip})`);
          const parts: string[] = [];
          if (inlined.length > 0) parts.push(`inlined ${inlined.join(", ")}`);
          if (skipped.length > 0) parts.push(`skipped ${skipped.join(", ")}`);
          if (parts.length > 0) log.pushInfo(t("app.atMentions", { parts: parts.join("; ") }));
        }
      }
      // Expand `@http(s)://...` URL mentions. Available in any mode (chat
      // OR code) since fetching a URL doesn't need a sandbox root. Awaits
      // the network sequentially across URLs 闂?for a typical 1-2 URLs in
      // a prompt this is fine; if a user pastes 10 URLs the latency adds
      // up but their prompt is also already huge.
      if (/(?:^|\s)@https?:\/\//.test(text)) {
        try {
          const urlExpanded = await expandAtUrls(modelInput, {
            fetcher: webFetch,
            cache: atUrlCache.current,
          });
          if (urlExpanded.expansions.length > 0) {
            modelInput = urlExpanded.text;
            const inlined = urlExpanded.expansions
              .filter((ex) => ex.ok)
              .map((ex) => {
                const tag = ex.title ? `${ex.title} (${ex.url})` : ex.url;
                const trunc = ex.truncated ? " 闂?truncated" : "";
                return `${tag} 闂?${(ex.chars ?? 0).toLocaleString()} chars${trunc}`;
              });
            const skipped = urlExpanded.expansions
              .filter((ex) => !ex.ok)
              .map((ex) => `${ex.url} (${ex.skip ?? "fetch-error"})`);
            const parts: string[] = [];
            if (inlined.length > 0) parts.push(`inlined ${inlined.join("; ")}`);
            if (skipped.length > 0) parts.push(`skipped ${skipped.join("; ")}`);
            if (parts.length > 0) log.pushInfo(t("app.atUrl", { parts: parts.join("; ") }));
          }
        } catch (err) {
          // expandAtUrls itself only throws on misconfiguration (no
          // fetcher). Per-URL failures are surfaced via the skip path.
          log.pushWarning(t("app.atUrlFailed"), (err as Error).message);
        }
      }

      try {
        let lastAssistantText = "";
        for await (const ev of loop.step(modelInput)) {
          writeTranscript(ev);
          // Mirror to the kernel event log sidecar. Pure passthrough 闂?          // Eventizer holds the small state (turn boundary detection +
          // tool callId correlation) needed to translate LoopEvent
          // shape into typed Event variants. Sink + eventizer share the
          // App's lifetime; nothing reads the artifact yet (future
          // replay / projection consumers will).
          {
            const sink = eventSinkRef.current;
            const eventizer = eventizerRef.current;
            if (sink && eventizer) {
              const ctx = {
                model: ev.stats?.model ?? loop.model ?? model,
                prefixHash,
                reasoningEffort: loop.reasoningEffort ?? "max",
              };
              for (const out of eventizer.consume(ev, ctx)) sink.append(out);
            }
          }
          if (eventSubscribersRef.current.size > 0) {
            const dashMsg = loopEventToDashboard(ev, { assistantId });
            if (dashMsg) broadcastDashboardEvent(dashMsg);
          }
          // Status lines are transient 闂?any primary event (streaming
          // starts, a tool fires, etc.) means whatever we were waiting
          // FOR has now arrived, so drop the hint. We do this uniformly
          // at the top of the loop body for every role except "status"
          // itself (which SETS the line).
          if (ev.role !== "status") {
            setStatusLine((cur) => (cur ? null : cur));
          }
          if (ev.role === "status") {
            setStatusLine(ev.content);
          } else if (ev.role === "assistant_delta") {
            if (ev.content) contentBuf.current += ev.content;
            if (ev.reasoningDelta) reasoningBuf.current += ev.reasoningDelta;
          } else if (ev.role === "tool_call_delta") {
            if (ev.toolName) {
              toolCallBuildBuf.current = {
                name: ev.toolName,
                chars: ev.toolCallArgsChars ?? 0,
                index: ev.toolCallIndex,
                readyCount: ev.toolCallReadyCount,
              };
            }
          } else if (ev.role === "assistant_final") {
            lastAssistantText = ev.content || streamRef.text;
            handleAssistantFinal(ev, {
              flush,
              translator,
              streamRef,
              contentBuf,
              reasoningBuf,
              toolCallBuildBuf,
              assistantId,
              setSummary,
              log,
              broadcastDashboardEvent,
              getSessionSummary: () => loop.stats.summary(),
              session: session ?? null,
              assistantIterCounter,
              codeModeOn: !!codeMode,
              currentRootDir,
              editModeRef,
              recordEdit,
              armUndoBanner,
              pendingEdits,
              syncPendingCount,
              ctxMax: DEEPSEEK_CONTEXT_TOKENS[loop.model] ?? DEFAULT_CONTEXT_TOKENS,
            });
            if (session) {
              const m = loadSessionMeta(session);
              const cost = (m.totalCostUsd ?? 0) + (ev.stats?.cost ?? 0);
              const turn = (m.turnCount ?? 0) + 1;
              const currency = walletCurrencyRef.current;
              const u = ev.stats?.usage;
              const cacheHitTokens = (m.cacheHitTokens ?? 0) + (u?.promptCacheHitTokens ?? 0);
              const cacheMissTokens = (m.cacheMissTokens ?? 0) + (u?.promptCacheMissTokens ?? 0);
              patchSessionMeta(session, {
                totalCostUsd: cost,
                turnCount: turn,
                cacheHitTokens,
                cacheMissTokens,
                ...(u?.promptTokens ? { lastPromptTokens: u.promptTokens } : {}),
                ...(currency ? { balanceCurrency: currency } : {}),
              });
            }
          } else if (ev.role === "tool_start") {
            handleToolStart(ev, {
              setOngoingTool,
              setToolProgress,
              toolStartedAtRef,
              translator,
              codeModeOn: !!codeMode,
              recordRecentFile,
            });
          } else if (ev.role === "tool") {
            handleToolEvent(ev, {
              flush,
              translator,
              setOngoingTool,
              setToolProgress,
              toolStartedAtRef,
              setPendingShell,
              setPendingPlan,
              setPendingRevision,
              setPendingChoice,
              planStepsRef,
              completedStepIdsRef,
              planBodyRef,
              planSummaryRef,
              persistPlanState,
              log,
              session: session ?? null,
              codeModeOn: !!codeMode,
            });
          } else if (ev.role === "error") {
            handleErrorEvent(ev, {
              log,
              setOngoingTool,
              setToolProgress,
              toolStartedAtRef,
              translator,
            });
          } else if (ev.role === "warning") {
            handleWarningEvent(ev, { log, setTurnOnPro });
          }
        }
        flush();

        // Stop hooks 闂?turn has ended (or aborted). Block decisions are
        // meaningless past this point so we treat every non-pass as a
        // warning. Natural place for "after every turn, run the
        // formatter / lint / tests" automation.
        if (hookList.some((h) => h.event === "Stop")) {
          const stopReport = await runHooks({
            hooks: hookList,
            payload: {
              event: "Stop",
              cwd: currentRootDir,
              lastAssistantText: streamRef.text,
              turn: loop.stats.summary().turns,
            },
          });
          for (const o of stopReport.outcomes) {
            if (o.decision === "pass") continue;
            log.pushWarning(t("app.hookStop"), formatHookOutcomeMessage(o));
          }
        }
        qq.maybeSendFinalReply(lastAssistantText);
      } finally {
        clearInterval(timer);
        // Esc aborted the turn 闂?close any in-flight cards (streaming /
        // reasoning / tool / branch) so they leave the live region. Without
        // this, stranded done=false cards stick in CardStream's live tail.
        if (abortedThisTurn.current) {
          translator.abort();
        }
        clearToolProgressDisplay();
        setSummary(loop.stats.summary());
        setBusy(false);
        submittingRef.current = false;
        qq.clearTurnReply();
        // Clear pro-on-turn badge; armed-for-next-turn already cleared
        // at turn start when it was consumed.
        setTurnOnPro(false);
        // Refresh balance lazily 闂?don't block the return.
        refreshBalance();
      }
    },
    [
      busy,
      codeApply,
      codeDiscard,
      codeHistory,
      codeMode,
      codeShowEdit,
      codeUndo,
      currentRootDir,
      quitProcess,
      hookList,
      loop,
      latestVersion,
      mcpSpecs,
      liveMcpServers,
      models,
      planMode,
      session,
      slashSelected,
      slashUsage,
      atState,
      atSelected,
      pickAtMention,
      recordRecentFile,
      slashArgMatches,
      slashArgContext,
      slashArgSelected,
      pickSlashArg,
      togglePlanMode,
      writeTranscript,
      recordEdit,
      armUndoBanner,
      sealCurrentEntry,
      editMode,
      editModeRef,
      setEditMode,
      pendingEdits,
      syncPendingCount,
      setCurrentRootDir,
      reloadHooks,
      setOngoingTool,
      setToolProgress,
      setStatusLine,
      clearToolProgressDisplay,
      refreshBalance,
      refreshLatestVersion,
      refreshModels,
      proArmed,
      setProArmed,
      setTurnOnPro,
      persistPlanState,
      stdout,
      stopLoop,
      startLoop,
      getLoopStatus,
      qq,
      isLoopActive,
      isLoopFiring,
      clearFiringFlag,
      startWalkthrough,
      startDashboard,
      stopDashboard,
      getDashboardUrl,
      broadcastDashboardEvent,
      touchedPaths,
      model,
      prefixHash,
      log,
      agentStore.dispatch,
      mcpRuntime,
      pushHistory,
      resetCursor,
    ],
  );

  // Mirror the latest handleSubmit so the /loop timer (set up below)
  // calls the freshest closure on each firing 闂?config changes during
  // the loop (model, mode, etc.) take effect immediately.
  useEffect(() => {
    handleSubmitRef.current = handleSubmit;
  }, [handleSubmit]);

  /**
   * ShellConfirm callback. Resolves the PauseGate so the
   * blocked tool function can proceed. The tool handles running the
   * command (or throwing on deny) 闂?no synthetic user message needed.
   */
  const handleShellConfirm = useCallback(
    (choice: ShellConfirmChoice, denyContext?: string) => {
      const pending = pendingShell;
      if (!pending || !codeMode) return;
      const { id, command: cmd, kind } = pending;
      setPendingShell(null);

      if (choice === "deny") {
        const context = denyContext ? ` because: ${denyContext}` : "";
        log.pushInfo(t("app.denied", { cmd, context }));
        pauseGate.resolve(id, { type: "deny", denyContext });
      } else if (choice === "always_allow") {
        const prefix = derivePrefix(cmd);
        log.pushInfo(t("app.alwaysAllowed", { prefix, dir: currentRootDir }));
        pauseGate.resolve(id, { type: "always_allow", prefix });
      } else {
        log.pushInfo(
          kind === "run_background"
            ? t("app.startingBackground", { cmd })
            : t("app.runningCommand", { cmd }),
        );
        pauseGate.resolve(id, { type: "run_once" });
      }
    },
    [pendingShell, codeMode, currentRootDir, log],
  );

  /** PathConfirm callback 闂?mirrors handleShellConfirm. Resolves the gate, no synthetic user message. */
  const handlePathConfirm = useCallback(
    (choice: "run_once" | "always_allow" | "deny", denyContext?: string) => {
      const pending = pendingPath;
      if (!pending) return;
      const { id, allowPrefix } = pending;
      setPendingPath(null);
      if (choice === "deny") {
        pauseGate.resolve(id, { type: "deny", denyContext });
      } else if (choice === "always_allow") {
        pauseGate.resolve(id, { type: "always_allow", prefix: allowPrefix });
      } else {
        pauseGate.resolve(id, { type: "run_once" });
      }
    },
    [pendingPath],
  );

  /** Bail out of every pending modal + the awaiting tool fn behind it.
   *  Called by Esc-during-busy and by /new 闂?without this, a tool stuck
   *  on `pauseGate.ask` ignores the AbortSignal and the turn never ends. */
  const resetPendingModals = useCallback(() => {
    const editResolve = editReviewResolveRef.current;
    if (editResolve) {
      editReviewResolveRef.current = null;
      setPendingEditReview(null);
      editResolve({ choice: "reject" });
    }
    setPendingShell(null);
    setPendingPath(null);
    setPendingPlan(null);
    setPendingCheckpoint(null);
    setPendingRevision(null);
    setPendingChoice(null);
    setStagedInput(null);
    setStagedChoiceCustom(null);
    setStagedCheckpointRevise(null);
    pendingGateIdRef.current = null;
    qq.resetInteractions();
    pauseGate.cancelAll();
  }, [qq]);

  // Drain queued submits after the in-flight turn tears down.
  // QQ pause-gate replies are the one exception: they need to re-enter
  // handleSubmit while the turn is still "busy" so the blocked
  // pauseGate.ask() can be resolved from the remote reply.
  useEffect(() => {
    if (queuedSubmit === null) return;
    const canBypassBusy = qq.canBypassBusy(queuedSubmit);
    if ((!busy && !submittingRef.current) || canBypassBusy) {
      const text = queuedSubmit;
      setQueuedSubmit(null);
      void handleSubmit(text);
    }
  }, [busy, queuedSubmit, handleSubmit, qq]);

  /**
   * PlanConfirm callback. Three outcomes, all ending with a synthetic
   * user message so the model sees the verdict on its next turn:
   *   - approve 闂?exit plan mode, tell the model to implement now.
   *   - refine  闂?stay in plan mode, tell the model to revise.
   *   - cancel  闂?exit plan mode, tell the model to drop the plan.
   * Mirrors handleShellConfirm's busy-queue dance 闂?if the turn is
   * still streaming "plan submitted, waiting" chatter when the user
   * picks, we abort it and queue the synthetic for the effect above.
   *
   * `approve` is also callable with no pending plan (via the
   * `/apply-plan` slash fallback, used when the model wrote a plan in
   * assistant text instead of calling submit_plan). In that case we
   * just flip plan mode off and push the implement-now message.
   */
  const handlePlanConfirm = useCallback(
    async (choice: PlanConfirmChoice) => {
      const hadPendingPlan = pendingPlan !== null;
      if (!hadPendingPlan && choice !== "approve") {
        // Refine / Cancel without a pending plan is a no-op; only the
        // /apply-plan fallback makes sense without one.
        return;
      }

      if (choice === "refine" || choice === "approve") {
        if (pendingPlan) {
          const questions = extractOpenQuestionsSection(pendingPlan) ?? undefined;
          setStagedInput({ plan: pendingPlan, mode: choice, questions });
          setPendingPlan(null);
        } else if (choice === "approve") {
          setStagedInput({ plan: "", mode: "approve" });
        }
        return;
      }

      if (choice === "revise") {
        if (pendingPlan) {
          setPendingReviseEditor(pendingPlan);
          setPendingPlan(null);
        }
        return;
      }

      // Cancel ("reject"). Open the same staged input as approve/refine so
      // the user can tell the model *why* 闂?symmetric with the deny-tool
      // "press Tab to add reason" pattern. Empty Enter still cancels cleanly.
      if (pendingPlan) {
        const questions = extractOpenQuestionsSection(pendingPlan) ?? undefined;
        setStagedInput({ plan: pendingPlan, mode: "reject", questions });
        setPendingPlan(null);
      }
    },
    [pendingPlan],
  );

  // Ref-wrapped stable alias. `handlePlanConfirm` has deps that churn
  // every turn (busy toggles while the model is still streaming its
  // wrap-up) 闂?passing it directly to `React.memo(PlanConfirm)` breaks
  // the memo's shallow prop compare, so even without the ticker the
  // picker re-rendered on every parent state change. The ref keeps the
  // identity stable across the whole picker lifetime; the callback
  // itself always reads the latest closure via `.current`.
  const handlePlanConfirmRef = useRef(handlePlanConfirm);
  useEffect(() => {
    handlePlanConfirmRef.current = handlePlanConfirm;
  }, [handlePlanConfirm]);
  useEffect(() => {
    handlePlanCancelRef.current = () => handlePlanConfirmRef.current("cancel");
  }, []);
  const stableHandlePlanConfirm = useCallback(
    async (choice: PlanConfirmChoice) => handlePlanConfirmRef.current(choice),
    [],
  );

  /**
   * Fired when the user submits feedback from the inline input. The
   * staged `mode` decides whether this is a refine or approve: refine
   * stays in plan mode and asks the model to revise; approve exits
   * plan mode and pushes the implement synthetic, with any user
   * guidance (answers to open questions, last-minute preferences)
   * included verbatim.
   */
  const handleStagedInputSubmit = useCallback(
    async (
      feedback: string,
      override?: { plan: string; mode: "refine" | "approve" | "reject" },
    ) => {
      // `override` lets the web `/dashboard` chat-bridge drive the same
      // dispatch path without first having to setStagedInput() (which
      // is async and would race the read below). When the override is
      // present we also clear pendingPlan ourselves since web flow
      // doesn't go through the picker 闂?input two-step.
      const staged = override ?? stagedInput;
      if (override) {
        setPendingPlan(null);
      } else {
        setStagedInput(null);
      }
      if (!staged) return;
      const trimmed = feedback.trim();
      const tail = trimmed.length > 50 ? `${trimmed.slice(0, 50)}...` : trimmed;

      let marker: string;
      if (staged.mode === "approve") {
        togglePlanMode(false);
        // Materialize the approved plan as an "active" card so PlanLiveRow
        // can dock it at the bottom 闂?without this dispatch, no card with
        // variant: "active" exists and the live strip stays empty.
        const approvedSteps = planStepsRef.current;
        if (approvedSteps && approvedSteps.length > 0) {
          completedStepIdsRef.current = new Set();
          log.showPlan({
            title: planSummaryRef.current ?? "plan",
            steps: approvedSteps.map((s) => ({
              id: s.id,
              title: s.title,
              status: "queued" as const,
            })),
            variant: "active",
          });
          persistPlanState();
        }
        marker = trimmed
          ? `plan approved + instructions - ${tail}`
          : "plan approved - implementing";
      } else if (staged.mode === "reject") {
        // Drop the structured plan state 闂?the user said this path is wrong,
        // no point keeping it around for resume.
        planStepsRef.current = null;
        completedStepIdsRef.current = new Set();
        planBodyRef.current = null;
        planSummaryRef.current = null;
        persistPlanState();
        togglePlanMode(false);
        agentStore.dispatch({ type: "plan.drop" });
        marker = trimmed ? `plan rejected - ${tail}` : "plan cancelled";
      } else {
        marker = trimmed ? `refining - ${tail}` : "refining - using safe defaults";
      }
      log.pushInfo(marker);

      // Resolve the PauseGate so the blocked submit_plan tool function
      // returns. The user's typed feedback rides on the verdict so the
      // model sees it as the tool result 闂?without this, refine looked
      // identical to "user requested refinement" with no payload (#533).
      const gateId = pendingGateIdRef.current;
      if (gateId !== null) {
        const fb = trimmed || undefined;
        if (staged.mode === "approve") {
          pauseGate.resolve(gateId, { type: "approve", feedback: fb });
        } else if (staged.mode === "reject") {
          pauseGate.resolve(gateId, { type: "cancel", feedback: fb });
        } else {
          pauseGate.resolve(gateId, { type: "refine", feedback: fb });
        }
      }
    },
    [stagedInput, togglePlanMode, persistPlanState, agentStore, log],
  );
  // Ref-mirror so startDashboard's resolvePlanConfirm closure can call
  // the latest function 闂?handleStagedInputSubmit's deps churn on every
  // stagedInput change, which would freeze a captured reference.
  const handleStagedInputSubmitRef = useRef(handleStagedInputSubmit);
  useEffect(() => {
    handleStagedInputSubmitRef.current = handleStagedInputSubmit;
  }, [handleStagedInputSubmit]);
  useEffect(() => {
    handlePlanFeedbackRef.current = (feedback, override) =>
      handleStagedInputSubmitRef.current(feedback, override);
  }, []);

  /** Esc on the inline input 闂?restore the picker without resuming. */
  const handleStagedInputCancel = useCallback(() => {
    if (stagedInput?.plan) setPendingPlan(stagedInput.plan);
    setStagedInput(null);
  }, [stagedInput]);

  /**
   * ChoiceConfirm callback. Pick fires a synthetic "user picked <id>"
   * and lets the model continue down that branch. Custom defers to a
   * free-form input. Cancel drops the question entirely.
   */
  const handleChoiceConfirm = useCallback(
    async (choice: ChoiceConfirmChoice) => {
      const snap = pendingChoice;
      if (!snap) return;
      setPendingChoice(null);
      if (choice.kind === "custom") {
        setStagedChoiceCustom(snap);
        return;
      }
      const gateId = pendingGateIdRef.current;
      if (choice.kind === "cancel") {
        if (gateId !== null) pauseGate.resolve(gateId, { type: "cancel" });
        return;
      }
      const picked = snap.options.find((o) => o.id === choice.optionId);
      if (gateId !== null) {
        pauseGate.resolve(gateId, { type: "pick", optionId: choice.optionId });
      }
    },
    [pendingChoice],
  );

  // Ref-wrap to keep ChoiceConfirm's React.memo from re-rendering on
  // every parent tick (same pattern as PlanConfirm / CheckpointConfirm).
  // Stable refs over the modal handlers 闂?used by the web chat-bridge
  // to drive the same code path as a TUI button click without
  // dragging the handlers (and their ever-shifting deps) into
  // startDashboard's useCallback closure.
  useEffect(() => {
    handleShellConfirmRef.current = handleShellConfirm;
  }, [handleShellConfirm]);
  useEffect(() => {
    handlePathConfirmRef.current = handlePathConfirm;
  }, [handlePathConfirm]);
  // Listen for pause requests from tool functions (via PauseGate).
  // Dispatches to the correct modal based on request.kind.
  // Also sends notifications to QQ channel when QQ is connected.
  // biome-ignore lint/correctness/useExhaustiveDependencies: setters, editModeRef, and chatScroll (store handle) are stable; the listener installs once per mount and reads only refs/setters from closure
  useEffect(() => {
    return pauseGate.on((request) => {
      const payload = request.payload as Record<string, unknown>;
      pendingGateIdRef.current = request.id;
      // Modal pickers reserve viewport rows from the bottom; if the chat is
      // scrolled up, the picker mounts off-screen and the user can't see it.
      chatScroll.jumpToBottom();

      qq.handlePauseRequest(request.kind, payload);

      switch (request.kind) {
        case "run_command":
        case "run_background": {
          const p = payload as {
            command: string;
            cwd?: string;
            timeoutSec?: number;
            waitSec?: number;
          };
          setPendingShell({
            id: request.id,
            command: p.command,
            kind: request.kind,
            cwd: p.cwd,
            timeoutSec: p.timeoutSec,
            waitSec: p.waitSec,
          });
          break;
        }
        case "path_access": {
          const auto = autoResolveVerdict(request, editModeRef.current);
          if (auto !== null) {
            pauseGate.resolve(request.id, auto);
            break;
          }
          const p = payload as {
            path: string;
            intent: "read" | "write";
            toolName: string;
            sandboxRoot: string;
            allowPrefix: string;
          };
          setPendingPath({
            id: request.id,
            path: p.path,
            intent: p.intent,
            toolName: p.toolName,
            sandboxRoot: p.sandboxRoot,
            allowPrefix: p.allowPrefix,
          });
          break;
        }
        case "plan_proposed": {
          const p = payload as { plan: string; steps?: PlanStep[]; summary?: string };
          setPendingPlan(p.plan);
          planStepsRef.current = p.steps ?? null;
          planSummaryRef.current = p.summary ?? null;
          planBodyRef.current = p.plan;
          break;
        }
        case "plan_checkpoint": {
          const p = payload as {
            stepId: string;
            title?: string;
            result: string;
            notes?: string;
          };
          // completed/total come from planStepsRef 闂?don't have them via gate
          const completed = completedStepIdsRef.current.size;
          const total = planStepsRef.current?.length ?? 0;
          // Shared policy (src/core/pause-policy.ts) decides whether to
          // auto-resolve. Per-step rollback snapshot still runs so /restore
          // granularity is preserved.
          if (shouldAutoResolveCheckpoint(editModeRef.current)) {
            handleAutoCheckpointContinueRef.current(p.stepId, p.title);
            pauseGate.resolve(request.id, { type: "continue" });
            break;
          }
          setPendingCheckpoint({
            stepId: p.stepId,
            title: p.title,
            completed,
            total,
          });
          break;
        }
        case "plan_revision": {
          const p = payload as {
            reason: string;
            remainingSteps: PlanStep[];
            summary?: string;
          };
          setPendingRevision({
            reason: p.reason,
            remainingSteps: p.remainingSteps,
            summary: p.summary,
          });
          break;
        }
        case "choice": {
          const p = payload as {
            question: string;
            options: unknown[];
            allowCustom: boolean;
          };
          setPendingChoice({
            question: p.question,
            options: p.options as ChoiceOption[],
            allowCustom: p.allowCustom,
          });
          break;
        }
      }
    });
  }, [log, qq]);
  // Ref-mirror of pendingPlan so the web's resolvePlanConfirm callback
  // (registered in startDashboard, frozen at boot) can read the live
  // body when the web resolves an approve/refine.
  const pendingPlanRef = useRef<string | null>(null);
  useEffect(() => {
    pendingPlanRef.current = pendingPlan;
  }, [pendingPlan]);
  const handleChoiceConfirmRef = useRef(handleChoiceConfirm);
  useEffect(() => {
    handleChoiceConfirmRef.current = handleChoiceConfirm;
  }, [handleChoiceConfirm]);
  const stableHandleChoiceConfirm = useCallback(
    async (choice: ChoiceConfirmChoice) => handleChoiceConfirmRef.current(choice),
    [],
  );

  /**
   * Checkpoint picker callback. Resolves the PauseGate so the blocked
   * mark_step_complete tool function can return (or throw).
   */
  const handleCheckpointConfirm = useCallback(
    (choice: "continue" | "revise" | "stop") => {
      const snap = pendingCheckpoint;
      if (!snap) return;
      setPendingCheckpoint(null);
      const gid = pendingGateIdRef.current;
      if (choice === "revise") {
        // Don't resolve the gate yet 闂?wait for the staged feedback input
        // and let handleCheckpointReviseSubmit resolve with the feedback text.
        setStagedCheckpointRevise(snap);
        return;
      }
      // Auto file-snapshot per plan step
      if (codeMode && choice === "continue") {
        const paths = touchedPaths();
        if (paths.length > 0) {
          try {
            const cpName = snap.title ? `${snap.stepId} - ${snap.title}` : snap.stepId;
            const meta = createCheckpoint({
              rootDir: codeMode.rootDir,
              name: cpName.slice(0, 60),
              paths,
              source: "auto-pre-restore",
            });
            log.pushInfo(
              t("app.checkpointSaved", {
                id: meta.id,
                count: meta.fileCount,
                s: meta.fileCount === 1 ? "" : "s",
              }),
            );
          } catch {
            /* best-effort */
          }
        }
      }
      if (gid !== null) {
        pauseGate.resolve(gid, {
          type: choice === "continue" ? "continue" : "stop",
        });
      }
      const label = snap.title ? `${snap.stepId} - ${snap.title}` : snap.stepId;
      const counter = snap.total > 0 ? ` (${snap.completed}/${snap.total})` : "";
      log.pushInfo(
        choice === "continue"
          ? t("app.continuingAfter", { label, counter })
          : t("app.planStoppedAt", { label, counter }),
      );
    },
    [pendingCheckpoint, codeMode, touchedPaths, log],
  );
  useEffect(() => {
    handleCheckpointConfirmRef.current = handleCheckpointConfirm;
  }, [handleCheckpointConfirm]);

  const handleAutoCheckpointContinue = useCallback(
    (stepId: string, title?: string) => {
      if (codeMode) {
        const paths = touchedPaths();
        if (paths.length > 0) {
          try {
            const cpName = title ? `${stepId} - ${title}` : stepId;
            createCheckpoint({
              rootDir: codeMode.rootDir,
              name: cpName.slice(0, 60),
              paths,
              source: "auto-pre-restore",
            });
          } catch {
            /* best-effort */
          }
        }
      }
      const completed = completedStepIdsRef.current.size;
      const total = planStepsRef.current?.length ?? 0;
      const label = title ? `${stepId} - ${title}` : stepId;
      const counter = total > 0 ? ` (${completed}/${total})` : "";
      log.pushInfo(t("app.continuingAfter", { label, counter }));
    },
    [codeMode, touchedPaths, log],
  );
  const handleAutoCheckpointContinueRef = useRef(handleAutoCheckpointContinue);
  useEffect(() => {
    handleAutoCheckpointContinueRef.current = handleAutoCheckpointContinue;
  }, [handleAutoCheckpointContinue]);
  const stableHandleCheckpointConfirm = useCallback(
    (choice: "continue" | "revise" | "stop") => handleCheckpointConfirmRef.current(choice),
    [],
  );

  /** Revise feedback submitted 闂?resolves the gate with feedback. */
  const handleCheckpointReviseSubmit = useCallback(
    (feedback: string, snapOverride?: { stepId: string; title?: string }) => {
      const snap = snapOverride;
      setStagedCheckpointRevise(null);
      if (!snap) return;
      const label = snap.title ? `${snap.stepId} - ${snap.title}` : snap.stepId;
      const trimmed = feedback.trim();
      const gid = pendingGateIdRef.current;
      if (gid !== null) {
        pauseGate.resolve(
          gid,
          trimmed ? { type: "revise", feedback: trimmed } : { type: "revise" },
        );
      }
      const marker = trimmed
        ? t("app.revisingAfter", {
            label,
            feedback: trimmed.length > 50 ? `${trimmed.slice(0, 50)}...` : trimmed,
          })
        : t("app.continuingAfter", { label, counter: "" });
      log.pushInfo(marker);
    },
    [log],
  );

  const handleCheckpointReviseCancel = useCallback(() => {
    const snap = stagedCheckpointRevise;
    setStagedCheckpointRevise(null);
    if (snap) setPendingCheckpoint(snap);
  }, [stagedCheckpointRevise]);

  // Ref-mirrors so the web's resolveXxx callbacks (registered in
  // startDashboard, frozen at boot) keep calling the latest handler.
  useEffect(() => {
    handleCheckpointReviseSubmitRef.current = handleCheckpointReviseSubmit;
  }, [handleCheckpointReviseSubmit]);

  /** Custom free-form answer submitted 闂?resolves the PauseGate with the typed text. */
  const handleChoiceCustomSubmit = useCallback((answer: string) => {
    setStagedChoiceCustom(null);
    const trimmed = answer.trim();
    const gateId = pendingGateIdRef.current;
    if (gateId !== null) {
      pauseGate.resolve(gateId, { type: "text", text: trimmed || "" });
    }
  }, []);

  /** Esc on the custom input 闂?restore the choice picker. */
  const handleChoiceCustomCancel = useCallback(() => {
    const snap = stagedChoiceCustom;
    setStagedChoiceCustom(null);
    if (snap) setPendingChoice(snap);
  }, [stagedChoiceCustom]);
  useEffect(() => {
    handleChoiceResolveRef.current = (resolution) => {
      if (resolution.type === "pick") {
        void handleChoiceConfirmRef.current({ kind: "pick", optionId: resolution.optionId });
        return;
      }
      if (resolution.type === "text") {
        setPendingChoice(null);
        handleChoiceCustomSubmit(resolution.text);
        return;
      }
      void handleChoiceConfirmRef.current({ kind: "cancel" });
    };
  }, [handleChoiceCustomSubmit]);

  /**
   * PlanReviseConfirm callback. Accept splices the new remaining
   * steps onto the done prefix and continues. Reject drops the
   * proposal and tells the model to stick with the original plan.
   */
  const handleReviseConfirm = useCallback(
    (choice: ReviseChoice) => {
      const snap = pendingRevision;
      if (!snap) return;
      setPendingRevision(null);
      const gateId = pendingGateIdRef.current;
      if (choice === "reject") {
        if (gateId !== null) pauseGate.resolve(gateId, { type: "rejected" });
        return;
      }
      // Accept: keep the done-step prefix from the existing plan, replace
      // the rest with the proposed remainingSteps. completedStepIds
      // stays intact 闂?done work isn't undone.
      const completed = completedStepIdsRef.current;
      const oldSteps = planStepsRef.current ?? [];
      const donePrefix = oldSteps.filter((s) => completed.has(s.id));
      const merged: PlanStep[] = [...donePrefix];
      for (const s of snap.remainingSteps) {
        if (completed.has(s.id)) continue; // already done 闂?don't re-queue
        merged.push(s);
      }
      planStepsRef.current = merged;
      persistPlanState();
      // Replace the live active card so PlanLiveRow shows the new tail 闂?      // existing card's stale ids would fail subsequent step completes.
      agentStore.dispatch({ type: "plan.drop" });
      log.showPlan({
        title: planSummaryRef.current ?? "plan",
        steps: merged.map((s) => ({
          id: s.id,
          title: s.title,
          status: completed.has(s.id) ? ("done" as const) : ("queued" as const),
        })),
        variant: "active",
      });
      if (gateId !== null) pauseGate.resolve(gateId, { type: "accepted" });
    },
    [pendingRevision, persistPlanState, agentStore, log],
  );

  // Ref-wrap to keep PlanReviseConfirm's React.memo from re-rendering.
  useEffect(() => {
    handleReviseConfirmRef.current = (choice) => {
      if (choice === "cancel") {
        const gateId = pendingGateIdRef.current;
        setPendingRevision(null);
        if (gateId !== null) pauseGate.resolve(gateId, { type: "cancelled" });
        return;
      }
      return handleReviseConfirm(choice);
    };
  }, [handleReviseConfirm]);
  const stableHandleReviseConfirm = useCallback(
    async (choice: ReviseChoice) => handleReviseConfirmRef.current(choice),
    [],
  );

  // Suspend cosmetic animations during modal interactions and idle so
  // a quiescent TUI is byte-stable.
  const tickerSuspended = modalOpen || (!busy && !isStreaming);

  if (!bootReady) return <BootSplash />;

  return (
    <>
      <HistoryTypingCapture
        input={input}
        setInput={setInput}
        enabled={!modalOpen && !busy}
        onReturnToBottom={chatScroll.jumpToBottom}
      />
      <TickerProvider disabled={tickerSuspended}>
        <ViewportBudgetProvider>
          <InflightProvider inflight={loop.inflight}>
            <Box flexDirection="row" height={stdout?.rows ?? 24}>
              <Box flexDirection="column" flexGrow={1}>
                <Box flexDirection="column" flexGrow={1}>
                  <LiveExpandContext.Provider value={liveExpand}>
                    <CardStream suppressLive={modalOpen} />
                  </LiveExpandContext.Provider>
                  {/*
          Welcome card on the empty state. Visible only when nothing
          has happened yet (no past events, nothing in flight, no
          modal up). Removes the "what do I type?" friction without
          surviving past the first turn.
        */}
                  {!hasConversation && !busy && !isStreaming && slashMatches === null ? (
                    <WelcomeBanner
                      inCodeMode={!!codeMode}
                      workspaceRoot={codeMode ? currentRootDir : undefined}
                      dashboardUrl={dashboardUrl}
                      languageVersion={languageVersion}
                    />
                  ) : null}
                  {/*
          Live rows are hidden while the ShellConfirm modal is up 闂?the
          model's concurrent "please confirm" stream is noise the user
          doesn't need, and the picker shouldn't fight it for visual
          attention. They come back naturally once the user chooses and
          the next turn begins.
        */}
                  {noTakeoverOverlay && ongoingTool ? (
                    <OngoingToolRow tool={ongoingTool} progress={toolProgress} />
                  ) : null}
                  {noTakeoverOverlay && subagentActivities.length > 0 ? (
                    <SubagentLiveStack activities={subagentActivities} max={3} />
                  ) : null}
                  {noTakeoverOverlay && !ongoingTool && statusLine ? (
                    <ThinkingRow text={statusLine} />
                  ) : null}
                  {undoBanner &&
                  !pendingShell &&
                  !pendingPlan &&
                  !pendingReviseEditor &&
                  !pendingSessionsPicker &&
                  !pendingCheckpointPicker &&
                  !pendingMcpHub &&
                  !stagedInput &&
                  !pendingEditReview &&
                  !pendingChoice &&
                  !stagedChoiceCustom &&
                  !pendingRevision &&
                  !stagedCheckpointRevise &&
                  !pendingCheckpoint ? (
                    <UndoBanner banner={undoBanner} />
                  ) : null}
                  {/* Activity row when no targeted indicator is visible 闂?phase label from useActivityLabel. */}
                  {noTakeoverOverlay && busy && !isStreaming && !ongoingTool && !statusLine ? (
                    <ThinkingRow text={activityLabel} />
                  ) : null}
                  {noTakeoverOverlay ? <PlanLiveRow /> : null}
                  <ToastRail />
                </Box>
                {stagedInput ? (
                  <PlanRefineInput
                    mode={stagedInput.mode}
                    questions={stagedInput.questions}
                    onSubmit={handleStagedInputSubmit}
                    onCancel={handleStagedInputCancel}
                  />
                ) : stagedChoiceCustom ? (
                  <PlanRefineInput
                    mode="choice-custom"
                    onSubmit={handleChoiceCustomSubmit}
                    onCancel={handleChoiceCustomCancel}
                  />
                ) : stagedCheckpointRevise ? (
                  <PlanRefineInput
                    mode="checkpoint-revise"
                    onSubmit={(text) => handleCheckpointReviseSubmit(text, stagedCheckpointRevise)}
                    onCancel={handleCheckpointReviseCancel}
                  />
                ) : pendingChoice ? (
                  <ChoiceConfirm
                    question={pendingChoice.question}
                    options={pendingChoice.options}
                    allowCustom={pendingChoice.allowCustom}
                    onChoose={stableHandleChoiceConfirm}
                  />
                ) : pendingRevision ? (
                  <PlanReviseConfirm
                    reason={pendingRevision.reason}
                    oldRemaining={(planStepsRef.current ?? []).filter(
                      (s) => !completedStepIdsRef.current.has(s.id),
                    )}
                    newRemaining={pendingRevision.remainingSteps}
                    summary={pendingRevision.summary}
                    onChoose={stableHandleReviseConfirm}
                  />
                ) : pendingCheckpoint ? (
                  <PlanCheckpointConfirm
                    stepId={pendingCheckpoint.stepId}
                    title={pendingCheckpoint.title}
                    completed={pendingCheckpoint.completed}
                    total={pendingCheckpoint.total}
                    steps={planStepsRef.current ?? undefined}
                    completedStepIds={completedStepIdsRef.current}
                    onChoose={stableHandleCheckpointConfirm}
                  />
                ) : pendingCheckpointPicker ? (
                  <CheckpointPicker
                    checkpoints={checkpointPickerList}
                    workspace={currentRootDir}
                    pickerPorts={pickerPorts}
                    onChoose={(outcome) => {
                      if (outcome.kind === "quit") {
                        setPendingCheckpointPicker(false);
                        return;
                      }
                      if (outcome.kind === "restore") {
                        const target = checkpointPickerList.find((c) => c.id === outcome.id);
                        setPendingCheckpointPicker(false);
                        if (!target) return;
                        const result = restoreCheckpoint(currentRootDir, target.id);
                        const lines = [
                          `闂?restored "${target.name}" (${target.id.slice(0, 7)}, ${fmtAgo(target.createdAt)})`,
                        ];
                        if (result.restored.length > 0) {
                          lines.push(
                            `  wrote ${result.restored.length} file${result.restored.length === 1 ? "" : "s"}`,
                          );
                        }
                        if (result.removed.length > 0) {
                          lines.push(
                            `  removed ${result.removed.length} file${result.removed.length === 1 ? "" : "s"}`,
                          );
                        }
                        if (result.skipped.length > 0) {
                          lines.push(
                            `  skipped ${result.skipped.length} file${result.skipped.length === 1 ? "" : "s"}`,
                          );
                        }
                        log.pushInfo(lines.join("\n"));
                        return;
                      }
                      if (outcome.kind === "delete") {
                        const target = checkpointPickerList.find((c) => c.id === outcome.id);
                        if (!target) return;
                        deleteCheckpoint(currentRootDir, target.id);
                        setCheckpointPickerList([...listCheckpoints(currentRootDir)].reverse());
                      }
                    }}
                  />
                ) : pendingSessionsPicker ? (
                  <SessionPicker
                    sessions={sessionsPickerList}
                    workspace={currentRootDir}
                    walletCurrency={walletCurrencyRef.current}
                    pickerPorts={pickerPorts}
                    onFocusChange={setSessionsPickerFocus}
                    onChoose={(outcome) => {
                      if (outcome.kind === "open") {
                        setPendingSessionsPicker(false);
                        if (onSwitchSession) {
                          onSwitchSession(outcome.name);
                        } else {
                          log.pushInfo(
                            `闂?to switch to "${outcome.name}", quit and run: reasonix chat --session ${outcome.name}`,
                          );
                        }
                        return;
                      }
                      if (outcome.kind === "new") {
                        setPendingSessionsPicker(false);
                        if (onSwitchSession) {
                          onSwitchSession(freshSessionName(session));
                        } else {
                          log.pushInfo(
                            "闂?to start a fresh session, quit and run: reasonix chat (no --session flag)",
                          );
                        }
                        return;
                      }
                      if (outcome.kind === "delete") {
                        deleteSession(outcome.name);
                        setSessionsPickerList(listSessionsForWorkspace(currentRootDir));
                        return;
                      }
                      if (outcome.kind === "rename") {
                        renameSession(outcome.name, outcome.newName);
                        setSessionsPickerList(listSessionsForWorkspace(currentRootDir));
                        return;
                      }
                      if (outcome.kind === "quit") {
                        setPendingSessionsPicker(false);
                      }
                    }}
                  />
                ) : pendingThemePicker ? (
                  <ThemePicker
                    currentPreference={loadTheme() ?? "auto"}
                    activeTheme={themeName}
                    onChoose={(outcome) => {
                      setPendingThemePicker(false);
                      if (outcome.kind === "quit") return;
                      saveTheme(outcome.value);
                      const active = resolveThemePreference(
                        outcome.value,
                        process.env.REASONIX_THEME,
                      );
                      setThemeName(active);
                      log.pushInfo(`闂?theme saved: ${outcome.value}\n  active now: ${active}`);
                    }}
                  />
                ) : pendingCopyMode ? (
                  <CopyMode
                    cards={agentStore.getState().cards}
                    onClose={(yanked) => {
                      setPendingCopyMode(false);
                      if (yanked) {
                        const path = yanked.filePath;
                        const info = yanked.osc52
                          ? t("copyMode.yankedToast", { size: yanked.size })
                          : t("copyMode.yankedToastFile", {
                              size: yanked.size,
                              path: path ?? "unknown",
                            });
                        log.pushInfo(info);
                      }
                    }}
                  />
                ) : pendingModelPicker ? (
                  <ModelPicker
                    models={models}
                    current={loop.model}
                    currentEffort={loop.reasoningEffort}
                    currentAutoEscalate={loop.autoEscalate}
                    onRefresh={refreshModels}
                    onChoose={(outcome) => {
                      setPendingModelPicker(false);
                      if (outcome.kind === "select") {
                        // Manual model pick = explicit pin: turn off auto-escalate
                        // so flash doesn't get bumped, persist inferred preset.
                        loop.configure({ model: outcome.id, autoEscalate: false });
                        agentStore.dispatch({ type: "session.model.change", model: outcome.id });
                        const inferred =
                          outcome.id === "deepseek-v4-pro"
                            ? "pro"
                            : outcome.id === "deepseek-v4-flash"
                              ? "flash"
                              : null;
                        setPreset(inferred ?? "flash");
                        agentStore.dispatch({
                          type: "session.preset.change",
                          preset: inferred,
                        });
                        if (inferred) {
                          try {
                            savePreset(inferred);
                          } catch {
                            /* disk full / perms 闂?runtime change still took effect */
                          }
                        }
                        log.pushInfo(`闂?model: ${outcome.id}`);
                        return;
                      }
                      if (outcome.kind === "preset") {
                        const p = PRESETS[outcome.name];
                        loop.configure({
                          model: p.model,
                          autoEscalate: p.autoEscalate,
                          reasoningEffort: p.reasoningEffort,
                        });
                        agentStore.dispatch({ type: "session.model.change", model: p.model });
                        setPreset(outcome.name);
                        agentStore.dispatch({
                          type: "session.preset.change",
                          preset: outcome.name,
                        });
                        try {
                          savePreset(outcome.name);
                        } catch {
                          /* disk full / perms 闂?runtime change still took effect */
                        }
                        log.pushInfo(`闂?preset: ${outcome.name} 闂?${p.model}`);
                      }
                    }}
                  />
                ) : pendingMcpHub ? (
                  <McpHub
                    initialTab={pendingMcpHub.tab}
                    liveServers={liveMcpServers}
                    configPath={defaultConfigPath()}
                    pickerPorts={pickerPorts}
                    onClose={() => setPendingMcpHub(null)}
                    postInfo={(text) => log.pushInfo(text)}
                    applyAppend={(target, addedTools) => {
                      const updated = applyMcpAppend(loop, target, addedTools);
                      setLiveMcpServers((prev) => replaceMcpServerSummary(prev, target, updated));
                      return updated;
                    }}
                    reloadMcp={
                      mcpRuntime
                        ? async () => {
                            const r = await mcpRuntime.reloadFromConfig(loop);
                            setLiveMcpServers(r.summaries);
                            return r;
                          }
                        : undefined
                    }
                  />
                ) : pendingPlan ? (
                  <PlanConfirm
                    plan={pendingPlan}
                    steps={planStepsRef.current ?? undefined}
                    summary={planSummaryRef.current ?? undefined}
                    onChoose={stableHandlePlanConfirm}
                    projectRoot={currentRootDir}
                  />
                ) : pendingReviseEditor ? (
                  <PlanReviseEditor
                    steps={planStepsRef.current ?? []}
                    completedStepIds={completedStepIdsRef.current}
                    onAccept={(revised, skippedIds) => {
                      planStepsRef.current = revised;
                      for (const id of skippedIds) completedStepIdsRef.current.add(id);
                      persistPlanState();
                      const planText = pendingReviseEditor;
                      setPendingReviseEditor(null);
                      setPendingPlan(planText);
                    }}
                    onCancel={() => {
                      const planText = pendingReviseEditor;
                      setPendingReviseEditor(null);
                      setPendingPlan(planText);
                    }}
                  />
                ) : pendingShell ? (
                  <ShellConfirm
                    command={pendingShell.command}
                    allowPrefix={derivePrefix(pendingShell.command)}
                    kind={pendingShell.kind}
                    cwd={pendingShell.cwd}
                    timeoutSec={pendingShell.timeoutSec}
                    waitSec={pendingShell.waitSec}
                    onChoose={handleShellConfirm}
                  />
                ) : pendingPath ? (
                  <PathConfirm
                    path={pendingPath.path}
                    intent={pendingPath.intent}
                    toolName={pendingPath.toolName}
                    sandboxRoot={pendingPath.sandboxRoot}
                    allowPrefix={pendingPath.allowPrefix}
                    onChoose={handlePathConfirm}
                  />
                ) : pendingEditReview ? (
                  <EditConfirm
                    block={pendingEditReview}
                    onChoose={(choice, denyContext) => {
                      const resolve = editReviewResolveRef.current;
                      if (resolve) {
                        editReviewResolveRef.current = null;
                        resolve({ choice, denyContext });
                      }
                    }}
                  />
                ) : walkthroughActive && pendingEdits.current.length > 0 ? (
                  <EditConfirm
                    // pendingTick re-keys the modal so each apply/discard
                    // forces a remount with the NEW first block. Without it,
                    // EditConfirm's internal scroll state would persist
                    // across blocks, which is the wrong UX.
                    key={`walk-${pendingTick}`}
                    block={pendingEdits.current[0]!}
                    onChoose={handleWalkChoice}
                  />
                ) : (
                  <InputAreaWithHistoryHint
                    inputArea={
                      <Box flexDirection="column" flexShrink={0} flexWrap="nowrap">
                        <Box flexDirection="column" flexShrink={0} flexWrap="nowrap">
                          {codeMode ? (
                            <ModeStatusBar
                              editMode={editMode}
                              pendingCount={pendingCount}
                              flash={modeFlash}
                              planMode={planMode}
                              undoArmed={!!undoBanner || hasUndoable()}
                              jobs={codeMode.jobs}
                            />
                          ) : null}
                          {activeLoop ? <LoopStatusRow loop={activeLoop} /> : null}
                          <StatusRow statusBar={statusBar} />
                          <PromptInput
                            value={input}
                            onChange={setInput}
                            onSubmit={handleSubmit}
                            disabled={busy}
                            onHistoryPrev={handleHistoryPrev}
                            onHistoryNext={handleHistoryNext}
                            onOpenExternalEditor={handleOpenExternalEditor}
                            onCursorChange={setComposerCursor}
                          />
                        </Box>
                        <Box flexDirection="column" flexShrink={0} flexWrap="nowrap">
                          {slashMatches !== null ? (
                            <SlashSuggestions
                              key={`slash-suggestions:${slashGroupMode ? "group" : "search"}`}
                              matches={slashMatches}
                              selectedIndex={slashSelected}
                              groupMode={slashGroupMode}
                              advancedHidden={slashAdvancedHidden}
                            />
                          ) : null}
                          {atState !== null ? (
                            <AtMentionSuggestions state={atState} selectedIndex={atSelected} />
                          ) : null}
                        </Box>
                        {slashArgContext ? (
                          <SlashArgPicker
                            matches={slashArgMatches}
                            selectedIndex={slashArgSelected}
                            spec={slashArgContext.spec}
                            kind={slashArgContext.kind}
                            partial={slashArgContext.partial}
                          />
                        ) : null}
                      </Box>
                    }
                  />
                )}
              </Box>
            </Box>
          </InflightProvider>
        </ViewportBudgetProvider>
      </TickerProvider>
    </>
  );
}
