import { lazy, Suspense, useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { CSSProperties, KeyboardEvent, MouseEvent as ReactMouseEvent, PointerEvent as ReactPointerEvent } from "react";
import { ShellExpandProvider, useShellExpand } from "./lib/shellExpand";
import gsap from "gsap";
import { useGSAP } from "@gsap/react";
import { Flip } from "gsap/Flip";
import { ScrollToPlugin } from "gsap/ScrollToPlugin";
gsap.registerPlugin(useGSAP, Flip, ScrollToPlugin);
import {
  Activity,
  CircleHelp,
  Command,
  Copy as RestoreIcon,
  Download,
  Minus,
  Search,
  Server,
  Square,
  SquarePen,
  PanelLeft,
  PanelRight,
  FileDown,
  FileImage,
  FileText,
  FileJson,
  GitBranch,
  MessageSquare,
  Settings as SettingsIcon,
  Pencil,
  Trash2,
  AlarmClock,
  Brain,
  Cpu,
  Palette,
  X,
} from "lucide-react";
import { useToast } from "./lib/toast";
import { useWailsResizeFix } from "./lib/useWailsResizeFix";
import { asArray } from "./lib/array";
import { clearLegacyLangPref, normalizeLangPref, readLegacyLangPref, t, useI18n, useT, type Translator } from "./lib/i18n";
import { localizedNoticeText, useController, type Item, type LiveStream } from "./lib/useController";
import { app, onEvent, onProjectTreeChanged, onReady, onRuntimeRebuilt, onSessionRecovered, openExternal } from "./lib/bridge";
import { generativeMusic, isGenerativeMusicEnabled } from "./lib/generative-music";
import { clearAttentionChimeKeys, playAttentionChime, playSuccessChime, shouldPlayAttentionChimeForEvent } from "./lib/sound";
import { NoticeCard, Transcript } from "./components/Transcript";
import { Composer } from "./components/Composer";
import { TranscriptSelectionMenu } from "./components/TranscriptSelectionMenu";
import { TodoPanel } from "./components/TodoPanel";
import { ApprovalModal } from "./components/ApprovalModal";
import { AskCard } from "./components/AskCard";
import { UndoRewindBanner } from "./components/UndoRewindBanner";
import { ClearContextCard } from "./components/ClearContextCard";

/** Footer decision surface kinds. Priority: tool/plan approval > ask > clear context. */
type DecisionSurfaceKind = "tool_approval" | "plan_approval" | "ask" | "clear_context";
import { StatusBar } from "./components/StatusBar";
import { RemoteHostKeyDialog } from "./components/RemoteHostKeyDialog";
import { onRemoteStatus, onRemoteForwards, onRemoteServer } from "./lib/bridge";
import { useRemoteStore, waitForRemoteConnection } from "./store/remote";
import { CommandPalette, type PaletteItem } from "./components/CommandPalette";
import { UpdateBanner } from "./components/UpdateBanner";
import { ContextPanel } from "./components/ContextPanel";
import { WorkspacePanel } from "./components/WorkspacePanel";
import { Tooltip } from "./components/Tooltip";
import { StartupSplash } from "./components/StartupSplash";
import { OnboardingOverlay } from "./components/OnboardingOverlay";
import { AppChrome } from "./components/AppChrome";
import { ShortcutsCheatsheet } from "./components/ShortcutsCheatsheet";
import { ProjectTree } from "./components/ProjectTree";
import { WorktreeBadge } from "./components/WorktreeBadge";
import { HeartbeatPanel } from "./custom/features/heartbeat/HeartbeatPanel";
import "./custom/features/heartbeat/heartbeat.css";
import { CopyButton } from "./components/CopyButton";
import { ExternalOpener } from "./components/ExternalOpener";
import { parseTodos } from "./lib/tools";
import {
  dismissedTodoKeyForScope,
  scopedTodoBatchKey,
  scopedTodoDismissalKey,
  shouldShowTodoPanel,
  todoBatchKey,
  todoDismissalKey,
  todoPanelScope,
} from "./lib/todoVisibility";
import {
  type BotConnectionView,
  type BotRuntimeStatusView,
  type BotSettingsView,
  type CollaborationMode,
  type ComposerInsertRequest,
  type DesktopStartupSettingsView,
  type Mode,
  modeHasPlan,
  type ProjectNode,
  type RemoteHostView,
  type SessionMeta,
  type SettingsView,
  type TabMeta,
  type TokenMode,
  type ToolApprovalMode,
} from "./lib/types";
import type { InvocationMetadataMap, StructuredInvocationSubmit } from "./lib/invocationDisplay";
import { formatSelectionReference, type SelectedTextInsertRequest } from "./lib/selectedTextContext";
import {
  composerProfileFromMeta,
  composerProfileFromTab,
  composerProfileMode,
  composerProfileWithMode,
  controllerComposerProfileCollaborationMode,
  defaultComposerProfile,
  displayedComposerProfileCollaborationMode,
  hydrateComposerProfileFromMeta,
  hydrateComposerProfilesFromTabs,
  patchComposerProfile,
  pruneUserPlanModeIntents,
  resolvePlanRestoreTabId,
  shouldRestoreUserPlanModeForProfile,
  updateUserPlanModeIntent,
  type ComposerProfile,
  type ComposerProfileField,
  type UserPlanModeIntents,
} from "./lib/composerProfile";
import {
  restorableToolApprovalMode,
  toggleYoloToolApprovalMode,
  type RestorableToolApprovalMode,
} from "./lib/toolApprovalMode";
import {
  CREATION_RIGHT_DOCK_MIN_RENDER_WIDTH,
  CREATION_RIGHT_DOCK_TREE_MIN_WIDTH,
  CREATION_SIDEBAR_MIN_WIDTH,
  RIGHT_DOCK_MAX_WIDTH,
  RIGHT_DOCK_MIN_RENDER_WIDTH,
  RIGHT_DOCK_PREVIEW_DEFAULT_WIDTH,
  RIGHT_DOCK_PREVIEW_MIN_WIDTH,
  RIGHT_DOCK_TREE_MAX_WIDTH,
  RIGHT_DOCK_TREE_MIN_WIDTH,
  type RightDockMode,
  SIDEBAR_MAX_WIDTH,
  SIDEBAR_MIN_WIDTH,
  applyLayoutStyleDefaults,
  clampCreationRightDockTreeWidth,
  clampCreationSidebarWidth,
  clampRightDockPreviewWidth,
  clampRightDockTreeWidth,
  clampSidebarWidth,
  defaultCreationRightDockTreeWidth,
  defaultCreationSidebarWidth,
  defaultRightDockTreeWidth,
  defaultSidebarWidth,
  saveRightDockPreviewWidth,
  saveRightDockTreeWidth,
  saveSidebarCollapsed,
  saveWorkspacePanelOpen,
  saveSidebarWidth,
  useLayoutStore,
} from "./store/layout";
import { useOverlayStore } from "./store/overlays";
import { hydrateDisplayMode } from "./lib/displayMode";
import { DEFAULT_STATUS_BAR_ITEMS, normalizeStatusBarItems, type StatusBarItemId } from "./lib/statusBarItems";
import { paletteSessionDisplayTitle, paletteSessionHint, paletteSessionKeywords, sessionActivityTime } from "./lib/session";
import { enqueueNavigationRequest, type PendingNavigationRequest } from "./lib/openTopicCoalescing";
import {
  applyTheme,
  clearLegacyThemePreference,
  getTheme,
  getThemeStyle,
  isThemeStyle,
  normalizeThemePreference,
  normalizeThemeStyleForTheme,
  readLegacyThemePreference,
  type Theme,
} from "./lib/theme";
import { applyConversationWidth } from "./lib/conversationWidth";
import { applyConfiguredBaseAppearance, applyThemePack, applyThemeScene, clearThemePack } from "./lib/themePack";
import { ThemeBackground } from "./components/ThemeBackground";
import { applyTextSize, DEFAULT_TEXT_SIZE, getTextSize, nextTextSize } from "./lib/textSize";
import { useViewportHeightVar, useWindowStatePersistence } from "./lib/windowState";
import { availableWorkspacePanelWidth, resolveLiveWorkspacePanelWidth, resolveWorkspacePanelWidth, workspacePanelAriaMinWidth } from "./lib/workspaceLayout";
import { createRafResizeUpdater } from "./lib/resizeDrag";
import { useGlobalShortcut } from "./lib/keyboardShortcuts";
import { topicShortcutIndexFromEvent, useTopicShortcuts, type TopicShortcutEntry } from "./lib/topicShortcuts";
import { composerDraftKeyForTab } from "./lib/composerDraftKey";
import { continueDelivery } from "./lib/deliveryContinue";
import logoWordmark from "./assets/logo-wordmark.svg";

function noticePreviewMockEnabled(): boolean {
  const value = browserMockScenarioParam();
  return value === "notice" || value === "notices" || value === "notice-preview";
}

function noticePreviewItems(): Item[] {
  const notice = (index: number, level: "info" | "warn", text: string, detail: string, code?: string): Item => ({
    kind: "notice",
    id: `notice-preview-${index}`,
    level,
    text: localizedNoticeText(text, code),
    detail,
  });
  return [
    {
      kind: "notice",
      id: "notice-preview-delivery",
      level: "info",
      variant: "delivery",
      title: t("notice.deliveryIncompleteTitle"),
      text: t("notice.deliveryIncompleteBody"),
      detail: "final-answer readiness failed 3 times: missing verification, review_report, and complete_step receipts",
      action: "continue_delivery",
    },
    notice(1, "info", "No visible answer was produced; asking the assistant to respond again.", "empty final answer blocked: qwen3.7-plus returned no visible answer text (finish=stop, reasoning=2314 chars); retrying", "empty_final"),
    notice(2, "info", "The assistant answered before taking action; asking it to use the required tools.", "executor handoff: assistant produced a proposal before running required repository commands; nudged to execute", "executor_handoff"),
    notice(3, "info", "Tool round limit reached; asking the assistant to summarize progress.", "tool budget reached after 128 tool calls; requesting a progress summary before continuing", "tool_budget"),
    notice(4, "info", "The assistant is stuck retrying a blocked action; asking it to change approach.", "loop guard: repeated command failure matched the same stderr signature across 3 attempts", "loop_guard"),
    notice(5, "info", "Context is getting large; preserving cache until cleanup is needed.", "context window 82% full; deferred cleanup to preserve reusable prompt cache"),
    notice(6, "info", "Context cleanup skipped for now.", "cleanup skipped: recent turn included unresolved user approval state"),
    notice(7, "info", "Automatic context cleanup paused because the context window is too small.", "configured compact threshold exceeds current model context window; auto cleanup paused for this model"),
    notice(8, "info", "Context was compacted without a generated summary.", "compaction completed after upstream summary generation returned empty content; retained transcript checkpoint"),
    notice(9, "info", "Planning mode enabled for this multi-step task.", "auto plan detector matched implementation task with repository edits and verification steps"),
    notice(10, "info", "Plan detection requested a plan.", "plan detector confidence=0.83; task includes multiple dependent UI/backend changes"),
    notice(11, "info", "Plan detection was uncertain; using the fallback planner heuristic.", "plan detector confidence=0.48; fallback heuristic selected because files and tests are involved"),
    notice(12, "info", "Goal is not ready to complete yet; continuing the remaining work.", "goal completion check found pending validation: desktop/frontend typecheck"),
    notice(13, "info", "Goal still has unfinished task state; continuing the remaining work.", "active goal has open task state: implement preview, verify browser, report result"),
    notice(14, "warn", "AutoResearch status update failed.", "autoresearch task completion update failed: write .reasonix/autoresearch/task-42/state/task_spec.json: permission denied"),
    notice(15, "warn", "AutoResearch task marked blocked.", "autoresearch task blocked: task-42\nreason: missing accepted verification evidence after three turns"),
    notice(16, "warn", "background export failed: needs attention", "background export failed: session archive upload returned 503 after 3 retries"),
    notice(17, "warn", "Job artifact migration failed.", "artifact migration failed for job job_123: checksum mismatch while moving output.zip"),
    notice(18, "warn", "Background job teardown timed out.", "job job_123 did not stop within 10s; process is still marked running by the supervisor"),
    notice(19, "warn", "Some plan-mode tool settings were ignored.", "plan-mode tool settings ignored: unsupported tool allowlist entry \"browser.screenshot\""),
    notice(20, "warn", "Some plan-mode command settings were ignored.", "plan-mode command settings ignored: invalid read-only prefix \"npm && test\""),
    notice(21, "warn", "Config migration did not complete.", "config migration failed at providers.defaultModel: unknown provider reference \"old/deepseek\""),
    notice(22, "warn", "Selected model is missing its API key.", "selected model deepseek/deepseek-v4-pro requires DEEPSEEK_API_KEY, but no key is configured"),
    notice(23, "warn", "An MCP server failed to start.", "mcp server \"github\" failed to start: command not found: mcp-server-github"),
    notice(24, "warn", "Some MCP servers failed to start; run /mcp for details.", "mcp startup failures: github(command not found), linear(authentication expired)"),
    notice(25, "warn", "Guardian was disabled because its model was not found.", "guardian model \"glm-5-guard\" is not present in the configured provider catalog"),
    notice(26, "warn", "Guardian was disabled because it could not start.", "guardian startup failed: provider returned 401 unauthorized"),
  ];
}

function NoticePreviewPanel() {
  return (
    <div
      style={{
        flex: "1 1 auto",
        minHeight: 0,
        overflow: "auto",
        padding: "44px 24px 128px",
      }}
    >
      <div style={{ maxWidth: 920, margin: "0 auto" }}>
        {noticePreviewItems().map((item) => {
          if (item.kind !== "notice") return null;
          return <NoticeCard key={item.id} item={item} onAction={item.action ? () => undefined : undefined} />;
        })}
      </div>
    </div>
  );
}

const HistoryPanel = lazy(() => import("./components/HistoryPanel").then((module) => ({ default: module.HistoryPanel })));
const SettingsPanel = lazy(() => import("./components/SettingsPanel").then((module) => ({ default: module.SettingsPanel })));
const RemotePanel = lazy(() => import("./components/RemotePanel").then((module) => ({ default: module.RemotePanel })));

const CHAT_MIN_WIDTH = 400;
const CHAT_COMFORT_MIN_WIDTH = 560;
const WORKSPACE_RESIZER_WIDTH = 8;

function stripGoalResearchFlags(arg: string): string {
  const parts = arg.trim().split(/\s+/).filter(Boolean);
  while (parts.length > 0) {
    const flag = parts[0].toLowerCase();
    if (flag !== "--research" && flag !== "--auto-research" && flag !== "--deep" && flag !== "--simple" && flag !== "--no-research") break;
    parts.shift();
  }
  return parts.join(" ");
}

function hasGoalResearchFlag(arg: string): boolean {
  const first = arg.trim().split(/\s+/, 1)[0]?.toLowerCase();
  return first === "--research" || first === "--auto-research" || first === "--deep" || first === "--simple" || first === "--no-research";
}

function isThemeMode(value: string): value is Theme {
  return value === "auto" || value === "light" || value === "dark";
}

type DesktopLayoutStyle = "classic" | "workbench" | "creation";

function normalizeDesktopLayoutStyle(style: string | undefined): DesktopLayoutStyle {
  if (style === "workbench") return "workbench";
  if (style === "creation") return "creation";
  return "classic";
}
const SHOW_CONTEXT_DOCK = true;
const DISMISSED_TODO_STORAGE_KEY = "todoPanel:dismissedKeys";
const MAX_DISMISSED_TODO_KEYS = 160;
type HistoryScopeFilter = { scope: "global" | "project"; workspaceRoot: string };
type WorkspaceInsertTarget = "composer" | "planRevision";
type DesktopPlatform = "darwin" | "windows" | "linux";

function useWindowsMaximised(enabled: boolean): readonly [boolean, () => void] {
  const [maximised, setMaximised] = useState(false);
  const syncGenerationRef = useRef(0);

  const syncMaximised = useCallback(() => {
    if (!enabled) return;
    const generation = ++syncGenerationRef.current;
    void app.IsMainWindowMaximised()
      .then((value) => {
        if (generation === syncGenerationRef.current) setMaximised(value);
      })
      .catch(() => {
        if (generation === syncGenerationRef.current) setMaximised(false);
      });
  }, [enabled]);

  useEffect(() => {
    if (!enabled) {
      syncGenerationRef.current += 1;
      setMaximised(false);
      return;
    }
    syncMaximised();
    window.addEventListener("resize", syncMaximised);
    window.addEventListener("focus", syncMaximised);
    return () => {
      syncGenerationRef.current += 1;
      window.removeEventListener("resize", syncMaximised);
      window.removeEventListener("focus", syncMaximised);
    };
  }, [enabled, syncMaximised]);

  return [maximised, syncMaximised] as const;
}

function WindowsWindowControls({
  maximised,
  syncMaximised,
}: {
  maximised: boolean;
  syncMaximised: () => void;
}) {
  const toggleMaximise = useCallback(() => {
    void app.ToggleMaximiseMainWindow()
      .then(() => window.setTimeout(syncMaximised, 80))
      .catch(() => undefined);
  }, [syncMaximised]);

  return (
    <div className="windows-window-controls" aria-label="Window controls">
      <button
        className="windows-window-control windows-window-control--minimize"
        type="button"
        aria-label="Minimize window"
        title="Minimize"
        onClick={() => void app.MinimiseMainWindow()}
      >
        <Minus size={13} strokeWidth={1.9} />
      </button>
      <button
        className="windows-window-control windows-window-control--maximize"
        type="button"
        aria-label="Maximize or restore window"
        aria-pressed={maximised}
        title={maximised ? "Restore" : "Maximize"}
        onClick={toggleMaximise}
      >
        {maximised ? <RestoreIcon size={12} strokeWidth={1.75} /> : <Square size={11} strokeWidth={1.8} />}
      </button>
      <button
        className="windows-window-control windows-window-control--close"
        type="button"
        aria-label="Close window"
        title="Close"
        onClick={() => void app.CloseMainWindow()}
      >
        <X size={13} strokeWidth={1.9} />
      </button>
    </div>
  );
}
type HistoryViewState =
  | { kind: "history"; source: "scope"; filter: HistoryScopeFilter; sessions: SessionMeta[] }
  | { kind: "history"; source: "all"; sessions: SessionMeta[] }
  | { kind: "trash"; sessions: SessionMeta[] };
type SidebarImPlatform = "qq" | "feishu" | "lark" | "weixin";
type SidebarImStatus = "connected" | "disabled" | "pending" | "error" | "disconnected";
type SidebarImConnection = {
  id: string;
  connectionId: string;
  platform: SidebarImPlatform;
  title: string;
  platformLabel: string;
  subtitle: string;
  status: SidebarImStatus;
  statusLabel: string;
  remoteId: string;
  sessionId: string;
  sessionSource: string;
  scope: "global" | "project";
  workspaceRoot: string;
  allowAll: boolean;
  allowlistEnabled: boolean;
  allowlistUsers: string[];
  allowlistMatched: boolean;
};
type DesktopNavigationInput =
  | { kind: "topic"; scope: string; workspaceRoot: string; topicId: string; sessionPath?: string }
  | { kind: "blank"; scope: string; workspaceRoot: string }
  | { kind: "delivery-worktree"; workspaceRoot: string }
  | { kind: "sidebar-im"; connection: SidebarImConnection }
  | { kind: "resume-session"; session: SessionMeta };
type PendingDesktopNavigationRequest = PendingNavigationRequest<DesktopNavigationInput>;
type SidebarImTopicSource = {
  platform: SidebarImPlatform;
  label: string;
  title: string;
  remoteId: string;
  connectionId: string;
};
type SidebarImConnectionDetailProps = {
  connection: SidebarImConnection;
  onClose: () => void;
  onOpenSession: () => void;
  onOpenSettings: () => void;
  onManageAllowlist: () => void;
};

function loadDismissedTodoKeys(): Set<string> {
  try {
    const saved = window.localStorage.getItem(DISMISSED_TODO_STORAGE_KEY);
    if (!saved) return new Set();
    const parsed = JSON.parse(saved) as unknown;
    if (!Array.isArray(parsed)) return new Set();
    return new Set(parsed.filter((value): value is string => typeof value === "string" && value.length > 0));
  } catch {
    return new Set();
  }
}

function saveDismissedTodoKeys(keys: ReadonlySet<string>): void {
  try {
    window.localStorage.setItem(
      DISMISSED_TODO_STORAGE_KEY,
      JSON.stringify(Array.from(keys).slice(-MAX_DISMISSED_TODO_KEYS)),
    );
  } catch {
    /* ignore quota errors */
  }
}

function isSidebarImConnection(connection: BotConnectionView): boolean {
  return connection.provider === "feishu" || connection.provider === "weixin";
}

function sidebarImPlatform(connection: BotConnectionView): SidebarImPlatform {
  if (connection.provider === "weixin") return "weixin";
  return connection.domain === "lark" ? "lark" : "feishu";
}

function sidebarImPlatformLabel(platform: SidebarImPlatform, translate: Translator): string {
  if (platform === "qq") return "QQ";
  if (platform === "lark") return "Lark";
  if (platform === "weixin") return translate("settings.botWeixin");
  return translate("settings.botFeishu");
}

function botMappingScope(mapping: BotConnectionView["sessionMappings"][number] | null | undefined, connectionWorkspaceRoot: string): "global" | "project" {
  if (mapping?.scope === "project") return "project";
  if ((mapping?.workspaceRoot ?? "").trim()) return "project";
  return connectionWorkspaceRoot.trim() ? "project" : "global";
}

function botMappingWorkspaceRoot(
  mapping: BotConnectionView["sessionMappings"][number] | null | undefined,
  connectionWorkspaceRoot: string,
): string {
  const workspaceRoot = (mapping?.workspaceRoot ?? "").trim() || connectionWorkspaceRoot.trim();
  return botMappingScope(mapping, connectionWorkspaceRoot) === "project" ? workspaceRoot : "";
}

function compactRemoteId(value: string): string {
  const trimmed = value.trim();
  if (trimmed.length <= 28) return trimmed;
  return `${trimmed.slice(0, 12)}…${trimmed.slice(-8)}`;
}

function botMappingIdentityLabel(mapping: BotConnectionView["sessionMappings"][number] | null | undefined): string {
  const chatType = (mapping?.chatType ?? "").trim();
  const userId = (mapping?.userId ?? "").trim();
  const threadId = (mapping?.threadId ?? "").trim();
  if (threadId) return compactRemoteId(threadId);
  if ((chatType === "group" || chatType === "guild") && userId) return compactRemoteId(userId);
  return "";
}

function sidebarImStatus(connection: BotConnectionView, botEnabled: boolean): SidebarImStatus {
  if (!botEnabled || !connection.enabled) return "disabled";
  if (connection.status === "connected") return "connected";
  if (connection.status === "pending") return "pending";
  if (connection.status === "error") return "error";
  return "disconnected";
}

function sidebarImStatusLabel(status: SidebarImStatus, translate: Translator): string {
  switch (status) {
    case "connected":
      return translate("sidebar.imConnected");
    case "disabled":
      return translate("sidebar.imDisabled");
    case "pending":
      return translate("sidebar.imPending");
    case "error":
      return translate("sidebar.imError");
    default:
      return translate("sidebar.imDisconnected");
  }
}

function uniqueTrimmedValues(values: string[]): string[] {
  return Array.from(new Set(values.map((value) => value.trim()).filter(Boolean)));
}

function sidebarImAllowlistUsers(bot: BotSettingsView, platform: SidebarImPlatform): string[] {
  if (platform === "qq") return uniqueTrimmedValues(asArray(bot.allowlist.qqUsers));
  if (platform === "weixin") return uniqueTrimmedValues(asArray(bot.allowlist.weixinUsers));
  return uniqueTrimmedValues(asArray(bot.allowlist.feishuUsers));
}

function sidebarImQQAdded(qq: BotSettingsView["qq"]): boolean {
  return Boolean(qq.enabled || qq.secretSet || qq.appId.trim());
}

function sidebarImQQStatus(bot: BotSettingsView, runtimeStatus: BotRuntimeStatusView | null | undefined): SidebarImStatus {
  const appId = bot.qq.appId.trim();
  if (!bot.enabled || !bot.qq.enabled) return "disabled";
  if (!appId || !bot.qq.secretSet) return "disconnected";
  if (typeof window !== "undefined" && !window.runtime) return "pending";
  if (!runtimeStatus) return "pending";
  const status = runtimeStatus.status.trim().toLowerCase();
  if (runtimeStatus.running && runtimeStatus.connections > 0 && status === "running") {
    return "connected";
  }
  if (status === "error" || status === "blocked" || status === "degraded") return "error";
  if (status === "stopped") return "disconnected";
  return "pending";
}

async function loadBotRuntimeStatus(): Promise<BotRuntimeStatusView | null> {
  if (typeof window !== "undefined" && !window.runtime) return null;
  try {
    return await app.BotRuntimeStatus();
  } catch (e) {
    console.warn("bot runtime status failed", e);
    return null;
  }
}

function sidebarImQQConnection(bot: BotSettingsView, translate: Translator, runtimeStatus?: BotRuntimeStatusView | null): SidebarImConnection | null {
  if (!sidebarImQQAdded(bot.qq)) return null;
  const remoteId = bot.qq.appId.trim();
  const status = sidebarImQQStatus(bot, runtimeStatus);
  const statusLabel = sidebarImStatusLabel(status, translate);
  const allowlistUsers = sidebarImAllowlistUsers(bot, "qq");
  const subtitleParts = [
    remoteId ? compactRemoteId(remoteId) : "QQ",
    statusLabel,
  ].filter(Boolean);
  return {
    id: "__qq_bot__",
    connectionId: "__qq_bot__",
    platform: "qq",
    title: "QQ Bot",
    platformLabel: "QQ",
    subtitle: subtitleParts.join(" · "),
    status,
    statusLabel,
    remoteId,
    sessionId: "",
    sessionSource: "",
    scope: "global",
    workspaceRoot: "",
    allowAll: bot.allowlist.allowAll,
    allowlistEnabled: bot.allowlist.enabled,
    allowlistUsers,
    allowlistMatched: remoteId ? allowlistUsers.includes(remoteId) : false,
  };
}

function sidebarImConnectionsFromBot(
  bot: BotSettingsView | null | undefined,
  translate: Translator,
  runtimeStatus?: BotRuntimeStatusView | null,
): SidebarImConnection[] {
  if (!bot) return [];
  const qqConnection = sidebarImQQConnection(bot, translate, runtimeStatus);
  const connectionItems: SidebarImConnection[] = [];
  for (const connection of asArray(bot.connections)) {
    if (!isSidebarImConnection(connection)) continue;
    const mappings = connection.sessionMappings.filter((mapping) => mapping.sessionId.trim() || mapping.remoteId.trim());
    const rowMappings = mappings.length > 0 ? mappings : [null];
    rowMappings.forEach((mapping, index) => {
      const platform = sidebarImPlatform(connection);
      const platformLabel = sidebarImPlatformLabel(platform, translate);
      const remoteId = mapping?.remoteId.trim() ?? "";
      const sessionId = mapping?.sessionId.trim() ?? "";
      const sessionSource = mapping?.sessionSource.trim() ?? "";
      const scope = botMappingScope(mapping, connection.workspaceRoot);
      const workspaceRoot = botMappingWorkspaceRoot(mapping, connection.workspaceRoot);
      const status = sidebarImStatus(connection, bot.enabled);
      const title = connection.label.trim() || platformLabel;
      const allowlistUsers = sidebarImAllowlistUsers(bot, platform);
      const identityLabel = botMappingIdentityLabel(mapping);
      const mappedUserId = mapping?.userId.trim() ?? "";
      const subtitleParts = [
        remoteId ? compactRemoteId(remoteId) : platformLabel,
        identityLabel,
        connection.model.trim() || "",
        sidebarImStatusLabel(status, translate),
      ].filter(Boolean);
      connectionItems.push({
        id: mapping ? `${connection.id}:mapping:${index}` : connection.id,
        connectionId: connection.id,
        platform,
        title,
        platformLabel,
        subtitle: subtitleParts.join(" · "),
        status,
        statusLabel: sidebarImStatusLabel(status, translate),
        remoteId,
        sessionId,
        sessionSource,
        scope,
        workspaceRoot,
        allowAll: bot.allowlist.allowAll,
        allowlistEnabled: bot.allowlist.enabled,
        allowlistUsers,
        allowlistMatched: remoteId
          ? allowlistUsers.includes(remoteId) || (mappedUserId ? allowlistUsers.includes(mappedUserId) : false)
          : false,
      });
    });
  }
  return qqConnection ? [qqConnection, ...connectionItems] : connectionItems;
}

function mappedSessionTarget(sessionId: string): { kind: "path" | "topic"; value: string } | null {
  const trimmed = sessionId.trim();
  if (!trimmed) return null;
  const lower = trimmed.toLowerCase();
  if (lower.startsWith("path:")) {
    const value = trimmed.slice(5).trim();
    return value ? { kind: "path", value } : null;
  }
  if (lower.startsWith("topic:")) {
    const value = trimmed.slice(6).trim();
    return value ? { kind: "topic", value } : null;
  }
  if (trimmed.endsWith(".jsonl") || trimmed.includes("/") || trimmed.includes("\\") || trimmed.startsWith("~")) {
    return { kind: "path", value: trimmed };
  }
  return { kind: "topic", value: trimmed };
}

function sidebarImSessionTarget(connection: SidebarImConnection): { kind: "path" | "topic"; value: string } | null {
  return mappedSessionTarget(connection.sessionId);
}

function isChannelSession(session: SessionMeta): boolean {
  return session.kind === "channel" || session.sessionSource === "auto";
}

function sidebarImTopicSourcesFromBot(bot: BotSettingsView | null | undefined, translate: Translator): Record<string, SidebarImTopicSource> {
  if (!bot?.connections?.length) return {};
  const sources: Record<string, SidebarImTopicSource> = {};
  for (const connection of bot.connections) {
    if (!isSidebarImConnection(connection)) continue;
    const platform = sidebarImPlatform(connection);
    const label = sidebarImPlatformLabel(platform, translate);
    const title = connection.label.trim() || label;
    for (const mapping of asArray(connection.sessionMappings)) {
      const scope = botMappingScope(mapping, connection.workspaceRoot);
      if (scope !== "global") continue;
      const target = mappedSessionTarget(mapping.sessionId);
      if (!target || target.kind !== "topic") continue;
      if (sources[target.value]) continue;
      sources[target.value] = {
        platform,
        label,
        title,
        remoteId: mapping.remoteId.trim(),
        connectionId: connection.id,
      };
    }
  }
  return sources;
}

function sidebarImScopeLabel(connection: SidebarImConnection, translate: Translator): string {
  if (connection.scope === "project") return translate("botDetail.scopeProject", { name: connection.workspaceRoot || "Project" });
  return translate("botDetail.scopeGlobal");
}

function sidebarImSessionLabel(connection: SidebarImConnection, translate: Translator): string {
  const target = sidebarImSessionTarget(connection);
  if (!target) {
    return connection.remoteId ? translate("botDetail.readOnlyChannel") : translate("botDetail.noSession");
  }
  if (connection.sessionSource === "auto") return translate("botDetail.readOnlyChannel");
  if (target.kind === "path") return target.value.split(/[\\/]/).pop() || target.value;
  return target.value;
}

function sidebarImAccessModeLabel(connection: SidebarImConnection, translate: Translator): string {
  if (connection.allowAll) return translate("botDetail.accessAllowAll");
  if (connection.allowlistEnabled) return translate("botDetail.accessWhitelist");
  return translate("botDetail.accessDisabled");
}

function sidebarImAccessStatusLabel(connection: SidebarImConnection, translate: Translator): string {
  if (connection.allowAll) return translate("botDetail.accessOpen");
  if (!connection.remoteId) return translate("botDetail.accessUnknown");
  return connection.allowlistMatched ? translate("botDetail.accessMatched") : translate("botDetail.accessMissing");
}

function sidebarImAccessStatusClass(connection: SidebarImConnection): string {
  if (connection.allowAll || connection.allowlistMatched) return "ok";
  if (!connection.remoteId) return "muted";
  return "warn";
}

function SidebarImConnectionDetail({ connection, onClose, onOpenSession, onOpenSettings, onManageAllowlist }: SidebarImConnectionDetailProps) {
  const translate = useT();
  const target = sidebarImSessionTarget(connection);
  const accessStatusClass = sidebarImAccessStatusClass(connection);
  return (
    <div className="bot-detail">
      <section className="bot-detail__summary">
        <div className={`bot-detail__avatar bot-detail__avatar--${connection.platform}`} aria-hidden="true">
          {connection.platform === "qq" ? "Q" : connection.platform === "weixin" ? "微" : connection.platform === "lark" ? "L" : "飞"}
        </div>
        <div className="bot-detail__summary-main">
          <span>{translate("botDetail.subtitle")}</span>
          <h2>{connection.title}</h2>
          <div className="bot-detail__chips">
            <span>{connection.platformLabel}</span>
            <span>{connection.statusLabel}</span>
            <span>{sidebarImScopeLabel(connection, translate)}</span>
          </div>
        </div>
        <div className="bot-detail__summary-actions">
          <button type="button" className="btn btn--primary btn--small bot-detail__primary" disabled={!target} title={target ? undefined : translate("botDetail.openDisabled")} onClick={onOpenSession}>
            <MessageSquare size={14} />
            {translate("botDetail.openSession")}
          </button>
          <button type="button" className="btn btn--secondary btn--small" onClick={onOpenSettings}>
            <SettingsIcon size={14} />
            {translate("botDetail.manage")}
          </button>
          <button type="button" className="btn btn--secondary btn--small" onClick={onClose}>
            {translate("botDetail.close")}
          </button>
        </div>
      </section>

      <section className="bot-detail__panel bot-detail__panel--access" aria-label={translate("botDetail.access")}>
        <div className="bot-detail__section-head">
          <span>{translate("botDetail.access")}</span>
          <div className="bot-detail__section-actions">
            {connection.remoteId ? (
              <CopyButton text={connection.remoteId} label={translate("botDetail.copyRemoteId")} />
            ) : null}
            <button type="button" className="btn btn--secondary btn--small" onClick={onManageAllowlist}>
              {translate("botDetail.manageAllowlist")}
            </button>
          </div>
        </div>
        <div className="bot-detail__access-grid">
          <div>
            <span>{translate("botDetail.accessMode")}</span>
            <strong>{sidebarImAccessModeLabel(connection, translate)}</strong>
          </div>
          <div>
            <span>{translate("botDetail.accessCurrentUser")}</span>
            <code title={connection.remoteId || undefined}>{connection.remoteId || "—"}</code>
          </div>
          <div>
            <span>{translate("botDetail.accessStatus")}</span>
            <strong className={`bot-detail__access-status bot-detail__access-status--${accessStatusClass}`}>
              {sidebarImAccessStatusLabel(connection, translate)}
            </strong>
          </div>
        </div>
        <div className="bot-detail__allowlist">
          <span>{translate("botDetail.channelAllowlistUsers")}</span>
          <div className="bot-detail__id-list">
            {connection.allowlistUsers.length > 0 ? (
              connection.allowlistUsers.map((id) => (
                <code
                  key={id}
                  className={id === connection.remoteId ? "bot-detail__id-list-item--active" : ""}
                  title={id}
                >
                  {id}
                </code>
              ))
            ) : (
              <em>{translate("botDetail.emptyAllowlistUsers")}</em>
            )}
          </div>
        </div>
      </section>

      <section className="bot-detail__panel bot-detail__panel--facts" aria-label={translate("botDetail.summary")}>
        <div className="bot-detail__section-head">
          <span>{translate("botDetail.summary")}</span>
        </div>
        <div className="bot-detail__facts">
          <div>
            <span>{translate("botDetail.remoteId")}</span>
            <code>{connection.remoteId || "—"}</code>
          </div>
          <div>
            <span>{translate("botDetail.localTopic")}</span>
            <strong>{sidebarImSessionLabel(connection, translate)}</strong>
          </div>
          <div>
            <span>{translate("botDetail.scope")}</span>
            <strong>{sidebarImScopeLabel(connection, translate)}</strong>
          </div>
        </div>
      </section>
    </div>
  );
}

function activeTopicTurnsFromTree(tree: ProjectNode[], tab?: TabMeta): number | undefined {
  if (!tab?.topicId) return undefined;
  const targetScope = tab.scope === "global" ? "global" : "project";
  const walk = (nodes: ProjectNode[]): number | undefined => {
    for (const node of nodes) {
      if (!node) continue;
      if (node.kind === "topic" || node.kind === "global_topic") {
        const scope = node.kind === "global_topic" ? "global" : "project";
        if (
          scope === targetScope &&
          node.topicId === tab.topicId &&
          (scope === "global" || node.root === tab.workspaceRoot)
        ) {
          return node.turns;
        }
      }
      const found = walk(asArray(node.children));
      if (found !== undefined) return found;
    }
    return undefined;
  };
  return walk(tree);
}

function normalizeDesktopPlatform(value: string): DesktopPlatform {
  if (value === "darwin" || value === "windows") return value;
  return "linux";
}

function browserPlatformOverride(): DesktopPlatform | null {
  if (typeof window === "undefined" || window.runtime) return null;
  const value = new URLSearchParams(window.location.search).get("platform");
  if (value === "darwin" || value === "windows" || value === "linux") return value;
  return null;
}

const GUIDANCE_QUEUE_MOCK_ITEMS = [
  "先确认发送后输入框为什么残留刚发的消息，再决定修哪里。",
  "保持真实 steer 协议不变，只调整前端乐观队列和按钮状态。",
  "最后补后端 submit 悬挂时的回归测试，确保输入框会立刻释放。",
] as const;

function browserMockScenarioParam(): string {
  if (typeof window === "undefined" || window.runtime) return "";
  return new URLSearchParams(window.location.search).get("mock")?.trim().toLowerCase() ?? "";
}

function isGuidanceMockScenario(value: string): boolean {
  return value === "guidance" || value === "guide" || value === "steer";
}

function detectBrowserPlatform(): DesktopPlatform {
  const override = browserPlatformOverride();
  if (override) return override;
  if (typeof navigator === "undefined") return "linux";
  const marker = `${navigator.platform} ${navigator.userAgent}`;
  if (/Win/i.test(marker)) return "windows";
  if (/Mac/i.test(marker)) return "darwin";
  return "linux";
}

function tabWorkspaceTitle(tab?: TabMeta): string {
  if (!tab) return "Global";
  if (tab.scope === "project") return tab.workspaceName || tab.workspaceRoot || "Project";
  if (tab.scope === "global") return tab.workspaceName || "Global";
  return tab.workspaceName || tab.workspaceRoot || "Global";
}

function topicTitle(tab?: TabMeta): string {
  if (!tab) return "Global";
  const workspaceTitle = tabWorkspaceTitle(tab);
  const topic = tab.topicTitle || (tab.scope === "global" ? workspaceTitle : "Untitled");
  return topic === workspaceTitle ? workspaceTitle : `${workspaceTitle} / ${topic}`;
}

function topicDisplayTitle(tab?: TabMeta): string {
  if (!tab) return "Global";
  return tab.topicTitle || (tab.scope === "global" ? tabWorkspaceTitle(tab) : "Untitled");
}

function sessionsForScope(sessions: SessionMeta[], filter: HistoryScopeFilter): SessionMeta[] {
  if (filter.scope === "project") {
    return sessions.filter((session) => session.scope === "project" && session.workspaceRoot === filter.workspaceRoot);
  }
  return sessions.filter((session) => (session.scope || "global") === "global");
}

function isMissingSessionError(err: unknown): boolean {
  const message = err instanceof Error ? err.message : String(err ?? "");
  return /no such file|cannot find the file|file does not exist|session is pending cleanup|session .*not found/i.test(message);
}

function workspaceDisplayName(path?: string): string {
  if (!path) return "";
  const parts = path.split(/[/\\]/).filter(Boolean);
  return parts.length > 0 ? parts[parts.length - 1] : path;
}

function materializeLiveItems(items: Item[], live?: LiveStream): Item[] {
  if (!live) return items;
  return items.map((item) => {
    if (item.kind !== "assistant" || item.id !== live.id) return item;
    return { ...item, text: live.text, reasoning: live.reasoning, streaming: true };
  });
}

function fence(label: string, value: string): string {
  if (!value.trim()) return "";
  const fenceToken = value.includes("```") ? "````" : "```";
  return `${label}\n${fenceToken}\n${value.trim()}\n${fenceToken}`;
}

function sessionItemsToMarkdown(title: string, items: Item[], live?: LiveStream): string {
  const lines: string[] = [`# ${title.trim() || "Reasonix session"}`, ""];
  for (const item of materializeLiveItems(items, live)) {
    switch (item.kind) {
      case "user":
        lines.push("## User", "", item.text.trim(), "");
        break;
      case "assistant":
        lines.push("## Assistant");
        if (item.reasoning.trim()) {
          lines.push("", "### Reasoning", "", item.reasoning.trim());
        }
        if (item.text.trim()) {
          lines.push("", item.text.trim());
        }
        lines.push("");
        break;
      case "tool":
        lines.push(`### Tool: ${item.name}`);
        if (item.args.trim()) lines.push("", fence("Args", item.args));
        if (item.output?.trim()) lines.push("", fence("Output", item.output));
        if (item.error?.trim()) lines.push("", fence("Error", item.error));
        lines.push("");
        break;
      case "phase":
        lines.push(`### Phase`, "", item.text.trim(), "");
        break;
      case "notice":
        lines.push(`### ${item.level === "warn" ? "Warning" : "Notice"}`, "", item.text.trim(), "");
        if (item.detail?.trim()) {
          lines.push("Details:", "", item.detail.trim(), "");
        }
        break;
      case "compaction":
        lines.push("### Context Compaction", "");
        if (item.pending) {
          lines.push("Compaction pending.");
        } else {
          lines.push(`Messages: ${item.messages}`);
          if (item.trigger) lines.push(`Trigger: ${item.trigger}`);
          if (item.summary.trim()) lines.push("", item.summary.trim());
        }
        lines.push("");
        break;
    }
  }
  return lines.join("\n").replace(/\n{3,}/g, "\n\n").trimEnd() + "\n";
}

function sessionItemsToJson(title: string, items: Item[], live?: LiveStream): string {
  return JSON.stringify(
    {
      title,
      exportedAt: new Date().toISOString(),
      items: materializeLiveItems(items, live),
    },
    null,
    2,
  );
}

function safeFilename(name: string): string {
  const cleaned = name.trim().replace(/[\\/:*?"<>|]+/g, "-").replace(/\s+/g, " ").slice(0, 80);
  return cleaned || "reasonix-session";
}

/** Global hotkey handler for shell-expand toggle (Ctrl/Cmd+B). */
function ShellHotkeys() {
  const shellExpand = useShellExpand();
  useGlobalShortcut("shell.toggle", () => shellExpand?.toggleLast(), [shellExpand], Boolean(shellExpand));
  return null;
}

/** Global hotkey handler for text-size shortcuts (Ctrl/Cmd + Plus/Minus/0). */
function TextSizeHotkeys() {
  useGlobalShortcut("textSize.increase", () => applyTextSize(nextTextSize(getTextSize(), 1)));
  useGlobalShortcut("textSize.decrease", () => applyTextSize(nextTextSize(getTextSize(), -1)));
  useGlobalShortcut("textSize.reset", () => applyTextSize(DEFAULT_TEXT_SIZE));
  return null;
}

export default function App() {
  const {
    state,
    activeTabId,
    sendToTab,
    recoverDeliveryToTab,
    runShellForTab,
    steerForTab,
    notice,
    cancel,
    approve,
    answerQuestion,
    setControllerMode,
    setCollaborationMode: setControllerCollaborationMode,
    setCollaborationModeForTab: setControllerCollaborationModeForTab,
    setToolApprovalMode: setControllerToolApprovalMode,
    setToolApprovalModeForTab: setControllerToolApprovalModeForTab,
    setGoal: setControllerGoal,
    setGoalForTab: setControllerGoalForTab,
    resumeGoalForTab: resumeControllerGoalForTab,
    clearGoal: clearControllerGoal,
    clearSession,
    listSessions,
    listTrashedSessions,
    resumeSession,
    openChannelSession,
    previewSession,
    deleteSession,
    restoreSession,
    purgeTrashedSession,
    renameSession,
    loadOlderHistory,
    refreshMeta,
    pickWorkspace,
    switchWorkspace,
    rewindForTab,
    setModel,
    setEffort,
    setTokenMode,
    switchTab,
    openProjectTab,
    createDeliveryWorktree,
    openGlobalTab,
    closeTab,
    reorderTabs,
    openTopicSession,
    activateTopic,
    noteNavigationIntent,
    syncActiveTab,
    ensureBlankTab,
    ensureBlankSurface,
  } = useController();
  const { locale, setPref: setLocalePref } = useI18n();
  const t = useT();
  const [composerProfilesByTab, setComposerProfilesByTab] = useState<Record<string, ComposerProfile>>({});
  const runtimeTransitionTabsRef = useRef<Set<string>>(new Set());
  const [runtimeTransitionsByTab, setRuntimeTransitionsByTab] = useState<Record<string, true>>({});
  const yoloRestoreToolApprovalModesRef = useRef<Record<string, RestorableToolApprovalMode>>({});
  const userPlanModeByTabRef = useRef<UserPlanModeIntents>({});
  const [tabMetas, setTabMetas] = useState<TabMeta[]>([]);
  const [tabOrderIds, setTabOrderIds] = useState<string[]>([]);
  const [tabRevealSignal, setTabRevealSignal] = useState(0);
  const [transcriptRevealSignal, setTranscriptRevealSignal] = useState(0);
  const startupSplashVisible = useOverlayStore((s) => s.startupSplashVisible);
  const setStartupSplashVisible = useOverlayStore((s) => s.setStartupSplashVisible);
  // null until the mount probe resolves; true shows the overlay. Probed once —
  // clearing the key mid-session is the Settings panel's job, not the gate's.
  const needsOnboarding = useOverlayStore((s) => s.needsOnboarding);
  const setNeedsOnboarding = useOverlayStore((s) => s.setNeedsOnboarding);
  const settingsTarget = useOverlayStore((s) => s.settingsTarget);
  const setSettingsTarget = useOverlayStore((s) => s.setSettingsTarget);
  const settingsFocus = useOverlayStore((s) => s.settingsFocus);
  const setSettingsFocus = useOverlayStore((s) => s.setSettingsFocus);
  const [desktopLayoutStyle, setDesktopLayoutStyle] = useState<DesktopLayoutStyle>("workbench");
  const [safeMode, setSafeMode] = useState(false);
  const singleSurfaceLayout = desktopLayoutStyle === "workbench" || desktopLayoutStyle === "creation";
  const [startupUpdateChecksEnabled, setStartupUpdateChecksEnabled] = useState<boolean | null>(null);
  const [histView, setHistView] = useState<HistoryViewState | null>(null);
  const paletteOpen = useOverlayStore((s) => s.paletteOpen);
  const setPaletteOpen = useOverlayStore((s) => s.setPaletteOpen);
  const remoteExplorerOpen = useRemoteStore((s) => s.explorerOpen);
  const remoteExplorerHostId = useRemoteStore((s) => s.explorerHostId);
  const remoteHosts = useRemoteStore((s) => s.hosts);
  const remoteStatuses = useRemoteStore((s) => s.statuses);
  const setRemoteHosts = useRemoteStore((s) => s.setHosts);
  const hydrateRemoteStatuses = useRemoteStore((s) => s.hydrateStatuses);
  const requestRemoteExplorer = useRemoteStore((s) => s.openExplorer);
  const closeRemoteExplorerRequest = useRemoteStore((s) => s.closeExplorer);
  const applyRemoteStatus = useRemoteStore((s) => s.applyStatus);
  const setRemoteForwards = useRemoteStore((s) => s.setForwards);
  const setRemoteServer = useRemoteStore((s) => s.setServer);
  const shortcutsOpen = useOverlayStore((s) => s.shortcutsOpen);
  const setShortcutsOpen = useOverlayStore((s) => s.setShortcutsOpen);
  const paletteSessions = useOverlayStore((s) => s.paletteSessions);
  const setPaletteSessions = useOverlayStore((s) => s.setPaletteSessions);
  const { showToast } = useToast();
  const [sidebarImConnections, setSidebarImConnections] = useState<SidebarImConnection[]>([]);
  const [imTopicSources, setImTopicSources] = useState<Record<string, SidebarImTopicSource>>({});
  const [sidebarImDetailConnectionId, setSidebarImDetailConnectionId] = useState("");
  const sidebarCollapsed = useLayoutStore((s) => s.sidebarCollapsed);
  const setSidebarCollapsed = useLayoutStore((s) => s.setSidebarCollapsed);
  const heartbeatOpen = useOverlayStore((s) => s.heartbeatOpen);
  const setHeartbeatOpen = useOverlayStore((s) => s.setHeartbeatOpen);
  type TimeFilter = "all" | "10" | "20" | "1h" | "3h" | "5h" | "1d";
  const [topicTimeFilter, setTopicTimeFilter] = useState<TimeFilter>(() => {
    try {
      const saved = localStorage.getItem("projectTree:timeFilter");
      if (saved === "all" || saved === "10" || saved === "20" || saved === "1h" || saved === "3h" || saved === "5h" || saved === "1d") return saved;
    } catch { /* localStorage unavailable */ }
    return "all";
  });
  useEffect(() => {
    try { localStorage.setItem("projectTree:timeFilter", topicTimeFilter); } catch { /* ignore */ }
  }, [topicTimeFilter]);
  const sidebarWidth = useLayoutStore((s) => s.sidebarWidth);
  const setSidebarWidth = useLayoutStore((s) => s.setSidebarWidth);
  const [sidebarResizing, setSidebarResizing] = useState(false);
  const [liveSidebarWidth, setLiveSidebarWidth] = useState<number | null>(null);
  const [viewportWidth, setViewportWidth] = useState(() => (typeof window === "undefined" ? 1440 : window.innerWidth));
  const workspacePanelOpen = useLayoutStore((s) => s.workspacePanelOpen);
  const setWorkspacePanelOpen = useLayoutStore((s) => s.setWorkspacePanelOpen);
  const rightDockTreeWidth = useLayoutStore((s) => s.rightDockTreeWidth);
  const setRightDockTreeWidth = useLayoutStore((s) => s.setRightDockTreeWidth);
  const rightDockPreviewWidth = useLayoutStore((s) => s.rightDockPreviewWidth);
  const setRightDockPreviewWidth = useLayoutStore((s) => s.setRightDockPreviewWidth);
  const workspacePreviewActive = useLayoutStore((s) => s.workspacePreviewActive);
  const setWorkspacePreviewActive = useLayoutStore((s) => s.setWorkspacePreviewActive);
  const attentionChimeEvents = useRef(new Set<string>());
  const workspaceScopeActiveTabRef = useRef(activeTabId);
  const [workspaceControllerEpoch, setWorkspaceControllerEpoch] = useState(0);
  workspaceScopeActiveTabRef.current = activeTabId;
  // Bump dockRefreshKey after each turn so WorkspacePanel/ContextPanel re-fetch
  // workspace changes, git history, and session metadata after AI tool writes.
  useEffect(() => {
    const unsub = onEvent((e) => {
      if (e.kind === "turn_done") {
        setDockRefreshKey((v) => v + 1);
      }
      if (shouldPlayAttentionChimeForEvent(e, attentionChimeEvents.current)) {
        playAttentionChime();
      }
      if (e.kind === "turn_done") {
        if (!e.err) playSuccessChime();
      }
    });
    // Runtime rebuilds (model/effort/settings switch) replace the controller,
    // whose approval/ask ids restart from "1" — stale dedupe keys would mute
    // the first prompt after a rebuild. agent:ready fires when a (re)build
    // completes; clear that tab's keys (or all, for tab-less ready events).
    const unsubReady = onReady((readyTabId) => {
      clearAttentionChimeKeys(attentionChimeEvents.current, readyTabId);
      if (!readyTabId || readyTabId === workspaceScopeActiveTabRef.current) {
        setWorkspaceControllerEpoch((value) => value + 1);
      }
    });
    // Model/effort/token-mode switches and clear-while-running replace the
    // controller WITHOUT an agent:ready — they signal runtime:rebuilt instead
    // (a ready here would trigger a full session reload the UI already did).
    const unsubRebuilt = onRuntimeRebuilt((rebuiltTabId) => {
      clearAttentionChimeKeys(attentionChimeEvents.current, rebuiltTabId);
      if (!rebuiltTabId || rebuiltTabId === workspaceScopeActiveTabRef.current) {
        setWorkspaceControllerEpoch((value) => value + 1);
      }
    });
    return () => {
      unsub();
      unsubReady();
      unsubRebuilt();
    };
  }, []);

  const [workspacePanelResizing, setWorkspacePanelResizing] = useState(false);
  const [liveWorkspacePanelRenderWidth, setLiveWorkspacePanelRenderWidth] = useState<number | null>(null);
  const workspacePanelMaximized = useLayoutStore((s) => s.workspacePanelMaximized);
  const setWorkspacePanelMaximized = useLayoutStore((s) => s.setWorkspacePanelMaximized);
  const rightDockMode = useLayoutStore((s) => s.rightDockMode);
  const setRightDockMode = useLayoutStore((s) => s.setRightDockMode);
  const [dockRefreshKey, setDockRefreshKey] = useState(0);
  const [fileRefRefreshKey, setFileRefRefreshKey] = useState(0);
  const refreshComposerFileRefs = useCallback(() => setFileRefRefreshKey((value) => value + 1), []);
  const composerFileRefRefreshKey = `${dockRefreshKey}:${fileRefRefreshKey}`;
  const [projectRevision, setProjectRevision] = useState(0);
  const [activeTopicTurns, setActiveTopicTurns] = useState<number | undefined>(undefined);
  const [composerInsertRequestsByTab, setComposerInsertRequestsByTab] = useState<Record<string, ComposerInsertRequest>>({});
  const [selectedTextRequestsByTab, setSelectedTextRequestsByTab] = useState<Record<string, SelectedTextInsertRequest>>({});
  const selectedTextRequestIdRef = useRef(0);
  const [planRevisionInsertRequest, setPlanRevisionInsertRequest] = useState<{
    tabId: string;
    approvalId: string;
    request: ComposerInsertRequest;
  } | null>(null);
  const [workspaceInsertTarget, setWorkspaceInsertTarget] = useState<WorkspaceInsertTarget>("composer");
  const transientOverlayDismissSignal = useOverlayStore((s) => s.transientOverlayDismissSignal);
  const setTransientOverlayDismissSignal = useOverlayStore((s) => s.setTransientOverlayDismissSignal);
  const [desktopPlatform, setDesktopPlatform] = useState<DesktopPlatform>(detectBrowserPlatform);
  const windowsFramelessChrome = desktopPlatform === "windows";
  const [mainWindowMaximised, syncMainWindowMaximised] = useWindowsMaximised(windowsFramelessChrome);
  useWailsResizeFix(windowsFramelessChrome, mainWindowMaximised);
  const [statusBarStyle, setStatusBarStyle] = useState<"icon" | "text">("text");
  const [statusBarItems, setStatusBarItems] = useState<StatusBarItemId[]>(() => [...DEFAULT_STATUS_BAR_ITEMS]);
  const [renamingTopicId, setRenamingTopicId] = useState<string | null>(null);
  const [topicTitleDraft, setTopicTitleDraft] = useState("");
  const topicExportOpen = useOverlayStore((s) => s.topicExportOpen);
  const setTopicExportOpen = useOverlayStore((s) => s.setTopicExportOpen);
  const sidebarSearchOpen = useOverlayStore((s) => s.sidebarSearchOpen);
  const setSidebarSearchOpen = useOverlayStore((s) => s.setSidebarSearchOpen);
  const sidebarSearchFocusSignal = useOverlayStore((s) => s.sidebarSearchFocusSignal);
  const setSidebarSearchFocusSignal = useOverlayStore((s) => s.setSidebarSearchFocusSignal);
  const [sidebarTogglePressed, setSidebarTogglePressed] = useState(false);
  const [workspaceTogglePressed, setWorkspaceTogglePressed] = useState(false);
  const [clearContextPending, setClearContextPending] = useState(false);
  const topicRenameSkipCommitRef = useRef(false);
  const prevDecisionSurfaceRef = useRef<DecisionSurfaceKind | null>(null);
  const decisionSurfaceRef = useRef<DecisionSurfaceKind | null>(null);
  const topicRenameCommitHandledRef = useRef(false);
  const appRef = useRef<HTMLDivElement>(null);
  const layoutRef = useRef<HTMLDivElement>(null);
  const sidebarTogglePressTimerRef = useRef<number | null>(null);
  const workspaceTogglePressTimerRef = useRef<number | null>(null);

  // Persist window geometry across launches.
  useWindowStatePersistence();
  useViewportHeightVar();
  useEffect(() => {
    document.documentElement.setAttribute("data-platform", desktopPlatform);
  }, [desktopPlatform]);

  const closeTransientOverlays = useCallback(() => {
    setTransientOverlayDismissSignal((signal) => signal + 1);
  }, []);

  const reloadSidebarImConnections = useCallback(async () => {
    const [settings, runtimeStatus] = await Promise.all([
      app.DesktopStartupSettings(),
      loadBotRuntimeStatus(),
    ]);
    setSidebarImConnections(sidebarImConnectionsFromBot(settings.bot, t, runtimeStatus));
    setImTopicSources(sidebarImTopicSourcesFromBot(settings.bot, t));
  }, [t]);

  const refreshSidebarImConnectionsFromSettings = useCallback(async (settings: Pick<SettingsView | DesktopStartupSettingsView, "bot">) => {
    const runtimeStatus = await loadBotRuntimeStatus();
    setSidebarImConnections(sidebarImConnectionsFromBot(settings.bot, t, runtimeStatus));
    setImTopicSources(sidebarImTopicSourcesFromBot(settings.bot, t));
  }, [t]);

  const openBotSettings = useCallback(() => {
    closeTransientOverlays();
    setSidebarImDetailConnectionId("");
    setSettingsFocus(null);
    setSettingsTarget("bots");
  }, [closeTransientOverlays]);

  const openBotAllowlistSettings = useCallback((connectionId: string) => {
    closeTransientOverlays();
    setSidebarImDetailConnectionId("");
    setSettingsFocus({ target: "bot-allowlist", connectionId });
    setSettingsTarget("bots");
  }, [closeTransientOverlays]);

  const pulseSidebarToggle = useCallback(() => {
    if (typeof window === "undefined") return;
    if (sidebarTogglePressTimerRef.current !== null) {
      window.clearTimeout(sidebarTogglePressTimerRef.current);
    }
    setSidebarTogglePressed(true);
    sidebarTogglePressTimerRef.current = window.setTimeout(() => {
      sidebarTogglePressTimerRef.current = null;
      setSidebarTogglePressed(false);
    }, 260);
  }, []);

  const pulseWorkspaceToggle = useCallback(() => {
    if (typeof window === "undefined") return;
    if (workspaceTogglePressTimerRef.current !== null) {
      window.clearTimeout(workspaceTogglePressTimerRef.current);
    }
    setWorkspaceTogglePressed(true);
    workspaceTogglePressTimerRef.current = window.setTimeout(() => {
      workspaceTogglePressTimerRef.current = null;
      setWorkspaceTogglePressed(false);
    }, 260);
  }, []);

  const anchorAppScrollToChat = useCallback(() => {
    if (typeof window === "undefined") return;
    const el = appRef.current;
    if (!el) return;
    const pin = () => {
      el.scrollLeft = 0;
    };
    pin();
    window.requestAnimationFrame(pin);
    window.setTimeout(pin, 300);
  }, []);

  useEffect(() => {
    return () => {
      if (sidebarTogglePressTimerRef.current !== null) {
        window.clearTimeout(sidebarTogglePressTimerRef.current);
      }
      if (workspaceTogglePressTimerRef.current !== null) {
        window.clearTimeout(workspaceTogglePressTimerRef.current);
      }
    };
  }, []);

  useEffect(() => {
    let cancelled = false;
    const override = browserPlatformOverride();
    if (override) {
      setDesktopPlatform(override);
      return () => {
        cancelled = true;
      };
    }
    void app.Platform()
      .then((value) => {
        if (!cancelled) setDesktopPlatform(normalizeDesktopPlatform(value));
      })
      .catch((e) => {
        console.warn("platform probe failed", e);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const applyDesktopPreferences = useCallback(
    (settings: Pick<SettingsView, "desktopTheme" | "desktopThemeStyle" | "desktopLayoutStyle" | "desktopLanguage" | "checkUpdates" | "statusBarStyle" | "statusBarItems" | "conversationWidth">) => {
      const nextTheme = normalizeThemePreference(settings.desktopTheme);
      const nextStyle = normalizeThemeStyleForTheme(settings.desktopThemeStyle, nextTheme);
      applyConfiguredBaseAppearance(nextTheme, nextStyle);
      applyConversationWidth(settings.conversationWidth);
      const nextLayoutStyle = normalizeDesktopLayoutStyle(settings.desktopLayoutStyle);
      setDesktopLayoutStyle(nextLayoutStyle);
      applyLayoutStyleDefaults(nextLayoutStyle);
      setLocalePref(normalizeLangPref(settings.desktopLanguage));
      setStartupUpdateChecksEnabled(settings.checkUpdates !== false);
      setStatusBarStyle(settings.statusBarStyle === "text" ? "text" : "icon");
      setStatusBarItems(normalizeStatusBarItems(settings.statusBarItems));
    },
    [setLocalePref],
  );

  useEffect(() => {
    let cancelled = false;
    const syncDesktopPreferences = async () => {
      const legacyLanguage = readLegacyLangPref();
      const legacyTheme = readLegacyThemePreference();
      if (legacyLanguage || legacyTheme.hasValue) {
        await app.MigrateDesktopPreferences(legacyLanguage, legacyTheme.theme, legacyTheme.style);
        clearLegacyLangPref();
        clearLegacyThemePreference();
      }
      const [settings, runtimeStatus] = await Promise.all([
        app.DesktopStartupSettings(),
        loadBotRuntimeStatus(),
      ]);
      if (cancelled) return;
      applyDesktopPreferences(settings);
      setSafeMode(settings.safeMode === true);
      hydrateDisplayMode(settings.displayMode);
      setSidebarImConnections(sidebarImConnectionsFromBot(settings.bot, t, runtimeStatus));
      setImTopicSources(sidebarImTopicSourcesFromBot(settings.bot, t));
      // Load unified theme experience after base appearance so pack tokens win.
      if (settings.safeMode === true) {
        clearThemePack();
      } else {
        try {
          const { loadThemeExperience, applyExperienceToDOM } = await import("./lib/themeExperience");
          const exp = await loadThemeExperience();
          if (cancelled) return;
          applyExperienceToDOM(exp);
        } catch (err) {
          console.warn("theme experience load failed", err);
          try {
            const active = await app.GetActiveThemePack();
            if (cancelled) return;
            if (active?.pack) applyThemePack(active.pack);
            else clearThemePack();
          } catch {
            clearThemePack();
          }
        }
      }
    };
    void syncDesktopPreferences().catch((e) => {
      console.warn("desktop preferences sync failed", e);
      setStartupUpdateChecksEnabled(true);
    });
    return () => {
      cancelled = true;
    };
  }, [applyDesktopPreferences, t]);

  useEffect(() => {
    setSidebarImDetailConnectionId((current) => {
      if (!current) return "";
      return sidebarImConnections.some((connection) => connection.id === current) ? current : "";
    });
  }, [sidebarImConnections]);

  // Open settings when the native menu item (CmdOrCtrl+,) is activated.
  useEffect(() => {
    if (typeof window === "undefined" || !window.runtime) return;
    return window.runtime.EventsOn("app:open-settings", () => {
      closeTransientOverlays();
      setSettingsTarget("general");
    });
  }, [closeTransientOverlays]);
  useEffect(() => {
    if (typeof window === "undefined") return;
    const onResize = () => setViewportWidth(window.innerWidth);
    window.addEventListener("resize", onResize);
    return () => window.removeEventListener("resize", onResize);
  }, []);

  const [pendingPlanRevisionsByTab, setPendingPlanRevisionsByTab] = useState<Record<string, string>>({});
  const [invocationMetadataByTab, setInvocationMetadataByTab] = useState<Record<string, InvocationMetadataMap>>({});
  const pendingPlanRevisionSendingTabsRef = useRef(new Set<string>());
  const [footerHeight, setFooterHeight] = useState(0);
  const footerHeightRef = useRef(0);
  const footerRef = useRef<HTMLElement>(null);
  const activeTabIdRef = useRef(activeTabId);
  const commitThenSendRef = useRef<(tabId: string, displayText: string, submitText?: string, structured?: StructuredInvocationSubmit) => Promise<void>>(async () => {});
  const handleInvocationMetadataChange = useCallback((metadata: InvocationMetadataMap) => {
    const sourceTabId = activeTabIdRef.current;
    if (!sourceTabId) return;
    setInvocationMetadataByTab((current) => {
      const previous = current[sourceTabId] ?? {};
      const names = Object.keys(metadata);
      if (names.length === Object.keys(previous).length && names.every((name) => (
        previous[name]?.kind === metadata[name]?.kind && previous[name]?.color === metadata[name]?.color
      ))) return current;
      return { ...current, [sourceTabId]: metadata };
    });
  }, []);
  const rightDockDetailActive = rightDockMode !== "context" && workspacePreviewActive;
  const preferredWorkspacePanelWidth = rightDockDetailActive ? rightDockPreviewWidth : rightDockTreeWidth;
  const rightDockTreeMinWidth = desktopLayoutStyle === "creation" ? CREATION_RIGHT_DOCK_TREE_MIN_WIDTH : RIGHT_DOCK_TREE_MIN_WIDTH;
  const rightDockTreeWidthClamp = desktopLayoutStyle === "creation" ? clampCreationRightDockTreeWidth : clampRightDockTreeWidth;
  const rightDockMinRenderWidth = desktopLayoutStyle === "creation" && !rightDockDetailActive
    ? CREATION_RIGHT_DOCK_MIN_RENDER_WIDTH
    : RIGHT_DOCK_MIN_RENDER_WIDTH;
  const workspacePanelMinWidth = rightDockDetailActive ? RIGHT_DOCK_PREVIEW_MIN_WIDTH : rightDockTreeMinWidth;
  const chatReservedWidth = workspacePanelOpen && !workspacePanelMaximized ? CHAT_COMFORT_MIN_WIDTH : CHAT_MIN_WIDTH;
  const workspacePanelAvailableWidth = availableWorkspacePanelWidth({
    viewportWidth,
    sidebarCollapsed,
    sidebarWidth,
    chatMinWidth: chatReservedWidth,
    resizerWidth: WORKSPACE_RESIZER_WIDTH,
  });

  const resolvedWorkspacePanelWidth = resolveWorkspacePanelWidth({
    open: workspacePanelOpen,
    maximized: workspacePanelMaximized,
    preferredWidth: preferredWorkspacePanelWidth,
    minWidth: workspacePanelMinWidth,
    availableWidth: workspacePanelAvailableWidth,
  });

  const storedWorkspacePanelRenderWidth = workspacePanelMaximized ? preferredWorkspacePanelWidth : resolvedWorkspacePanelWidth;
  const workspacePanelRenderWidth = liveWorkspacePanelRenderWidth ?? storedWorkspacePanelRenderWidth;
  const workspacePanelRenderable =
    workspacePanelOpen && (workspacePanelMaximized || workspacePanelRenderWidth >= rightDockMinRenderWidth);
  const workspacePanelGridOpen = workspacePanelRenderable && !workspacePanelMaximized;
  const resolveLiveWorkspacePanelRenderWidth = useCallback(
    (preferredWidth: number, nextSidebarWidth = sidebarWidth) =>
      resolveLiveWorkspacePanelWidth({
        viewportWidth,
        sidebarCollapsed,
        sidebarWidth: nextSidebarWidth,
        chatMinWidth: chatReservedWidth,
        resizerWidth: WORKSPACE_RESIZER_WIDTH,
        open: workspacePanelOpen,
        maximized: workspacePanelMaximized,
        preferredWidth,
        minWidth: workspacePanelMinWidth,
      }),
    [chatReservedWidth, sidebarCollapsed, sidebarWidth, viewportWidth, workspacePanelMaximized, workspacePanelMinWidth, workspacePanelOpen],
  );
  const activeTab = useMemo(
    () => tabMetas.find((tab) => tab.id === activeTabId) ?? tabMetas.find((tab) => tab.active),
    [activeTabId, tabMetas],
  );
  const activePlanRevisionInsertRequest =
    planRevisionInsertRequest &&
    planRevisionInsertRequest.tabId === activeTabId &&
    planRevisionInsertRequest.approvalId === state.approval?.id
      ? planRevisionInsertRequest.request
      : null;
  const composerInsertRequest = activeTabId ? composerInsertRequestsByTab[activeTabId] ?? null : null;
  const selectedTextRequest = activeTabId ? selectedTextRequestsByTab[activeTabId] ?? null : null;
  const prefillSubagentCommand = useCallback((command: string) => {
    if (!activeTabId) return;
    setComposerInsertRequestsByTab((current) => ({
      ...current,
      [activeTabId]: { id: Date.now(), text: command, mode: "prefix" },
    }));
  }, [activeTabId]);
  const composerSessionKey = useMemo(() => {
    return composerDraftKeyForTab(activeTab, activeTabId);
  }, [activeTab, activeTabId]);
  const workspaceScopeKey = [
    activeTabId ?? "",
    activeTab?.sessionPath ?? "",
    state.meta?.sessionPath ?? "",
    state.meta?.cwd ?? "",
    state.sessionGen,
    workspaceControllerEpoch,
  ].join("\u0000");
  const sidebarImDetailConnection = useMemo(
    () => sidebarImConnections.find((connection) => connection.id === sidebarImDetailConnectionId) ?? null,
    [sidebarImConnections, sidebarImDetailConnectionId],
  );
  useEffect(() => {
    let cancelled = false;
    if (!activeTab?.topicId) {
      setActiveTopicTurns(undefined);
      return () => {
        cancelled = true;
      };
    }
    void app.ListProjectTree()
      .then((tree) => {
        if (!cancelled) setActiveTopicTurns(activeTopicTurnsFromTree(asArray(tree), activeTab));
      })
      .catch(() => {
        if (!cancelled) setActiveTopicTurns(undefined);
      });
    return () => {
      cancelled = true;
    };
  }, [activeTab?.scope, activeTab?.topicId, activeTab?.workspaceRoot, projectRevision]);
  const sessionTurns = useMemo(() => {
    const visibleUserTurns = state.items.reduce((count, item) => (item.kind === "user" ? count + 1 : count), 0);
    const currentTabTurns = Math.max(state.checkpoints.length, visibleUserTurns);
    return currentTabTurns > 0 ? currentTabTurns : activeTopicTurns ?? 0;
  }, [activeTopicTurns, state.checkpoints.length, state.items]);
  const startupSplashHold = !activeTabId && state.meta?.ready !== true && !state.meta?.startupErr;
  const activeComposerProfile = activeTabId ? composerProfilesByTab[activeTabId] : undefined;
  const backendActiveComposerProfile = useMemo(() => {
    if (state.meta) {
      return composerProfileFromMeta(
        state.meta,
        activeTab ? composerProfileMode(composerProfileFromTab(activeTab, activeComposerProfile?.toolApprovalMode)) : undefined,
        activeComposerProfile?.toolApprovalMode,
      );
    }
    return composerProfileFromTab(activeTab, activeComposerProfile?.toolApprovalMode);
  }, [activeComposerProfile?.toolApprovalMode, activeTab, state.meta]);
  const composerProfile = activeTabId
    ? activeComposerProfile ?? backendActiveComposerProfile
    : defaultComposerProfile;
  const goal = composerProfile.goal;
  const collaborationMode = displayedComposerProfileCollaborationMode(composerProfile);
  const toolApprovalMode = composerProfile.toolApprovalMode;
  const tokenMode: TokenMode = composerProfile.tokenMode;
  const runtimeTransitioning = Boolean(activeTabId && runtimeTransitionsByTab[activeTabId]);
  const controllerReady = state.meta?.ready === true && !state.backendActivationPending && !runtimeTransitioning;
  // Single footer decision surface. Composer stays mounted underneath and is
  // only visually/a11y-hidden so per-session draft caches survive.
  const decisionSurface = useMemo((): DecisionSurfaceKind | null => {
    if (state.approval) {
      return state.approval.tool === "exit_plan_mode" ? "plan_approval" : "tool_approval";
    }
    if (state.ask) return "ask";
    if (clearContextPending) return "clear_context";
    return null;
  }, [clearContextPending, state.approval, state.ask]);
  decisionSurfaceRef.current = decisionSurface;
  useEffect(() => {
    // Close composer menus/popovers when a decision takes over the footer.
    if (decisionSurface) {
      closeTransientOverlays();
      prevDecisionSurfaceRef.current = decisionSurface;
      return;
    }
    // Restore composer focus on the next frame only if the tab did not switch
    // and no new decision arrived (remote resolution / rapid consecutive prompts).
    const hadDecision = prevDecisionSurfaceRef.current != null;
    prevDecisionSurfaceRef.current = null;
    if (!hadDecision) return;
    const tabAtRelease = activeTabId;
    const frame = requestAnimationFrame(() => {
      if (decisionSurfaceRef.current != null) return;
      if (activeTabIdRef.current !== tabAtRelease) return;
      const input = document.getElementById("composer-input") as HTMLTextAreaElement | null;
      input?.focus({ preventScroll: true });
    });
    return () => cancelAnimationFrame(frame);
  }, [activeTabId, closeTransientOverlays, decisionSurface]);
  const patchActiveComposerProfile = useCallback(
    (patch: Partial<Omit<ComposerProfile, "pending">>, pendingFields: ComposerProfileField[]) => {
      if (!activeTabId) return;
      setComposerProfilesByTab((current) => patchComposerProfile(current, activeTabId, composerProfile, patch, pendingFields));
    },
    [activeTabId, composerProfile],
  );
  const topicbarEditing = Boolean(activeTab?.topicId && activeTab.topicId === renamingTopicId);
  const visibleTabId = activeTabId;
  const visibleTabs = useMemo(() => {
    const byId = new Map(tabMetas.map((tab) => [tab.id, tab]));
    const ordered = tabOrderIds.map((id) => byId.get(id)).filter((tab): tab is TabMeta => Boolean(tab));
    const missing = tabMetas.filter((tab) => !tabOrderIds.includes(tab.id));
    return [...ordered, ...missing].map((tab) => {
      const profile = composerProfilesByTab[tab.id] ?? composerProfileFromTab(tab);
      return {
        ...tab,
        running: tab.id === visibleTabId ? tab.running || state.running : tab.running,
        mode: composerProfileMode(profile),
        collaborationMode: displayedComposerProfileCollaborationMode(profile),
        toolApprovalMode: profile.toolApprovalMode,
        tokenMode: profile.tokenMode,
        goal: profile.goal,
        active: tab.id === visibleTabId,
      };
    });
  }, [composerProfilesByTab, state.running, tabMetas, tabOrderIds, visibleTabId]);

  useEffect(() => {
    const ids = tabMetas.map((tab) => tab.id);
    setTabOrderIds((current) => {
      const next = current.filter((id) => ids.includes(id));
      for (const id of ids) {
        if (!next.includes(id)) next.push(id);
      }
      return next.join("\u0000") === current.join("\u0000") ? current : next;
    });
  }, [tabMetas]);

  useEffect(() => {
    const ids = new Set(tabMetas.map((tab) => tab.id));
    for (const id of Object.keys(yoloRestoreToolApprovalModesRef.current)) {
      if (!ids.has(id)) delete yoloRestoreToolApprovalModesRef.current[id];
    }
    userPlanModeByTabRef.current = pruneUserPlanModeIntents(userPlanModeByTabRef.current, ids);
    setComposerProfilesByTab((current) => hydrateComposerProfilesFromTabs(current, tabMetas));
  }, [tabMetas]);

  useEffect(() => {
    if (!renamingTopicId || activeTab?.topicId === renamingTopicId) return;
    topicRenameSkipCommitRef.current = false;
    topicRenameCommitHandledRef.current = false;
    setRenamingTopicId(null);
    setTopicTitleDraft("");
  }, [activeTab?.topicId, renamingTopicId]);

  useEffect(() => {
    if (!activeTabId || !state.meta) return;
    setComposerProfilesByTab((current) => hydrateComposerProfileFromMeta(current, activeTabId, state.meta!));
  }, [activeTabId, state.meta]);

  const syncModeToController = useCallback((m: Mode) => setControllerMode(m), [setControllerMode]);

  useEffect(() => {
    void app.SetTrayLocale(locale).catch(() => {});
  }, [locale]);

  // applyMode is the single source of truth for the input mode: it updates the
  // local pill and pushes the matching gate state to the controller (plan = read
  // only; yolo = auto-approve approval-gated tools while user decisions still wait).
  // normal clears both.
  const applyMode = useCallback(
    (m: Mode) => {
      userPlanModeByTabRef.current = updateUserPlanModeIntent(userPlanModeByTabRef.current, activeTabId, modeHasPlan(m));
      patchActiveComposerProfile(composerProfileWithMode(m), ["collaborationMode", "toolApprovalMode", "goal"]);
      void syncModeToController(m);
    },
    [activeTabId, patchActiveComposerProfile, syncModeToController],
  );
  const applyCollaborationMode = useCallback(
    async (m: CollaborationMode): Promise<void> => {
      userPlanModeByTabRef.current = updateUserPlanModeIntent(userPlanModeByTabRef.current, activeTabId, m === "plan");
      if (m === "goal") {
        patchActiveComposerProfile({ collaborationMode: "normal", goalDraftMode: true, goal: "" }, ["collaborationMode", "goal"]);
        return setControllerCollaborationMode("normal");
      }
      patchActiveComposerProfile({ collaborationMode: m, goalDraftMode: false, goal: "" }, ["collaborationMode", "goal"]);
      if (goal.trim()) await clearControllerGoal();
      await setControllerCollaborationMode(m);
    },
    [activeTabId, clearControllerGoal, goal, patchActiveComposerProfile, setControllerCollaborationMode],
  );
  const applyToolApprovalMode = useCallback(
    (m: ToolApprovalMode) => {
      if (!activeTabId) return;
      if (m === "yolo") {
        if (toolApprovalMode !== "yolo") {
          yoloRestoreToolApprovalModesRef.current[activeTabId] = restorableToolApprovalMode(toolApprovalMode);
        }
      } else {
        yoloRestoreToolApprovalModesRef.current[activeTabId] = restorableToolApprovalMode(m);
      }
      patchActiveComposerProfile({ toolApprovalMode: m }, ["toolApprovalMode"]);
      void setControllerToolApprovalMode(m);
    },
    [activeTabId, patchActiveComposerProfile, setControllerToolApprovalMode, toolApprovalMode],
  );
  const toggleYoloApprovalMode = useCallback(() => {
    if (!activeTabId) return;
    const next = toggleYoloToolApprovalMode(
      toolApprovalMode,
      yoloRestoreToolApprovalModesRef.current[activeTabId],
    );
    if (next.restore) {
      yoloRestoreToolApprovalModesRef.current[activeTabId] = next.restore;
    }
    applyToolApprovalMode(next.mode);
  }, [activeTabId, applyToolApprovalMode, toolApprovalMode]);
  const applyGoal = useCallback(
    async (nextGoal: string): Promise<void> => {
      userPlanModeByTabRef.current = updateUserPlanModeIntent(userPlanModeByTabRef.current, activeTabId, false);
      const trimmed = nextGoal.trim();
      patchActiveComposerProfile({
        collaborationMode: trimmed ? "goal" : "normal",
        goalDraftMode: false,
        goal: trimmed,
      }, ["collaborationMode", "goal"]);
      await (trimmed ? setControllerGoal(trimmed) : clearControllerGoal());
    },
    [activeTabId, clearControllerGoal, patchActiveComposerProfile, setControllerGoal],
  );
  const applyTokenMode = useCallback(
    async (m: TokenMode): Promise<void> => {
      const tabId = activeTabId;
      if (!tabId || runtimeTransitionTabsRef.current.has(tabId) || m === composerProfile.tokenMode) return;
      const previous = composerProfile.tokenMode;
      runtimeTransitionTabsRef.current.add(tabId);
      setRuntimeTransitionsByTab((current) => ({ ...current, [tabId]: true }));
      setComposerProfilesByTab((current) => patchComposerProfile(current, tabId, composerProfile, { tokenMode: m }, ["tokenMode"]));
      const switched = await setTokenMode(m);
      if (!switched) {
        setComposerProfilesByTab((current) => {
          const profile = current[tabId] ?? composerProfile;
          const pending = { ...profile.pending };
          delete pending.tokenMode;
          return { ...current, [tabId]: { ...profile, tokenMode: previous, pending } };
        });
      }
      runtimeTransitionTabsRef.current.delete(tabId);
      setRuntimeTransitionsByTab((current) => {
        if (!current[tabId]) return current;
        const next = { ...current };
        delete next[tabId];
        return next;
      });
    },
    [activeTabId, composerProfile, setTokenMode],
  );
  // Shift+Tab toggles only the collaboration axis; Ctrl/Cmd+Y toggles YOLO on the
  // tool-permission axis while preserving the Ask/Auto base mode.
  const cycleMode = useCallback(() => {
    applyCollaborationMode(collaborationMode === "plan" ? "normal" : "plan");
  }, [applyCollaborationMode, collaborationMode]);

  // Switching models rebuilds the controller, which starts in normal mode — so
  // re-apply the current mode, or the pill would say plan/YOLO while the fresh
  // controller silently uses normal gating.
  const switchModel = useCallback(
    async (name: string) => {
      await setModel(name);
      await setControllerCollaborationMode(controllerComposerProfileCollaborationMode(composerProfile));
      await setControllerToolApprovalMode(toolApprovalMode);
      if (goal.trim()) await setControllerGoal(goal);
    },
    [composerProfile, goal, setControllerCollaborationMode, setControllerGoal, setControllerToolApprovalMode, setModel, toolApprovalMode],
  );

  // Startup and workspace/model rebuilds create a fresh controller in normal
  // mode. Re-apply the UI mode once the controller is ready, including the case
  // where the user picked YOLO while boot was still loading and the legacy
  // SetBypass binding was a harmless no-op.
  useEffect(() => {
    if (!controllerReady) return;
    void setControllerCollaborationMode(controllerComposerProfileCollaborationMode(composerProfile));
    void setControllerToolApprovalMode(toolApprovalMode);
    if (goal.trim()) void setControllerGoal(goal);
  }, [composerProfile, controllerReady, goal, setControllerCollaborationMode, setControllerGoal, setControllerToolApprovalMode, toolApprovalMode]);

  // The live task list pinned above the composer comes from the most recent
  // successful top-level todo_write result; failed or still-running attempts do
  // not advance the canonical panel state. Incomplete lists are always shown so
  // a stale local dismissal cannot hide work that still blocks final readiness;
  // every new list starts collapsed while its header keeps showing live progress
  // and the current task; completed lists can then be dismissed. The dismissal
  // key is still based on stable todo content/state so history reloads
  // do not resurrect the same finished list under a different event id. The
  // batch key ignores status changes so progress within the same task list does
  // not look like a brand-new task batch. Dismissal and open state are scoped to
  // the active session/topic/tab so different projects and sessions do not hide
  // or reopen each other's todo panels.
  const todoEntry = useMemo(() => {
    for (let i = state.items.length - 1; i >= 0; i--) {
      const it = state.items[i];
      if (it.kind === "tool" && it.name === "todo_write" && !it.parentId && it.status === "done" && !it.error) {
        return { item: it, index: i };
      }
    }
    return null;
  }, [state.items]);
  const todoItem = todoEntry?.item ?? null;
  const todos = useMemo(() => (todoItem ? parseTodos(todoItem.args) : []), [todoItem]);
  const [dismissedTodoKeys, setDismissedTodoKeys] = useState<Set<string>>(loadDismissedTodoKeys);
  const todoKey = useMemo(() => todoDismissalKey(todos), [todos]);
  const todoBatch = useMemo(() => todoBatchKey(todos), [todos]);
  const todoScope = useMemo(
    () => todoPanelScope({ activeTab, activeTabId, eventChannel: state.meta?.eventChannel }),
    [activeTab, activeTabId, state.meta?.eventChannel],
  );
  const dismissedTodo = useMemo(
    () => dismissedTodoKeyForScope(todoScope, dismissedTodoKeys, todoKey),
    [dismissedTodoKeys, todoKey, todoScope],
  );
  const scopedTodoKey = useMemo(() => scopedTodoDismissalKey(todoScope, todoKey), [todoKey, todoScope]);
  const scopedTodoBatch = useMemo(() => scopedTodoBatchKey(todoScope, todoBatch), [todoBatch, todoScope]);
  const showTodos = shouldShowTodoPanel(todoKey, dismissedTodo, todos);
  const dismissTodos = useCallback(() => {
    if (!scopedTodoKey) return;
    setDismissedTodoKeys((current) => {
      if (current.has(scopedTodoKey)) return current;
      const next = new Set(current);
      next.add(scopedTodoKey);
      saveDismissedTodoKeys(next);
      return next;
    });
  }, [scopedTodoKey]);

  const sessionTitle = topicTitle(activeTab);
  const sessionHasContent = state.items.length > 0 || Boolean(state.live?.text || state.live?.reasoning);

  // Theme pack scene: home when the session is empty, task once content exists.
  useEffect(() => {
    applyThemeScene(sessionHasContent ? "task" : "home");
  }, [sessionHasContent]);
  const getSessionMarkdown = useCallback(
    () => sessionItemsToMarkdown(sessionTitle, state.items, state.live),
    [sessionTitle, state.items, state.live],
  );
  const getSessionJson = useCallback(
    () => sessionItemsToJson(sessionTitle, state.items, state.live),
    [sessionTitle, state.items, state.live],
  );

  useEffect(() => {
    if (!topicExportOpen) return;
    const onDown = (event: MouseEvent) => {
      const target = event.target as Element | null;
      if (!target?.closest(".topicbar__export")) setTopicExportOpen(false);
    };
    document.addEventListener("mousedown", onDown);
    return () => document.removeEventListener("mousedown", onDown);
  }, [topicExportOpen]);

  const exportSession = useCallback(
    async (format: "markdown" | "json" | "pdf" | "image") => {
      const base = safeFilename(sessionTitle);
      setTopicExportOpen(false);
      try {
        if (format === "json") {
          const path = await app.PickExportFile(`${base}.json`, "application/json");
          if (path) {
            await app.SaveExportFile(path, getSessionJson(), false);
            showToast(t("topicBar.exportSuccess", { count: 1 }), "info");
          }
        } else if (format === "pdf") {
          const path = await app.PickExportFile(`${base}.pdf`, "application/pdf");
          if (!path) return;
          const { blobToBase64, renderSessionPdfBlob } = await import("./lib/sessionExport");
          const blob = await renderSessionPdfBlob(getSessionMarkdown(), sessionTitle);
          await app.SaveExportFile(path, await blobToBase64(blob), true);
          showToast(t("topicBar.exportSuccess", { count: 1 }), "info");
        } else if (format === "image") {
          const path = await app.PickExportFile(`${base}.png`, "image/png");
          if (!path) return;
          const { renderSessionImageBase64Payloads } = await import("./lib/sessionExport");
          const payloads = await renderSessionImageBase64Payloads(getSessionMarkdown());
          await app.SaveExportImageFiles(path, payloads);
          showToast(
            payloads.length > 1
              ? t("topicBar.exportImageParts", { count: payloads.length })
              : t("topicBar.exportSuccess", { count: 1 }),
            "info",
          );
        } else {
          const path = await app.PickExportFile(`${base}.md`, "text/markdown");
          if (path) {
            await app.SaveExportFile(path, getSessionMarkdown(), false);
            showToast(t("topicBar.exportSuccess", { count: 1 }), "info");
          }
        }
      } catch (err) {
        console.error("Failed to export session", err);
        showToast(
          t("topicBar.exportFailed", { error: err instanceof Error ? err.message : String(err) }),
          "error",
          { durationMs: 8000 },
        );
      }
    },
    [getSessionJson, getSessionMarkdown, sessionTitle, showToast, t],
  );

  useEffect(() => {
    if (!activeTabId || state.running) return;
    const text = pendingPlanRevisionsByTab[activeTabId];
    if (!text || pendingPlanRevisionSendingTabsRef.current.has(activeTabId)) return;
    pendingPlanRevisionSendingTabsRef.current.add(activeTabId);
    void commitThenSendRef.current(activeTabId, text)
      .then(() => {
        setPendingPlanRevisionsByTab((current) => {
          if (current[activeTabId] !== text) return current;
          const next = { ...current };
          delete next[activeTabId];
          return next;
        });
      })
      .catch((err) => {
        console.warn("Failed to submit pending plan revision", err);
      })
      .finally(() => {
        pendingPlanRevisionSendingTabsRef.current.delete(activeTabId);
      });
  }, [activeTabId, pendingPlanRevisionsByTab, state.running]);

  useEffect(() => {
    setClearContextPending(false);
    setWorkspaceInsertTarget("composer");
  }, [activeTabId]);

  const cancelClearContext = useCallback(() => {
    setClearContextPending(false);
  }, []);

  const confirmClearContext = useCallback(async () => {
    setClearContextPending(false);
    try {
      await clearSession();
      setDockRefreshKey((v) => v + 1);
      notice(t("clearContext.done"));
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      notice(msg || t("clearContext.failed"), "warn");
    }
  }, [clearSession, notice, t]);

  useEffect(() => {
    activeTabIdRef.current = activeTabId;
  }, [activeTabId]);

  // handleSend intercepts slash commands that need a desktop-native action before
  // they reach the backend: "/model <ref>" rebuilds on that model, "/memory"
  // opens Settings, and "/clear" shows an in-app confirmation card. Everything else — skills (/init, …),
  // custom commands, bare /model and the other read-only management verbs
  // (/skill, /hooks, /mcp) — goes straight to Submit, which the controller
  // resolves (a turn, or a listing Notice).
  const handleSend = useCallback(
    async (displayText: string, submitText = displayText, requestedTabId = activeTabId, structured?: StructuredInvocationSubmit) => {
      const sourceTabId = requestedTabId || activeTabId;
      if (!sourceTabId) throw new Error(t("composer.workspaceStarting"));
      const trimmed = displayText.trim();
      // "!<cmd>" runs a shell command directly, bypassing the model.
      if (trimmed.startsWith("!")) {
        const cmd = trimmed.slice(1).trim();
        if (!cmd) {
          notice("usage: !<command>  (e.g. !ls -la)");
          return;
        }
        await runShellForTab(sourceTabId, cmd);
        return;
      }
      const model = /^\/model\s+(\S+)$/.exec(trimmed);
      if (model) {
        void switchModel(model[1]);
        return;
      }
      if (trimmed === "/memory") {
        if (activeTabIdRef.current !== sourceTabId) return;
        closeTransientOverlays();
        setSettingsTarget("memory");
        return;
      }
      if (trimmed === "/clear") {
        if (activeTabIdRef.current !== sourceTabId) return;
        setClearContextPending(true);
        return;
      }
      const goalCommand = /^\/goal(?:\s+(.*))?$/.exec(trimmed);
      if (goalCommand) {
        const arg = (goalCommand[1] ?? "").trim();
        const displayGoal = stripGoalResearchFlags(arg);
        if (displayGoal && !["status", "clear", "off", "stop", "done"].includes(displayGoal.toLowerCase())) {
          if (hasGoalResearchFlag(arg)) {
            userPlanModeByTabRef.current = updateUserPlanModeIntent(userPlanModeByTabRef.current, activeTabId, false);
            patchActiveComposerProfile({
              collaborationMode: "goal",
              goalDraftMode: false,
              goal: displayGoal,
            }, ["collaborationMode", "goal"]);
          } else {
            await applyGoal(displayGoal);
          }
        } else if (["clear", "off", "stop", "done"].includes(displayGoal.toLowerCase())) {
          await applyGoal("");
        }
        if (!controllerReady) return;
        await commitThenSendRef.current(sourceTabId, trimmed, submitText.trim());
        return;
      }
      if (collaborationMode === "goal" && !goal.trim()) {
        if (!controllerReady) return;
        await applyGoal(trimmed);
        await commitThenSendRef.current(sourceTabId, trimmed, `/goal ${submitText.trim()}`);
        return;
      }
      const theme = /^\/theme(?:\s+(\S+))?$/.exec(trimmed);
      if (theme) {
        const arg = theme[1]?.toLowerCase();
        if (!arg) {
          const cur = getTheme();
          notice(t("settings.themeCurrent", { theme: cur, style: getThemeStyle(cur) }));
          return;
        }
        if (arg === "reset" || arg === "default" || arg === "clear") {
          try {
            await app.ResetThemePack();
            clearThemePack();
            notice(t("settings.themeReset"));
          } catch (err) {
            showToast(err instanceof Error ? err.message : String(err), "error");
          }
          return;
        }
        if (isThemeMode(arg)) {
          const next = arg;
          const style = getThemeStyle(next);
          try {
            await app.SetDesktopAppearance(next, style);
            applyTheme(next, style);
            notice(t("settings.themeChanged", { theme: next, style }));
          } catch (err) {
            showToast(err instanceof Error ? err.message : String(err), "error");
          }
          return;
        }
        if (isThemeStyle(arg)) {
          const cur = getTheme();
          try {
            await app.SetDesktopAppearance(cur, arg);
            applyTheme(cur, arg);
            notice(t("settings.themeChanged", { theme: cur, style: arg }));
          } catch (err) {
            showToast(err instanceof Error ? err.message : String(err), "error");
          }
          return;
        }
        notice(t("settings.themeUnknown", { name: arg }), "warn");
        return;
      }
      if (!controllerReady) return;
      await setControllerCollaborationModeForTab(sourceTabId, controllerComposerProfileCollaborationMode(composerProfile));
      await setControllerToolApprovalModeForTab(sourceTabId, toolApprovalMode);
      if (goal.trim()) await setControllerGoalForTab(sourceTabId, goal);
      await commitThenSendRef.current(sourceTabId, trimmed, submitText.trim(), structured);
    },
    [activeTabId, applyGoal, closeTransientOverlays, collaborationMode, composerProfile, controllerReady, goal, notice, runShellForTab,
      setControllerCollaborationModeForTab, setControllerGoalForTab, setControllerToolApprovalModeForTab, switchModel, t, toolApprovalMode, showToast],
  );

  const handleSteer = useCallback(async (text: string, requestedTabId = activeTabId) => {
    const sourceTabId = requestedTabId || activeTabId;
    if (!sourceTabId) throw new Error(t("composer.workspaceStarting"));
    await steerForTab(sourceTabId, text.trim());
  }, [activeTabId, steerForTab, t]);

  const refreshTabMetas = useCallback(async (): Promise<TabMeta[]> => {
    const tabs = asArray(await app.ListTabs().catch(() => [] as TabMeta[]));
    setTabMetas(tabs);
    return tabs;
  }, []);
  const seedActiveTabMeta = useCallback((tab: TabMeta): void => {
    setTabMetas((current) => {
      const seeded = { ...tab, active: true };
      let found = false;
      const next = current.map((existing) => {
        if (existing.id === tab.id) {
          found = true;
          return { ...existing, ...seeded };
        }
        return existing.active ? { ...existing, active: false } : existing;
      });
      return found ? next : [...next, seeded];
    });
    setTabOrderIds((current) => current.includes(tab.id) ? current : [...current, tab.id]);
  }, []);

  useEffect(() => {
    const unsub = onEvent((e) => {
      if (e.kind !== "turn_done") return;
      const turnTabId = resolvePlanRestoreTabId(e.tabId, activeTabIdRef.current);
      window.setTimeout(() => {
        setProjectRevision((value) => value + 1);
        refreshTabMetas().then((tabs) => {
          if (!turnTabId) return;
          const tab = tabs.find((item) => item.id === turnTabId);
          const baseProfile = tab ? composerProfileFromTab(tab) : defaultComposerProfile;
          if (!shouldRestoreUserPlanModeForProfile(userPlanModeByTabRef.current, turnTabId, baseProfile)) {
            if (baseProfile.goal.trim()) {
              userPlanModeByTabRef.current = updateUserPlanModeIntent(userPlanModeByTabRef.current, turnTabId, false);
            }
            return;
          }
          setComposerProfilesByTab((current) => patchComposerProfile(
            current,
            turnTabId,
            current[turnTabId] ?? baseProfile,
            { collaborationMode: "plan", goalDraftMode: false, goal: "" },
            ["collaborationMode", "goal"],
          ));
          if (activeTabIdRef.current === turnTabId) {
            void setControllerCollaborationMode("plan");
          }
        });
      }, 250);
    });
    return unsub;
  }, [refreshTabMetas, setControllerCollaborationMode]);

  const blankSessionTarget = useCallback(() => {
    const activeWorkspaceRoot = activeTab?.scope === "project" ? activeTab.workspaceRoot || "" : "";
    const scope = activeWorkspaceRoot ? "project" : "global";
    return { scope, workspaceRoot: activeWorkspaceRoot };
  }, [activeTab?.scope, activeTab?.workspaceRoot]);

  useEffect(() => {
    void refreshTabMetas();
    const id = window.setInterval(() => void refreshTabMetas(), 2000);
    return () => window.clearInterval(id);
  }, [refreshTabMetas]);

  useEffect(() => {
    return onProjectTreeChanged(() => {
      setProjectRevision((value) => value + 1);
      void refreshTabMetas();
    });
  }, [refreshTabMetas]);

  // Bridge remote:* events into the remote store once, app-wide, so the
  // StatusBar chip, host manager, and explorer all see the same live state.
  useEffect(() => {
    const offStatus = onRemoteStatus((s) => applyRemoteStatus(s));
    const offForwards = onRemoteForwards((e) => setRemoteForwards(e.hostId, e.forwards));
    const offServer = onRemoteServer((s) => setRemoteServer(s));
    return () => {
      offStatus();
      offForwards();
      offServer();
    };
  }, [applyRemoteStatus, setRemoteForwards, setRemoteServer]);

  useEffect(() => {
    let cancelled = false;
    void app.RemoteHosts()
      .then((hosts) => {
        if (!cancelled) setRemoteHosts(hosts);
      })
      .catch(() => {});
    void app.RemoteConnectionStatuses()
      .then((statuses) => {
        if (!cancelled) hydrateRemoteStatuses(statuses);
      })
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, [hydrateRemoteStatuses, setRemoteHosts]);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const needs = await app.NeedsOnboarding();
        if (!cancelled) setNeedsOnboarding(needs);
      } catch {
        // Bridge unavailable (browser dev seam) — skip the gate; a real key
        // failure still surfaces via the topbar startupError banner.
        if (!cancelled) setNeedsOnboarding(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    const el = footerRef.current;
    if (!el || typeof ResizeObserver === "undefined") return;
    let frame = 0;
    const update = () => {
      if (frame) window.cancelAnimationFrame(frame);
      frame = window.requestAnimationFrame(() => {
        frame = 0;
        const next = Math.round(el.getBoundingClientRect().height);
        if (Math.abs(footerHeightRef.current - next) < 2) return;
        footerHeightRef.current = next;
        setFooterHeight(next);
      });
    };
    update();
    const observer = new ResizeObserver(update);
    observer.observe(el);
    return () => {
      if (frame) window.cancelAnimationFrame(frame);
      observer.disconnect();
    };
  }, []);

  // Run the ambient engine only while the agent is generating.
  useEffect(() => {
    if (state.running && isGenerativeMusicEnabled()) {
      generativeMusic.start();
    } else {
      generativeMusic.stop();
    }
    return () => generativeMusic.stop();
  }, [state.running]);

  // playTokenNote no-ops unless the engine is running, so subscribe unconditionally.
  useEffect(() => {
    const unsub = onEvent((e) => {
      if (e.kind === "text" || e.kind === "reasoning" || e.kind === "tool_dispatch") {
        generativeMusic.playTokenNote();
      }
    });
    return unsub;
  }, []);

  const toggleSidebar = useCallback(() => {
    closeTransientOverlays();
    pulseSidebarToggle();
    anchorAppScrollToChat();
    const nextCollapsed = !sidebarCollapsed;
    if (nextCollapsed) setSidebarSearchOpen(false);
    setSidebarCollapsed(nextCollapsed);
    saveSidebarCollapsed(nextCollapsed);
  }, [anchorAppScrollToChat, closeTransientOverlays, pulseSidebarToggle, sidebarCollapsed]);

  const sidebarWidthClamp = desktopLayoutStyle === "creation" ? clampCreationSidebarWidth : clampSidebarWidth;
  const sidebarRenderWidth = liveSidebarWidth ?? sidebarWidth;
  const sidebarResizeMinWidth = desktopLayoutStyle === "creation" ? CREATION_SIDEBAR_MIN_WIDTH : SIDEBAR_MIN_WIDTH;

  useEffect(() => {
    if (desktopLayoutStyle === "creation" || sidebarWidth >= SIDEBAR_MIN_WIDTH) return;
    setSidebarWidth(SIDEBAR_MIN_WIDTH);
    saveSidebarWidth(SIDEBAR_MIN_WIDTH);
  }, [desktopLayoutStyle, sidebarWidth]);

  useEffect(() => {
    if (desktopLayoutStyle === "creation") {
      if (rightDockTreeWidth >= CREATION_RIGHT_DOCK_TREE_MIN_WIDTH) return;
      setRightDockTreeWidth(CREATION_RIGHT_DOCK_TREE_MIN_WIDTH);
      saveRightDockTreeWidth(CREATION_RIGHT_DOCK_TREE_MIN_WIDTH);
      return;
    }
    if (rightDockTreeWidth >= RIGHT_DOCK_TREE_MIN_WIDTH) return;
    setRightDockTreeWidth(RIGHT_DOCK_TREE_MIN_WIDTH);
    saveRightDockTreeWidth(RIGHT_DOCK_TREE_MIN_WIDTH);
  }, [desktopLayoutStyle, rightDockTreeWidth]);

  // Creation no longer exposes the overview tab. If a previous session left
  // rightDockMode on "context", coerce it to files so 文件 stays selected.
  useEffect(() => {
    if (desktopLayoutStyle !== "creation") return;
    if (rightDockMode !== "context") return;
    setRightDockMode("files");
  }, [desktopLayoutStyle, rightDockMode, setRightDockMode]);

  const setExpandedSidebarWidth = useCallback((width: number) => {
    closeTransientOverlays();
    const next = sidebarWidthClamp(width);
    setSidebarWidth(next);
    saveSidebarWidth(next);
  }, [closeTransientOverlays, sidebarWidthClamp]);

  const startSidebarResize = useCallback(
    (event: ReactPointerEvent<HTMLButtonElement>) => {
      if (sidebarCollapsed) return;
      const layout = layoutRef.current;
      if (!layout) return;
      event.preventDefault();
      closeTransientOverlays();
      setSidebarResizing(true);
      let nextWidth = sidebarWidth;
      const liveResize = createRafResizeUpdater({
        target: layout,
        separator: event.currentTarget,
        cssVar: "--sidebar-expanded-width",
        onApply: setLiveSidebarWidth,
      });
      const dockLiveResize = createRafResizeUpdater({
        target: layout,
        cssVar: "--workspace-width",
        onApply: setLiveWorkspacePanelRenderWidth,
      });
      const onMove = (moveEvent: PointerEvent) => {
        nextWidth = sidebarWidthClamp(moveEvent.clientX);
        liveResize.schedule(nextWidth);
        dockLiveResize.schedule(resolveLiveWorkspacePanelRenderWidth(preferredWorkspacePanelWidth, nextWidth));
      };
      const onDone = () => {
        liveResize.flush();
        dockLiveResize.flush();
        setSidebarWidth(nextWidth);
        saveSidebarWidth(nextWidth);
        setLiveSidebarWidth(null);
        setLiveWorkspacePanelRenderWidth(null);
        setSidebarResizing(false);
        window.removeEventListener("pointermove", onMove);
        window.removeEventListener("pointerup", onDone);
        window.removeEventListener("pointercancel", onDone);
        document.body.style.cursor = "";
        document.body.style.userSelect = "";
      };
      document.body.style.cursor = "col-resize";
      document.body.style.userSelect = "none";
      window.addEventListener("pointermove", onMove);
      window.addEventListener("pointerup", onDone);
      window.addEventListener("pointercancel", onDone);
    },
    [closeTransientOverlays, preferredWorkspacePanelWidth, resolveLiveWorkspacePanelRenderWidth, sidebarCollapsed, sidebarWidth, sidebarWidthClamp],
  );

  const resizeSidebarWithKeyboard = useCallback(
    (event: KeyboardEvent<HTMLButtonElement>) => {
      if (sidebarCollapsed) return;
      if (event.key === "ArrowLeft" || event.key === "ArrowRight") {
        event.preventDefault();
        setExpandedSidebarWidth(sidebarWidth + (event.key === "ArrowRight" ? 16 : -16));
      } else if (event.key === "Home") {
        event.preventDefault();
        setExpandedSidebarWidth(sidebarResizeMinWidth);
      } else if (event.key === "End") {
        event.preventDefault();
        setExpandedSidebarWidth(SIDEBAR_MAX_WIDTH);
      }
    },
    [setExpandedSidebarWidth, sidebarCollapsed, sidebarWidth, sidebarResizeMinWidth],
  );

  const setSavedWorkspacePanelWidth = useCallback(
    (width: number) => {
      closeTransientOverlays();
      if (rightDockDetailActive) {
        const next = clampRightDockPreviewWidth(width);
        setRightDockPreviewWidth(next);
        saveRightDockPreviewWidth(next);
        return;
      }
      const next = rightDockTreeWidthClamp(width);
      setRightDockTreeWidth(next);
      saveRightDockTreeWidth(next);
    },
    [closeTransientOverlays, rightDockDetailActive, rightDockTreeWidthClamp],
  );

  const ensureWorkspacePanelWidth = useCallback(
    (width: number) => {
      closeTransientOverlays();
      if (rightDockMode === "context") return;
      const next = clampRightDockPreviewWidth(width);
      setRightDockPreviewWidth(next);
      saveRightDockPreviewWidth(next);
    },
    [closeTransientOverlays, rightDockMode],
  );

  const startWorkspacePanelResize = useCallback(
    (event: ReactPointerEvent<HTMLButtonElement>) => {
      if (!workspacePanelOpen) return;
      const layout = layoutRef.current;
      if (!layout) return;
      event.preventDefault();
      closeTransientOverlays();
      setWorkspacePanelResizing(true);
      const startX = event.clientX;
      const startDockWidth = workspacePanelRenderWidth;
      let nextDockWidth = startDockWidth;
      const liveResize = createRafResizeUpdater({
        target: layout,
        separator: event.currentTarget,
        cssVar: "--workspace-width",
        onApply: setLiveWorkspacePanelRenderWidth,
      });
      const onMove = (moveEvent: PointerEvent) => {
        const delta = moveEvent.clientX - startX;
        nextDockWidth = startDockWidth - delta;
        if (rightDockDetailActive) {
          nextDockWidth = clampRightDockPreviewWidth(nextDockWidth);
        } else {
          nextDockWidth = rightDockTreeWidthClamp(nextDockWidth);
        }
        liveResize.schedule(resolveLiveWorkspacePanelRenderWidth(nextDockWidth));
      };
      const onDone = () => {
        liveResize.flush();
        setSavedWorkspacePanelWidth(nextDockWidth);
        setLiveWorkspacePanelRenderWidth(null);
        setWorkspacePanelResizing(false);
        window.removeEventListener("pointermove", onMove);
        window.removeEventListener("pointerup", onDone);
        window.removeEventListener("pointercancel", onDone);
        document.body.style.cursor = "";
        document.body.style.userSelect = "";
      };
      document.body.style.cursor = "col-resize";
      document.body.style.userSelect = "none";
      window.addEventListener("pointermove", onMove);
      window.addEventListener("pointerup", onDone);
      window.addEventListener("pointercancel", onDone);
    },
    [closeTransientOverlays, resolveLiveWorkspacePanelRenderWidth, rightDockDetailActive, rightDockTreeWidthClamp, setSavedWorkspacePanelWidth, workspacePanelOpen, workspacePanelRenderWidth],
  );

  const resizeWorkspacePanelWithKeyboard = useCallback(
    (event: KeyboardEvent<HTMLButtonElement>) => {
      if (event.key === "ArrowLeft" || event.key === "ArrowRight") {
        event.preventDefault();
        setSavedWorkspacePanelWidth(workspacePanelRenderWidth + (event.key === "ArrowLeft" ? 16 : -16));
      } else if (event.key === "Home") {
        event.preventDefault();
        setSavedWorkspacePanelWidth(rightDockDetailActive ? RIGHT_DOCK_PREVIEW_MIN_WIDTH : rightDockTreeMinWidth);
      } else if (event.key === "End") {
        event.preventDefault();
        setSavedWorkspacePanelWidth(rightDockDetailActive ? RIGHT_DOCK_MAX_WIDTH : RIGHT_DOCK_TREE_MAX_WIDTH);
      }
    },
    [rightDockDetailActive, rightDockTreeMinWidth, setSavedWorkspacePanelWidth, workspacePanelRenderWidth],
  );

  const openWorkspacePanel = useCallback(
    (mode: RightDockMode = rightDockMode) => {
      closeTransientOverlays();
      if (mode === "context" || mode !== rightDockMode) {
        setWorkspacePreviewActive(false);
      }
      setRightDockMode(mode);
      let nextMaximized = workspacePanelMaximized;
      if (mode === "context") {
        nextMaximized = false;
        setWorkspacePanelMaximized(false);
      } else {
        // Keep file/change views docked; the rendered dock width is clamped to
        // the viewport so opening it reflows instead of forcing maximize.
        nextMaximized = false;
        setWorkspacePanelMaximized(false);
      }
      if (workspacePanelOpen && workspacePanelMaximized === nextMaximized) {
        return;
      }
      setWorkspacePanelOpen(true);
      saveWorkspacePanelOpen(true);
    },
    [closeTransientOverlays, rightDockMode, workspacePanelMaximized, workspacePanelOpen],
  );

  const closeWorkspacePanel = useCallback(() => {
    closeTransientOverlays();
    if (!workspacePanelOpen) {
      return;
    }
    setLiveWorkspacePanelRenderWidth(null);
    setWorkspacePanelMaximized(false);
    setWorkspacePanelOpen(false);
    saveWorkspacePanelOpen(false);
  }, [closeTransientOverlays, workspacePanelOpen]);

  const toggleWorkspacePanel = useCallback(() => {
    pulseWorkspaceToggle();
    if (workspacePanelRenderable) {
      closeWorkspacePanel();
      return;
    }
    // Creation hides the overview tab; never reopen into the invisible "context"
    // mode or neither 文件/改动 will show an active selection.
    if (desktopLayoutStyle === "creation") {
      openWorkspacePanel(rightDockMode === "changed" ? "changed" : "files");
      return;
    }
    openWorkspacePanel("context");
  }, [closeWorkspacePanel, desktopLayoutStyle, openWorkspacePanel, pulseWorkspaceToggle, rightDockMode, workspacePanelRenderable]);

  const openRightDockMode = useCallback(
    (mode: RightDockMode) => {
      openWorkspacePanel(mode);
    },
    [openWorkspacePanel],
  );

  useEffect(() => {
    if (!remoteExplorerOpen) return;
    openRightDockMode("remote");
    closeRemoteExplorerRequest();
  }, [closeRemoteExplorerRequest, openRightDockMode, remoteExplorerOpen]);

  useEffect(() => {
    if (remoteHosts.length > 0 || rightDockMode !== "remote") return;
    setRightDockMode("files");
  }, [remoteHosts.length, rightDockMode, setRightDockMode]);

  const openRemoteDock = useCallback(() => {
    const fallback = remoteHosts.find((host) => {
      const state = useRemoteStore.getState().statuses[host.id]?.state;
      return state === "connected" || state === "degraded";
    }) ?? remoteHosts[0];
    const hostId = remoteExplorerHostId && remoteHosts.some((host) => host.id === remoteExplorerHostId)
      ? remoteExplorerHostId
      : fallback?.id;
    if (hostId) requestRemoteExplorer(hostId);
  }, [remoteExplorerHostId, remoteHosts, requestRemoteExplorer]);

  const launchRemoteWorkspace = useCallback(async (host: RemoteHostView) => {
    const workspace = host.defaultWorkspace.trim();
    if (!workspace) {
      useRemoteStore.getState().setExplorerTab("server");
      requestRemoteExplorer(host.id);
      return;
    }
    await app.OpenRemoteWorkspace(host.id, workspace);
  }, [requestRemoteExplorer]);

  const openRemoteWorkspaceFromStatus = useCallback((host: RemoteHostView) => {
    void launchRemoteWorkspace(host).catch((err) => {
      showToast(err instanceof Error ? err.message : String(err), "error", { durationMs: 6000 });
    });
  }, [launchRemoteWorkspace, showToast]);

  const connectAndOpenRemoteWorkspace = useCallback((host: RemoteHostView) => {
    void (async () => {
      const status = useRemoteStore.getState().statuses[host.id]?.state;
      if (status !== "connected" && status !== "degraded") {
        // Clear any stale failure before the new generation starts; otherwise a
        // previous stopped+error snapshot could make the waiter reject before
        // the kernel's fresh connecting event reaches the frontend.
        useRemoteStore.getState().applyStatus({ hostId: host.id, state: "connecting" });
        await app.ConnectRemoteHost(host.id);
        await waitForRemoteConnection(host.id);
      }
      await launchRemoteWorkspace(host);
    })().catch((err) => {
      showToast(err instanceof Error ? err.message : String(err), "error", { durationMs: 6000 });
    });
  }, [launchRemoteWorkspace, showToast]);

  const handleWorkspacePreviewModeChange = useCallback(
    (active: boolean) => {
      if (workspacePreviewActive === active) return;
      closeTransientOverlays();
      setWorkspacePreviewActive(active);
    },
    [closeTransientOverlays, workspacePreviewActive],
  );

  const layoutStyle = useMemo(
    () =>
      ({
        "--sidebar-expanded-width": `${sidebarRenderWidth}px`,
        "--chat-min-width": `${chatReservedWidth}px`,
        "--workspace-width": `${workspacePanelRenderWidth}px`,
        "--workspace-resizer-width": `${WORKSPACE_RESIZER_WIDTH}px`,
      }) as CSSProperties,
    [chatReservedWidth, sidebarRenderWidth, workspacePanelRenderWidth],
  );

  const setWorkspacePanel = useCallback((open: boolean) => {
    if (open) {
      openWorkspacePanel();
    } else {
      closeWorkspacePanel();
    }
  }, [closeWorkspacePanel, openWorkspacePanel]);

  const addWorkspaceTextToComposer = useCallback((text: string) => {
    if (activeTabId && workspaceInsertTarget === "planRevision" && state.approval?.tool === "exit_plan_mode") {
      setPlanRevisionInsertRequest({
        tabId: activeTabId,
        approvalId: state.approval.id,
        request: { id: Date.now(), text },
      });
      return;
    }
    if (activeTabId) {
      setComposerInsertRequestsByTab((current) => ({
        ...current,
        [activeTabId]: { id: Date.now(), text },
      }));
    }
  }, [activeTabId, state.approval, workspaceInsertTarget]);

  const addSelectedTextToComposer = useCallback((text: string) => {
    const selected = text.trim();
    if (!activeTabId || !selected) return;
    selectedTextRequestIdRef.current += 1;
    setSelectedTextRequestsByTab((current) => ({
      ...current,
      [activeTabId]: { id: selectedTextRequestIdRef.current, text: selected },
    }));
  }, [activeTabId]);

  const addWorkspaceCodeToComposer = useCallback((path: string, code: string) => {
    if (!activeTabId || !code.trim()) return;
    if (workspaceInsertTarget === "planRevision" && state.approval?.tool === "exit_plan_mode") {
      // The plan-revision input is plain text and only consumes request.text,
      // so hand it the fenced rendering instead of a structured reference.
      setPlanRevisionInsertRequest({
        tabId: activeTabId,
        approvalId: state.approval.id,
        request: { id: Date.now(), text: formatSelectionReference(path, code) },
      });
      return;
    }
    selectedTextRequestIdRef.current += 1;
    setSelectedTextRequestsByTab((current) => ({
      ...current,
      [activeTabId]: { id: selectedTextRequestIdRef.current, text: code, path },
    }));
  }, [activeTabId, state.approval, workspaceInsertTarget]);

  // Coalesce tab-bar switches through the same last-click-wins scheduler that
  // openTopic/blank/resume navigation uses, so rapidly clicking between two
  // running sessions can't run two switchTab() calls concurrently. Concurrent
  // switches race on the backend SetActiveTab/confirmBackendActiveTab ordering,
  // which lands events + hydration on the wrong session (#5352). switchTab's own
  // loadSessionDataForTab is already seq-guarded; this serializes the backend
  // activation around it.
  const tabSwitchSeqRef = useRef(0);
  const tabSwitchRunningRef = useRef(false);
  const tabSwitchPendingRef = useRef<PendingNavigationRequest<{ tabId: string; optimisticTab?: TabMeta }> | null>(null);
  const enqueueTabSwitch = useCallback(
    (tabId: string, optimisticTab?: TabMeta): Promise<void> =>
      enqueueNavigationRequest(
        { seqRef: tabSwitchSeqRef, runningRef: tabSwitchRunningRef, pendingRef: tabSwitchPendingRef },
        { tabId, optimisticTab },
        async (request) => {
          await switchTab(request.tabId, request.optimisticTab);
          await refreshTabMetas();
        },
      ),
    [switchTab, refreshTabMetas],
  );

  const handleTabChange = useCallback((id: string) => {
    closeTransientOverlays();
    const selected = tabMetas.find((tab) => tab.id === id);
    setTabMetas((current) => current.map((tab) => ({ ...tab, active: tab.id === id })));
    void enqueueTabSwitch(id, selected);
    setTabRevealSignal((signal) => signal + 1);
  }, [closeTransientOverlays, enqueueTabSwitch, tabMetas]);

  const handleTabClose = useCallback(async (id: string) => {
    closeTransientOverlays();
    setComposerProfilesByTab((current) => {
      if (!(id in current)) return current;
      const next = { ...current };
      delete next[id];
      return next;
    });
    setTabMetas((current) => {
      if (current.length <= 1) return current;
      const closingIndex = current.findIndex((tab) => tab.id === id);
      if (closingIndex < 0) return current;
      const closingTab = current[closingIndex];
      const remaining = current.filter((tab) => tab.id !== id);
      if (!closingTab.active && closingTab.id !== activeTabId) return remaining;
      const nextIndex = Math.min(closingIndex, remaining.length - 1);
      const nextActiveId = remaining[nextIndex]?.id;
      return remaining.map((tab) => ({ ...tab, active: tab.id === nextActiveId }));
    });
    await closeTab(id);
    await refreshTabMetas();
    setTabRevealSignal((signal) => signal + 1);
  }, [activeTabId, closeTab, closeTransientOverlays, refreshTabMetas]);

  const handleTabsClose = useCallback(async (ids: string[], nextActiveTabId?: string) => {
    closeTransientOverlays();
    const currentIds = tabMetas.map((tab) => tab.id);
    const targets = ids.filter((id, index) => currentIds.includes(id) && ids.indexOf(id) === index);
    if (targets.length === 0) return;
    for (const id of targets) {
      await closeTab(id);
    }
    if (nextActiveTabId && currentIds.includes(nextActiveTabId)) {
      const selected = tabMetas.find((tab) => tab.id === nextActiveTabId);
      setTabMetas((current) => current.map((tab) => ({ ...tab, active: tab.id === nextActiveTabId })));
      void enqueueTabSwitch(nextActiveTabId, selected);
    }
    await refreshTabMetas();
    setTabRevealSignal((signal) => signal + 1);
  }, [closeTab, closeTransientOverlays, enqueueTabSwitch, refreshTabMetas, tabMetas]);

  const handleTabsReorder = useCallback(async (ids: string[]) => {
    setTabOrderIds(ids);
    setTabMetas((current) => {
      const byId = new Map(current.map((tab) => [tab.id, tab]));
      const ordered = ids.map((id) => byId.get(id)).filter((tab): tab is TabMeta => Boolean(tab));
      return ordered.length === current.length ? ordered : current;
    });
    await reorderTabs(ids);
    await refreshTabMetas();
    setTabRevealSignal((signal) => signal + 1);
  }, [refreshTabMetas, reorderTabs]);

  const [rewindSignal, setRewindSignal] = useState(0);

  // ── Optimistic rewind ─────────────────────────────────────────────────
  // Rewind is optimistic: the UI immediately truncates, scrolls to the
  // target, fills the composer, and shows an undo banner.  The real Go
  // Rewind is deferred until the user SENDS a new message.  Undo simply
  // restores the full items list — no Go call needed.
  type RewindState = {
    turn: number;
    scope: string;
    fullItems: Item[];     // pre-truncation items (for undo)
    boundaryIdx: number;   // first item index of the rewound-to turn
    turnDiff: number;      // turns rolled back
    prompt: string;        // user message text for composer fill
  };
  const [rewindStatesByTab, setRewindStatesByTab] = useState<Record<string, RewindState>>({});
  const rewindStatesByTabRef = useRef(rewindStatesByTab);
  rewindStatesByTabRef.current = rewindStatesByTab;
  const [rewindCommittingByTab, setRewindCommittingByTab] = useState<Record<string, boolean>>({});
  const rewindState = activeTabId ? rewindStatesByTab[activeTabId] ?? null : null;
  const rewindCommitting = Boolean(activeTabId && rewindCommittingByTab[activeTabId]);

  const setRewindStateForTab = useCallback((tabId: string, nextState: RewindState | null) => {
    if (!tabId) return;
    const next = { ...rewindStatesByTabRef.current };
    if (nextState) next[tabId] = nextState;
    else delete next[tabId];
    rewindStatesByTabRef.current = next;
    setRewindStatesByTab(next);
  }, []);

  const setRewindCommittingForTab = useCallback((tabId: string, committing: boolean) => {
    setRewindCommittingByTab((current) => {
      const next = { ...current };
      if (committing) next[tabId] = true;
      else delete next[tabId];
      return next;
    });
  }, []);

  const hydratePlaceholderActive = Boolean(
    state.hydrating &&
    state.items.length === 0 &&
    state.hydratePlaceholderItems?.length,
  );
  const transcriptHydrating = state.hydrating && !state.hydrateHistoryLoaded;
  const transcriptItems = hydratePlaceholderActive ? state.hydratePlaceholderItems! : state.items;

  // Display items: truncated when an optimistic rewind is pending.
  const displayItems = useMemo(() => {
    if (!rewindState) return transcriptItems;
    return transcriptItems.slice(0, rewindState.boundaryIdx).filter((it) => it.kind !== "compaction");
  }, [transcriptItems, rewindState]);
  const latestGuidanceConsumed = useMemo(() => {
    for (let i = state.items.length - 1; i >= 0; i--) {
      const item = state.items[i];
      if (item.kind === "notice" && item.text.startsWith("↪ ")) {
        return { key: item.id, text: item.text.slice(2) };
      }
    }
    return null;
  }, [state.items]);

  // send wrapper: commits any pending optimistic rewind before sending.
  const commitThenSend = useCallback(async (sourceTabId: string, displayText: string, submitText?: string, structured?: StructuredInvocationSubmit) => {
    const sourceTab = tabMetas.find((tab) => tab.id === sourceTabId);
    if (!sourceTab) throw new Error(t("composer.workspaceStarting"));
    if (sourceTab.readOnly) throw new Error(t("composer.readOnlyChannel"));
    if (sourceTab.ready === false) throw new Error(t("composer.workspaceStarting"));
    const rs = rewindStatesByTabRef.current[sourceTabId];
    if (rs) {
      setRewindStateForTab(sourceTabId, null);
      setRewindCommittingForTab(sourceTabId, true);
      let ok = false;
      try {
        ok = await rewindForTab(sourceTabId, rs.turn, rs.scope);
      } finally {
        setRewindCommittingForTab(sourceTabId, false);
      }
      if (!ok) {
        // Rewind failed: the Go conversation is intact. Do not send; the
        // controller emits a notice with the reason.
        setRewindStateForTab(sourceTabId, rs);
        throw new Error(t("rewind.failed"));
      }
      setRewindSignal((v) => v + 1);
      if (rs.scope === "both") {
        // Code was only reverted now (deferred), so refresh the dock here.
        setDockRefreshKey((v) => v + 1);
        setProjectRevision((v) => v + 1);
      }
    }
    await sendToTab(sourceTabId, displayText, submitText, undefined, structured);
  }, [rewindForTab, sendToTab, setRewindCommittingForTab, setRewindStateForTab, t, tabMetas]);

  const handleTranscriptPrompt = useCallback((text: string) => {
    if (!activeTabId || !controllerReady) return;
    void commitThenSend(activeTabId, text).catch((err) => {
      console.warn("Failed to submit transcript prompt", err);
    });
  }, [activeTabId, commitThenSend, controllerReady]);

  const handleDeliveryContinue = useCallback(async () => {
    await continueDelivery({
      tabId: activeTabIdRef.current,
      ready: controllerReady,
      goal: state.meta?.goal,
      activeTabId: () => activeTabIdRef.current,
      resumeGoal: resumeControllerGoalForTab,
      send: (tabId) => recoverDeliveryToTab(tabId, t("notice.deliveryIncompleteContinuePrompt")),
    });
  }, [controllerReady, recoverDeliveryToTab, resumeControllerGoalForTab, state.meta?.goal, t]);
  commitThenSendRef.current = commitThenSend;

  const handleMessageAction = useCallback((turn: number, scope: string) => {
    const sourceTabId = activeTabId;
    if (!sourceTabId || activeTab?.readOnly) return;
    if (hydratePlaceholderActive) return;
    if (scope === "fork") {
      // Fork still goes through the controller (not optimistic).
      rewindForTab(sourceTabId, turn, scope).then((ok) => {
        if (!ok) return;
        refreshTabMetas();
        setProjectRevision((v) => v + 1);
      });
      return;
    }

    // Code-only rewind only affects files — no message truncation,
    // no optimistic UI needed.  Execute immediately.
    if (scope === "code") {
      rewindForTab(sourceTabId, turn, scope).then((ok) => {
        if (!ok) return;
        setDockRefreshKey((v) => v + 1);
        setProjectRevision((v) => v + 1);
      });
      return;
    }

    // Summarize only compresses the conversation log — no files touched,
    // no optimistic UI needed. Execute immediately like code-only rewind.
    if (scope === "summ-from" || scope === "summ-upto") {
      rewindForTab(sourceTabId, turn, scope).then((ok) => {
        if (!ok) return;
        setDockRefreshKey((v) => v + 1);
        setProjectRevision((v) => v + 1);
      });
      return;
    }

    const items = state.items;
    const hasCheckpointTurns = items.some((it) => it.kind === "user" && it.checkpointTurn != null);
    let boundaryIdx = -1;
    let userCount = 0;
    let targetUserCount = -1;
    for (let i = 0; i < items.length; i++) {
      if (items[i].kind === "user") {
        const item = items[i] as Extract<Item, { kind: "user" }>;
        const matches = hasCheckpointTurns ? item.checkpointTurn === turn : userCount === turn;
        if (matches) {
          boundaryIdx = i;
          targetUserCount = userCount;
          break;
        }
        userCount++;
      }
    }
    if (boundaryIdx < 0) {
      rewindForTab(sourceTabId, turn, scope).then((ok) => {
        if (!ok) return;
        if (scope === "both") {
          setDockRefreshKey((v) => v + 1);
          setProjectRevision((v) => v + 1);
        }
      });
      return;
    }

    const prevUserCount = items.filter((it) => it.kind === "user").length;
    const turnDiff = prevUserCount - targetUserCount;

    // Save full items for undo.
    const userItem = items[boundaryIdx]?.kind === "user" ? items[boundaryIdx] as Extract<Item, { kind: "user" }> : undefined;
    const prompt = userItem?.text ?? "";
    setRewindStateForTab(sourceTabId, {
      turn,
      scope,
      fullItems: items,
      boundaryIdx,
      turnDiff,
      prompt,
    });

    // Fill composer with the rewound-to user message.
    const insertId = Date.now();
    setComposerInsertRequestsByTab((current) => ({
      ...current,
      [sourceTabId]: { id: insertId, text: prompt, mode: "replace" },
    }));

    setRewindSignal((v) => v + 1);
  }, [activeTab?.readOnly, activeTabId, hydratePlaceholderActive, state.items, rewindForTab, refreshTabMetas, setRewindStateForTab]);

  const handleEditPrompt = useCallback(async (turn: number, displayText: string, submitText?: string): Promise<boolean> => {
    const sourceTabId = activeTabId;
    if (!sourceTabId || activeTab?.readOnly || !controllerReady || hydratePlaceholderActive || rewindStatesByTabRef.current[sourceTabId] || state.running || state.messageAction != null || state.approval != null || state.ask != null || clearContextPending) return false;
    const next = displayText.trim();
    if (!next) return false;
    const submit = (submitText ?? displayText).trim();
    const hasCheckpointTurns = state.items.some((it) => it.kind === "user" && it.checkpointTurn != null);
    let original = "";
    let userCount = 0;
    for (const item of state.items) {
      if (item.kind !== "user") continue;
      const matches = hasCheckpointTurns ? item.checkpointTurn === turn : userCount === turn;
      if (matches) {
        original = (item.submitText ?? item.text).trim();
        break;
      }
      userCount++;
    }
    const ok = await rewindForTab(sourceTabId, turn, "conversation");
    if (!ok) return false;
    setRewindSignal((v) => v + 1);
    try {
      await sendToTab(sourceTabId, next, submit, original);
      return true;
    } catch {
      return false;
    }
  }, [activeTab?.readOnly, activeTabId, clearContextPending, controllerReady, hydratePlaceholderActive, sendToTab, state.approval, state.ask, state.items, state.messageAction, state.running, rewindForTab]);

  const openTrash = useCallback(async () => {
    closeTransientOverlays();
    setHistView({ kind: "trash", sessions: await listTrashedSessions() });
  }, [closeTransientOverlays, listTrashedSessions]);
  const closeHistory = useCallback(() => {
    closeTransientOverlays();
    setHistView(null);
  }, [closeTransientOverlays]);
  const refreshHistoryView = useCallback(async () => {
    const sessions = await listSessions().catch(() => null);
    if (!sessions) return;
    setHistView((cur) =>
      cur === null || cur.kind !== "history"
        ? cur
        : cur.source === "scope"
          ? { ...cur, sessions: sessionsForScope(sessions, cur.filter) }
          : { ...cur, sessions },
    );
  }, [listSessions]);

  const navigationSeqRef = useRef(0);
  const navigationRunningRef = useRef(false);
  const navigationPendingRef = useRef<PendingDesktopNavigationRequest | null>(null);
  const runNavigationRequest = useCallback(async (request: PendingDesktopNavigationRequest) => {
    const latest = () => request.seq === navigationSeqRef.current;
    const refreshLatestTabMetas = async (): Promise<TabMeta[]> => {
      const tabs = asArray(await app.ListTabs().catch(() => [] as TabMeta[]));
      if (latest()) setTabMetas(tabs);
      return tabs;
    };
    const openTopicTarget = async (scope: string, workspaceRoot: string, topicId: string, sessionPath?: string): Promise<TabMeta> => {
      if (singleSurfaceLayout) return activateTopic(scope, workspaceRoot, topicId, sessionPath || "");
      if (sessionPath) return openTopicSession(scope, workspaceRoot, topicId, sessionPath);
      if (scope === "global") return openGlobalTab(topicId);
      return openProjectTab(workspaceRoot, topicId);
    };
    const openBlankTarget = async (scope: string, workspaceRoot: string): Promise<TabMeta> => {
      const root = scope === "project" ? workspaceRoot : "";
      return singleSurfaceLayout ? ensureBlankSurface(scope, root) : ensureBlankTab(scope, root);
    };

    try {
      if (request.kind === "topic") {
        const openedTab = await openTopicTarget(request.scope, request.workspaceRoot, request.topicId, request.sessionPath);
        if (!latest()) return;
        seedActiveTabMeta(openedTab);
        void refreshLatestTabMetas();
        setTabRevealSignal((signal) => signal + 1);
        setTranscriptRevealSignal((signal) => signal + 1);
        return;
      }

      if (request.kind === "blank") {
        const openedTab = await openBlankTarget(request.scope, request.workspaceRoot);
        if (!latest()) return;
        seedActiveTabMeta(openedTab);
        setProjectRevision((value) => value + 1);
        await refreshLatestTabMetas();
        if (!latest()) return;
        setTabRevealSignal((signal) => signal + 1);
        setTranscriptRevealSignal((signal) => signal + 1);
        return;
      }

      if (request.kind === "delivery-worktree") {
        const result = await createDeliveryWorktree(request.workspaceRoot);
        if (!latest()) return;
        seedActiveTabMeta(result.tab);
        setProjectRevision((value) => value + 1);
        await refreshLatestTabMetas();
        if (!latest()) return;
        showToast(
          result.sourceDirty
            ? t("projectTree.worktreeCreatedDirty", { branch: result.branch })
            : t("projectTree.worktreeCreated", { branch: result.branch }),
          result.sourceDirty ? "warn" : "info",
          { durationMs: result.sourceDirty ? 7000 : 3500 },
        );
        setTabRevealSignal((signal) => signal + 1);
        setTranscriptRevealSignal((signal) => signal + 1);
        return;
      }

      if (request.kind === "sidebar-im") {
        const { connection } = request;
        const target = sidebarImSessionTarget(connection);
        if (!target) {
          if (latest()) showToast(t("sidebar.imWaiting", { name: connection.title }));
          return;
        }
        let openedTab: TabMeta | undefined;
        if (connection.sessionSource === "auto" && target.kind === "path") {
          openedTab = await openBlankTarget(connection.scope, connection.workspaceRoot);
          if (!latest()) return;
          await openChannelSession(target.value, openedTab.id);
        } else if (target.kind === "path") {
          openedTab = await openBlankTarget(connection.scope, connection.workspaceRoot);
          if (!latest()) return;
          await resumeSession(target.value, openedTab.id);
        } else {
          openedTab = await openTopicTarget(connection.scope, connection.workspaceRoot, target.value);
        }
        if (!latest()) return;
        if (openedTab) seedActiveTabMeta(openedTab);
        await refreshLatestTabMetas();
        if (!latest()) return;
        setTabRevealSignal((value) => value + 1);
        setTranscriptRevealSignal((value) => value + 1);
        setProjectRevision((value) => value + 1);
        return;
      }

      const { session } = request;
      const scope = session.scope || (session.workspaceRoot ? "project" : "global");
      let targetTab: TabMeta;
      if (isChannelSession(session)) {
        targetTab = await openBlankTarget(scope === "project" ? "project" : "global", scope === "project" ? session.workspaceRoot || "" : "");
        if (!latest()) return;
        await openChannelSession(session.path, targetTab.id);
      } else if (scope === "project" && session.workspaceRoot && session.topicId) {
        targetTab = await openTopicTarget("project", session.workspaceRoot, session.topicId, session.path);
      } else if (scope === "global" && session.topicId) {
        targetTab = await openTopicTarget("global", "", session.topicId, session.path);
      } else {
        throw new Error(scope === "global" && !session.topicId
          ? t("history.failedOpenSession")
          : (session.topicId ? t("history.missingWorkspaceRoot") : t("history.failedOpenSession")));
      }
      if (!latest()) return;
      seedActiveTabMeta(targetTab);
      setHistView(null);
      void refreshLatestTabMetas();
      setTabRevealSignal((value) => value + 1);
      setTranscriptRevealSignal((value) => value + 1);
    } catch (err: any) {
      if (!latest()) return;
      if (request.kind === "topic" || request.kind === "blank") {
        console.warn("desktop navigation failed", err);
        showToast(t("history.failedOpenSession"), "error");
        void refreshLatestTabMetas();
        return;
      }
      if (request.kind === "delivery-worktree") {
        console.warn("isolated Delivery workspace creation failed", err);
        showToast(err instanceof Error ? err.message : String(err), "error", { durationMs: 6000 });
        return;
      }
      if (request.kind === "sidebar-im") {
        console.warn("bot sidebar open failed", err);
        showToast(t("sidebar.imOpenFailed", { name: request.connection.title }));
        return;
      }
      await refreshHistoryView();
      if (!latest() || isMissingSessionError(err)) return;
      setHistView(null);
      const session = request.session;
      const scope = session.scope || (session.workspaceRoot ? "project" : "global");
      if (scope === "project" && session.workspaceRoot) {
        const name = workspaceDisplayName(session.workspaceRoot);
        showToast(t("history.failedOpenProject", { name, path: session.workspaceRoot }));
      } else {
        showToast(err?.message || String(err));
      }
    }
  }, [activateTopic, createDeliveryWorktree, ensureBlankSurface, ensureBlankTab, openChannelSession, openGlobalTab, openProjectTab, openTopicSession, refreshHistoryView, resumeSession, seedActiveTabMeta, showToast, singleSurfaceLayout, t]);

  const enqueueNavigation = useCallback((input: DesktopNavigationInput): Promise<void> => {
    // Invalidate any in-flight activation's stale apply at ENQUEUE time. The
    // queue serializes requests, so a click made while another request runs
    // only advances the controller's navigation epoch when it eventually
    // starts — too late: the running request's ActivateTopic would resolve,
    // pass the controller-local guard, flip the visible tab, and prune the
    // newer surface's cached state (#6613 review).
    noteNavigationIntent();
    return enqueueNavigationRequest(
      { seqRef: navigationSeqRef, runningRef: navigationRunningRef, pendingRef: navigationPendingRef },
      input,
      runNavigationRequest,
    );
  }, [noteNavigationIntent, runNavigationRequest]);

  const openBlankSession = useCallback((scope: string, workspaceRoot: string): Promise<void> =>
    enqueueNavigation({ kind: "blank", scope, workspaceRoot: scope === "project" ? workspaceRoot : "" }),
  [enqueueNavigation]);

  useEffect(() => onSessionRecovered(() => {
    setProjectRevision((value) => value + 1);
    void refreshTabMetas();
  }), [refreshTabMetas]);

  const handleNewTab = useCallback(async () => {
    closeTransientOverlays();
    setSidebarImDetailConnectionId("");
    const target = blankSessionTarget();
    await openBlankSession(target.scope, target.workspaceRoot);
  }, [blankSessionTarget, closeTransientOverlays, openBlankSession]);

  const handleOpenTopic = useCallback((scope: string, workspaceRoot: string, topicId: string, sessionPath?: string): Promise<void> => {
    closeTransientOverlays();
    setSidebarImDetailConnectionId("");
    return enqueueNavigation({ kind: "topic", scope, workspaceRoot, topicId, sessionPath });
  }, [closeTransientOverlays, enqueueNavigation]);

  const openSidebarImConnectionSession = useCallback((connection: SidebarImConnection): Promise<void> => {
    setSidebarImDetailConnectionId("");
    return enqueueNavigation({ kind: "sidebar-im", connection });
  }, [enqueueNavigation]);

  const onResumeSession = useCallback((session: SessionMeta): Promise<void> => {
    if (state.running && !singleSurfaceLayout) return Promise.resolve();
    return enqueueNavigation({ kind: "resume-session", session });
  }, [enqueueNavigation, singleSurfaceLayout, state.running]);

  // Command palette: ⌘K / Ctrl+K opens a fuzzy navigator over commands and
  // recent sessions. Sessions are snapshotted on open so the list is stable
  // while the palette is up.
  const openPalette = useCallback(async () => {
    closeTransientOverlays();
    setPaletteOpen(true);
    setPaletteSessions(await listSessions().catch(() => []));
  }, [closeTransientOverlays, listSessions]);
  useGlobalShortcut("commandPalette.open", () => {
    setPaletteOpen((current) => {
      if (!current) void openPalette();
      return !current; // ← fix: toggle the state so the palette actually opens/closes
    });
  }, [openPalette]);
  useGlobalShortcut("app.newSession", () => void handleNewTab(), [handleNewTab]);
  useGlobalShortcut("settings.open", () => {
    closeTransientOverlays();
    setSettingsTarget("general");
  }, [closeTransientOverlays]);
  useGlobalShortcut("tab.close", () => {
    if (activeTabId) void handleTabClose(activeTabId);
  }, [activeTabId, handleTabClose], Boolean(activeTabId));
  useGlobalShortcut("shortcuts.show", () => setShortcutsOpen(true));
  useGlobalShortcut("sidebar.toggle", toggleSidebar, [toggleSidebar]);

  // --- Topic shortcut navigation (Cmd/Ctrl+1-9) ---
  const visibleTopicsRef = useRef<TopicShortcutEntry[]>([]);
  const handleVisibleTopicsChange = useCallback((topics: TopicShortcutEntry[]) => {
    visibleTopicsRef.current = topics;
  }, []);
  const handleNavigateTopic = useCallback((entry: TopicShortcutEntry) => {
    void handleOpenTopic(entry.scope, entry.workspaceRoot, entry.topicId, entry.sessionPath);
  }, [handleOpenTopic]);
  const { showBadges: showTopicBadges } = useTopicShortcuts(!sidebarCollapsed, desktopPlatform);

  // Register Cmd/Ctrl+1-9 shortcuts for topic navigation
  useEffect(() => {
    if (sidebarCollapsed) return;
    const onKeydown = (event: globalThis.KeyboardEvent) => {
      const idx = topicShortcutIndexFromEvent(event, desktopPlatform);
      if (idx === null) return;
      event.preventDefault();
      const topics = visibleTopicsRef.current;
      if (idx < topics.length) {
        handleNavigateTopic(topics[idx]);
      }
    };
    document.addEventListener("keydown", onKeydown);
    return () => document.removeEventListener("keydown", onKeydown);
  }, [sidebarCollapsed, desktopPlatform, handleNavigateTopic]);

  const paletteItems = useMemo<PaletteItem[]>(() => {
    const cmds: PaletteItem[] = [
      { id: "cmd-new", group: t("palette.group.commands"), title: t("palette.cmd.newSession"), icon: <SquarePen size={15} />, compact: true, keywords: ["new", "新建"], run: () => void handleNewTab() },
      { id: "cmd-trash", group: t("palette.group.commands"), title: t("palette.cmd.trash"), icon: <Trash2 size={15} />, compact: true, keywords: ["trash", "回收站"], run: () => void openTrash() },
      { id: "cmd-settings", group: t("palette.group.commands"), title: t("palette.cmd.settings"), icon: <SettingsIcon size={15} />, compact: true, keywords: ["settings", "设置"], run: () => setSettingsTarget("general") },
      { id: "cmd-appearance", group: t("palette.group.commands"), title: t("palette.cmd.appearance"), icon: <Palette size={15} />, compact: true, keywords: ["theme", "appearance", "外观", "主题"], run: () => setSettingsTarget("appearance") },
      {
        id: "cmd-theme-reset",
        group: t("palette.group.commands"),
        title: t("settings.themeLibrary.reset"),
        icon: <Palette size={15} />,
        compact: true,
        keywords: ["theme", "reset", "default", "恢复默认", "主题"],
        run: () => {
          void app.ResetThemePack()
            .then(() => {
              clearThemePack();
              notice(t("settings.themeReset"));
            })
            .catch((err) => showToast(err instanceof Error ? err.message : String(err), "error"));
        },
      },
      { id: "cmd-memory", group: t("palette.group.commands"), title: t("palette.cmd.memory"), icon: <Brain size={15} />, compact: true, keywords: ["memory", "记忆"], run: () => setSettingsTarget("memory") },
      { id: "cmd-models", group: t("palette.group.commands"), title: t("palette.cmd.models"), icon: <Cpu size={15} />, compact: true, keywords: ["model", "模型"], run: () => setSettingsTarget("models") },
    ];
    const startOfDay = (d: Date) => new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
    const dayLabel = (ms: number) => {
      const days = Math.round((startOfDay(new Date()) - startOfDay(new Date(ms))) / 86_400_000);
      if (days <= 0) return t("history.today");
      if (days === 1) return t("history.yesterday");
      return new Date(ms).toLocaleDateString();
    };
    const sessionItems: PaletteItem[] = paletteSessions.slice(0, 12).map((s) => ({
      id: `sess-${s.path}`,
      group: t("palette.group.sessions"),
      title: paletteSessionDisplayTitle(s, t("history.emptySession")),
      hint: paletteSessionHint(s),
      keywords: paletteSessionKeywords(s),
      meta: dayLabel(sessionActivityTime(s)),
      badge: t(s.turns === 1 ? "history.turnOne" : "history.turnOther", { n: s.turns }),
      run: () => void onResumeSession(s),
    }));
    const remoteItems: PaletteItem[] = remoteHosts.map((host) => {
      const status = remoteStatuses[host.id];
      const connected = status?.state === "connected" || status?.state === "degraded";
      const target = `${host.user ? `${host.user}@` : ""}${host.host}${host.port && host.port !== 22 ? `:${host.port}` : ""}`;
      return {
        id: `remote-${host.id}`,
        group: t("palette.group.remote"),
        title: connected
          ? t("palette.remote.open", { host: host.label })
          : t("palette.remote.connect", { host: host.label }),
        hint: host.defaultWorkspace || target,
        icon: <Server size={15} />,
        keywords: ["ssh", "remote", "远程", "连接", host.label, host.host],
        run: () => {
          if (connected) openRemoteWorkspaceFromStatus(host);
          else connectAndOpenRemoteWorkspace(host);
        },
      };
    });
    return [...cmds, ...remoteItems, ...sessionItems];
  }, [t, paletteSessions, remoteHosts, remoteStatuses, handleNewTab, openTrash, onResumeSession, openRemoteWorkspaceFromStatus, connectAndOpenRemoteWorkspace]);
  // Delete / rename act on disk, then re-fetch so the panel reflects the change.
  const onDeleteSession = useCallback(
    async (path: string) => {
      if (state.running) return;
      try {
        await deleteSession(path);
      } catch {
        await refreshHistoryView();
        return;
      }
      // Local state removal: filter the deleted session out of the current
      // history view instead of re-fetching the full list from the backend.
      setHistView((cur) =>
        cur === null || cur.kind !== "history"
          ? cur
          : { ...cur, sessions: cur.sessions.filter((s) => s.path !== path) },
      );
    },
    [state.running, deleteSession, refreshHistoryView],
  );
  const onDeleteManySessions = useCallback(
    async (paths: string[]) => {
      if (state.running) return;
      const uniquePaths = Array.from(new Set(paths));
      for (const path of uniquePaths) {
        // Best effort per path: one locked/missing file must not abandon the
        // rest of the sweep. The guarded backend method revalidates actual
        // branch and parent content before moving anything.
        await app.DeleteRecoveryCopy(path).catch(() => undefined);
      }
      await refreshHistoryView();
    },
    [state.running, refreshHistoryView],
  );
  const onRenameSession = useCallback(
    async (path: string, title: string) => {
      if (state.running) return;
      await renameSession(path, title);
      const sessions = await listSessions();
      setHistView((cur) =>
        cur === null
          ? null
          : cur.kind === "history"
            ? { ...cur, sessions: cur.source === "scope" ? sessionsForScope(sessions, cur.filter) : sessions }
            : cur,
      );
    },
    [state.running, renameSession, listSessions],
  );
  const onRestoreTrashedSession = useCallback(
    async (path: string) => {
      await restoreSession(path);
      const trashed = await listTrashedSessions();
      setHistView((cur) => (cur === null ? null : { kind: "trash", sessions: trashed }));
    },
    [restoreSession, listTrashedSessions],
  );
  const onPurgeTrashedSession = useCallback(
    async (path: string) => {
      await purgeTrashedSession(path);
      const trashed = await listTrashedSessions();
      setHistView((cur) => (cur === null ? null : { kind: "trash", sessions: trashed }));
    },
    [purgeTrashedSession, listTrashedSessions],
  );
  const onPurgeAllTrashedSessions = useCallback(
    async (paths: string[]) => {
      const uniquePaths = Array.from(new Set(paths));
      for (const path of uniquePaths) {
        await purgeTrashedSession(path);
      }
      const trashed = await listTrashedSessions();
      setHistView((cur) => (cur === null ? null : { kind: "trash", sessions: trashed }));
    },
    [purgeTrashedSession, listTrashedSessions],
  );
  const onPurgeRecoveryCopies = useCallback(
    async (paths: string[]) => {
      const uniquePaths = Array.from(new Set(paths));
      for (const path of uniquePaths) {
        // Permanent copy cleanup must not trust the list result: the backend
        // rechecks the trashed transcript and its live parent for every path.
        await app.PurgeRecoveryCopy(path).catch(() => undefined);
      }
      const trashed = await listTrashedSessions();
      setHistView((cur) => (cur === null ? null : { kind: "trash", sessions: trashed }));
    },
    [listTrashedSessions],
  );

  // Workspace: open the folder chooser and switch projects. The hook resets the
  // transcript and refreshes meta on a pick. A cancel is a no-op.
  const switchFolder = useCallback(async (path?: string) => {
    const picked = path === undefined ? await pickWorkspace() : await switchWorkspace(path);
    if (picked) {
      setProjectRevision((value) => value + 1);
      await refreshTabMetas();
    }
    return picked;
  }, [pickWorkspace, switchWorkspace, refreshTabMetas]);

  const refreshProjectsAndTabs = useCallback(async () => {
    setProjectRevision((value) => value + 1);
    const tabs = await refreshTabMetas();
    if (activeTabId && !tabs.some((tab) => tab.id === activeTabId)) {
      await syncActiveTab(false);
    }
  }, [activeTabId, refreshTabMetas, syncActiveTab]);

  const renameTopic = useCallback(async (topicId: string, title: string) => {
    const nextTitle = title.trim();
    if (!topicId || !nextTitle) return;
    try {
      await app.RenameTopic(topicId, nextTitle);
      await refreshProjectsAndTabs();
    } catch (err) {
      showToast(err instanceof Error ? err.message : String(err), "error");
    }
  }, [refreshProjectsAndTabs, showToast]);

  const startActiveTopicRename = useCallback(() => {
    if (!activeTab?.topicId) return;
    topicRenameSkipCommitRef.current = false;
    topicRenameCommitHandledRef.current = false;
    setRenamingTopicId(activeTab.topicId);
    setTopicTitleDraft(activeTab.topicTitle || "");
  }, [activeTab?.topicId, activeTab?.topicTitle]);

  const cancelActiveTopicRename = useCallback(() => {
    topicRenameSkipCommitRef.current = true;
    topicRenameCommitHandledRef.current = true;
    setRenamingTopicId(null);
    setTopicTitleDraft("");
  }, []);

  const commitActiveTopicRename = useCallback(async () => {
    if (topicRenameSkipCommitRef.current) {
      topicRenameSkipCommitRef.current = false;
      topicRenameCommitHandledRef.current = false;
      setRenamingTopicId(null);
      return;
    }
    if (topicRenameCommitHandledRef.current) return;
    topicRenameCommitHandledRef.current = true;
    const topicId = renamingTopicId;
    setRenamingTopicId(null);
    if (!topicId) return;
    const nextTitle = topicTitleDraft.trim();
    if (!nextTitle) return;
    try {
      await renameTopic(topicId, nextTitle);
    } catch {
      /* keep the app usable if a stale topic cannot be renamed */
    }
  }, [renameTopic, renamingTopicId, topicTitleDraft]);

  const sidebarExpandBlocked = false;
  const sidebarToggleTitle = sidebarCollapsed
      ? t("sidebar.expand")
      : t("sidebar.collapse");
  const sidebarNavTooltipDisabled = !sidebarCollapsed;
  const browserPreviewChrome = typeof window !== "undefined" && !window.runtime;
  const browserMockScenario = browserPreviewChrome ? browserMockScenarioParam() : "";
  const guidanceQueueMockItems = isGuidanceMockScenario(browserMockScenario) ? GUIDANCE_QUEUE_MOCK_ITEMS : undefined;
  const workspacePanelResetWidth = rightDockDetailActive
    ? RIGHT_DOCK_PREVIEW_DEFAULT_WIDTH
    : desktopLayoutStyle === "creation"
      ? defaultCreationRightDockTreeWidth()
      : defaultRightDockTreeWidth();
  const workspacePanelResizeMinWidth = workspacePanelAriaMinWidth(workspacePanelMinWidth, workspacePanelRenderWidth);
  const workspacePanelMaxWidth = rightDockDetailActive ? RIGHT_DOCK_MAX_WIDTH : RIGHT_DOCK_TREE_MAX_WIDTH;
  const sidebarCreation = desktopLayoutStyle === "creation";
  const topicbarTitle = sidebarImDetailConnection ? t("botDetail.title", { name: sidebarImDetailConnection.title }) : topicDisplayTitle(activeTab);
  const topicbarWorkspaceLabel = sidebarImDetailConnection ? t("botDetail.subtitle") : activeTab ? tabWorkspaceTitle(activeTab) : "";
  const topicbarWorkspacePath = activeTab?.scope === "project" ? activeTab.workspaceRoot || state.meta?.cwd : "";
  const topicbarImSource = activeTab?.scope === "global" && activeTab.topicId ? imTopicSources[activeTab.topicId] : undefined;
  const topicbarImSourceLabel = sidebarImDetailConnection
    ? sidebarImDetailConnection.platformLabel
    : topicbarImSource ? t("msg.fromIm", { source: topicbarImSource.label }) : "";
  const topicbarImSourcePlatform = sidebarImDetailConnection?.platform ?? topicbarImSource?.platform;
  const topicbarSubtitleVisible = !sidebarCreation && Boolean(topicbarWorkspaceLabel || topicbarImSourceLabel);
  const topicbarSubtitleTitle = sidebarImDetailConnection
    ? [topicbarWorkspaceLabel, topicbarImSourceLabel, sidebarImScopeLabel(sidebarImDetailConnection, t)].filter(Boolean).join(" · ")
    : [topicbarWorkspacePath || topicbarWorkspaceLabel, topicbarImSourceLabel].filter(Boolean).join(" · ");
  const topicbarCanRename = !sidebarImDetailConnection && Boolean(activeTab?.topicId);
  const topicbarTitleEditSize = Math.min(56, Math.max(4, topicTitleDraft.length || topicbarTitle.length || 1));
  const sidebarWorkbench = desktopLayoutStyle === "workbench";
  const handleWindowsTitlebarDoubleClick = useCallback((event: ReactMouseEvent<HTMLDivElement>) => {
    if (!windowsFramelessChrome) return;
    const target = event.target as HTMLElement | null;
    if (!target?.closest(".app-chrome, .topicbar, .workbench-dock__tools")) return;
    if (target.closest("button, input, textarea, select, a, [role='button'], [role='tab'], .windows-window-controls")) return;
    event.preventDefault();
    void app.ToggleMaximiseMainWindow()
      .then(() => window.setTimeout(syncMainWindowMaximised, 80))
      .catch(() => undefined);
  }, [syncMainWindowMaximised, windowsFramelessChrome]);
  // Creation keeps the classic sidebar/chat structure while gating chrome tweaks
  // behind its own style flag so classic/workbench remain unchanged.
  const appChromeHidden = sidebarWorkbench || sidebarCreation;
  const workbenchChromeHidden = sidebarWorkbench;
  const sidebarClassName = [
    "sidebar",
    sidebarCollapsed ? "sidebar--collapsed" : "",
    sidebarWorkbench ? "sidebar--workbench" : "",
  ].filter(Boolean).join(" ");

  return (
    <ShellExpandProvider>
    <ShellHotkeys />
    <TextSizeHotkeys />
      <div
        ref={appRef}
        onDoubleClickCapture={handleWindowsTitlebarDoubleClick}
        className={[
        "app",
        `app--${desktopPlatform}`,
        windowsFramelessChrome ? "app--windows-frameless" : "",
        browserPreviewChrome ? "app--browser-preview" : "",
        sidebarWorkbench ? "app--workbench" : "",
        sidebarCreation ? "app--creation" : "",
        !sidebarWorkbench && !sidebarCreation ? "app--classic" : "",
      ].filter(Boolean).join(" ")}
    >
      <ThemeBackground />
      <div
        ref={layoutRef}
        className={[
          "layout",
          sidebarWorkbench ? "layout--workbench" : "",
          workbenchChromeHidden ? "layout--workbench-chrome-hidden" : "",
          sidebarCreation ? "layout--creation-chrome-hidden" : "",
          sidebarCollapsed ? "layout--sidebar-collapsed" : "",
          sidebarResizing ? "layout--resizing layout--sidebar-resizing" : "",
          workspacePanelGridOpen ? "layout--workspace-open" : "",
          workspacePanelOpen && workspacePanelMaximized ? "layout--workspace-maximized" : "",
          workspacePanelResizing ? "layout--resizing layout--workspace-resizing" : "",
        ]
          .filter(Boolean)
          .join(" ")}
        style={layoutStyle}
      >
        {!appChromeHidden && (
          <AppChrome
            platform={desktopPlatform}
            browserPreviewChrome={browserPreviewChrome}
            workbenchChrome={sidebarWorkbench}
            tabs={visibleTabs}
            activeTabId={visibleTabId}
            revealActiveSignal={tabRevealSignal}
            commandCompact={true}
            sidebarTogglePressed={sidebarTogglePressed}
            sidebarExpandBlocked={sidebarExpandBlocked}
            sidebarCollapsed={sidebarCollapsed}
            sidebarToggleTitle={sidebarToggleTitle}
            workspacePanelMaximized={workspacePanelMaximized}
            workspacePanelRenderable={workspacePanelRenderable}
            workspaceTogglePressed={workspaceTogglePressed}
            workspacePanelLabel={workspacePanelRenderable ? t("rightDock.collapse") : t("rightDock.expand")}
            onToggleSidebar={toggleSidebar}
            onToggleWorkspacePanel={toggleWorkspacePanel}
            onTabChange={(id) => void handleTabChange(id)}
            onTabClose={(id) => void handleTabClose(id)}
            onTabsClose={(ids, nextActiveTabId) => void handleTabsClose(ids, nextActiveTabId)}
            onTabsReorder={(ids) => void handleTabsReorder(ids)}
            onNewTab={() => void handleNewTab()}
            onOpenPalette={() => void openPalette()}
          />
        )}
        <a className="skip-to-composer" href="#composer-input">
          {t("shortcuts.skipToComposer")}
        </a>

        <aside className={sidebarClassName} aria-label={t("sidebar.navigation")}>
          {sidebarWorkbench ? (
            <>
              <div className="sidebar__head" aria-hidden={sidebarCollapsed}>
                <div className="sidebar__brand sidebar__brand--workbench">
                  <img src={logoWordmark} alt="Reasonix" className="sidebar__brand-logo sidebar__brand-logo--workbench" draggable={false} />
                </div>
              </div>

              <div className="sidebar__quick-actions">
                <button
                  className="sidebar__quick-action"
                  type="button"
                  onClick={() => {
                    void handleNewTab();
                  }}
                >
                  <MessageSquare size={18} aria-hidden="true" />
                  <span>{t("topbar.newSession")}</span>
                </button>
              </div>
            </>
          ) : (
            <>
              <div className="sidebar__brand" aria-hidden={sidebarCollapsed}>
                <img src={logoWordmark} alt="Reasonix" className="sidebar__brand-logo" draggable={false} />
              </div>

              <button
                className="sidebar__new"
                onClick={() => {
                  void handleNewTab();
                }}
              >
                <SquarePen size={18} />
                <span>{sidebarCreation ? t("creation.sidebar.newChat") : t("topbar.newSession")}</span>
              </button>
            </>
          )}

          {sidebarCreation && (
            <section className="sidebar-feature-zone" aria-label={t("settings.title")}>
              <div className="sidebar-feature-zone__title">{t("creation.sidebar.features")}</div>
              <div className="sidebar-feature-zone__items">
                <button
                  className="sidebar-feature-zone__item"
                  type="button"
                  onClick={() => {
                    closeTransientOverlays();
                    setSettingsTarget("skills");
                  }}
                >
                  <Command size={14} aria-hidden="true" />
                  <span>{t("creation.sidebar.skills")}</span>
                </button>
                <button
                  className="sidebar-feature-zone__item"
                  type="button"
                  onClick={() => {
                    closeTransientOverlays();
                    setSettingsTarget("memory");
                  }}
                >
                  <Brain size={14} aria-hidden="true" />
                  <span>{t("settings.tab.memory")}</span>
                </button>
                <button
                  className="sidebar-feature-zone__item"
                  type="button"
                  onClick={() => {
                    closeTransientOverlays();
                    setSettingsTarget("bots");
                  }}
                >
                  <MessageSquare size={14} aria-hidden="true" />
                  <span>{t("creation.sidebar.messageChannels")}</span>
                </button>
                <button
                  className="sidebar-feature-zone__item"
                  type="button"
                  onClick={() => setHeartbeatOpen(true)}
                >
                  <AlarmClock size={14} aria-hidden="true" />
                  <span>{t("sidebar.automation")}</span>
                </button>
              </div>
            </section>
          )}

          <section className="sidebar__section sidebar__section--projects">
            <ProjectTree
              activeScope={activeTab?.scope}
              activeWorkspaceRoot={activeTab?.workspaceRoot}
              activeTopicId={activeTab?.topicId}
              activeSessionPath={activeTab?.sessionPath}
              imTopicSources={imTopicSources}
              onOpenTopic={handleOpenTopic}
              onCreateTopic={(scope, workspaceRoot) => openBlankSession(scope, scope === "project" ? workspaceRoot : "")}
              onCreateDeliveryWorktree={(workspaceRoot) => enqueueNavigation({ kind: "delivery-worktree", workspaceRoot })}
              onTopicsChanged={refreshProjectsAndTabs}
              onRenameTopic={renameTopic}
              refreshSignal={projectRevision}
              onAddProject={async () => {
                await switchFolder();
              }}
              timeFilter={topicTimeFilter}
              onTimeFilterChange={setTopicTimeFilter}
              variant={sidebarWorkbench ? "workbench" : sidebarCreation ? "creation" : "classic"}
              searchExpanded={!sidebarCreation || sidebarSearchOpen}
              searchFocusSignal={sidebarSearchFocusSignal}
              showShortcutBadges={showTopicBadges}
              shortcutPlatform={desktopPlatform}
              onVisibleTopicsChange={handleVisibleTopicsChange}
            />
          </section>

          {sidebarWorkbench ? (
            <nav className="sidebar__nav sidebar__nav--footer">
              <div className="sidebar__utility-row" aria-label={t("sidebar.utilityActions")}>
                <Tooltip label={t("sidebar.trash")} fill side="top">
                  <button
                    className="sidebar__utility-button"
                    type="button"
                    onClick={() => void openTrash()}
                  >
                    <Trash2 size={16} aria-hidden="true" />
                    <span className="sr-only">{t("sidebar.trash")}</span>
                  </button>
                </Tooltip>
                <Tooltip label={t("heartbeat.scheduler")} fill side="top">
                  <button
                    className="sidebar__utility-button"
                    type="button"
                    onClick={() => setHeartbeatOpen(true)}
                  >
                    <AlarmClock size={16} aria-hidden="true" />
                    <span className="sr-only">{t("sidebar.automation")}</span>
                  </button>
                </Tooltip>
                <Tooltip label={t("topbar.settings")} fill side="top">
                  <button
                    className="sidebar__utility-button"
                    type="button"
                    onClick={() => {
                      closeTransientOverlays();
                      setSettingsTarget("general");
                    }}
                  >
                    <SettingsIcon size={16} aria-hidden="true" />
                    <span className="sr-only">{t("topbar.settings")}</span>
                  </button>
                </Tooltip>
              </div>
            </nav>
          ) : (
            <nav className="sidebar__nav">
              {sidebarCreation && (
                <Tooltip label={t("projectTree.searchPlaceholder")} fill side="right" disabled={sidebarNavTooltipDisabled}>
                  <button
                    className={`sidebar__navitem sidebar__navitem--search${sidebarSearchOpen ? " sidebar__navitem--active" : ""}`}
                    type="button"
                    aria-label={t("projectTree.searchPlaceholder")}
                    aria-pressed={sidebarSearchOpen}
                    onClick={() => {
                      setSidebarSearchOpen((open) => !open);
                      setSidebarSearchFocusSignal((signal) => signal + 1);
                    }}
                  >
                    <Search size={15} />
                    <span>{t("tabBar.commandSearchCompact")}</span>
                  </button>
                </Tooltip>
              )}
              <Tooltip label={t("sidebar.trash")} fill side="right" disabled={sidebarNavTooltipDisabled}>
                <button
                  className="sidebar__navitem"
                  onClick={() => void openTrash()}
                >
                  <Trash2 size={15} />
                  <span>{t("sidebar.trash")}</span>
                </button>
              </Tooltip>
              {!sidebarCreation && (
                <Tooltip label={t("heartbeat.scheduler")} fill side="right" disabled={sidebarNavTooltipDisabled}>
                  <button
                    className="sidebar__navitem"
                    onClick={() => setHeartbeatOpen(true)}
                  >
                    <AlarmClock size={15} />
                    <span>{t("sidebar.automation")}</span>
                  </button>
                </Tooltip>
              )}
              <Tooltip label={t("topbar.settings")} fill side="right" disabled={sidebarNavTooltipDisabled}>
                <button
                  className="sidebar__navitem"
                  onClick={() => {
                    closeTransientOverlays();
                    setSettingsTarget("general");
                  }}
                >
                  <SettingsIcon size={15} />
                  <span>{t("topbar.settings")}</span>
                </button>
              </Tooltip>
            </nav>
          )}

        </aside>
        <button
          className="sidebar-resizer"
          type="button"
          role="separator"
          aria-orientation="vertical"
          aria-label={t("sidebar.resize")}
          aria-valuemin={sidebarResizeMinWidth}
          aria-valuemax={SIDEBAR_MAX_WIDTH}
          aria-valuenow={sidebarRenderWidth}
          onPointerDown={startSidebarResize}
          onKeyDown={resizeSidebarWithKeyboard}
          onDoubleClick={() => setExpandedSidebarWidth(desktopLayoutStyle === "creation" ? defaultCreationSidebarWidth() : defaultSidebarWidth())}
        />
        {sidebarCreation && (
          <button
            className={`sidebar-collapse-toggle${sidebarCollapsed ? " sidebar-collapse-toggle--collapsed" : ""}${sidebarTogglePressed ? " sidebar-collapse-toggle--pressed" : ""}`}
            type="button"
            onClick={toggleSidebar}
            aria-label={sidebarToggleTitle}
            aria-pressed={!sidebarCollapsed}
            title={sidebarToggleTitle}
          >
            {sidebarCollapsed ? <PanelRight size={14} /> : <PanelLeft size={14} />}
          </button>
        )}

        <section className={`chat-pane${sidebarCreation && !sessionHasContent ? " chat-pane--creation-empty" : ""}`}>
          <>
          <header className="topicbar">
            {workbenchChromeHidden && (
              <Tooltip label={sidebarToggleTitle}>
                <button
                  className={[
                    "topicbar__chrome-btn",
                    sidebarExpandBlocked ? "topicbar__chrome-btn--blocked" : "",
                    sidebarTogglePressed ? "topicbar__chrome-btn--pressed" : "",
                  ].filter(Boolean).join(" ")}
                  type="button"
                  onClick={sidebarExpandBlocked ? undefined : toggleSidebar}
                  aria-label={sidebarToggleTitle}
                  aria-pressed={!sidebarCollapsed}
                  aria-disabled={sidebarExpandBlocked}
                >
                  <PanelLeft size={15} />
                </button>
              </Tooltip>
            )}
            <div className="topicbar__identity">
              <div className="topicbar__title-row">
                {topicbarEditing ? (
                  <div className="topicbar__title-edit">
                    <input
                      autoFocus
                      className="topicbar__title-input"
                      aria-label={t("topicBar.renameSession")}
                      size={sidebarCreation ? topicbarTitleEditSize : undefined}
                      value={topicTitleDraft}
                      onChange={(event) => setTopicTitleDraft(event.target.value)}
                      onKeyDown={(event: KeyboardEvent<HTMLInputElement>) => {
                        if (event.key === "Enter") {
                          event.preventDefault();
                          void commitActiveTopicRename();
                        }
                        if (event.key === "Escape") {
                          event.preventDefault();
                          cancelActiveTopicRename();
                        }
                      }}
                      onBlur={() => void commitActiveTopicRename()}
                    />
                  </div>
                ) : sidebarCreation && topicbarCanRename ? (
                  <h1 title={topicTitle(activeTab)}>
                    <button
                      className="topicbar__title-button"
                      type="button"
                      onClick={startActiveTopicRename}
                      aria-label={t("topicBar.renameSession")}
                    >
                      {topicbarTitle}
                    </button>
                  </h1>
                ) : (
                  <h1 title={sidebarImDetailConnection ? topicbarTitle : topicTitle(activeTab)}>{topicbarTitle}</h1>
                )}
                {!sidebarCreation && (
                  <Tooltip label={t("topicBar.renameSession")}>
                    <button
                      className="topicbar__icon-btn"
                      type="button"
                      disabled={!topicbarCanRename || topicbarEditing}
                      onClick={startActiveTopicRename}
                      aria-label={t("topicBar.renameSession")}
                    >
                      <Pencil size={14} />
                    </button>
                  </Tooltip>
                )}
              </div>
              {topicbarSubtitleVisible && (
                <div className="topicbar__subtitle" title={topicbarSubtitleTitle}>
                  {topicbarWorkspaceLabel && <span>{topicbarWorkspaceLabel}</span>}
                  {activeTab?.isolatedWorktree && <WorktreeBadge size={11} />}
                  {topicbarImSourcePlatform && (
                    <span className={`topicbar__source-chip topicbar__source-chip--${topicbarImSourcePlatform}`}>
                      {topicbarImSourceLabel}
                    </span>
                  )}
                </div>
              )}
            </div>
            <div className="topicbar__spacer" />
            <div className="topicbar__actions">
              {workbenchChromeHidden && (
                <Tooltip label={workspacePanelRenderable ? t("rightDock.collapse") : t("rightDock.expand")}>
                  <button
                    className={[
                      "topicbar__chrome-btn",
                      "topicbar__chrome-btn--workspace",
                      workspacePanelRenderable ? "topicbar__chrome-btn--active" : "",
                      workspaceTogglePressed ? "topicbar__chrome-btn--pressed" : "",
                    ].filter(Boolean).join(" ")}
                    type="button"
                    onClick={toggleWorkspacePanel}
                    aria-label={workspacePanelRenderable ? t("rightDock.collapse") : t("rightDock.expand")}
                    aria-pressed={workspacePanelRenderable}
                  >
                    <PanelRight size={15} />
                  </button>
                </Tooltip>
              )}
              {!sidebarImDetailConnection && (
              <>
              <Tooltip label={t("topicBar.copyAll")}>
                <CopyButton
                  getText={getSessionMarkdown}
                  label={t("topicBar.copyAll")}
                  className="topicbar__action-btn topicbar__action-btn--icon topicbar__action-btn--utility"
                  showInlineLabel={false}
                />
              </Tooltip>
              <div className={`topicbar__export${topicExportOpen ? " topicbar__export--open" : ""}`}>
                <Tooltip label={t("topicBar.export")}>
                  <button
                    className="topicbar__action-btn topicbar__action-btn--icon topicbar__action-btn--utility"
                    type="button"
                    disabled={!sessionHasContent}
                    aria-label={t("topicBar.export")}
                    aria-haspopup="menu"
                    aria-expanded={topicExportOpen}
                    onClick={() => setTopicExportOpen((open) => !open)}
                  >
                    <Download size={14} />
                  </button>
                </Tooltip>
                {topicExportOpen && (
                  <div className="topicbar__export-menu" role="menu">
                    <button type="button" role="menuitem" onClick={() => void exportSession("markdown")}>
                      <FileText size={13} />
                      <span>{t("topicBar.exportMarkdown")}</span>
                    </button>
                    <button type="button" role="menuitem" onClick={() => void exportSession("json")}>
                      <FileJson size={13} />
                      <span>{t("topicBar.exportJson")}</span>
                    </button>
                    <button type="button" role="menuitem" onClick={() => void exportSession("pdf")}>
                      <FileDown size={13} />
                      <span>{t("topicBar.exportPdf")}</span>
                    </button>
                    <button type="button" role="menuitem" onClick={() => void exportSession("image")}>
                      <FileImage size={13} />
                      <span>{t("topicBar.exportImage")}</span>
                    </button>
                  </div>
                )}
              </div>
              </>
              )}
              {!sidebarCreation && (
                <Tooltip label={t("workspace.changedTab")}>
                  <button
                    className="topicbar__action-btn topicbar__action-btn--label"
                    type="button"
                    aria-label={t("workspace.changedTab")}
                    aria-pressed={workspacePanelRenderable && rightDockMode === "changed"}
                    onClick={() => openRightDockMode("changed")}
                  >
                    <GitBranch size={14} />
                    <span>{t("workspace.changedTab")}</span>
                  </button>
                </Tooltip>
              )}
              {!sidebarImDetailConnection && activeTab?.scope === "project" && (
                <ExternalOpener tabId={activeTab.id} dismissSignal={transientOverlayDismissSignal} />
              )}
              <Tooltip label={t("shortcuts.cheatsheetTitle")}>
                <button
                  className="topicbar__action-btn topicbar__action-btn--icon topicbar__action-btn--utility"
                  type="button"
                  aria-label={t("shortcuts.cheatsheetTitle")}
                  onClick={() => {
                    closeTransientOverlays();
                    setSettingsFocus(null);
                    setSettingsTarget("shortcuts");
                  }}
                >
                  <CircleHelp size={14} />
                </button>
              </Tooltip>
              <Tooltip label={t("topicBar.command")}>
                <button
                  className={
                    sidebarCreation
                      ? "topicbar__action-btn topicbar__action-btn--icon topicbar__action-btn--utility"
                      : "topicbar__action-btn topicbar__action-btn--label topicbar__action-btn--accent"
                  }
                  type="button"
                  aria-label={t("topicBar.command")}
                  onClick={() => void openPalette()}
                >
                  <Command size={14} />
                  {!sidebarCreation && <span>{t("topicBar.command")}</span>}
                </button>
              </Tooltip>
              {sidebarCreation && (
                <Tooltip label={workspacePanelRenderable ? t("rightDock.collapse") : t("rightDock.expand")}>
                  <button
                    className={[
                      "topicbar__chrome-btn",
                      "topicbar__chrome-btn--workspace",
                      workspacePanelRenderable ? "topicbar__chrome-btn--active" : "",
                      workspaceTogglePressed ? "topicbar__chrome-btn--pressed" : "",
                    ].filter(Boolean).join(" ")}
                    type="button"
                    onClick={toggleWorkspacePanel}
                    aria-label={workspacePanelRenderable ? t("rightDock.collapse") : t("rightDock.expand")}
                    aria-pressed={workspacePanelRenderable}
                  >
                    <PanelRight size={15} />
                  </button>
                </Tooltip>
              )}
            </div>
          </header>

          {state.meta?.startupErr && (
            <div className="banner banner--error">{t("topbar.startupError", { msg: state.meta.startupErr })}</div>
          )}
          {safeMode && (
            <div className="banner banner--warning">{t("guard.safeMode")}</div>
          )}

          <UpdateBanner
            enabled={startupUpdateChecksEnabled === true}
            onShowReleaseNotes={(latest) => {
              const version = latest.replace(/^(?:desktop-)?v/, "");
              void openExternal(`https://reasonix.io/changelog/v${version}/`);
            }}
          />

          <main className="main">
            {sidebarImDetailConnection ? (
              <SidebarImConnectionDetail
                connection={sidebarImDetailConnection}
                onClose={() => setSidebarImDetailConnectionId("")}
                onOpenSettings={openBotSettings}
                onManageAllowlist={() => openBotAllowlistSettings(sidebarImDetailConnection.connectionId)}
                onOpenSession={() => void openSidebarImConnectionSession(sidebarImDetailConnection)}
              />
            ) : noticePreviewMockEnabled() ? (
              <NoticePreviewPanel />
            ) : (
              <Transcript
                items={displayItems}
                live={state.live}
                tabId={activeTabId}
                footerHeight={footerHeight}
                onPrompt={handleTranscriptPrompt}
                onDeliveryContinue={() => void handleDeliveryContinue()}
                onEditPrompt={handleEditPrompt}
                onRewind={handleMessageAction}
                checkpoints={state.checkpoints}
                actionPending={state.messageAction != null}
                rewindDisabled={Boolean(activeTab?.readOnly) || !controllerReady || hydratePlaceholderActive || rewindState != null || rewindCommitting || state.running || state.messageAction != null || state.approval != null || state.ask != null || clearContextPending}
                running={state.running || rewindCommitting}
                turnStartAt={state.turnStartAt}
                welcomeVariant={sidebarCreation ? "creation" : "default"}
                creationMode={sidebarCreation}
                actionHoverMenus={sidebarCreation && !hydratePlaceholderActive}
                rewindSignal={rewindSignal}
                revealSignal={transcriptRevealSignal}
                hydrating={transcriptHydrating}
                hasOlderHistory={state.historyHasOlder && !rewindState}
                olderHistoryCount={state.historyStartTurn}
                loadingOlderHistory={state.historyOlderLoading}
                onLoadOlderHistory={() => activeTabId && loadOlderHistory(activeTabId)}
                invocationMetadata={activeTabId ? invocationMetadataByTab[activeTabId] : undefined}
              />
            )}
          </main>

          {!sidebarImDetailConnection && (
          <footer className="footer" ref={footerRef}>
            {showTodos && (
              <TodoPanel
                key={scopedTodoBatch}
                stateKey={scopedTodoBatch}
                todos={todos}
                onDismiss={dismissTodos}
              />
            )}
            {rewindState && (
              <UndoRewindBanner
                meta={{
                  turns: rewindState.turnDiff,
                  filesRestored: [], // optimistic: files haven't changed yet
                  filesRemoved: [],
                  onUndo: () => {
                    if (activeTabId) setRewindStateForTab(activeTabId, null);
                    if (activeTabId) {
                      setComposerInsertRequestsByTab((current) => ({
                        ...current,
                        [activeTabId]: { id: Date.now(), text: "", mode: "replace" },
                      }));
                    }
                  },
                }}
              />
            )}
            {decisionSurface === "tool_approval" || decisionSurface === "plan_approval"
              ? state.approval && (
              <ApprovalModal
                key={`${activeTabId ?? ""}:${state.approval.id}`}
                approval={state.approval}
                cwd={state.meta?.cwd}
                tabId={activeTabId}
                workspaceScopeKey={workspaceScopeKey}
                insertRequest={activePlanRevisionInsertRequest}
                onRevisionActiveChange={(active) => setWorkspaceInsertTarget(active ? "planRevision" : "composer")}
                onAnswer={async (allow, session, persist) => {
                  // Approving an exit_plan_mode plan leaves plan mode; await the
                  // mode switch before sending the approval so the controller
                  // observes the updated state before it unblocks.
                  if (state.approval!.tool === "exit_plan_mode" && allow) await applyCollaborationMode("normal");
                  approve(state.approval!.id, allow, session, persist);
                }}
                onRevisePlan={(text) => {
                  if (activeTabId) {
                    setPendingPlanRevisionsByTab((current) => ({ ...current, [activeTabId]: text }));
                  }
                  approve(state.approval!.id, false, false, false);
                }}
                onExitPlan={async () => {
                  await applyCollaborationMode("normal");
                  approve(state.approval!.id, false, false, false);
                }}
                onStop={() => {
                  cancel();
                }}
                toolApprovalMode={toolApprovalMode}
              />
              )
            : decisionSurface === "ask"
              ? state.ask && (
              <AskCard
                key={`${activeTabId ?? ""}:${state.ask.id}`}
                ask={state.ask}
                onAnswer={answerQuestion}
                onDismiss={() => answerQuestion(state.ask!.id, [])}
                onStop={() => {
                  cancel();
                }}
              />
              )
            : decisionSurface === "clear_context" ? (
              <ClearContextCard
                onCancel={cancelClearContext}
                onConfirm={() => {
                  void confirmClearContext();
                }}
              />
            ) : null}
            {/* Composer stays mounted under a decision so per-session draft
                caches (text, attachments, paste blocks, guidance) survive. */}
            <div
              className={decisionSurface ? "composer-decision-host composer-decision-host--hidden" : "composer-decision-host"}
              hidden={Boolean(decisionSurface) || undefined}
              inert={decisionSurface ? true : undefined}
              aria-hidden={decisionSurface ? true : undefined}
            >
            <Composer
              running={state.running || rewindCommitting}
              collaborationMode={collaborationMode}
              toolApprovalMode={toolApprovalMode}
              tokenMode={tokenMode}
              goal={goal}
              cwd={state.meta?.cwd}
              modelLabel={state.meta?.label ?? t("status.connecting")}
              imageInputEnabled={state.meta?.imageInputEnabled !== false}
              tabId={activeTabId}
              effort={state.effort}
              onSend={handleSend}
              onInvocationMetadataChange={handleInvocationMetadataChange}
              onSteer={handleSteer}
              onCancel={cancel}
              onCycleMode={cycleMode}
              onSetMode={applyMode}
              onSetCollaborationMode={applyCollaborationMode}
              onSetToolApprovalMode={applyToolApprovalMode}
              onToggleYoloApprovalMode={toggleYoloApprovalMode}
              onClearGoal={() => applyGoal("")}
              onSwitchModel={switchModel}
              onSetEffort={setEffort}
              onSetTokenMode={applyTokenMode}
              insertRequest={composerInsertRequest}
              selectedTextRequest={selectedTextRequest}
              readOnly={Boolean(activeTab?.readOnly)}
              disabled={runtimeTransitioning || rewindCommitting || state.messageAction != null || Boolean(decisionSurface)}
              submitDisabled={!controllerReady}
              decisionPending={rewindCommitting || state.messageAction != null || Boolean(decisionSurface)}
              ready={controllerReady}
              turnStartAt={state.turnStartAt}
              turnWaitAccumMs={state.turnWaitAccumMs}
              promptWaitStartedAt={state.promptWaitStartedAt}
              turnTokens={state.turnTokens}
              turnArgChars={state.turnArgChars}
              retry={state.retry}
              suspendedByDecision={Boolean(decisionSurface)}
              transientDismissSignal={transientOverlayDismissSignal}
              sessionKey={composerSessionKey}
              workspaceScopeKey={workspaceScopeKey}
              fileRefRefreshKey={composerFileRefRefreshKey}
              guidanceConsumedKey={latestGuidanceConsumed?.key}
              guidanceConsumedText={latestGuidanceConsumed?.text}
              guidanceQueuePreviewItems={guidanceQueueMockItems}
              showContextWindowRing={sidebarCreation}
              context={state.context}
              turnCost={state.turnCost}
              cacheHitTokens={state.usage?.cacheHitTokens}
              cacheMissTokens={state.usage?.cacheMissTokens}
              balance={state.balance}
            />
            </div>
            <StatusBar
              context={state.context}
              usage={state.usage}
              balance={state.balance}
              running={state.running || rewindCommitting}
              sessionTurns={sessionTurns}
              sessionTokens={state.sessionTokens}
              turnTokens={state.turnTotalTokens}
              turnCost={state.turnCost}
              cost={state.sessionCost}
              currency={state.sessionCurrency}
              modelLabel={state.meta?.label}
              labelStyle={statusBarStyle}
              items={statusBarItems}
	              workspacePath={state.meta?.workspacePath || state.meta?.workspaceRoot || state.meta?.cwd}
	              workspaceName={state.meta?.workspaceName}
	              gitBranch={state.meta?.gitBranch}
              onConnectRemote={connectAndOpenRemoteWorkspace}
              onDisconnectRemote={(hostId) => void app.DisconnectRemoteHost(hostId).catch(() => {})}
              onManageRemote={() => setSettingsTarget("remote")}
              onOpenRemote={requestRemoteExplorer}
              onOpenRemoteWorkspace={openRemoteWorkspaceFromStatus}
              remoteHosts={remoteHosts}
              remoteStatuses={remoteStatuses}
            />
          </footer>
          )}
          </>
        </section>

        {workspacePanelGridOpen && (
          <button
            className="workspace-panel-resizer"
            type="button"
            role="separator"
            aria-orientation="vertical"
            aria-label={t("rightDock.resize")}
            aria-valuemin={workspacePanelResizeMinWidth}
            aria-valuemax={Math.max(workspacePanelMaxWidth, workspacePanelRenderWidth)}
            aria-valuenow={workspacePanelRenderWidth}
            onPointerDown={startWorkspacePanelResize}
            onKeyDown={resizeWorkspacePanelWithKeyboard}
            onDoubleClick={() => setSavedWorkspacePanelWidth(workspacePanelResetWidth)}
          />
        )}

        {workspacePanelRenderable && (
          <aside
            className={[
              "workbench-dock",
              `workbench-dock--${rightDockMode}`,
            ].join(" ")}
            aria-label={t("rightDock.workbench")}
          >
            <div className="workbench-dock__tools">
              <div className="workbench-dock__tabs" role="tablist" aria-label={t("rightDock.views")}>
                {SHOW_CONTEXT_DOCK && desktopLayoutStyle !== "creation" && (
                  <button
                    type="button"
                    role="tab"
                    aria-selected={rightDockMode === "context"}
                    className={`workbench-dock__tab${rightDockMode === "context" ? " workbench-dock__tab--active" : ""}`}
                    onClick={() => openRightDockMode("context")}
                  >
                    <Activity size={13} />
                    <span className="workbench-dock__tab-label">{t("rightDock.overview")}</span>
                  </button>
                )}
                <button
                  type="button"
                  role="tab"
                  aria-selected={rightDockMode === "files"}
                  className={`workbench-dock__tab${rightDockMode === "files" ? " workbench-dock__tab--active" : ""}`}
                  onClick={() => openRightDockMode("files")}
                >
                  <FileText size={13} />
                  <span className="workbench-dock__tab-label">{t("workspace.filesTab")}</span>
                </button>
                <button
                  type="button"
                  role="tab"
                  aria-selected={rightDockMode === "changed"}
                  className={`workbench-dock__tab${rightDockMode === "changed" ? " workbench-dock__tab--active" : ""}`}
                  onClick={() => openRightDockMode("changed")}
                >
                  <GitBranch size={13} />
                  <span className="workbench-dock__tab-label">{t("workspace.changedTab")}</span>
                </button>
                {remoteHosts.length > 0 && (
                  <button
                    type="button"
                    role="tab"
                    aria-selected={rightDockMode === "remote"}
                    className={`workbench-dock__tab${rightDockMode === "remote" ? " workbench-dock__tab--active" : ""}`}
                    onClick={openRemoteDock}
                  >
                    <Server size={13} />
                    <span className="workbench-dock__tab-label">{t("rightDock.remote")}</span>
                  </button>
                )}
              </div>
            </div>
            <div className="workbench-dock__body">
              {rightDockMode === "remote" ? (
                <Suspense fallback={null}>
                  <RemotePanel onClose={() => setWorkspacePanel(false)} />
                </Suspense>
              ) : rightDockMode === "context" && desktopLayoutStyle !== "creation" ? (
                <ContextPanel
                  tabId={activeTabId}
                  context={state.context}
                  usage={state.usage}
                  sessionTokens={state.sessionTokens}
                  sessionCost={state.sessionCost}
                  sessionCurrency={state.sessionCurrency}
                  sessionTurns={sessionTurns}
                  turnTokens={state.turnTotalTokens}
                  turnCost={state.turnCost}
                  balance={state.balance}
                  sessionGen={state.sessionGen}
                  refreshKey={dockRefreshKey}
                  usageSeq={state.usageSeq}
                />
              ) : (
                <WorkspacePanel
                  open={workspacePanelRenderable}
                  tabId={activeTabId}
                  cwd={state.meta?.cwd}
                  workspaceScopeKey={workspaceScopeKey}
                  maximized={workspacePanelMaximized}
                  panelWidth={workspacePanelRenderWidth}
                  onClose={() => setWorkspacePanel(false)}
                  onToggleMaximized={() => {
                    closeTransientOverlays();
                    setWorkspacePanelMaximized((value) => !value);
                  }}
                  onPreviewModeChange={handleWorkspacePreviewModeChange}
                  onAddToChat={addWorkspaceTextToComposer}
                  onAddCodeToChat={addWorkspaceCodeToComposer}
                  onRequestPanelWidth={ensureWorkspacePanelWidth}
                  onFileTreeRefresh={refreshComposerFileRefs}
                  refreshKey={dockRefreshKey}
                  initialViewMode={rightDockMode === "changed" ? "changed" : "files"}
                  showViewTabs={false}
                  creationMode={sidebarCreation}
                />
              )}
            </div>
          </aside>
        )}
      </div>

      {histView !== null && (
        <Suspense fallback={null}>
          <HistoryPanel
            kind={histView.kind}
            sessions={histView.sessions}
            running={state.running}
            onResume={onResumeSession}
            onPreview={previewSession}
            onDelete={onDeleteSession}
            onRename={onRenameSession}
            onRestore={onRestoreTrashedSession}
            onPurge={onPurgeTrashedSession}
            onPurgeAll={onPurgeAllTrashedSessions}
            onPurgeRecoveryCopies={onPurgeRecoveryCopies}
            onDeleteMany={onDeleteManySessions}
            onClose={closeHistory}
          />
        </Suspense>
      )}

      {settingsTarget !== null && (
        <Suspense fallback={null}>
          <SettingsPanel
            initialTab={settingsTarget}
            initialFocus={settingsFocus ?? undefined}
            agentRunning={state.running}
            desktopPlatform={desktopPlatform}
            onUseSubagent={prefillSubagentCommand}
            onClose={() => {
              setSettingsFocus(null);
              setSettingsTarget(null);
            }}
            onChanged={(settings) => {
              void refreshMeta();
              if (settings) {
                applyDesktopPreferences(settings);
                void refreshSidebarImConnectionsFromSettings(settings).catch((e) => console.warn("bot sidebar refresh failed", e));
                return;
              }
              void reloadSidebarImConnections().catch((e) => console.warn("bot sidebar refresh failed", e));
              void app.DesktopStartupSettings()
                .then(applyDesktopPreferences)
                .catch((e) => console.warn("desktop preferences refresh failed", e));
            }}
          />
        </Suspense>
      )}

      <RemoteHostKeyDialog />

      <CommandPalette
        open={paletteOpen}
        onClose={() => setPaletteOpen(false)}
        items={paletteItems}
        placeholder={t("palette.placeholder")}
        emptyText={t("palette.empty")}
      />

      <ShortcutsCheatsheet
        open={shortcutsOpen}
        platform={desktopPlatform}
        onClose={() => setShortcutsOpen(false)}
        t={t}
      />

      {startupSplashVisible && (
        <StartupSplash hold={startupSplashHold} onDone={() => setStartupSplashVisible(false)} />
      )}

      {needsOnboarding && <OnboardingOverlay onComplete={() => setNeedsOnboarding(false)} />}

      <HeartbeatPanel open={heartbeatOpen} onClose={() => setHeartbeatOpen(false)} onOpenTopic={(scope, workspaceRoot, topicId) => {
        void handleOpenTopic(scope, workspaceRoot, topicId);
      }} />
      <TranscriptSelectionMenu
        enabled={Boolean(activeTabId && !activeTab?.readOnly && !decisionSurface && !sidebarImDetailConnection && !hydratePlaceholderActive)}
        resetKey={activeTabId ?? ""}
        onAddToChat={addSelectedTextToComposer}
      />
      {windowsFramelessChrome && (
        <WindowsWindowControls
          maximised={mainWindowMaximised}
          syncMaximised={syncMainWindowMaximised}
        />
      )}
    </div>
    </ShellExpandProvider>
  );
}
