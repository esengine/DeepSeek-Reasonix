import { lazy, Suspense, useCallback, useEffect, useMemo, useRef, useState, type CSSProperties, type KeyboardEvent, type MouseEvent as ReactMouseEvent, type PointerEvent as ReactPointerEvent } from "react";
import { ShellExpandProvider, useShellExpand } from "./lib/shellExpand";
import {
  Activity,
  Command,
  Copy as RestoreIcon,
  Minus,
  Search,
  Server,
  Square,
  SquarePen,
  PanelLeft,
  PanelRight,
  FileText,
  GitBranch,
  MessageSquare,
  Settings as SettingsIcon,
  RotateCw,
  Trash2,
  AlarmClock,
  BarChart3,
  Brain,
  Cpu,
  Palette,
  Puzzle,
  X,
  TerminalSquare,
} from "lucide-react";
import { useToast } from "./lib/toast";
import { useGoalActionHandler } from "./lib/goalAction";
import { useWailsResizeFix } from "./lib/useWailsResizeFix";
import { asArray } from "./lib/array";
import { activeLeaseBlockedTab, createBoundedRefreshCoordinator, sameTabMetaLists, seedActiveTabMetaList, shouldRefreshTabMetaForEvent, TAB_META_MAX_IN_FLIGHT } from "./lib/tabMetaRefresh";
import { clearLegacyLangPref, normalizeLangPref, readLegacyLangPref, t, useI18n, useT, type Translator } from "./lib/i18n";
import { useActiveRemoteSession } from "./lib/useRemoteSession";
import { useRemoteTabOpened } from "./lib/useRemoteTabOpened";
import { publishNavigationIntent } from "./lib/useNavigationIntentFence";
import { renameCurrentRemoteSession } from "./lib/remoteSessionActions";
import { localizedNoticeText, useController, type HistoryLoadTrigger, type Item } from "./lib/useController";
import { app, onEvent, onProjectTreeChanged, onReady, onRemoteForwards, onRemoteServer, onRemoteStatus, onRuntimeRebuilt, openExternal } from "./lib/bridge";
import { useConfigLoadWarnings } from "./lib/useConfigLoadWarnings";
import { generativeMusic, isGenerativeMusicEnabled } from "./lib/generative-music";
import { clearAttentionChimeKeys, playAttentionChime, playSuccessChime, shouldPlayAttentionChimeForEvent } from "./lib/sound";
import { NoticeCard, Transcript } from "./components/Transcript";
import { Composer } from "./components/Composer";
import { TodoPanel } from "./components/TodoPanel";
import { ApprovalModal } from "./components/ApprovalModal";
import { AskCard } from "./components/AskCard";
import { ClearContextCard } from "./components/ClearContextCard";
import { RuntimeDecisionCard } from "./components/RuntimeDecisionCard";
import { decisionSurfaceMockFromInput, type DecisionSurfaceKind as MockDecisionSurfaceKind } from "./lib/decisionSurfaceMock";
const UndoRewindBanner = lazy(() => import("./components/UndoRewindBanner").then((module) => ({ default: module.UndoRewindBanner })));
const SessionTakeoverDialog = lazy(() => import("./components/SessionTakeoverDialog").then((module) => ({ default: module.SessionTakeoverDialog })));
const ProjectTree = lazy(() => import("./components/ProjectTree").then((module) => ({ default: module.ProjectTree })));
const RemoteSessionSurface = lazy(() => import("./components/RemoteSessionSurface").then((module) => ({ default: module.RemoteSessionSurface })));
const ExtensionFormDialog = lazy(() => import("./components/ExtensionFormDialog").then((module) => ({ default: module.ExtensionFormDialog })));
const MCPInteractionCard = lazy(() => import("./components/MCPInteractionCard").then((module) => ({ default: module.MCPInteractionCard })));
const WorktreeMergeModal = lazy(() => import("./components/WorktreeMergeModal").then((module) => ({ default: module.WorktreeMergeModal })));
/** Footer decision surface kinds. Runtime blockers are explicit recovery choices. */
type DecisionSurfaceKind = MockDecisionSurfaceKind | "extension_form";
import { StatusBar } from "./components/StatusBar";
import { RemoteHostKeyDialog } from "./components/RemoteHostKeyDialog";
import { RemoteSecretDialog } from "./components/RemoteSecretDialog";
import { RemoteConnectionTimeoutError, useRemoteStore, waitForRemoteConnection } from "./store/remote";
import { RemoteWorkspaceLaunchGate, resolveRemoteWorkspace } from "./lib/remoteWorkspace";
import { CommandPalette, type PaletteItem } from "./components/CommandPalette";
import { UpdateBanner } from "./components/UpdateBanner";
import { UpdaterProvider } from "./lib/useUpdater";
import { Tooltip } from "./components/Tooltip";
import { StartupSplash } from "./components/StartupSplash";
import { OnboardingOverlay } from "./components/OnboardingOverlay";
import { dismissOnboarding, shouldOpenOnboarding } from "./lib/onboarding";
import { AppChrome } from "./components/AppChrome";
import { ShortcutsCheatsheet } from "./components/ShortcutsCheatsheet";
import { WorktreeBadge } from "./components/WorktreeBadge";
import { CopyButton } from "./components/CopyButton";
import { ExternalOpener, shouldMountExternalOpener } from "./components/ExternalOpener";
import { TopicbarMoreMenu } from "./components/TopicbarMoreMenu";
import { RemoteReclaimBanner } from "./components/RemoteReclaimBanner";
import { startTerminalEventBridge } from "./lib/terminalEvents";
import { applyTerminalThemePreference } from "./lib/terminalTheme";
import { formatTerminalOutputForComposer } from "./lib/terminalOutput";
import { useTerminalStore } from "./store/terminal";
import { hydrateReasoningDisplayMode, setReasoningDisplayPending } from "./lib/reasoningDisplayPreference";
import { parseTodos } from "./lib/tools";
import {
  dismissedTodoKeyForScope,
  resolveTodoPanelTodos,
  scopedTodoBatchKey,
  scopedTodoDismissalKey,
  shouldShowTodoPanel,
  todoBatchKey,
  todoContinueTarget,
  todoDismissalKey,
  todoPanelScope,
} from "./lib/todoVisibility";
import {
  type BotConnectionView,
  type BotRuntimeStatusView,
  type BotSettingsView,
  type ActiveWorkView,
  type BackgroundRuntimeView,
  type CollaborationMode,
  type ComposerInsertRequest,
  type DesktopStartupSettingsView,
  type Mode,
  type RewindResultView,
  type RemoteHostView,
  type SessionMeta,
  type SettingsView,
  type QualityFloor,
  type TabMeta,
  type ToolApprovalMode,
  type WireCompletionSummary,
  type WorkspaceConflictView,
} from "./lib/types";
import { runWorktreeMergeLifecycle } from "./lib/worktreeMergeLifecycle";
import { showWorktreeCleanupNotice } from "./lib/worktreeCleanupNotice";
import { requestSessionVersions } from "./lib/sessionRecoveryVersionHostBridge";
import type { WorkspaceVerificationRevealRequest } from "./components/WorkspacePanel";
import type { InvocationMetadataMap, StructuredInvocationSubmit } from "./lib/invocationDisplay";
import type { RewindUndoState } from "./lib/rewindTypes";
import { formatSelectionReference, type SelectedTextInsertRequest } from "./lib/selectedTextContext";
import { resolveTaskMonitorSession } from "./lib/taskMonitorNavigation";
import {
  composerProfileFromMeta,
  composerProfileFromTab,
  composerProfileMode,
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
  toggleYoloToolApprovalMode,
  type RestorableToolApprovalMode,
} from "./lib/toolApprovalMode";
import { useComposerModeActions } from "./lib/useComposerModeActions";
import { openRemoteNewSession, useRemoteComposerProfileSync, useRemoteComposerRuntimeActions, useRemoteComposerSend } from "./lib/useRemoteComposerIntegration";
import {
  CREATION_RIGHT_DOCK_MIN_RENDER_WIDTH,
  CREATION_RIGHT_DOCK_TREE_MIN_WIDTH,
  CREATION_SIDEBAR_MIN_WIDTH,
  RIGHT_DOCK_MIN_RENDER_WIDTH,
  RIGHT_DOCK_TREE_MIN_WIDTH,
  type RightDockMode,
  SIDEBAR_MAX_WIDTH,
  SIDEBAR_MIN_WIDTH,
  TERMINAL_DEFAULT_HEIGHT,
  TERMINAL_MIN_HEIGHT,
  applyLayoutStyleDefaults,
  clampCreationRightDockTreeWidth,
  clampCreationSidebarWidth,
  clampRightDockTreeWidth,
  clampSidebarWidth,
  clampTerminalHeight,
  defaultCreationRightDockTreeWidth,
  defaultCreationSidebarWidth,
  defaultRightDockTreeWidth,
  defaultSidebarWidth,
  saveRightDockTreeWidth,
  saveSidebarCollapsed,
  saveSidebarWidth,
  saveTerminalHeight,
  saveTerminalPanelOpen,
  terminalMaxHeight,
  saveWorkspacePanelOpen,
  loadWorkspacePanelOpen,
  useLayoutStore,
} from "./store/layout";
import { useOverlayStore } from "./store/overlays";
import { hydrateDisplayMode } from "./lib/displayMode";
import { recordFrontendDiagnostic } from "./lib/frontendDiagnosticBridge";
import { DEFAULT_STATUS_BAR_ITEMS, normalizeStatusBarItems, type StatusBarItemId } from "./lib/statusBarItems";
import { paletteSessionDisplayTitle, paletteSessionHint, paletteSessionKeywords, sessionActivityTime } from "./lib/session";
import { enqueueNavigationRequest, type PendingNavigationRequest } from "./lib/openTopicCoalescing";
import {
  guardBackendNavigationResult,
} from "./lib/navigationSurfaceTransition";
import { useNavigationSurface } from "./lib/useNavigationSurface";
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
import { availableWorkspacePanelWidth, resolveLiveWorkspacePanelWidth, resolveWorkspacePanelPlacement, workspacePanelAriaMinWidth } from "./lib/workspaceLayout";
import { createPointerResizeLifecycle, createRafResizeUpdater } from "./lib/resizeDrag";
import { formatShortcutCombo, resolvedShortcutCombo, useGlobalShortcut } from "./lib/keyboardShortcuts";
import { useWarmTerminalPanel } from "./lib/useWarmTerminalPanel";
import { topicShortcutIndexFromEvent, useTopicShortcuts, type TopicShortcutEntry } from "./lib/topicShortcuts";
import { composerDraftKeyForTab } from "./lib/composerDraftKey";
import { continueDelivery } from "./lib/deliveryContinue";
import { activateGoalAndSubmitOnTab } from "./lib/goalSubmit";
import logoWordmark from "./assets/logo-wordmark.svg";
// Hold reasoning UI until the authoritative desktop startup settings arrive;
// this prevents a hidden preference from flashing content during first paint.
setReasoningDisplayPending();
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
    notice(9, "info", "Goal is not ready to complete yet; continuing the remaining work.", "goal completion check found pending validation: desktop/frontend typecheck"),
    notice(13, "info", "Goal still has unfinished task state; continuing the remaining work.", "active goal has open task state: implement preview, verify browser, report result"),
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
          return (
            <NoticeCard
              key={item.id}
              item={item}
              onAction={item.action ? () => undefined : undefined}
              onAccept={item.action === "continue_delivery" ? () => undefined : undefined}
            />
          );
        })}
      </div>
    </div>
  );
}

const TranscriptSelectionMenu = lazy(() => import("./components/TranscriptSelectionMenu").then((module) => ({ default: module.TranscriptSelectionMenu })));
const ContextPanel = lazy(() => import("./components/ContextPanel").then((module) => ({ default: module.ContextPanel })));
const HistoryPanel = lazy(() => import("./components/HistoryPanel").then((module) => ({ default: module.HistoryPanel })));
const SessionRecoveryVersionsHost = lazy(() => import("./components/SessionRecoveryVersionsHost").then((module) => ({ default: module.SessionRecoveryVersionsHost })));
const HeartbeatView = lazy(() => import("./custom/features/heartbeat/HeartbeatPanel").then((module) => ({ default: module.HeartbeatView })));
const SettingsPanel = lazy(() => import("./components/SettingsPanelEntry").then((module) => ({ default: module.SettingsPanel })));
const RemotePanel = lazy(() => import("./components/RemotePanel").then((module) => ({ default: module.RemotePanel })));
const TerminalPanel = lazy(() => import("./components/TerminalPanel").then((module) => ({ default: module.TerminalPanel })));
const TaskMonitorPanel = lazy(() => import("./components/TaskMonitorPanel").then((module) => ({ default: module.TaskMonitorPanel })));
const WorkspacePanel = lazy(async () => {
  const [module] = await Promise.all([
    import("./components/WorkspacePanel"),
    import("./components/WorkspacePanelStability.css"),
  ]);
  return { default: module.WorkspacePanel };
});

const CHAT_MIN_WIDTH = 400;
const WORKSPACE_RESIZER_WIDTH = 8;
function stripLegacyGoalBudgetFlags(arg: string): string {
  const parts = arg.trim().split(/\s+/).filter(Boolean);
  while (parts.length > 0) {
    const flag = parts[0].toLowerCase();
    if (flag !== "--research" && flag !== "--auto-research" && flag !== "--deep" && flag !== "--simple" && flag !== "--no-research") break;
    parts.shift();
  }
  return parts.join(" ");
}
function hasLegacyGoalBudgetFlag(arg: string): boolean {
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
const MACOS_WORKBENCH_TITLEBAR_HEIGHT = 46;

function isMacOSWorkbenchSidebarTitlebar(target: HTMLElement | null, clientY: number, platform: DesktopPlatform): boolean {
  if (platform !== "darwin") return false;
  const sidebar = target?.closest(".sidebar--workbench");
  if (!(sidebar instanceof HTMLElement)) return false;
  const offsetY = clientY - sidebar.getBoundingClientRect().top;
  return offsetY >= 0 && offsetY < MACOS_WORKBENCH_TITLEBAR_HEIGHT;
}

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
type DesktopNavigationIntent =
  | { kind: "topic"; scope: string; workspaceRoot: string; topicId: string; sessionPath?: string }
  | { kind: "blank"; scope: string; workspaceRoot: string }
  | { kind: "isolated-worktree"; workspaceRoot: string }
  | { kind: "sidebar-im"; connection: SidebarImConnection }
  | { kind: "resume-session"; session: SessionMeta };
type DesktopNavigationInput = DesktopNavigationIntent & { navigationIntentSeq: number };
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

function taskSessionIDFromPath(path: string): string {
  const base = path.replace(/\\/g, "/").split("/").pop() || "";
  const extension = base.lastIndexOf(".");
  return extension > 0 ? base.slice(0, extension) : base;
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
    liveStore,
    activeTabId,
    sendToTab,
    recoverDeliveryToTab,
    runShellForTab,
    steerForTab,
    notice,
    cancel,
    approve,
    resolvePlanDecision,
    resolveRecovery,
    answerQuestion,
    answerMCPInteraction,
    setControllerMode,
    dismissExtensionForm,
    drainExtensionNotifications,
    setCollaborationMode: setControllerCollaborationMode,
    setToolApprovalMode: setControllerToolApprovalMode,
    setQualityFloor: setControllerQualityFloor,
    setComposerProfileForTab: setControllerComposerProfileForTab,
    setGoalForTab: setControllerGoalForTab,
    resumeGoalForTab: resumeControllerGoalForTab,
    pauseGoalForTab: pauseControllerGoalForTab,
    clearGoal: clearControllerGoal,
    clearGoalForTab: clearControllerGoalForTab,
    clearSession,
    newSession,
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
    retrySessionHistory,
    refreshMeta,
    pickWorkspace,
    switchWorkspace,
    rewindForTab,
    rewindForTabDetailed,
    undoRewindForTab,
    setModel,
    setEffort,
    cancelJob,
    switchTab,
    switchRemoteTab,
    openProjectTab,
    createIsolatedWorktree,
    openGlobalTab,
    closeTab,
    reorderTabs,
    openTopicSession,
    activateTopic,
    noteNavigationIntent,
    registeredNavigationIntent,
    isNavigationIntentCurrent,
    reassertVisibleTabAfterStaleNavigation,
    syncActiveTab,
    ensureBlankTab,
    ensureBlankSurface,
    commitSingleSurfaceNavigation,
  } = useController();
  const { locale, setPref: setLocalePref } = useI18n();
  const t = useT();
  const [composerProfilesByTab, setComposerProfilesByTab] = useState<Record<string, ComposerProfile>>({});
  const yoloRestoreToolApprovalModesRef = useRef<Record<string, RestorableToolApprovalMode>>({});
  const userPlanModeByTabRef = useRef<UserPlanModeIntents>({});
  const [tabMetas, setTabMetas] = useState<TabMeta[]>([]);
  const [tabOrderIds, setTabOrderIds] = useState<string[]>([]);
  const {
    surface: navigationSurface, intent: navigationSurfaceIntent, transitioning: runtimeTransitioning, dataReady: navigationTargetDataReady,
    preserved: preservedTranscriptSurface, renderedRef: renderedTranscriptSurfaceRef, begin: beginNavigationSurface, maskTarget: settleNavigationSurface, commitPaint: commitNavigationSurfacePaint,
  } = useNavigationSurface({
    activeTabId, ready: state.meta?.ready === true, backendActivationPending: Boolean(state.backendActivationPending), hydrating: Boolean(state.hydrating), hydrateError: state.hydrateError,
  });
  const [tabRevealSignal, setTabRevealSignal] = useState(0);
  const [transcriptRevealSignal, setTranscriptRevealSignal] = useState(0);
  const startupSplashVisible = useOverlayStore((s) => s.startupSplashVisible);
  const setStartupSplashVisible = useOverlayStore((s) => s.setStartupSplashVisible);
  // null until the mount probe resolves; true shows the first-run guide.
  const needsOnboarding = useOverlayStore((s) => s.needsOnboarding);
  const setNeedsOnboarding = useOverlayStore((s) => s.setNeedsOnboarding);
  const [providerSetupNeeded, setProviderSetupNeeded] = useState(false);
  const settingsTarget = useOverlayStore((s) => s.settingsTarget);
  const setSettingsTarget = useOverlayStore((s) => s.setSettingsTarget);
  const settingsFocus = useOverlayStore((s) => s.settingsFocus);
  const setSettingsFocus = useOverlayStore((s) => s.setSettingsFocus);
  const [desktopLayoutStyle, setDesktopLayoutStyle] = useState<DesktopLayoutStyle>("workbench");
  const singleSurfaceLayout = desktopLayoutStyle === "workbench" || desktopLayoutStyle === "creation";
  const { configLoadWarnings, applySnapshot: applyConfigWarningSnapshot, reload: reloadConfigWarnings, dismiss: dismissConfigWarnings } = useConfigLoadWarnings();
  const [startupUpdateChecksEnabled, setStartupUpdateChecksEnabled] = useState<boolean | null>(null);
  const [histView, setHistView] = useState<HistoryViewState | null>(null);
  const paletteOpen = useOverlayStore((s) => s.paletteOpen);
  const setPaletteOpen = useOverlayStore((s) => s.setPaletteOpen);
  const paletteExtensionActions = useOverlayStore((s) => s.paletteExtensionActions);
  const setPaletteExtensionActions = useOverlayStore((s) => s.setPaletteExtensionActions);
  const remoteExplorerOpen = useRemoteStore((s) => s.explorerOpen);
  const remoteExplorerHostId = useRemoteStore((s) => s.explorerHostId);
  const remoteHosts = useRemoteStore((s) => s.hosts);
  const remoteStatuses = useRemoteStore((s) => s.statuses);
  const { showToast } = useToast();
  const { runGoalAction, handleGoalActionError } = useGoalActionHandler();
  const setRemoteHosts = useRemoteStore((s) => s.setHosts);
  const hydrateRemoteStatuses = useRemoteStore((s) => s.hydrateStatuses);
  const requestRemoteExplorer = useRemoteStore((s) => s.openExplorer);
  const closeRemoteExplorerRequest = useRemoteStore((s) => s.closeExplorer);
  const applyRemoteStatus = useRemoteStore((s) => s.applyStatus);
  const requestRemoteStatusPopover = useRemoteStore((s) => s.requestStatusPopover);
  const setRemoteForwards = useRemoteStore((s) => s.setForwards);
  const setRemoteServer = useRemoteStore((s) => s.setServer);

  const shortcutsOpen = useOverlayStore((s) => s.shortcutsOpen);
  const setShortcutsOpen = useOverlayStore((s) => s.setShortcutsOpen);
  const paletteSessions = useOverlayStore((s) => s.paletteSessions);
  const setPaletteSessions = useOverlayStore((s) => s.setPaletteSessions);
  const [sidebarImConnections, setSidebarImConnections] = useState<SidebarImConnection[]>([]);
  const [imTopicSources, setImTopicSources] = useState<Record<string, SidebarImTopicSource>>({});
  const [sidebarImDetailConnectionId, setSidebarImDetailConnectionId] = useState("");
  const sidebarCollapsed = useLayoutStore((s) => s.sidebarCollapsed);
  const setSidebarCollapsed = useLayoutStore((s) => s.setSidebarCollapsed);
  const mainView = useOverlayStore((s) => s.mainView);
  const setMainView = useOverlayStore((s) => s.setMainView);
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
  const [tasksOpen, setTasksOpen] = useState<false | "session" | "all">(false);
  const [takeoverDialogTab, setTakeoverDialogTab] = useState<string | null>(null);
  const [reclaimBusyTab, setReclaimBusyTab] = useState<string | null>(null);
  const [liveSidebarWidth, setLiveSidebarWidth] = useState<number | null>(null);
  const [viewportWidth, setViewportWidth] = useState(() => (typeof window === "undefined" ? 1440 : window.innerWidth));
  const [viewportHeight, setViewportHeight] = useState(() => (typeof window === "undefined" ? 720 : window.innerHeight));
  const workspacePanelOpen = useLayoutStore((s) => s.workspacePanelOpen);
  const setWorkspacePanelOpen = useLayoutStore((s) => s.setWorkspacePanelOpen);
  const rightDockTreeWidth = useLayoutStore((s) => s.rightDockTreeWidth);
  const setRightDockTreeWidth = useLayoutStore((s) => s.setRightDockTreeWidth);
  const rightDockPreviewWidth = useLayoutStore((s) => s.rightDockPreviewWidth);
  const workspacePreviewActive = useLayoutStore((s) => s.workspacePreviewActive);
  const setWorkspacePreviewActive = useLayoutStore((s) => s.setWorkspacePreviewActive);
  const attentionChimeEvents = useRef(new Set<string>());
  const workspaceScopeActiveTabRef = useRef(activeTabId);
  const [workspaceControllerEpoch, setWorkspaceControllerEpoch] = useState(0);
  workspaceScopeActiveTabRef.current = activeTabId;
  // ContextPanel still uses this turn sequence for usage/session metadata;
  // WorkspacePanel listens to resource-level workspace revisions instead.
  useEffect(() => {
    startTerminalEventBridge();
    const unsub = onEvent((e) => {
      recordFrontendDiagnostic("runtime", "runtime.event", {
        action: e.kind,
        status: e.err ? "error" : "ok",
      });
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
      recordFrontendDiagnostic("runtime", "runtime.ready", { ready: true, hasActiveTab: Boolean(readyTabId) });
      clearAttentionChimeKeys(attentionChimeEvents.current, readyTabId);
      // A failed startup (lease blocked, no model) never emits agent events,
      // so the tabMetas list would keep the stale "starting" runtime and the
      // takeover banner/button would have nothing to render. Refresh the list
      // on every ready signal — the coordinator bounds the fetch rate.
      void refreshTabMetas();
      if (!readyTabId || readyTabId === workspaceScopeActiveTabRef.current) {
        setWorkspaceControllerEpoch((value) => value + 1);
      }
    });
    // Model/effort/token-mode switches and clear-while-running replace the
    // controller WITHOUT an agent:ready — they signal runtime:rebuilt instead
    // (a ready here would trigger a full session reload the UI already did).
    const unsubRebuilt = onRuntimeRebuilt((rebuiltTabId) => {
      recordFrontendDiagnostic("runtime", "runtime.rebuilt", { ready: true, hasActiveTab: Boolean(rebuiltTabId) });
      clearAttentionChimeKeys(attentionChimeEvents.current, rebuiltTabId);
      if (!rebuiltTabId || rebuiltTabId === workspaceScopeActiveTabRef.current) {
        setWorkspaceControllerEpoch((value) => value + 1);
      }
    });
    // The backend pushes authoritative per-tab meta after state changes that
    // produce no agent events (e.g. a lease-blocked startup). Refresh the list
    // so the takeover banner/button render even when the active tab differs.
    return () => {
      unsub();
      unsubReady();
      unsubRebuilt();
    };
  }, []);

  useEffect(() => {
    recordFrontendDiagnostic("app", "app.surface", {
      hasActiveTab: Boolean(activeTabId),
      tabCount: tabMetas.length,
    });
  }, [activeTabId, tabMetas.length]);

  const [workspacePanelResizing, setWorkspacePanelResizing] = useState(false);
  const [liveWorkspacePanelRenderWidth, setLiveWorkspacePanelRenderWidth] = useState<number | null>(null);
  const [liveTerminalHeight, setLiveTerminalHeight] = useState<number | null>(null);
  const terminalResizing = liveTerminalHeight !== null;
  const workspacePanelMaximized = useLayoutStore((s) => s.workspacePanelMaximized);
  const setWorkspacePanelMaximized = useLayoutStore((s) => s.setWorkspacePanelMaximized);
  const rightDockMode = useLayoutStore((s) => s.rightDockMode);
  const setRightDockMode = useLayoutStore((s) => s.setRightDockMode);
  const terminalPanelOpen = useLayoutStore((s) => s.terminalPanelOpen);
  const setTerminalPanelOpen = useLayoutStore((s) => s.setTerminalPanelOpen);
  const { mounted: terminalContentVisible, fitEnabled: terminalFitEnabled, prefetch: prefetchTerminalPanel } = useWarmTerminalPanel(terminalPanelOpen, terminalResizing);
  const terminalHeight = useLayoutStore((s) => s.terminalHeight);
  const setTerminalHeight = useLayoutStore((s) => s.setTerminalHeight);
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

  // Leaving the automation view: any overlay/sidebar surface that signals the
  // user is returning to the chat workspace switches mainView back to "chat".
  // Navigation paths (open topic / new session / resume) are covered by
  // enqueueNavigation.
  useEffect(() => {
    if (mainView !== "automation") return;
    // Any chat-side overlay/surface counts as "returning to the chat
    // workspace": settings, palette, sidebar search, history, the shortcuts
    // cheatsheet and the topic export menu all switch mainView back to chat.
    if (settingsTarget !== null || paletteOpen || sidebarSearchOpen || histView !== null
      || shortcutsOpen || topicExportOpen) {
      setMainView("chat");
    }
  }, [mainView, settingsTarget, paletteOpen, sidebarSearchOpen, histView, shortcutsOpen, topicExportOpen, setMainView]);
  const sidebarSearchFocusSignal = useOverlayStore((s) => s.sidebarSearchFocusSignal);
  const setSidebarSearchFocusSignal = useOverlayStore((s) => s.setSidebarSearchFocusSignal);
  const [sidebarTogglePressed, setSidebarTogglePressed] = useState(false);
  const [clearContextPending, setClearContextPending] = useState(false);
  const [backgroundRuntimes, setBackgroundRuntimes] = useState<BackgroundRuntimeView[]>([]);
  const [workspaceConflict, setWorkspaceConflict] = useState<WorkspaceConflictView | null>(null);
  const [pendingClose, setPendingClose] = useState<{ tabId: string; work: ActiveWorkView; stopping: boolean } | null>(null);
  const [worktreeMergeTabId, setWorktreeMergeTabId] = useState<string | null>(null);
  const topicRenameSkipCommitRef = useRef(false);
  const prevDecisionSurfaceRef = useRef<DecisionSurfaceKind | null>(null);
  const decisionSurfaceRef = useRef<DecisionSurfaceKind | null>(null);
  const topicRenameCommitHandledRef = useRef(false);
  const appRef = useRef<HTMLDivElement>(null);
  const layoutRef = useRef<HTMLDivElement>(null);
  const workspacePanelResizeFinishRef = useRef<(() => void) | null>(null);
  const sidebarTogglePressTimerRef = useRef<number | null>(null);

  // Persist window geometry across launches.
  useWindowStatePersistence();
  useViewportHeightVar();
  useEffect(() => () => workspacePanelResizeFinishRef.current?.(), []);
  useEffect(() => {
    document.documentElement.setAttribute("data-platform", desktopPlatform);
  }, [desktopPlatform]);

  const refreshBackgroundRuntimes = useCallback(async () => {
    try {
      setBackgroundRuntimes(await app.BackgroundRuntimes());
    } catch {
      // The global recovery entry is supplementary; the active-tab job list
      // remains available even when the detached-runtime list is unavailable.
    }
  }, []);

  useEffect(() => {
    let disposed = false;
    const refresh = async () => {
      if (disposed) return;
      await refreshBackgroundRuntimes();
    };
    void refresh();
    const timer = window.setInterval(() => void refresh(), 1000);
    return () => {
      disposed = true;
      window.clearInterval(timer);
    };
  }, [refreshBackgroundRuntimes]);

  useEffect(() => {
    if (!activeTabId || !state.running) {
      setWorkspaceConflict(null);
      return;
    }
    let disposed = false;
    const inspect = async () => {
      try {
        const conflict = await app.WorkspaceConflictForTab(activeTabId);
        if (!disposed) setWorkspaceConflict(conflict.state === "none" ? null : conflict);
      } catch {
        if (!disposed) setWorkspaceConflict(null);
      }
    };
    void inspect();
    const timer = window.setInterval(() => void inspect(), 500);
    return () => {
      disposed = true;
      window.clearInterval(timer);
    };
  }, [activeTabId, state.running]);

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
    (settings: Pick<SettingsView, "desktopTheme" | "desktopThemeStyle" | "desktopTerminalTheme" | "desktopLayoutStyle" | "desktopLanguage" | "checkUpdates" | "statusBarStyle" | "statusBarItems" | "conversationWidth"> & { reasoningDisplayMode?: string; reasoningDisplayModeExplicit?: boolean }) => {
      const nextTheme = normalizeThemePreference(settings.desktopTheme);
      const nextStyle = normalizeThemeStyleForTheme(settings.desktopThemeStyle, nextTheme);
      applyConfiguredBaseAppearance(nextTheme, nextStyle);
      applyTerminalThemePreference(settings.desktopTerminalTheme);
      applyConversationWidth(settings.conversationWidth);
      const nextLayoutStyle = normalizeDesktopLayoutStyle(settings.desktopLayoutStyle);
      setDesktopLayoutStyle(nextLayoutStyle);
      applyLayoutStyleDefaults(nextLayoutStyle);
      setLocalePref(normalizeLangPref(settings.desktopLanguage));
      setStartupUpdateChecksEnabled(settings.checkUpdates !== false);
      setStatusBarStyle(settings.statusBarStyle === "text" ? "text" : "icon");
      setStatusBarItems(normalizeStatusBarItems(settings.statusBarItems));
      hydrateReasoningDisplayMode(settings.reasoningDisplayMode, settings.reasoningDisplayModeExplicit === true);
    },
    [setLocalePref],
  );

  useEffect(() => {
    setReasoningDisplayPending();
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
      applyConfigWarningSnapshot(settings.configWarnings, settings.configWarningsRevision);
      hydrateDisplayMode(settings.displayMode);
      setSidebarImConnections(sidebarImConnectionsFromBot(settings.bot, t, runtimeStatus));
      setImTopicSources(sidebarImTopicSourcesFromBot(settings.bot, t));
      // Load unified theme experience after base appearance so pack tokens win.
      {
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
      hydrateReasoningDisplayMode("auto", false);
    });
    return () => {
      cancelled = true;
    };
  }, [applyConfigWarningSnapshot, applyDesktopPreferences, t]);

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
    const onResize = () => {
      setViewportWidth(window.innerWidth);
      setViewportHeight(window.innerHeight);
    };
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
  const commitThenSendRef = useRef<(
    tabId: string,
    displayText: string,
    submitText?: string,
    structured?: StructuredInvocationSubmit,
    initialGoal?: {
      goal: string;
      collaborationMode: CollaborationMode;
      toolApprovalMode: ToolApprovalMode;
    },
  ) => Promise<void>>(async () => {});
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
  // The dock keeps one width across tab switches (context/files/changed):
  // the tree width is the single source so toggling tabs never resizes the
  // sidebar. Preview detail stays inside the dock without widening it.
  const preferredWorkspacePanelWidth = rightDockTreeWidth;
  const rightDockTreeMinWidth = desktopLayoutStyle === "creation" ? CREATION_RIGHT_DOCK_TREE_MIN_WIDTH : RIGHT_DOCK_TREE_MIN_WIDTH;
  const rightDockTreeWidthClamp = desktopLayoutStyle === "creation" ? clampCreationRightDockTreeWidth : clampRightDockTreeWidth;
  const rightDockMinRenderWidth = desktopLayoutStyle === "creation" && !rightDockDetailActive
    ? CREATION_RIGHT_DOCK_MIN_RENDER_WIDTH
    : RIGHT_DOCK_MIN_RENDER_WIDTH;
  const workspacePanelMinWidth = rightDockTreeMinWidth;
  const chatReservedWidth = CHAT_MIN_WIDTH;
  const workspacePanelAvailableWidth = availableWorkspacePanelWidth({
    viewportWidth,
    sidebarCollapsed,
    sidebarWidth,
    chatMinWidth: chatReservedWidth,
    resizerWidth: WORKSPACE_RESIZER_WIDTH,
  });
  const {
    renderWidth: workspacePanelRenderWidth,
    overlay: workspacePanelOverlay,
    // The automation page fills the main content area; the workbench dock must
    // not overlay it. main-v2 keeps automation as a popup so its placement
    // helper has no view concept — apply the exclusion here on top.
    renderable: workspacePanelRenderable,
    gridOpen: workspacePanelGridOpen,
  } = resolveWorkspacePanelPlacement({
    viewportWidth, sidebarCollapsed, sidebarWidth, chatMinWidth: chatReservedWidth,
    resizerWidth: WORKSPACE_RESIZER_WIDTH, open: workspacePanelOpen,
    maximized: workspacePanelMaximized, preferredWidth: preferredWorkspacePanelWidth,
    minWidth: workspacePanelMinWidth, minRenderWidth: rightDockMinRenderWidth,
    liveWidth: liveWorkspacePanelRenderWidth,
  });
  const automationView = mainView === "automation";
  const effectiveWorkspacePanelRenderable = automationView ? false : workspacePanelRenderable;
  const effectiveWorkspacePanelGridOpen = automationView ? false : workspacePanelGridOpen;
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
  const { active: remoteSurfaceActive, session: remoteSession, ready: remoteComposerReady, onSend: remoteSend, onCancel: remoteCancel } = useActiveRemoteSession(activeTab, showToast);

  // Remote tab became ready: refresh the tab list so the spectator banner
  // (takenOver) renders. The agent:ready event only fires for local tabs;
  // remote tabs publish readiness via remote-tab:<id>:state, which
  const visibleRuntimeState = remoteSurfaceActive ? remoteSession.transcript : state;
  const localWorkspaceDockBlocked = remoteSurfaceActive && (rightDockMode === "files" || rightDockMode === "changed");
  const surfaceWorkspacePanelRenderable = effectiveWorkspacePanelRenderable && !localWorkspaceDockBlocked;
  const surfaceWorkspacePanelGridOpen = effectiveWorkspacePanelGridOpen && !localWorkspaceDockBlocked;
  const terminalSurfaceOpen = terminalPanelOpen && !remoteSurfaceActive;
  const activePlanRevisionInsertRequest =
    planRevisionInsertRequest &&
    planRevisionInsertRequest.tabId === activeTabId &&
    planRevisionInsertRequest.approvalId === state.approval?.id
      ? planRevisionInsertRequest.request
      : null;
  const composerInsertRequest = activeTabId ? composerInsertRequestsByTab[activeTabId] ?? null : null;
  const handleRevisionActiveChange = useCallback((active: boolean) => {
    setWorkspaceInsertTarget(active ? "planRevision" : "composer");
  }, []);
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
  const transcriptGeometrySessionKey = useMemo(() => {
    const sessionPath = (activeTab?.sessionPath ?? state.meta?.sessionPath ?? "").trim();
    const sessionGeneration = activeTab?.sessionGeneration ?? state.meta?.sessionGeneration ?? state.sessionGen;
    if (sessionPath) return ["session", sessionPath, String(sessionGeneration ?? 0)].join("\u0000");
    return [
      "topic",
      activeTab?.scope ?? "",
      activeTab?.workspaceRoot ?? state.meta?.cwd ?? "",
      activeTab?.topicId ?? "",
      activeTabId ?? "",
    ].join("\u0000");
  }, [activeTab, activeTabId, state.meta?.cwd, state.meta?.sessionGeneration, state.meta?.sessionPath, state.sessionGen]);
  const workspaceScopeKey = [
    activeTabId ?? "",
    activeTab?.sessionPath ?? "",
    state.meta?.sessionPath ?? "",
    state.meta?.cwd ?? "",
    state.sessionGen,
    workspaceControllerEpoch,
  ].join("\u0000");
  // Workspace navigation belongs to the project, not to a single conversation.
  // A session switch inside the same project must therefore retain the dock,
  // tree and selection state.
  const workspaceTreeMemoryKey = [
    activeTab?.scope ?? "",
    activeTab?.workspaceRoot ?? state.meta?.cwd ?? "",
  ].join("\u0000");
  const restoreWorkspaceDockWidths = useCallback((treeWidth: number, _previewWidth: number) => {
    // Single-width dock: only the tree width is meaningful; clamp it to the
    // dynamic available width (chat keeps its 400px floor), never a fixed
    // 560 ceiling, so the user's remembered width is preserved when reopened.
    setRightDockTreeWidth(rightDockTreeWidthClamp(treeWidth, workspacePanelAvailableWidth));
  }, [rightDockTreeWidthClamp, workspacePanelAvailableWidth]);
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
    void app.GetTopicSummary({
      scope: activeTab.scope === "global" ? "global" : "project",
      workspaceRoot: activeTab.scope === "global" ? "" : activeTab.workspaceRoot,
      topicId: activeTab.topicId,
    })
      .then((topic) => {
        if (!cancelled) setActiveTopicTurns(topic.turns);
      })
      .catch(() => {
        if (!cancelled) setActiveTopicTurns(undefined);
      });
    return () => {
      cancelled = true;
    };
  }, [activeTab?.scope, activeTab?.topicId, activeTab?.workspaceRoot, projectRevision]);
  const visibleUserTurns = visibleRuntimeState.items.reduce((count, item) => (item.kind === "user" ? count + 1 : count), 0);
  const currentTabTurns = Math.max(visibleRuntimeState.checkpoints.length, visibleUserTurns);
  const sessionTurns = currentTabTurns > 0 ? currentTabTurns : remoteSurfaceActive ? 0 : activeTopicTurns ?? 0;
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
  const remoteComposerProfileReady = useRemoteComposerProfileSync({ activeTabId, remote: remoteSurfaceActive,
    remoteProfile: remoteSession.composerProfile, collaborationMode, toolApprovalMode, goal,
    qualityFloor: composerProfile.qualityFloor, pending: composerProfile.pending, setProfiles: setComposerProfilesByTab });
  const controllerReady =
    state.meta?.ready === true &&
    (!state.meta.runtime || state.meta.runtime.phase === "ready") &&
    !state.meta.startupErr &&
    !state.backendActivationPending &&
    !runtimeTransitioning;

  useEffect(() => {
    recordFrontendDiagnostic("app", "app.runtime-state", {
      ready: controllerReady,
      running: state.running,
      hydrating: state.hydrating,
      runtimeTransitioning,
      contentRevision: state.historyLayoutRevision,
    });
  }, [controllerReady, runtimeTransitioning, state.hydrating, state.historyLayoutRevision, state.running]);
  // Single footer decision surface. Composer stays mounted underneath and is
  // only visually/a11y-hidden so per-session draft caches survive.
  const decisionSurface = useMemo((): DecisionSurfaceKind | null => {
    if (state.approval) {
      return state.approval.tool === "exit_plan_mode" ? "plan_approval" : "tool_approval";
    }
    if (state.ask) return "ask";
    if (state.mcpInteraction) return "mcp_interaction";
    if (state.extensionForm) return "extension_form";
    if (workspaceConflict) return "workspace_conflict";
    if (pendingClose) return "close_active";
    if (clearContextPending) return "clear_context";
    return null;
  }, [clearContextPending, pendingClose, state.approval, state.ask, state.extensionForm, state.mcpInteraction, workspaceConflict]);
  const visibleDecisionSurface = decisionSurface;
  const composerSurfaceHidden = runtimeTransitioning || Boolean(decisionSurface);
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

  // Extension form surface (stage 8b2): submit delivers the structured values
  // to the owning sidecar; cancel reports values{"cancelled": true} over the
  // same channel. A failed cancel still dismisses — the sidecar that could not
  // be reached is gone either way.
  const [extensionFormBusy, setExtensionFormBusy] = useState(false);
  const submitExtensionForm = useCallback(async (values: Record<string, unknown>) => {
    const pending = state.extensionForm;
    if (!pending || !activeTabId || extensionFormBusy) return;
    setExtensionFormBusy(true);
    try {
      await app.SubmitExtensionForm(activeTabId, pending.pluginId, pending.surfaceId, values);
      dismissExtensionForm();
    } catch (err) {
      showToast(err instanceof Error ? err.message : String(err), "error");
    } finally {
      setExtensionFormBusy(false);
    }
  }, [activeTabId, dismissExtensionForm, extensionFormBusy, showToast, state.extensionForm]);
  const cancelExtensionForm = useCallback(async () => {
    const pending = state.extensionForm;
    if (!pending || extensionFormBusy) return;
    setExtensionFormBusy(true);
    try {
      if (activeTabId) {
        await app.SubmitExtensionForm(activeTabId, pending.pluginId, pending.surfaceId, { cancelled: true }).catch(() => {});
      }
      dismissExtensionForm();
    } finally {
      setExtensionFormBusy(false);
    }
  }, [activeTabId, dismissExtensionForm, extensionFormBusy, state.extensionForm]);

  // Extension notifications queue in per-tab state (the reducer cannot reach
  // the toast context); drain the active tab's queue into toasts here.
  useEffect(() => {
    const pending = state.extensionNotifications;
    if (!pending || pending.length === 0) return;
    for (const notification of pending) {
      const level = notification.severity === "error" ? "error" : notification.severity === "warn" ? "warn" : "info";
      showToast(notification.body ? `${notification.title} — ${notification.body}` : notification.title, level);
    }
    drainExtensionNotifications();
  }, [state.extensionNotifications, showToast, drainExtensionNotifications]);
  const extensionStatusList = useMemo(() => Object.values(state.extensionStatuses ?? {}), [state.extensionStatuses]);
  const patchActiveComposerProfile = useCallback(
    (patch: Partial<Omit<ComposerProfile, "pending">>, pendingFields: ComposerProfileField[]) => {
      if (!activeTabId) return;
      setComposerProfilesByTab((current) => patchComposerProfile(current, activeTabId, composerProfile, patch, pendingFields));
    },
    [activeTabId, composerProfile],
  );
  const patchComposerProfileForTab = useCallback(
    (tabId: string, patch: Partial<Omit<ComposerProfile, "pending">>, pendingFields: ComposerProfileField[]) => {
      if (!tabId) return;
      setComposerProfilesByTab((current) => {
        const base = current[tabId] ?? composerProfileFromTab(tabMetas.find((tab) => tab.id === tabId));
        return patchComposerProfile(current, tabId, base, patch, pendingFields);
      });
    },
    [tabMetas],
  );
  const topicbarEditing = Boolean(activeTab && (activeTab.remote ? activeTab.id : activeTab.topicId) === renamingTopicId && (activeTab.remote || activeTab.topicId));
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
    const activeRenameId = activeTab?.remote ? activeTab.id : activeTab?.topicId;
    if (!renamingTopicId || activeRenameId === renamingTopicId) return;
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

  const { applyMode, applyCollaborationMode, applyToolApprovalMode } = useComposerModeActions({
    activeTabId, remote: remoteSurfaceActive, collaborationMode, toolApprovalMode, goal,
    planIntentRef: userPlanModeByTabRef, yoloRestoreRef: yoloRestoreToolApprovalModesRef,
    patchProfile: patchActiveComposerProfile, setControllerMode: syncModeToController,
    setControllerCollaborationMode, setControllerToolApprovalMode, clearControllerGoal, drainRemoteApprovals: remoteSession.drainApprovals,
    showError: (message) => showToast(message, "error"),
  });
  const applyQualityFloor = useCallback(
    (floor: QualityFloor) => {
      if (!activeTabId) return;
      if (remoteSurfaceActive) {
        void remoteSession.setQualityFloor(floor).catch((error) => showToast(error instanceof Error ? error.message : String(error), "error"));
        return;
      }
      patchActiveComposerProfile({ qualityFloor: floor }, ["qualityFloor"]);
      void setControllerQualityFloor(floor);
    },
    [activeTabId, patchActiveComposerProfile, remoteSession, remoteSurfaceActive, setControllerQualityFloor, showToast],
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
  const patchActivatedGoalForTab = useCallback(
    (tabId: string, nextGoal: string): void => {
      const trimmed = nextGoal.trim();
      patchComposerProfileForTab(tabId, {
        collaborationMode: trimmed ? "goal" : "normal",
        goalDraftMode: false,
        goal: trimmed,
      }, ["collaborationMode", "goal"]);
      userPlanModeByTabRef.current = updateUserPlanModeIntent(userPlanModeByTabRef.current, tabId, false);
    },
    [patchComposerProfileForTab],
  );
  const applyGoalForTab = useCallback(
    async (tabId: string, nextGoal: string): Promise<void> => {
      if (!tabId) return;
      const trimmed = nextGoal.trim();
      // Activate the backend Goal first. Only then patch the local profile so a
      // failed SetGoalForTab cannot leave the Composer thinking a Goal is active.
      if (tabMetas.some((tab) => tab.id === tabId && tab.remote)) {
        await app.SetRemoteTabGoal(tabId, trimmed);
        patchActivatedGoalForTab(tabId, trimmed);
        return;
      }
      await (trimmed ? setControllerGoalForTab(tabId, trimmed) : clearControllerGoalForTab(tabId));
      patchActivatedGoalForTab(tabId, trimmed);
    },
    [clearControllerGoalForTab, patchActivatedGoalForTab, setControllerGoalForTab, tabMetas],
  );
  const applyGoal = useCallback(
    async (nextGoal: string): Promise<void> => {
      if (!activeTabId) return;
      await applyGoalForTab(activeTabId, nextGoal);
    },
    [activeTabId, applyGoalForTab],
  );
  const remoteComposerSend = useRemoteComposerSend(activeTab?.remote, activeTabId, collaborationMode, goal,
    remoteSession, remoteSend, applyGoalForTab, useCallback(() => setClearContextPending(true), []));
  const cancelRuntimeJob = useCallback(async (tabId: string, jobId: string): Promise<boolean> => {
    try {
      const cancelled = await app.CancelJobForTab(tabId, jobId);
      await refreshBackgroundRuntimes();
      return cancelled;
    } catch (err) {
      showToast(err instanceof Error ? err.message : String(err), "error");
      return false;
    }
  }, [refreshBackgroundRuntimes, showToast]);
  // Shift+Tab toggles only the collaboration axis; Ctrl/Cmd+Y toggles YOLO on the
  // tool-permission axis while preserving the Ask/Auto base mode.
  const cycleMode = useCallback(() => {
    runGoalAction(() => applyCollaborationMode(collaborationMode === "plan" ? "normal" : "plan"));
  }, [applyCollaborationMode, collaborationMode, runGoalAction]);
  // Switching models rebuilds the controller, which starts in normal mode — re-apply
  // it or the pill would say plan/YOLO while the fresh controller uses normal gating.
  const switchModel = useCallback(
    async (name: string) => {
      if (remoteSurfaceActive && activeTabId) return remoteSession.setModel(name).then(() => true);
      const switched = await setModel(name);
      if (!switched) return false;
      if (!activeTabId) return false;
      const profileApplied = await setControllerComposerProfileForTab(
        activeTabId,
        controllerComposerProfileCollaborationMode(composerProfile),
        toolApprovalMode,
        goal,
        { propagateError: true },
      );
      return profileApplied;
    },
    [activeTabId, composerProfile, goal, remoteSession, remoteSurfaceActive, setControllerComposerProfileForTab, setModel, toolApprovalMode],
  );

  // Startup and workspace/model rebuilds create a fresh controller in normal
  // mode. Re-apply the UI mode once the controller is ready, including the case
  // where the user picked YOLO while boot was still loading and the legacy
  // SetBypass binding was a harmless no-op.
  useEffect(() => {
    if (!controllerReady || !activeTabId || remoteSurfaceActive) return;
    runGoalAction(async () => {
      await setControllerComposerProfileForTab(
        activeTabId,
        controllerComposerProfileCollaborationMode(composerProfile),
        toolApprovalMode,
        goal,
        { propagateError: true },
      );
    });
  }, [activeTabId, composerProfile, controllerReady, goal, remoteSurfaceActive, runGoalAction, setControllerComposerProfileForTab, toolApprovalMode]);

  // The live task list pinned above the composer comes from the most recent
  // successful top-level todo_write result; failed or still-running attempts do
  // not advance the canonical panel state. Incomplete lists are always shown so
  // a stale local dismissal cannot hide work that still blocks final readiness;
  // every new list starts collapsed while its header keeps showing live progress
  // and the current task. Live completion briefly shows 3/3 before retirement;
  // restored completed lists stay in transcript only. The dismissal key is
  // still based on stable todo content/state so history reloads do not
  // resurrect the same finished list. The status-agnostic batch key prevents
  // false new batches; dismissal remains session-scoped and sidecar-persisted.
  const todoEntry = useMemo(() => {
    for (let i = visibleRuntimeState.items.length - 1; i >= 0; i--) {
      const it = visibleRuntimeState.items[i];
      if (it.kind === "tool" && it.name === "todo_write" && !it.parentId && it.status === "done" && !it.error) {
        return { item: it, index: i };
      }
    }
    return null;
  }, [visibleRuntimeState.items]);
  const todoItem = todoEntry?.item ?? null;
  const metaTodos = remoteSurfaceActive ? undefined : state.meta?.canonicalTodos;
  const todos = useMemo(
    () => resolveTodoPanelTodos(metaTodos, todoItem ? parseTodos(todoItem.args) : undefined),
    [metaTodos, todoItem],
  );
  const [dismissedTodoKeys, setDismissedTodoKeys] = useState<Set<string>>(loadDismissedTodoKeys);
  const todoKey = useMemo(() => todoDismissalKey(todos), [todos]);
  const todoBatch = useMemo(() => todoBatchKey(todos), [todos]);
  const todoScope = useMemo(
    () => todoPanelScope({ activeTab, activeTabId, eventChannel: remoteSurfaceActive ? undefined : state.meta?.eventChannel }),
    [activeTab, activeTabId, remoteSurfaceActive, state.meta?.eventChannel],
  );
  const dismissedTodo = useMemo(
    () => dismissedTodoKeyForScope(todoScope, dismissedTodoKeys, todoKey),
    [dismissedTodoKeys, todoKey, todoScope],
  );
  const scopedTodoKey = useMemo(() => scopedTodoDismissalKey(todoScope, todoKey), [todoKey, todoScope]);
  const scopedTodoBatch = useMemo(() => scopedTodoBatchKey(todoScope, todoBatch), [todoBatch, todoScope]);
  const showTodos = shouldShowTodoPanel(todoKey, dismissedTodo, todos, { batchKey: todoBatch, batches: !remoteSurfaceActive && state.meta?.sessionPath === activeTab?.sessionPath ? state.meta?.dismissedTodoBatches : undefined });
  const dismissTodos = useCallback(() => {
    if (!scopedTodoKey) return;
    setDismissedTodoKeys((current) => {
      if (current.has(scopedTodoKey)) return current;
      const next = new Set(current);
      next.add(scopedTodoKey);
      saveDismissedTodoKeys(next);
      return next;
    });
    if (!remoteSurfaceActive && activeTabId && todoBatch) void app.DismissTodoBatchForTab(activeTabId, todoBatch).catch(() => undefined);
  }, [activeTabId, remoteSurfaceActive, scopedTodoKey, todoBatch]);
  const handleTodoContinue = useCallback(() => {
    const targetTabId = todoContinueTarget(activeTabId, activeTabIdRef.current, {
      ready: remoteSurfaceActive ? remoteComposerReady : controllerReady,
      readOnly: Boolean(activeTab?.readOnly),
      running: visibleRuntimeState.running,
      pendingPrompt: visibleRuntimeState.pendingPrompt,
    });
    if (!targetTabId) return;
    const prompt = t("todo.continue");
    if (remoteSurfaceActive) {
      void remoteSend(prompt);
      return;
    }
    void sendToTab(targetTabId, prompt);
  }, [activeTab?.readOnly, activeTabId, controllerReady, remoteComposerReady, remoteSend, remoteSurfaceActive, sendToTab, t, visibleRuntimeState.pendingPrompt, visibleRuntimeState.running]);

  const sessionTitle = topicTitle(activeTab);
  const exportItems = remoteSurfaceActive ? remoteSession.transcript.items : state.items;
  const exportLive = remoteSurfaceActive
    ? remoteSession.transcript.live
    : liveStore.getSnapshot(activeTabId) ?? state.live;
  const sessionHasContent = exportItems.length > 0 || Boolean(exportLive?.text || exportLive?.reasoning);

  // Theme pack scene: home when the session is empty, task once content exists.
  useEffect(() => {
    applyThemeScene(sessionHasContent ? "task" : "home");
  }, [sessionHasContent]);
  const getSessionMarkdown = useCallback(
    async () => (await import("./lib/sessionExportData")).sessionItemsToMarkdown(sessionTitle, exportItems, exportLive),
    [exportItems, exportLive, sessionTitle],
  );
  const getSessionJson = useCallback(
    async () => (await import("./lib/sessionExportData")).sessionItemsToJson(sessionTitle, exportItems, exportLive),
    [exportItems, exportLive, sessionTitle],
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
            await app.SaveExportFile(path, await getSessionJson(), false);
            showToast(t("topicBar.exportSuccess", { count: 1 }), "info");
          }
        } else if (format === "pdf") {
          const path = await app.PickExportFile(`${base}.pdf`, "application/pdf");
          if (!path) return;
          const { blobToBase64, renderSessionPdfBlob } = await import("./lib/sessionExport");
          const blob = await renderSessionPdfBlob(await getSessionMarkdown(), sessionTitle);
          await app.SaveExportFile(path, await blobToBase64(blob), true);
          showToast(t("topicBar.exportSuccess", { count: 1 }), "info");
        } else if (format === "image") {
          const path = await app.PickExportFile(`${base}.png`, "image/png");
          if (!path) return;
          const { renderSessionImageBase64Payloads } = await import("./lib/sessionExport");
          const payloads = await renderSessionImageBase64Payloads(await getSessionMarkdown());
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
            await app.SaveExportFile(path, await getSessionMarkdown(), false);
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
      if (remoteSurfaceActive && activeTabId) {
        await app.ClearRemoteTabSession(activeTabId);
        await remoteSession.retryHydration();
      } else {
        await clearSession();
      }
      setDockRefreshKey((v) => v + 1);
      notice(t("clearContext.done"));
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      notice(msg || t("clearContext.failed"), "warn");
    }
  }, [activeTabId, clearSession, notice, remoteSession, remoteSurfaceActive, t]);

  useEffect(() => {
    activeTabIdRef.current = activeTabId;
  }, [activeTabId]);
  // handleSend reserves only commands that need a desktop-native UI action.
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
        await switchModel(model[1]);
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
      if (trimmed === "/new") {
        if (activeTabIdRef.current !== sourceTabId) return;
        await newSession();
        return;
      }
      const decisionMock = typeof window !== "undefined" && !window.runtime
        ? decisionSurfaceMockFromInput(trimmed)
        : null;
      if (decisionMock === "workspace_conflict" || decisionMock === "mode_jobs" || decisionMock === "close_active" || decisionMock === "clear_context") {
        if (activeTabIdRef.current !== sourceTabId) return;
        closeTransientOverlays();
        setWorkspaceConflict(null);
        setPendingClose(null);
        setClearContextPending(false);
        const mockWork: ActiveWorkView = {
          running: true,
          pendingPrompt: false,
          cancellable: true,
          jobs: [
            { id: "mock-decision-build", kind: "bash", label: "pnpm build", status: "running", startedAt: Date.now() - 42_000 },
            { id: "mock-decision-test", kind: "bash", label: "go test ./...", status: "running", startedAt: Date.now() - 18_000 },
          ],
        };
        if (decisionMock === "workspace_conflict") {
          setWorkspaceConflict({
            state: "local",
            ownerTabId: "mock-workspace-writer",
            ownerTitle: t("mock.topicDevStandard"),
            ownerWork: mockWork,
            canReveal: true,
            canCreateWorktree: true,
          });
        } else if (decisionMock === "close_active") {
          setPendingClose({ tabId: sourceTabId, work: mockWork, stopping: false });
        } else {
          setClearContextPending(true);
        }
        return;
      }
      const goalCommand = /^\/goal(?:\s+(.*))?$/.exec(trimmed);
      if (goalCommand) {
        const arg = (goalCommand[1] ?? "").trim();
        const displayGoal = stripLegacyGoalBudgetFlags(arg);
        if (displayGoal && !["status", "clear", "off", "stop", "done", "pause", "resume"].includes(displayGoal.toLowerCase())) {
          if (hasLegacyGoalBudgetFlag(arg)) {
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
        await activateGoalAndSubmitOnTab({
          tabId: sourceTabId,
          displayText: trimmed,
          submitText,
          structured,
          sendToTab: (tabId, nextGoal, display, routedSubmit, routedStructured) =>
            commitThenSendRef.current(
              tabId,
              display,
              routedSubmit,
              routedStructured,
              {
                goal: nextGoal,
                collaborationMode: controllerComposerProfileCollaborationMode(composerProfile),
                toolApprovalMode,
              },
            ),
        });
        patchActivatedGoalForTab(sourceTabId, trimmed);
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
      const profileApplied = await setControllerComposerProfileForTab(
        sourceTabId,
        controllerComposerProfileCollaborationMode(composerProfile),
        toolApprovalMode,
        goal,
      );
      if (!profileApplied) return;
      await commitThenSendRef.current(sourceTabId, trimmed, submitText.trim(), structured);
    },
    [activeTabId, applyGoal, closeTransientOverlays, collaborationMode, composerProfile, controllerReady, goal, notice, runShellForTab,
      patchActivatedGoalForTab, setControllerComposerProfileForTab, switchModel, t, toolApprovalMode, showToast],
  );

  const handleSteer = useCallback(async (text: string, requestedTabId = activeTabId) => {
    const sourceTabId = requestedTabId || activeTabId;
    if (!sourceTabId) throw new Error(t("composer.workspaceStarting"));
    if (tabMetas.some((tab) => tab.id === sourceTabId && tab.remote)) {
      await app.SteerRemoteTab(sourceTabId, text.trim());
      return;
    }
    await steerForTab(sourceTabId, text.trim());
  }, [activeTabId, steerForTab, t, tabMetas]);

  const setCollaborationModeFromUi = useCallback((mode: CollaborationMode) => {
    runGoalAction(() => applyCollaborationMode(mode));
  }, [applyCollaborationMode, runGoalAction]);
  const clearGoalFromUi = useCallback(() => {
    runGoalAction(() => applyGoal(""));
  }, [applyGoal, runGoalAction]);
  const { pauseGoal: pauseGoalFromUi, resumeGoal: resumeGoalFromUi, setEffort: setEffortFromUi } = useRemoteComposerRuntimeActions({
    activeTabIdRef, remote: remoteSurfaceActive, session: remoteSession, runGoalAction,
    pauseLocal: pauseControllerGoalForTab, resumeLocal: resumeControllerGoalForTab,
    setLocalEffort: setEffort, showError: (message) => showToast(message, "error"),
  });
  const switchModelFromUi = useCallback(async (name: string): Promise<boolean> => {
    try {
      return await switchModel(name);
    } catch (error) {
      handleGoalActionError(error);
      return false;
    }
  }, [handleGoalActionError, switchModel]);

  const tabMetaRefreshCoordinatorRef = useRef<ReturnType<typeof createBoundedRefreshCoordinator<TabMeta[]>> | null>(null);
  if (!tabMetaRefreshCoordinatorRef.current) {
    tabMetaRefreshCoordinatorRef.current = createBoundedRefreshCoordinator<TabMeta[]>(TAB_META_MAX_IN_FLIGHT);
  }
  const refreshTabMetas = useCallback(async (
    apply?: () => boolean,
    options?: { afterMutation?: boolean },
  ): Promise<TabMeta[]> => {
    const result = await tabMetaRefreshCoordinatorRef.current!.run(
      async () => asArray(await app.ListTabs().catch(() => [] as TabMeta[])),
      options?.afterMutation ? { invalidate: true } : undefined,
    );
    const tabs = result.value;
    if (result.latest && (!apply || apply())) {
      setTabMetas((current) => sameTabMetaLists(current, tabs) ? current : tabs);
    }
    return tabs;
  }, []);
  const seedActiveTabMeta = useCallback((tab: TabMeta): void => {
    setTabMetas((current) => seedActiveTabMetaList(current, tab));
    setTabOrderIds((current) => current.includes(tab.id) ? current : [...current, tab.id]);
  }, []);
  const updateRemoteTabMeta = useCallback((tab: TabMeta): void => {
    setTabMetas((current) => current.map((existing) => existing.id === tab.id
      ? { ...existing, ...tab, active: existing.active }
      : existing));
  }, []);

  useRemoteTabOpened(activeTabIdRef, seedActiveTabMeta, updateRemoteTabMeta, switchRemoteTab);

  useEffect(() => {
    const unsub = onEvent((e) => {
      if (shouldRefreshTabMetaForEvent(e.kind)) {
        void refreshTabMetas(undefined, { afterMutation: true });
      }
      if (e.kind !== "turn_done") return;
      const turnTabId = resolvePlanRestoreTabId(e.tabId, activeTabIdRef.current);
      window.setTimeout(() => {
        setProjectRevision((value) => value + 1);
        refreshTabMetas(undefined, { afterMutation: true }).then((tabs) => {
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
    let live = true;
    const ready = import("./lib/workspaceRefreshStore")
      .then(({ default: startWorkspaceFocusReconciliation }) => live ? startWorkspaceFocusReconciliation(activeTabId, workspaceScopeKey, refreshTabMetas) : undefined)
      .catch(() => undefined);
    const stopProjectTree = onProjectTreeChanged(() => {
      setProjectRevision((value) => value + 1);
      void refreshTabMetas(undefined, { afterMutation: true });
    });
    return () => {
      live = false;
      stopProjectTree();
      void ready.then((stop) => stop?.());
    };
  }, [activeTabId, refreshTabMetas, workspaceScopeKey]);

  // Bridge remote:* events into the remote store once, app-wide, so the
  // StatusBar chip, host manager, and explorer all see the same live state.
  useEffect(() => {
    const offStatus = onRemoteStatus((s) => {
      applyRemoteStatus(s);
      if (s.state === "stopped" && s.error) requestRemoteStatusPopover(s.hostId);
    });
    const offForwards = onRemoteForwards((e) => setRemoteForwards(e.hostId, e.forwards));
    const offServer = onRemoteServer((s) => setRemoteServer(s));
    return () => {
      offStatus();
      offForwards();
      offServer();
    };
  }, [applyRemoteStatus, requestRemoteStatusPopover, setRemoteForwards, setRemoteServer]);

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

  const refreshProviderSetupState = useCallback(async () => {
    const needs = await app.NeedsOnboarding();
    setProviderSetupNeeded(needs);
    return needs;
  }, []);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const needs = await app.NeedsOnboarding();
        if (!cancelled) {
          setProviderSetupNeeded(needs);
          setNeedsOnboarding(shouldOpenOnboarding(needs));
        }
      } catch {
        // Bridge unavailable (browser dev seam) — skip the gate; a real key
        // failure still surfaces via the topbar startupError banner.
        if (!cancelled) setNeedsOnboarding(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [setNeedsOnboarding]);

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
      const next = rightDockTreeWidthClamp(width, workspacePanelAvailableWidth);
      setRightDockTreeWidth(next);
      saveRightDockTreeWidth(next);
    },
    [closeTransientOverlays, rightDockTreeWidthClamp, workspacePanelAvailableWidth],
  );

  const ensureWorkspacePanelWidth = useCallback(
    (width: number) => {
      closeTransientOverlays();
      if (rightDockMode === "context") return;
      const next = rightDockTreeWidthClamp(width, workspacePanelAvailableWidth);
      setRightDockTreeWidth(next);
      saveRightDockTreeWidth(next);
    },
    [closeTransientOverlays, rightDockTreeWidthClamp, workspacePanelAvailableWidth],
  );

  const startWorkspacePanelResize = useCallback(
    (event: ReactPointerEvent<HTMLButtonElement>) => {
      if (event.button !== 0 || !workspacePanelOpen) return;
      const layout = layoutRef.current;
      if (!layout) return;
      event.preventDefault();
      workspacePanelResizeFinishRef.current?.();
      closeTransientOverlays();
      setWorkspacePanelResizing(true);
      const separator = event.currentTarget;
      const pointerId = event.pointerId;
      const startX = event.clientX;
      const startDockWidth = workspacePanelRenderWidth;
      let nextDockWidth = startDockWidth;
      const liveResize = createRafResizeUpdater({
        target: layout,
        separator,
        cssVar: "--workspace-width",
        onApply: setLiveWorkspacePanelRenderWidth,
      });
      const onMove = (moveEvent: PointerEvent) => {
        const delta = moveEvent.clientX - startX;
        nextDockWidth = startDockWidth - delta;
        nextDockWidth = rightDockTreeWidthClamp(nextDockWidth, workspacePanelAvailableWidth);
        liveResize.schedule(resolveLiveWorkspacePanelRenderWidth(nextDockWidth));
      };
      const lifecycle = createPointerResizeLifecycle({
        separator,
        pointerId,
        onMove,
        onFinish: () => {
          liveResize.flush();
          setSavedWorkspacePanelWidth(nextDockWidth);
          setLiveWorkspacePanelRenderWidth(null);
          setWorkspacePanelResizing(false);
          workspacePanelResizeFinishRef.current = null;
          document.body.style.cursor = "";
          document.body.style.userSelect = "";
        },
      });
      workspacePanelResizeFinishRef.current = lifecycle.finish;
      document.body.style.cursor = "col-resize";
      document.body.style.userSelect = "none";
    },
    [closeTransientOverlays, resolveLiveWorkspacePanelRenderWidth, rightDockDetailActive, rightDockTreeWidthClamp, setSavedWorkspacePanelWidth, workspacePanelAvailableWidth, workspacePanelOpen, workspacePanelRenderWidth],
  );

  const resizeWorkspacePanelWithKeyboard = useCallback(
    (event: KeyboardEvent<HTMLButtonElement>) => {
      if (event.key === "ArrowLeft" || event.key === "ArrowRight") {
        event.preventDefault();
        setSavedWorkspacePanelWidth(workspacePanelRenderWidth + (event.key === "ArrowLeft" ? 16 : -16));
      } else if (event.key === "Home") {
        event.preventDefault();
        setSavedWorkspacePanelWidth(rightDockTreeMinWidth);
      } else if (event.key === "End") {
        event.preventDefault();
        setSavedWorkspacePanelWidth(workspacePanelAvailableWidth);
      }
    },
    [rightDockDetailActive, rightDockTreeMinWidth, setSavedWorkspacePanelWidth, workspacePanelAvailableWidth, workspacePanelRenderWidth],
  );

  const terminalRenderHeight = clampTerminalHeight(terminalHeight, viewportHeight);
  const terminalResizeMaxHeight = terminalMaxHeight(viewportHeight);
  const setSavedTerminalHeight = useCallback(
    (height: number) => {
      const next = clampTerminalHeight(height, viewportHeight);
      setTerminalHeight(next);
      saveTerminalHeight(next);
    },
    [setTerminalHeight, viewportHeight],
  );

  const startTerminalResize = useCallback(
    (event: ReactPointerEvent<HTMLButtonElement>) => {
      if (!terminalPanelOpen) return;
      const layout = layoutRef.current;
      if (!layout) return;
      event.preventDefault();
      closeTransientOverlays();
      const startY = event.clientY;
      const startHeight = terminalRenderHeight;
      let nextHeight = startHeight;
      const liveResize = createRafResizeUpdater({
        target: layout,
        separator: event.currentTarget,
        cssVar: "--terminal-height",
        onApply: setLiveTerminalHeight,
      });
      const onMove = (moveEvent: PointerEvent) => {
        const delta = startY - moveEvent.clientY;
        nextHeight = clampTerminalHeight(startHeight + delta, viewportHeight);
        liveResize.schedule(nextHeight);
      };
      const onDone = () => {
        liveResize.flush();
        setLiveTerminalHeight(null);
        setSavedTerminalHeight(nextHeight);
        window.removeEventListener("pointermove", onMove);
        window.removeEventListener("pointerup", onDone);
        window.removeEventListener("pointercancel", onDone);
        document.body.style.cursor = "";
        document.body.style.userSelect = "";
      };
      document.body.style.cursor = "row-resize";
      document.body.style.userSelect = "none";
      window.addEventListener("pointermove", onMove);
      window.addEventListener("pointerup", onDone);
      window.addEventListener("pointercancel", onDone);
    },
    [closeTransientOverlays, setLiveTerminalHeight, setSavedTerminalHeight, terminalPanelOpen, terminalRenderHeight, viewportHeight],
  );

  const resizeTerminalWithKeyboard = useCallback(
    (event: KeyboardEvent<HTMLButtonElement>) => {
      if (!terminalPanelOpen) return;
      if (event.key === "ArrowUp" || event.key === "ArrowDown") {
        event.preventDefault();
        setSavedTerminalHeight(terminalRenderHeight + (event.key === "ArrowUp" ? 16 : -16));
      } else if (event.key === "Home") {
        event.preventDefault();
        setSavedTerminalHeight(TERMINAL_MIN_HEIGHT);
      } else if (event.key === "End") {
        event.preventDefault();
        setSavedTerminalHeight(terminalResizeMaxHeight);
      }
    },
    [setSavedTerminalHeight, terminalPanelOpen, terminalRenderHeight, terminalResizeMaxHeight],
  );

  const activeWorkspaceRoot = activeTab?.workspaceRoot ?? state.meta?.cwd ?? "";

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
      saveWorkspacePanelOpen(true, activeWorkspaceRoot);
    },
    [activeWorkspaceRoot, closeTransientOverlays, rightDockMode, workspacePanelMaximized, workspacePanelOpen],
  );

  const closeWorkspacePanel = useCallback(() => {
    closeTransientOverlays();
    if (!workspacePanelOpen) {
      return;
    }
    setLiveWorkspacePanelRenderWidth(null);
    setWorkspacePanelMaximized(false);
    setWorkspacePanelOpen(false);
    saveWorkspacePanelOpen(false, activeWorkspaceRoot);
  }, [activeWorkspaceRoot, closeTransientOverlays, workspacePanelOpen]);

  // Restore the right dock's open/closed state per project: switching to a
  // different workspace root (or a global session) restores that scope's own
  // preference instead of carrying the previous project's state over.
  useEffect(() => {
    setWorkspacePanelOpen(loadWorkspacePanelOpen(activeWorkspaceRoot));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeWorkspaceRoot]);

  const toggleWorkspacePanel = useCallback(() => {
    if (effectiveWorkspacePanelRenderable) {
      closeWorkspacePanel();
      return;
    }
    // Creation hides the overview tab; never reopen into the invisible "context"
    // mode or neither 文件/改动 will show an active selection.
    if (desktopLayoutStyle === "creation") {
      openWorkspacePanel(rightDockMode === "changed" ? "changed" : "files");
      return;
    }
    // Reopen with the previously active tab (rightDockMode is kept in the
    // store across close/open) instead of forcing "context".
    openWorkspacePanel();
  }, [closeWorkspacePanel, desktopLayoutStyle, effectiveWorkspacePanelRenderable, openWorkspacePanel, rightDockMode]);

  const openRightDockMode = useCallback(
    (mode: RightDockMode) => {
      openWorkspacePanel(mode);
    },
    [openWorkspacePanel],
  );

  const verificationRevealSequenceRef = useRef(0);
  const [verificationRevealRequest, setVerificationRevealRequest] = useState<WorkspaceVerificationRevealRequest | null>(null);
  const openTurnVerification = useCallback((summary: WireCompletionSummary) => {
    openRightDockMode("changed");
    verificationRevealSequenceRef.current += 1;
    setVerificationRevealRequest({
      id: verificationRevealSequenceRef.current,
      summary,
      tabId: activeTabId ?? "",
      turnStartAt: state.turnStartAt,
      currentSummary: state.completionSummary,
    });
  }, [activeTabId, openRightDockMode, state.completionSummary, state.turnStartAt]);

  useEffect(() => { setVerificationRevealRequest(null); }, [activeTabId, state.completionSummary, state.turnStartAt]);

  const toggleTerminalPanel = useCallback(() => { if (remoteSurfaceActive) return;
    setTerminalPanelOpen((prev) => {
      const next = !prev;
      saveTerminalPanelOpen(next);
      return next;
    });
  }, [remoteSurfaceActive, setTerminalPanelOpen]);

  const openTerminalForPath = useCallback(
    (path = ".") => { if (remoteSurfaceActive) return;
      setTerminalPanelOpen(true);
      saveTerminalPanelOpen(true);
      if (!activeTabId) return;
      void useTerminalStore.getState().createSession(activeTabId, path || ".", "default").catch(() => {});
    },
    [activeTabId, remoteSurfaceActive, setTerminalPanelOpen],
  );

  useGlobalShortcut("terminal.toggle", () => {
    toggleTerminalPanel();
  }, [toggleTerminalPanel]);
  useGlobalShortcut("terminal.newSession", () => {
    if (!activeTabId || remoteSurfaceActive) return;
    setTerminalPanelOpen(true); saveTerminalPanelOpen(true);
    void useTerminalStore.getState().createSession(activeTabId, ".", "default").catch(() => {});
  }, [activeTabId, remoteSurfaceActive, setTerminalPanelOpen]);

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

  const remoteWorkspaceLaunchGate = useRef(new RemoteWorkspaceLaunchGate());
  const launchRemoteWorkspace = useCallback(async (host: RemoteHostView, requestSeq: number) => {
    const lastWorkspace = await app.RemoteLastWorkspace(host.id).catch(() => "");
    const workspace = resolveRemoteWorkspace(lastWorkspace, host.defaultWorkspace);
    if (!remoteWorkspaceLaunchGate.current.isCurrent(host.id, requestSeq)) return;
    await publishNavigationIntent("remote-workspace");
    await app.OpenRemoteWorkspace(host.id, workspace);
  }, []);

  const openRemoteWorkspaceFromStatus = useCallback((host: RemoteHostView) => {
    const requestSeq = remoteWorkspaceLaunchGate.current.begin(host.id);
    void launchRemoteWorkspace(host, requestSeq).catch((err) => {
      showToast(err instanceof Error ? err.message : String(err), "error", { durationMs: 6000 });
    });
  }, [launchRemoteWorkspace, showToast]);

  const connectAndOpenRemoteWorkspace = useCallback(function connectRemoteWorkspace(host: RemoteHostView) {
    const requestSeq = remoteWorkspaceLaunchGate.current.begin(host.id);
    void (async () => {
      try {
        const status = useRemoteStore.getState().statuses[host.id]?.state;
        if (status !== "connected" && status !== "degraded") {
          // Clear any stale failure before the new generation starts; otherwise a
          // previous stopped+error snapshot could make the waiter reject before
          // the kernel's fresh connecting event reaches the frontend.
          useRemoteStore.getState().applyStatus({ hostId: host.id, state: "connecting" });
          await app.ConnectRemoteHost(host.id);
          await waitForRemoteConnection(host.id);
        }
      } catch (err) {
        if (err instanceof RemoteConnectionTimeoutError) {
          showToast(t("remote.error.timeout", { host: host.label }), "error", {
            actionLabel: t("remote.error.stopAndRetry"),
            durationMs: 10_000,
            onAction: () => {
              void app.DisconnectRemoteHost(host.id)
                .catch(() => undefined)
                .then(() => connectRemoteWorkspace(host));
            },
          });
          return;
        }
        // Connection failures are host-scoped. Keep the persistent error and its
        // recovery actions beside the Remote SSH status entry instead of
        // stretching a raw backend error across the native titlebar.
        requestRemoteStatusPopover(host.id);
        return;
      }

      try {
        await launchRemoteWorkspace(host, requestSeq);
      } catch (err) {
        showToast(err instanceof Error ? err.message : String(err), "error", { durationMs: 6000 });
      }
    })();
  }, [launchRemoteWorkspace, requestRemoteStatusPopover, showToast, t]);

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
        "--terminal-height": `${terminalSurfaceOpen ? liveTerminalHeight ?? terminalRenderHeight : 0}px`,
      }) as CSSProperties,
    [chatReservedWidth, liveTerminalHeight, sidebarRenderWidth, terminalRenderHeight, terminalSurfaceOpen, workspacePanelRenderWidth],
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

  const addTerminalOutputToComposer = useCallback(async (sessionId: string) => {
    if (!activeTabId) return;
    try {
      const output = await app.TerminalOutputForTab(activeTabId, sessionId);
      const formatted = formatTerminalOutputForComposer(output);
      if (!formatted) {
        showToast(t("terminal.noOutput"), "info");
        return;
      }
      addWorkspaceTextToComposer(formatted);
    } catch (error) {
      showToast(error instanceof Error ? error.message : String(error), "error");
    }
  }, [activeTabId, addWorkspaceTextToComposer, showToast, t]);

  const addSelectedTextToComposer = useCallback((text: string, source?: SelectedTextInsertRequest["source"]) => {
    const selected = text.trim();
    if (!activeTabId || !selected) return;
    selectedTextRequestIdRef.current += 1;
    setSelectedTextRequestsByTab((current) => ({
      ...current,
      [activeTabId]: { id: selectedTextRequestIdRef.current, text: selected, ...(source ? { source } : {}) },
    }));
  }, [activeTabId]);

  const addTerminalSelectionToComposer = useCallback((text: string) => addSelectedTextToComposer(text, "terminal"), [addSelectedTextToComposer]);
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
  const tabSwitchPendingRef = useRef<PendingNavigationRequest<{ tabId: string; optimisticTab?: TabMeta; navigationIntentSeq: number }> | null>(null);
  const enterChatViewForTabNavigation = useCallback(() => {
    setMainView("chat");
  }, [setMainView]);
  const enqueueTabSwitch = useCallback(
    (tabId: string, optimisticTab?: TabMeta): Promise<void> => {
      enterChatViewForTabNavigation();
      // Claim the shared navigation epoch at click time, before this request
      // can wait behind an older tab switch. That immediately invalidates any
      // in-flight blank/topic completion from a previous user intent.
      const navigationIntentSeq = noteNavigationIntent();
      beginNavigationSurface(navigationIntentSeq);
      return enqueueNavigationRequest(
        { seqRef: tabSwitchSeqRef, runningRef: tabSwitchRunningRef, pendingRef: tabSwitchPendingRef },
        { tabId, optimisticTab, navigationIntentSeq },
        async (request) => {
          try {
            if (!isNavigationIntentCurrent(request.navigationIntentSeq)) return;
            if (request.optimisticTab?.remote) await switchRemoteTab(request.optimisticTab, request.navigationIntentSeq);
            else await switchTab(request.tabId, request.optimisticTab, request.navigationIntentSeq);
            if (!isNavigationIntentCurrent(request.navigationIntentSeq)) return;
            await refreshTabMetas(
              () => isNavigationIntentCurrent(request.navigationIntentSeq),
              { afterMutation: true },
            );
          } finally {
            settleNavigationSurface(request.navigationIntentSeq);
          }
        },
      );
    },
    [beginNavigationSurface, enterChatViewForTabNavigation, isNavigationIntentCurrent, noteNavigationIntent, refreshTabMetas, settleNavigationSurface, switchRemoteTab, switchTab],
  );

  const revealBackgroundRuntime = useCallback(async (tabId: string): Promise<void> => {
    enterChatViewForTabNavigation();
    const navigationIntentSeq = noteNavigationIntent();
    beginNavigationSurface(navigationIntentSeq);
    try {
      const meta = await app.RevealBackgroundRuntime(tabId);
      if (!await guardBackendNavigationResult({
        intent: navigationIntentSeq,
        targetTabId: meta.id,
        kind: "tab.reveal-background",
        isIntentCurrent: isNavigationIntentCurrent,
        reassert: reassertVisibleTabAfterStaleNavigation,
      })) return;
      await switchTab(meta.id, meta, navigationIntentSeq);
      if (!isNavigationIntentCurrent(navigationIntentSeq)) return;
      await refreshTabMetas(
        () => isNavigationIntentCurrent(navigationIntentSeq),
        { afterMutation: true },
      );
    } catch (err) {
      if (isNavigationIntentCurrent(navigationIntentSeq)) showToast(err instanceof Error ? err.message : String(err), "error");
    } finally {
      settleNavigationSurface(navigationIntentSeq);
    }
  }, [beginNavigationSurface, enterChatViewForTabNavigation, isNavigationIntentCurrent, noteNavigationIntent, reassertVisibleTabAfterStaleNavigation, refreshTabMetas, settleNavigationSurface, showToast, switchTab]);

  const handleTabChange = useCallback((id: string) => {
    closeTransientOverlays();
    const selected = tabMetas.find((tab) => tab.id === id);
    setTabMetas((current) => current.map((tab) => ({ ...tab, active: tab.id === id })));
    void enqueueTabSwitch(id, selected);
    setTabRevealSignal((signal) => signal + 1);
  }, [closeTransientOverlays, enqueueTabSwitch, tabMetas]);

  const finishTabClose = useCallback(async (
    id: string,
    policy: "keep_running" | "stop_and_close",
  ): Promise<boolean> => {
    closeTransientOverlays();
    const closed = await closeTab(id, policy);
    if (!closed) {
      showToast(t("runtime.closeFailed"), "error");
      return false;
    }
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
    await refreshTabMetas(undefined, { afterMutation: true });
    await refreshBackgroundRuntimes();
    setTabRevealSignal((signal) => signal + 1);
    return true;
  }, [activeTabId, closeTab, closeTransientOverlays, refreshBackgroundRuntimes, refreshTabMetas, showToast, t]);

  const handleTabClose = useCallback(async (id: string) => {
    try {
      const work = await app.ActiveWorkForTab(id);
      if (work.running || work.pendingPrompt || work.jobs.length > 0) {
        setPendingClose({ tabId: id, work, stopping: false });
        return;
      }
    } catch {
      // CloseTabWithPolicy re-checks the controller state atomically.
    }
    await finishTabClose(id, "stop_and_close");
  }, [finishTabClose]);

  const resolvePendingClose = useCallback(async (policy: "keep_running" | "stop_and_close") => {
    const request = pendingClose;
    if (!request || request.stopping) return;
    if (policy === "stop_and_close") setPendingClose({ ...request, stopping: true });
    const closed = await finishTabClose(request.tabId, policy);
    if (closed) setPendingClose(null);
    else setPendingClose((current) => current?.tabId === request.tabId ? { ...current, stopping: false } : current);
  }, [finishTabClose, pendingClose]);

  const revealWorkspaceWriter = useCallback(async () => {
    if (!activeTabId) return;
    enterChatViewForTabNavigation();
    const navigationIntentSeq = noteNavigationIntent();
    beginNavigationSurface(navigationIntentSeq);
    try {
      const meta = await app.RevealWorkspaceWriterForTab(activeTabId);
      if (!await guardBackendNavigationResult({
        intent: navigationIntentSeq,
        targetTabId: meta.id,
        kind: "tab.reveal-workspace-writer",
        isIntentCurrent: isNavigationIntentCurrent,
        reassert: reassertVisibleTabAfterStaleNavigation,
      })) return;
      setWorkspaceConflict(null);
      await switchTab(meta.id, meta, navigationIntentSeq);
      if (!isNavigationIntentCurrent(navigationIntentSeq)) return;
      await refreshTabMetas(
        () => isNavigationIntentCurrent(navigationIntentSeq),
        { afterMutation: true },
      );
    } catch (err) {
      if (isNavigationIntentCurrent(navigationIntentSeq)) showToast(err instanceof Error ? err.message : String(err), "error");
    } finally {
      settleNavigationSurface(navigationIntentSeq);
    }
  }, [activeTabId, beginNavigationSurface, enterChatViewForTabNavigation, isNavigationIntentCurrent, noteNavigationIntent, reassertVisibleTabAfterStaleNavigation, refreshTabMetas, settleNavigationSurface, showToast, switchTab]);

  const continueInDeliveryWorktree = useCallback(async () => {
    const root = state.meta?.workspaceRoot || state.meta?.workspacePath || state.meta?.cwd;
    if (!root) return;
    cancel();
    setWorkspaceConflict(null);
    const navigationIntentSeq = noteNavigationIntent();
    beginNavigationSurface(navigationIntentSeq);
    try {
      await createIsolatedWorktree(root, navigationIntentSeq);
      await refreshTabMetas(undefined, { afterMutation: true });
    } catch (err) {
      if (isNavigationIntentCurrent(navigationIntentSeq)) showToast(err instanceof Error ? err.message : String(err), "error");
    } finally {
      settleNavigationSurface(navigationIntentSeq);
    }
  }, [beginNavigationSurface, cancel, createIsolatedWorktree, isNavigationIntentCurrent, noteNavigationIntent, refreshTabMetas, settleNavigationSurface, showToast, state.meta?.cwd, state.meta?.workspacePath, state.meta?.workspaceRoot]);

  const handleTabsClose = useCallback(async (ids: string[], nextActiveTabId?: string) => {
    closeTransientOverlays();
    const currentIds = tabMetas.map((tab) => tab.id);
    const targets = ids.filter((id, index) => currentIds.includes(id) && ids.indexOf(id) === index);
    if (targets.length === 0) return;
    for (const id of targets) {
      let work: ActiveWorkView | null = null;
      try {
        work = await app.ActiveWorkForTab(id);
      } catch { /* the close path remains authoritative */ }
      if (work && (work.running || work.pendingPrompt || work.jobs.length > 0)) {
        setPendingClose({ tabId: id, work, stopping: false });
        return;
      }
      await finishTabClose(id, "stop_and_close");
    }
    if (nextActiveTabId && currentIds.includes(nextActiveTabId)) {
      const selected = tabMetas.find((tab) => tab.id === nextActiveTabId);
      setTabMetas((current) => current.map((tab) => ({ ...tab, active: tab.id === nextActiveTabId })));
      void enqueueTabSwitch(nextActiveTabId, selected);
    }
    await refreshTabMetas(undefined, { afterMutation: true });
    setTabRevealSignal((signal) => signal + 1);
  }, [closeTransientOverlays, enqueueTabSwitch, finishTabClose, refreshTabMetas, tabMetas]);

  const handleTabsReorder = useCallback(async (ids: string[]) => {
    setTabOrderIds(ids);
    setTabMetas((current) => {
      const byId = new Map(current.map((tab) => [tab.id, tab]));
      const ordered = ids.map((id) => byId.get(id)).filter((tab): tab is TabMeta => Boolean(tab));
      return ordered.length === current.length ? ordered : current;
    });
    await reorderTabs(ids);
    await refreshTabMetas(undefined, { afterMutation: true });
    setTabRevealSignal((signal) => signal + 1);
  }, [refreshTabMetas, reorderTabs]);

  const [rewindSignal, setRewindSignal] = useState(0);

  // ── Immediate rewind ──────────────────────────────────────────────────
  // On confirm, call Go prepare+commit immediately. Only after success does
  // the UI truncate the transcript, refresh files, and fill the composer.
  // Real backend undo uses UndoRewindForTab when a transaction id is available.
  const [rewindStatesByTab, setRewindStatesByTab] = useState<Record<string, RewindUndoState>>({});
  const rewindStatesByTabRef = useRef(rewindStatesByTab);
  rewindStatesByTabRef.current = rewindStatesByTab;
  const [rewindCommittingByTab, setRewindCommittingByTab] = useState<Record<string, boolean>>({});
  const rewindState = activeTabId ? rewindStatesByTab[activeTabId] ?? null : null;
  const rewindCommitting = Boolean(activeTabId && rewindCommittingByTab[activeTabId]);

  const setRewindStateForTab = useCallback((tabId: string, nextState: RewindUndoState | null) => {
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

  const handleSessionRevertCommitted = useCallback((sourceTabId: string, outcome: RewindResultView) => {
    if (!sourceTabId || !outcome.ok) return;
    setRewindStateForTab(sourceTabId, {
      turnDiff: 0,
      transactionId: outcome.transactionId,
      undoAvailable: outcome.undoAvailable,
      filesRestored: outcome.written ?? [],
      filesRemoved: outcome.deleted ?? [],
    });
    setDockRefreshKey((value) => value + 1);
    setProjectRevision((value) => value + 1);
  }, [setRewindStateForTab]);

  const hydratePlaceholderActive = Boolean(
    state.hydrating &&
    state.items.length === 0 &&
    state.hydratePlaceholderItems?.length,
  );
  const transcriptHydrating = state.hydrating && !state.hydrateHistoryLoaded;
  // Creation hero only after history hydration settles on a truly empty session.
  // Avoid flash while switching tabs: items may be empty while placeholders show.
  // Exclude IM/Bot detail: hero CSS collapses .main, which also hosts that panel.
  // (desktopLayoutStyle is available here; sidebarCreation is declared later.)
  const creationEmptyHero =
    desktopLayoutStyle === "creation" &&
    !runtimeTransitioning &&
    !sidebarImDetailConnection &&
    !sessionHasContent &&
    !transcriptHydrating &&
    !hydratePlaceholderActive;
  const transcriptItems = hydratePlaceholderActive ? state.hydratePlaceholderItems! : state.items;
  const handleLoadOlderHistory = useCallback((targetTurn?: number, trigger: HistoryLoadTrigger = "retry") => {
    return activeTabId ? loadOlderHistory(activeTabId, targetTurn, trigger) : Promise.resolve(false);
  }, [activeTabId, loadOlderHistory]);

  // Display items: backend history is authoritative after immediate commit.
  // rewindState only drives the undo banner, not optimistic truncation.
  const displayItems = transcriptItems;
  // Keep a render-level snapshot of the last stable transcript. Navigation
  // starts before the controller swaps activeTabId, so this ref gives the
  // transition layer a synchronous, immutable surface to retain as its
  // background instead of rendering the target tab's empty state.
  if (!runtimeTransitioning) {
    renderedTranscriptSurfaceRef.current = {
      tabId: activeTabId,
      items: displayItems,
      geometrySessionKey: transcriptGeometrySessionKey,
    };
  }
  const visibleTranscriptSurface = runtimeTransitioning && !navigationTargetDataReady && preservedTranscriptSurface
    ? preservedTranscriptSurface
    : null;
  const visibleTranscriptItems = visibleTranscriptSurface?.items ?? displayItems;
  const visibleTranscriptTabId = visibleTranscriptSurface?.tabId ?? activeTabId;
  const visibleTranscriptGeometryKey = visibleTranscriptSurface?.geometrySessionKey ?? transcriptGeometrySessionKey;
  const surfaceCommitToken = navigationTargetDataReady && navigationSurfaceIntent !== null
    ? `navigation-${navigationSurfaceIntent}-${activeTabId ?? "blank"}`
    : undefined;
  const handleSurfacePaintReady = useCallback((token: string, outcome: "ready" | "degraded") => {
    const match = /^navigation-(\d+)-/.exec(token);
    if (!match) return;
    const intent = Number(match[1]);
    if (navigationSurface?.intent !== intent) return;
    if (singleSurfaceLayout && activeTabId) commitSingleSurfaceNavigation(activeTabId);
    commitNavigationSurfacePaint(intent, outcome);
  }, [activeTabId, commitNavigationSurfacePaint, commitSingleSurfaceNavigation, navigationSurface?.intent, singleSurfaceLayout]);
  const latestGuidanceConsumed = useMemo(() => {
    for (let i = state.items.length - 1; i >= 0; i--) {
      const item = state.items[i];
      if (item.kind === "notice" && item.text.startsWith("↪ ")) {
        return { key: item.id, itemId: item.inboxItemId, text: item.text.slice(2) };
      }
    }
    return null;
  }, [state.items]);

  // send wrapper: clear local undo banner state before sending a new turn
  // (new mutation invalidates undo). Rewind itself already committed immediately.
  const commitThenSend = useCallback(async (
    sourceTabId: string,
    displayText: string,
    submitText?: string,
    structured?: StructuredInvocationSubmit,
    initialGoal?: {
      goal: string;
      collaborationMode: CollaborationMode;
      toolApprovalMode: ToolApprovalMode;
    },
  ) => {
    const sourceTab = tabMetas.find((tab) => tab.id === sourceTabId);
    if (!sourceTab) throw new Error(t("composer.workspaceStarting"));
    if (sourceTab.readOnly) throw new Error(t("composer.readOnlyChannel"));
    if (
      sourceTab.ready !== true ||
      (sourceTab.runtime && sourceTab.runtime.phase !== "ready") ||
      sourceTab.startupErr
    ) {
      throw new Error(sourceTab.runtime?.issue?.message || sourceTab.startupErr || t("composer.workspaceStarting"));
    }
    // New turn invalidates the last undo slot.
    if (rewindStatesByTabRef.current[sourceTabId]) {
      setRewindStateForTab(sourceTabId, null);
    }
    await sendToTab(sourceTabId, displayText, submitText, undefined, structured, initialGoal);
  }, [sendToTab, setRewindStateForTab, t, tabMetas]);

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
        void refreshTabMetas(undefined, { afterMutation: true });
        setProjectRevision((v) => v + 1);
      });
      return;
    }

    // Code-only rewind only affects files — no message truncation,
    // no optimistic UI needed.  Execute immediately.
    if (scope === "code") {
      setRewindCommittingForTab(sourceTabId, true);
      void rewindForTabDetailed(sourceTabId, turn, scope).then((outcome) => {
        setRewindCommittingForTab(sourceTabId, false);
        if (!outcome.ok) return;
        setRewindStateForTab(sourceTabId, {
          turnDiff: 0,
          transactionId: outcome.transactionId,
          undoAvailable: outcome.undoAvailable,
          filesRestored: outcome.written ?? [],
          filesRemoved: outcome.deleted ?? [],
        });
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
    const userItem = items[boundaryIdx]?.kind === "user" ? items[boundaryIdx] as Extract<Item, { kind: "user" }> : undefined;
    const prompt = userItem?.text ?? "";

    // Immediate backend commit — only update UI after success.
    setRewindCommittingForTab(sourceTabId, true);
    void rewindForTabDetailed(sourceTabId, turn, scope).then((outcome) => {
      setRewindCommittingForTab(sourceTabId, false);
      if (!outcome.ok) {
        // Keep conversation/files as-is; notices already carry the reason.
        return;
      }
      const targetTabId = outcome.tabId || sourceTabId;
      setRewindStateForTab(targetTabId, {
        turnDiff: outcome.tabId ? 0 : turnDiff,
        transactionId: outcome.transactionId,
        undoAvailable: outcome.undoAvailable,
        undoTabId: sourceTabId,
        filesRestored: outcome.written ?? [],
        filesRemoved: outcome.deleted ?? [],
      });
      const insertId = Date.now();
      setComposerInsertRequestsByTab((current) => ({
        ...current,
        [targetTabId]: { id: insertId, text: prompt, mode: "replace" },
      }));
      setRewindSignal((v) => v + 1);
      if (scope === "both" || scope === "code") {
        setDockRefreshKey((v) => v + 1);
        setProjectRevision((v) => v + 1);
      }
    });
  }, [activeTab?.readOnly, activeTabId, hydratePlaceholderActive, state.items, rewindForTab, rewindForTabDetailed, refreshTabMetas, setRewindStateForTab, setRewindCommittingForTab]);

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
    const outcome = await rewindForTabDetailed(sourceTabId, turn, "conversation");
    if (!outcome.ok) return false;
    setRewindSignal((v) => v + 1);
    const targetTabId = outcome.tabId || sourceTabId;
    try {
      await sendToTab(targetTabId, next, submit, original);
      return true;
    } catch {
      return false;
    }
  }, [activeTab?.readOnly, activeTabId, clearContextPending, controllerReady, hydratePlaceholderActive, sendToTab, state.approval, state.ask, state.items, state.messageAction, state.running, rewindForTabDetailed]);

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
    const latest = () => request.seq === navigationSeqRef.current && isNavigationIntentCurrent(request.navigationIntentSeq);
    if (!latest()) return;
    const refreshLatestTabMetas = async (): Promise<TabMeta[]> => {
      const tabs = asArray(await app.ListTabs().catch(() => [] as TabMeta[]));
      if (latest()) setTabMetas(tabs);
      return tabs;
    };
    const openTopicTarget = async (scope: string, workspaceRoot: string, topicId: string, sessionPath?: string): Promise<TabMeta> => {
      if (singleSurfaceLayout) return activateTopic(scope, workspaceRoot, topicId, sessionPath || "", request.navigationIntentSeq);
      if (sessionPath) return openTopicSession(scope, workspaceRoot, topicId, sessionPath, request.navigationIntentSeq);
      if (scope === "global") return openGlobalTab(topicId, request.navigationIntentSeq);
      return openProjectTab(workspaceRoot, topicId, request.navigationIntentSeq);
    };
    const openBlankTarget = async (scope: string, workspaceRoot: string): Promise<TabMeta> => {
      const root = scope === "project" ? workspaceRoot : "";
      return singleSurfaceLayout
        ? ensureBlankSurface(scope, root, request.navigationIntentSeq)
        : ensureBlankTab(scope, root, request.navigationIntentSeq);
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

      if (request.kind === "isolated-worktree") {
        const result = await createIsolatedWorktree(request.workspaceRoot, request.navigationIntentSeq);
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
          await openChannelSession(target.value, openedTab.id, request.navigationIntentSeq);
        } else if (target.kind === "path") {
          openedTab = await openBlankTarget(connection.scope, connection.workspaceRoot);
          if (!latest()) return;
          await resumeSession(target.value, openedTab.id, request.navigationIntentSeq);
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
        await openChannelSession(session.path, targetTab.id, request.navigationIntentSeq);
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
      if (request.kind === "isolated-worktree") {
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
  }, [activateTopic, createIsolatedWorktree, ensureBlankSurface, ensureBlankTab, isNavigationIntentCurrent, openChannelSession, openGlobalTab, openProjectTab, openTopicSession, refreshHistoryView, resumeSession, seedActiveTabMeta, showToast, singleSurfaceLayout, t]);

  const enqueueNavigationWithIntent = useCallback((input: DesktopNavigationIntent, navigationIntentSeq: number): Promise<void> => {
    beginNavigationSurface(navigationIntentSeq);
    return enqueueNavigationRequest(
      { seqRef: navigationSeqRef, runningRef: navigationRunningRef, pendingRef: navigationPendingRef },
      { ...input, navigationIntentSeq } as DesktopNavigationInput,
      async (request) => {
        try {
          await runNavigationRequest(request);
        } finally {
          settleNavigationSurface(request.navigationIntentSeq);
        }
      },
    );
  }, [beginNavigationSurface, runNavigationRequest, settleNavigationSurface]);

  const enqueueNavigation = useCallback((input: DesktopNavigationIntent): Promise<void> => {
    // Any navigation (open topic / new session / resume) leaves the automation
    // view and returns to the chat workspace.
    setMainView("chat");
    // Invalidate any in-flight activation's stale apply at ENQUEUE time. The
    // queue serializes requests, so a click made while another request runs
    // only advances the controller's navigation epoch when it eventually
    // starts — too late: the running request's ActivateTopic would resolve,
    // pass the controller-local guard, flip the visible tab, and prune the
    // newer surface's cached state (#6613 review).
    const navigationIntentSeq = noteNavigationIntent();
    return enqueueNavigationWithIntent(input, navigationIntentSeq);
  }, [enqueueNavigationWithIntent, noteNavigationIntent, setMainView]);

  const openBlankSession = useCallback((scope: string, workspaceRoot: string): Promise<void> =>
    enqueueNavigation({ kind: "blank", scope, workspaceRoot: scope === "project" ? workspaceRoot : "" }),
  [enqueueNavigation]);

  const handleNewTab = useCallback(async () => {
    closeTransientOverlays();
    setSidebarImDetailConnectionId("");
    if (activeTab?.remote) return openRemoteNewSession(activeTab.remote, remoteSession.retryHydration);
    const target = blankSessionTarget();
    await openBlankSession(target.scope, target.workspaceRoot);
  }, [activeTab?.remote, blankSessionTarget, closeTransientOverlays, openBlankSession, remoteSession]);

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

  const onRecoveryCreated = useCallback(() => {
    setProjectRevision((value) => value + 1);
    void refreshTabMetas(undefined, { afterMutation: true });
  }, [refreshTabMetas]);
  const onRecoveryLineageChanged = useCallback(() => {
    setProjectRevision((value) => value + 1);
    void refreshHistoryView();
  }, [refreshHistoryView]);

  const openTaskMonitorSession = useCallback(async (tabID: string, taskID: string): Promise<boolean> => {
    if (state.running && !singleSurfaceLayout) {
      throw new Error(t("history.failedOpenSession"));
    }
    // Claim the navigation epoch before the first Wails await. If the user
    // switches tabs while the task/session lookup is pending, its completion is
    // stale and must not enqueue a newer navigation request.
    const navigationIntentSeq = noteNavigationIntent();
    beginNavigationSurface(navigationIntentSeq);
    let session: SessionMeta | null;
    try {
      session = await resolveTaskMonitorSession({
        tabID,
        taskID,
        intentSeq: navigationIntentSeq,
        isIntentCurrent: isNavigationIntentCurrent,
        openTaskSessionForTab: (sourceTabID, sourceTaskID) => app.OpenTaskSessionForTab(sourceTabID, sourceTaskID),
        listSessionsForTab: async (sourceTabID) => asArray(await app.ListSessionsForTab(sourceTabID)),
        sessionIDFromPath: taskSessionIDFromPath,
      });
    } catch (error) {
      settleNavigationSurface(navigationIntentSeq);
      throw error;
    }
    if (!session) {
      settleNavigationSurface(navigationIntentSeq);
      return false;
    }
    await enqueueNavigationWithIntent({ kind: "resume-session", session }, navigationIntentSeq);
    return isNavigationIntentCurrent(navigationIntentSeq);
  }, [beginNavigationSurface, enqueueNavigationWithIntent, isNavigationIntentCurrent, noteNavigationIntent, settleNavigationSurface, singleSurfaceLayout, state.running, t]);

  // Command palette: ⌘K / Ctrl+K opens a fuzzy navigator over commands and
  // recent sessions. Sessions are snapshotted on open so the list is stable
  // while the palette is up; extension actions follow the same snapshot rule.
  const openPalette = useCallback(async () => {
    closeTransientOverlays();
    setPaletteOpen(true);
    setPaletteSessions(await listSessions().catch(() => []));
    setPaletteExtensionActions(await app.ExtensionActions(activeTabIdRef.current ?? "").catch(() => []));
  }, [closeTransientOverlays, listSessions, setPaletteExtensionActions]);
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
      {
        id: "cmd-usage-stats",
        group: t("palette.group.commands"),
        title: t("palette.cmd.usageStats"),
        icon: <BarChart3 size={15} />,
        compact: true,
        keywords: ["usage", "stats", "statistics", "用量", "统计"],
        run: () => {
          setSettingsFocus((current) => ({
            target: "model-stats",
            requestId: (current?.requestId ?? 0) + 1,
          }));
          setSettingsTarget("models");
        },
      },
      { id: "cmd-task-center", group: t("palette.group.commands"), title: t("palette.cmd.taskCenter"), icon: <Activity size={15} />, compact: true, keywords: ["task", "tasks", "center", "任务", "任务中心"], run: () => setTasksOpen("all") },
      { id: "cmd-terminal", group: t("palette.group.commands"), title: t("rightDock.terminal"), icon: <TerminalSquare size={15} />, compact: true, keywords: ["terminal", "shell", "终端"], run: () => toggleTerminalPanel() },
      {
        id: "cmd-reload-runtime",
        group: t("palette.group.commands"),
        title: t("palette.cmd.reloadRuntime"),
        icon: <RotateCw size={15} />,
        compact: true,
        keywords: ["reload", "runtime", "重载", "运行时"],
        run: () => {
          const tabID = activeTab?.id;
          if (!tabID) return;
          // Success/queued feedback arrives as a tab notice; only hard failures need a toast.
          void app.ReloadRuntime(tabID).catch((err) => showToast(err instanceof Error ? err.message : String(err), "error"));
        },
      },
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
    const extensionItems: PaletteItem[] = paletteExtensionActions.map((action) => ({
      id: `ext-${action.slash}`,
      group: t("palette.group.extensions"),
      title: action.description || action.slash,
      hint: action.slash,
      icon: <Puzzle size={15} />,
      keywords: ["extension", "扩展", action.plugin, action.action, action.slash],
      run: () => {
        const tabID = activeTab?.id;
        if (!tabID) return;
        // The extension's result message is user-facing feedback; only hard
        // failures need an error toast.
        void app.InvokeExtensionAction(tabID, action.slash, {})
          .then((message) => {
            if (message) showToast(message, "info");
          })
          .catch((err) => showToast(err instanceof Error ? err.message : String(err), "error"));
      },
    }));
    return [...(remoteSurfaceActive ? cmds.filter((item) => item.id !== "cmd-terminal" && item.id !== "cmd-reload-runtime") : cmds), ...extensionItems, ...remoteItems, ...sessionItems];
  }, [t, paletteSessions, paletteExtensionActions, remoteHosts, remoteStatuses, activeTab?.id, handleNewTab, openTrash, onResumeSession, openRemoteWorkspaceFromStatus, connectAndOpenRemoteWorkspace, openRightDockMode, remoteSurfaceActive, showToast]);
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
  const onRenameHistorySession = useCallback(
    async (session: SessionMeta, title: string) => {
      if (state.running) return;
      if (session.topicId) await app.RenameTopic(session.topicId, title);
      else await renameSession(session.path, title);
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
  // Workspace: open the folder chooser and switch projects. The hook resets the
  // transcript and refreshes meta on a pick. A cancel is a no-op.
  const switchFolder = useCallback(async (path?: string) => {
    const navigationIntentSeq = noteNavigationIntent();
    beginNavigationSurface(navigationIntentSeq);
    try {
      const picked = path === undefined
        ? await pickWorkspace(navigationIntentSeq)
        : await switchWorkspace(path, navigationIntentSeq);
      if (!isNavigationIntentCurrent(navigationIntentSeq)) return picked;
      if (picked) {
        setProjectRevision((value) => value + 1);
        await refreshTabMetas(
          () => isNavigationIntentCurrent(navigationIntentSeq),
          { afterMutation: true },
        );
      }
      return picked;
    } finally {
      settleNavigationSurface(navigationIntentSeq);
    }
  }, [beginNavigationSurface, isNavigationIntentCurrent, noteNavigationIntent, pickWorkspace, refreshTabMetas, settleNavigationSurface, switchWorkspace]);

  const refreshProjectsAndTabs = useCallback(async () => {
    setProjectRevision((value) => value + 1);
    const tabs = await refreshTabMetas(undefined, { afterMutation: true });
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
    if (!activeTab?.remote && !activeTab?.topicId) return;
    topicRenameSkipCommitRef.current = false;
    topicRenameCommitHandledRef.current = false;
    setRenamingTopicId(activeTab.remote ? activeTab.id : activeTab.topicId);
    setTopicTitleDraft(activeTab.topicTitle || "");
  }, [activeTab?.id, activeTab?.remote, activeTab?.topicId, activeTab?.topicTitle]);

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
      if (await renameCurrentRemoteSession(activeTab, nextTitle)) return;
      await renameTopic(topicId, nextTitle);
    } catch {
      /* keep the app usable if a stale topic cannot be renamed */
    }
  }, [activeTab, renameTopic, renamingTopicId, topicTitleDraft]);

  const sidebarExpandBlocked = false;
  const sidebarToggleTitle = sidebarCollapsed
      ? t("sidebar.expand")
      : t("sidebar.collapse");
  const sidebarNavTooltipDisabled = !sidebarCollapsed;
  const browserPreviewChrome = typeof window !== "undefined" && !window.runtime;
  const browserMockScenario = browserPreviewChrome ? browserMockScenarioParam() : "";
  const guidanceQueueMockItems = isGuidanceMockScenario(browserMockScenario) ? GUIDANCE_QUEUE_MOCK_ITEMS : undefined;
  const workspacePanelResetWidth = desktopLayoutStyle === "creation"
    ? defaultCreationRightDockTreeWidth()
    : defaultRightDockTreeWidth();
  const workspacePanelResizeMinWidth = workspacePanelAriaMinWidth(workspacePanelMinWidth, workspacePanelRenderWidth);
  const workspacePanelResizeMaxWidth = workspacePanelAvailableWidth;
  const sidebarCreation = desktopLayoutStyle === "creation";
  // Command palette shortcut label (⌘K / Ctrl+K), platform-aware.
  const commandPaletteShortcut = formatShortcutCombo(
    resolvedShortcutCombo("commandPalette.open", desktopPlatform),
    desktopPlatform,
  );
  // Dock collapse/expand toggle. Rendered in the dock's own tools row when the
  // dock is open (its top-right corner), and in the topic bar when closed.
  const dockToggleButton = (
    <Tooltip label={surfaceWorkspacePanelRenderable ? t("rightDock.collapse") : t("rightDock.expand")}>
      <button
        className={[
          "topicbar__chrome-btn",
          "topicbar__chrome-btn--workspace",
          surfaceWorkspacePanelRenderable ? "topicbar__chrome-btn--active" : "",
        ].filter(Boolean).join(" ")}
        type="button"
        onClick={toggleWorkspacePanel}
        aria-label={surfaceWorkspacePanelRenderable ? t("rightDock.collapse") : t("rightDock.expand")}
        aria-pressed={surfaceWorkspacePanelRenderable}
      >
        <PanelRight size={15} />
      </button>
    </Tooltip>
  );
  const topicbarTitle = sidebarImDetailConnection ? t("botDetail.title", { name: sidebarImDetailConnection.title }) : topicDisplayTitle(activeTab);
  const topicbarWorkspaceLabel = sidebarImDetailConnection ? t("botDetail.subtitle") : activeTab ? tabWorkspaceTitle(activeTab) : "";
  const topicbarWorkspacePath = activeTab?.scope === "project" ? activeTab.workspaceRoot || state.meta?.cwd : "";
  const topicbarImSource = activeTab?.scope === "global" && activeTab.topicId ? imTopicSources[activeTab.topicId] : undefined;
  const topicbarImSourceLabel = sidebarImDetailConnection
    ? sidebarImDetailConnection.platformLabel
    : topicbarImSource ? t("msg.fromIm", { source: topicbarImSource.label }) : "";
  const topicbarImSourcePlatform = sidebarImDetailConnection?.platform ?? topicbarImSource?.platform;
  const topicbarSubtitleVisible = !sidebarCreation && Boolean(activeTab?.isolatedWorktree || topicbarImSourceLabel);
  const topicbarSubtitleTitle = sidebarImDetailConnection
    ? [topicbarWorkspaceLabel, topicbarImSourceLabel, sidebarImScopeLabel(sidebarImDetailConnection, t)].filter(Boolean).join(" · ")
    : [topicbarWorkspacePath || topicbarWorkspaceLabel, topicbarImSourceLabel].filter(Boolean).join(" · ");
  const topicbarCanRename = !sidebarImDetailConnection && (Boolean(activeTab?.topicId) || Boolean(activeTab?.remote));
  const topicbarTitleEditSize = Math.min(56, Math.max(4, topicTitleDraft.length || topicbarTitle.length || 1));
  const sidebarWorkbench = desktopLayoutStyle === "workbench";
  // The Wails drag runtime ignores anything with detail !== 1, so a double click
  // on a --wails-draggable region never reaches the OS. Both platforms that hide
  // their native title bar need this handled here.
  const chromeDoubleClickZooms = windowsFramelessChrome || desktopPlatform === "darwin";
  const handleChromeTitlebarDoubleClick = useCallback((event: ReactMouseEvent<HTMLDivElement>) => {
    if (!chromeDoubleClickZooms) return;
    const target = event.target as HTMLElement | null;
    const onChromeSurface = target?.closest(".app-chrome, .topicbar, .workbench-dock__tools");
    const onMacOSWorkbenchSidebarTitlebar = isMacOSWorkbenchSidebarTitlebar(target, event.clientY, desktopPlatform);
    if (!onChromeSurface && !onMacOSWorkbenchSidebarTitlebar) return;
    if (target?.closest("button, input, textarea, select, a, [role='button'], [role='tab'], .windows-window-controls")) return;
    event.preventDefault();
    void app.ToggleMaximiseMainWindow()
      .then(() => window.setTimeout(syncMainWindowMaximised, 80))
      .catch(() => undefined);
  }, [chromeDoubleClickZooms, desktopPlatform, syncMainWindowMaximised]);
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
    <UpdaterProvider>
    <ShellHotkeys />
    <TextSizeHotkeys />
      <div
        ref={appRef}
        onDoubleClickCapture={handleChromeTitlebarDoubleClick}
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
      {sidebarWorkbench && <div className="app__dock-toggle">{dockToggleButton}</div>}
      <div
        ref={layoutRef}
        className={[
          "layout",
          sidebarWorkbench ? "layout--workbench" : "",
          workbenchChromeHidden ? "layout--workbench-chrome-hidden" : "",
          sidebarCreation ? "layout--creation-chrome-hidden" : "",
          sidebarImDetailConnection ? "layout--statusbar-hidden" : "",
          sidebarCollapsed ? "layout--sidebar-collapsed" : "",
          sidebarResizing ? "layout--resizing layout--sidebar-resizing" : "",
          surfaceWorkspacePanelGridOpen ? "layout--workspace-open" : "",
          workspacePanelOverlay ? "layout--workspace-overlay" : "",
          "layout--terminal-drawer-open",
          terminalSurfaceOpen ? "layout--terminal-drawer-expanded" : "",
          terminalResizing ? "layout--terminal-resizing" : "",
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
            workspacePanelRenderable={surfaceWorkspacePanelRenderable}
            workspacePanelLabel={surfaceWorkspacePanelRenderable ? t("rightDock.collapse") : t("rightDock.expand")}
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
                  onClick={() => setMainView("automation")}
                >
                  <AlarmClock size={14} aria-hidden="true" />
                  <span>{t("sidebar.automation")}</span>
                </button>
              </div>
            </section>
          )}

          <section className="sidebar__section sidebar__section--projects">
            <Suspense fallback={null}><ProjectTree
                activeScope={activeTab?.scope}
                activeWorkspaceRoot={activeTab?.workspaceRoot}
                activeTopicId={activeTab?.topicId}
                activeSessionPath={activeTab?.sessionPath}
                activeRemote={activeTab?.remote}
                imTopicSources={imTopicSources}
                onOpenTopic={handleOpenTopic}
                onCreateTopic={(scope, workspaceRoot) => openBlankSession(scope, scope === "project" ? workspaceRoot : "")}
                onCreateIsolatedWorktree={(workspaceRoot) => enqueueNavigation({ kind: "isolated-worktree", workspaceRoot })}
                onTopicsChanged={refreshProjectsAndTabs}
                onRenameTopic={renameTopic}
                refreshSignal={projectRevision}
                onAddProject={async (path) => {
                  await switchFolder(path);
                }}
                timeFilter={topicTimeFilter}
                onTimeFilterChange={setTopicTimeFilter}
                variant={sidebarWorkbench ? "workbench" : sidebarCreation ? "creation" : "classic"}
                searchExpanded={!sidebarCreation || sidebarSearchOpen}
                searchFocusSignal={sidebarSearchFocusSignal}
                showShortcutBadges={showTopicBadges}
                shortcutPlatform={desktopPlatform}
                onVisibleTopicsChange={handleVisibleTopicsChange}
              /></Suspense>
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
                    onClick={() => setMainView("automation")}
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
                    onClick={() => setMainView("automation")}
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

        <section className={`chat-pane${creationEmptyHero ? " chat-pane--creation-empty" : ""}`}>
          {mainView === "automation" ? (
            <Suspense fallback={<div className="heartbeat-page" />}>
              <HeartbeatView onOpenTopic={(scope, workspaceRoot, topicId) => {
                void handleOpenTopic(scope, workspaceRoot, topicId);
              }} />
            </Suspense>
          ) : (
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
                      onFocus={(event) => event.currentTarget.select()}
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
                ) : topicbarCanRename ? (
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
                {topicbarWorkspaceLabel && (
                  <span className="topicbar__workspace-label" title={topicbarWorkspaceLabel}>
                    {topicbarWorkspaceLabel}
                  </span>
                )}
              </div>
              {topicbarSubtitleVisible && (
                <div className="topicbar__subtitle" title={topicbarSubtitleTitle}>
                  {activeTab?.isolatedWorktree && <WorktreeBadge size={11} />}
                  {activeTab?.isolatedWorktree && (
                    <button
                      type="button"
                      className="topicbar__worktree-btn"
                      onClick={() => setWorktreeMergeTabId(activeTab.id)}
                      title={t("worktree.mergeButtonTooltip")}
                    >
                      <span>{t("worktree.mergeAction")}</span>
                    </button>
                  )}
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
              <Tooltip label={`${t("shortcuts.action.commandPalette")} ${commandPaletteShortcut}`}>
                <button
                  className="topicbar__action-btn topicbar__action-btn--icon topicbar__action-btn--utility"
                  type="button"
                  aria-label={t("shortcuts.action.commandPalette")}
                  onClick={() => void openPalette()}
                >
                  <Search size={15} />
                </button>
              </Tooltip>
              {shouldMountExternalOpener(activeTab, Boolean(sidebarImDetailConnection)) && activeTab && (
                <ExternalOpener key={activeTab.id} tabId={activeTab.id} dismissSignal={transientOverlayDismissSignal} />
              )}
              {!sidebarImDetailConnection && (
                <TopicbarMoreMenu
                  sessionHasContent={sessionHasContent}
                  getSessionMarkdown={getSessionMarkdown}
                  exportSession={(format) => void exportSession(format)}
                  openChangedDock={() => openRightDockMode("changed")}
                  toggleTerminal={toggleTerminalPanel} terminalEnabled={!remoteSurfaceActive}
                  prefetchTerminal={prefetchTerminalPanel}
                  openSessionSummary={() => setTasksOpen((open) => open ? false : "session")}
                  tasksOpen={Boolean(tasksOpen)}
                />
              )}
              {sidebarCreation && dockToggleButton}
              {tasksOpen && (
                <div className="taskmonitor-popover" role="dialog" aria-label={t("summary.session")}>
                  <Suspense fallback={null}>
                    <TaskMonitorPanel
                      key={`${activeTab?.id || activeTabId || "none"}:${activeTab?.workspaceRoot || "global"}:${activeTab?.sessionPath || ""}:${tasksOpen}`}
                      tabID={activeTab?.id || activeTabId || ""}
                      initialOpen initialScope={tasksOpen || "session"}
                      popover
                      summaryMode
                      onClose={() => setTasksOpen(false)}
                      onOpenSession={openTaskMonitorSession}
                    />
                  </Suspense>
                </div>
              )}
            </div>
          </header>

          {activeTab?.takenOver ? (
            <RemoteReclaimBanner
              tabId={activeTab.id}
              busyTabId={reclaimBusyTab}
              onReclaim={(tabId) => {
                if (reclaimBusyTab) return;
                setReclaimBusyTab(tabId);
                (activeTab.remote ? app.ReclaimRemoteTabSession(tabId) : app.TakeoverSession(tabId, "wait"))
                  .catch((error) => console.warn("[takeover] reclaim failed", error))
                  .finally(() => setReclaimBusyTab(null));
              }}
            />
          ) : null}
          {(() => {
            const blocked = activeLeaseBlockedTab(tabMetas, activeTab?.id ?? activeTabId);
            if (blocked) {
              return (
                <div className="banner banner--error">
                  <span className="banner__msg">{t("topbar.startupError", { msg: blocked.runtime!.issue!.message })}</span>
                  <span className="banner__spacer" />
                  <button type="button" className="btn btn--small" onClick={() => setTakeoverDialogTab(blocked.id)}>
                    {t("takeover.bannerButton")}
                  </button>
                </div>
              );
            }
            return state.meta?.startupErr ? (
              <div className="banner banner--error">
                <span className="banner__msg">{t("topbar.startupError", { msg: state.meta.startupErr })}</span>
              </div>
            ) : null;
          })()}
          {takeoverDialogTab ? (
            <Suspense fallback={null}>
              <SessionTakeoverDialog tabId={takeoverDialogTab} onClose={() => setTakeoverDialogTab(null)} />
            </Suspense>
          ) : null}
          {configLoadWarnings.length > 0 && (
            <div className="banner banner--warning banner--actionable">
              <span className="banner__msg" title={configLoadWarnings.join("\n")}>
                {t("config.loadWarning", { msg: configLoadWarnings[0] })}
              </span>
              <span className="banner__spacer" />
              <button
                type="button"
                className="btn btn--small"
                onClick={() => void app.OpenUserConfigPath?.().catch(() => {})}
              >
                {t("config.openConfig")}
              </button>
              <button
                type="button"
                className="btn btn--small"
                onClick={() => {
                  void (async () => {
                    try {
                      const view = await app.ReloadUserConfig?.();
                      reloadConfigWarnings(view?.configWarnings, view?.configWarningsRevision);
                    } catch {
                      /* keep banner */
                    }
                  })();
                }}
              >
                {t("config.reloadConfig")}
              </button>
              <span className="banner__hint">{t("config.doctorHint")}</span>
              <button type="button" className="btn btn--small" onClick={dismissConfigWarnings}>
                {t("updater.dismiss")}
              </button>
            </div>
          )}
          {providerSetupNeeded && !needsOnboarding && (
            <div className="banner banner--warning banner--actionable">
              <span className="banner__msg">{t("onboarding.inlinePrompt")}</span>
              <span className="banner__spacer" />
              <button type="button" className="btn btn--small" onClick={() => {
                setSettingsFocus({ target: "model-access" });
                setSettingsTarget("models");
              }}>
                {t("onboarding.configureProvider")}
              </button>
            </div>
          )}

          <UpdateBanner
            enabled={startupUpdateChecksEnabled === true}
            onShowReleaseNotes={(latest) => {
              const version = latest.replace(/^(?:desktop-)?v/, "");
              void openExternal(`https://reasonix.io/changelog/v${version}/`);
            }}
          />

          <main className="main">
            {sidebarImDetailConnection && !runtimeTransitioning ? (
              <SidebarImConnectionDetail
                connection={sidebarImDetailConnection}
                onClose={() => setSidebarImDetailConnectionId("")}
                onOpenSettings={openBotSettings}
                onManageAllowlist={() => openBotAllowlistSettings(sidebarImDetailConnection.connectionId)}
                onOpenSession={() => void openSidebarImConnectionSession(sidebarImDetailConnection)}
              />
            ) : noticePreviewMockEnabled() ? (
              <NoticePreviewPanel />
            ) : activeTab?.remote ? (
              <Suspense fallback={null}><RemoteSessionSurface tab={activeTab} session={remoteSession} /></Suspense>
            ) : (
              <>
                <div className="transcript-navigation-surface" aria-busy={runtimeTransitioning}>
                  <div
                    className="transcript-navigation-content"
                    aria-hidden={runtimeTransitioning || undefined}
                    ref={(node) => {
                      if (!node) return;
                      (node as HTMLElement & { inert?: boolean }).inert = runtimeTransitioning;
                    }}
                  >
                    <Transcript
                      items={visibleTranscriptItems}
                      live={runtimeTransitioning ? undefined : state.live}
                      liveStore={liveStore}
                      tabId={visibleTranscriptTabId}
                      geometrySessionKey={visibleTranscriptGeometryKey}
                      footerHeight={footerHeight}
                      onPrompt={handleTranscriptPrompt}
                      onDeliveryContinue={() => void handleDeliveryContinue()}
                      onAcceptDelivery={() => void app.AcceptDeliveryToTab(activeTabIdRef.current ?? "")}
                      onOpenChanges={() => openRightDockMode("changed")}
                      onOpenVerification={openTurnVerification}
                      onEditPrompt={handleEditPrompt}
                      onRewind={handleMessageAction}
                      checkpoints={state.checkpoints}
                      actionPending={state.messageAction != null}
                      rewindDisabled={Boolean(activeTab?.readOnly) || !controllerReady || hydratePlaceholderActive || rewindState != null || rewindCommitting || state.running || state.messageAction != null || state.approval != null || state.ask != null || clearContextPending || runtimeTransitioning}
                      running={state.running || rewindCommitting}
                      turnStartAt={state.turnStartAt}
                      contentRevision={state.historyLayoutRevision}
                      historyMutation={state.historyMutation}
                      welcomeVariant={sidebarCreation ? "creation" : "default"}
                      creationMode={sidebarCreation}
                      actionHoverMenus={sidebarCreation && !hydratePlaceholderActive && !runtimeTransitioning}
                      rewindSignal={rewindSignal}
                      revealSignal={transcriptRevealSignal}
                      hydrating={transcriptHydrating || (runtimeTransitioning && !navigationTargetDataReady)}
                      hasOlderHistory={!runtimeTransitioning && state.historyHasOlder && !rewindState}
                      historyStartTurn={state.historyStartTurn}
                      historyTotalTurns={state.historyTotalTurns}
                      loadingOlderHistory={state.historyOlderLoading}
                      olderHistoryError={state.historyOlderError}
                      onLoadOlderHistory={handleLoadOlderHistory}
                      invocationMetadata={visibleTranscriptTabId ? invocationMetadataByTab[visibleTranscriptTabId] : undefined}
                      surfaceCommitToken={surfaceCommitToken}
                      onSurfacePaintReady={handleSurfacePaintReady}
                    />
                  </div>
                  {runtimeTransitioning ? (
                    <div className="transcript-navigation-overlay" role="status" aria-live="polite">
                      <span className="transcript-navigation-overlay__spinner" aria-hidden="true" />
                      <span>{t("common.loading")}</span>
                    </div>
                  ) : null}
                </div>
                {!runtimeTransitioning && state.hydrateError ? <div className="history-load-error" role="alert"><span>{state.hydrateError}</span><button type="button" className="btn btn--small" onClick={() => void retrySessionHistory(activeTabId)}>{t("common.retry")}</button></div> : null}
              </>
            )}
          </main>

          {!sidebarImDetailConnection && (
          <footer
            className={["footer", terminalSurfaceOpen && !sidebarCreation ? "footer--compact" : "", visibleDecisionSurface ? "footer--decision" : "", runtimeTransitioning ? "footer--navigation-hidden" : ""].filter(Boolean).join(" ")} ref={footerRef} style={navigationSurface?.phase === "source-retained" && footerHeight > 0 ? { height: footerHeight, minHeight: footerHeight, boxSizing: "border-box" } : undefined} inert={runtimeTransitioning || undefined} aria-hidden={runtimeTransitioning || undefined}
          >
            {showTodos && (
              <TodoPanel
                key={scopedTodoBatch}
                stateKey={scopedTodoBatch}
                todos={todos}
                running={visibleRuntimeState.running}
                pendingPrompt={visibleRuntimeState.pendingPrompt}
                onContinue={activeTabId && !activeTab?.readOnly && (remoteSurfaceActive ? remoteComposerReady : controllerReady) ? handleTodoContinue : undefined}
                onDismiss={dismissTodos}
              />
            )}
            {rewindState && (
              <Suspense fallback={null}><UndoRewindBanner
                meta={{
                  turns: rewindState.turnDiff,
                  filesRestored: rewindState.filesRestored ?? [],
                  filesRemoved: rewindState.filesRemoved ?? [],
                  onUndo: () => {
                    const tabId = activeTabId;
                    if (!tabId) return;
                    const tx = rewindState.transactionId;
                    const undoTabId = rewindState.undoTabId || tabId;
                    const undo = tx && rewindState.undoAvailable ? undoRewindForTab(undoTabId, tx) : Promise.resolve(true);
                    void undo.then((ok) => {
                      if (!ok) return;
                      setRewindStateForTab(tabId, null);
                      setComposerInsertRequestsByTab((current) => ({
                        ...current,
                        [tabId]: { id: Date.now(), text: "", mode: "replace" },
                      }));
                      setRewindSignal((v) => v + 1);
                      setDockRefreshKey((v) => v + 1);
                      setProjectRevision((v) => v + 1);
                    });
                  },
                }}
              /></Suspense>
            )}
            {visibleDecisionSurface === "tool_approval" || visibleDecisionSurface === "plan_approval"
              ? state.approval && (
              <ApprovalModal
                key={`${activeTabId ?? ""}:${state.approval.id}`}
                approval={state.approval}
                cwd={state.meta?.cwd}
                tabId={activeTabId}
                workspaceScopeKey={workspaceScopeKey}
                insertRequest={activePlanRevisionInsertRequest}
                onRevisionActiveChange={handleRevisionActiveChange}
                onAnswer={async (allow, session, persist) => {
                  // Approving an exit_plan_mode plan leaves plan mode; await the
                  // mode switch before sending the approval so the controller
                  // observes the updated state before it unblocks.
                  if (state.approval!.tool === "exit_plan_mode") {
                    if (allow) {
                      await applyCollaborationMode("normal");
                      resolvePlanDecision(state.approval!.id, "start_execution");
                    } else {
                      resolvePlanDecision(state.approval!.id, "revise_plan");
                    }
                    return;
                  }
                  approve(state.approval!.id, allow, session, persist);
                }}
                onResolveRecovery={(action, feedback) => {
                  resolveRecovery(state.approval!.id, action, feedback ?? "");
                }}
                onRevisePlan={(text) => {
                  if (activeTabId) {
                    setPendingPlanRevisionsByTab((current) => ({ ...current, [activeTabId]: text }));
                  }
                  resolvePlanDecision(state.approval!.id, "revise_plan");
                }}
                onExitPlan={async () => {
                  await applyCollaborationMode("normal");
                  resolvePlanDecision(state.approval!.id, "exit_plan");
                }}
                onStop={() => {
                  cancel();
                }}
                toolApprovalMode={toolApprovalMode}
              />
              )
            : visibleDecisionSurface === "ask"
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
            : visibleDecisionSurface === "mcp_interaction"
              ? state.mcpInteraction && (
              <Suspense fallback={null}>
                <MCPInteractionCard
                  key={`${activeTabId ?? ""}:${state.mcpInteraction.id}`}
                  interaction={state.mcpInteraction}
                  busy={false}
                  onAnswer={(id, action, content) => answerMCPInteraction(id, action, content)}
                  onOpenLink={(url) => openExternal(url)}
                />
              </Suspense>
              )
            : visibleDecisionSurface === "extension_form"
              ? state.extensionForm && (
              <Suspense fallback={null}>
                <ExtensionFormDialog
                  key={`${activeTabId ?? ""}:${state.extensionForm.pluginId}:${state.extensionForm.surfaceId}`}
                  surface={state.extensionForm}
                  busy={extensionFormBusy}
                  onSubmit={(values) => void submitExtensionForm(values)}
                  onCancel={() => void cancelExtensionForm()}
                />
              </Suspense>
              )
            : visibleDecisionSurface === "workspace_conflict" && workspaceConflict ? (
              <RuntimeDecisionCard
                id="workspace-conflict"
                title={t("runtime.workspaceConflictTitle")}
                badge={t("runtime.workspaceConflictBadge")}
                meta={workspaceConflict.state === "local"
                  ? t("runtime.workspaceConflictLocal", { title: workspaceConflict.ownerTitle || t("runtime.unknownTask"), label: workspaceConflict.ownerLabel || t("workspace.title") })
                  : t("runtime.workspaceConflictExternal")}
                note={t("runtime.workspaceConflictNote")}
                onCancel={() => {
                  cancel();
                  setWorkspaceConflict(null);
                }}
                actions={[
                  ...(workspaceConflict.canReveal ? [{
                    key: "1", label: t("runtime.revealWriter"), description: t("runtime.revealWriterDesc"),
                    onClick: () => void revealWorkspaceWriter(),
                  }] : []),
                  ...(workspaceConflict.canCreateWorktree ? [{
                    key: "2", label: t("runtime.openWorktree"), description: t("runtime.openWorktreeDesc"),
                    onClick: () => void continueInDeliveryWorktree(),
                  }] : []),
                ]}
                secondaryAction={{
                  key: "Esc", label: t("runtime.cancelWait"), description: t("runtime.cancelWaitDesc"),
                  onClick: () => { cancel(); setWorkspaceConflict(null); },
                }}
              />
            )
            : visibleDecisionSurface === "close_active" && pendingClose ? (
              <RuntimeDecisionCard
                id="close-active"
                title={t("runtime.closeTitle")}
                badge={t("status.jobs", { n: pendingClose.work.jobs.length })}
                meta={t("runtime.closeMeta")}
                onCancel={() => setPendingClose(null)}
                actions={[
                  {
                    key: "1", label: t("runtime.keepRunning"), description: t("runtime.keepRunningDesc"),
                    onClick: () => void resolvePendingClose("keep_running"), disabled: pendingClose.stopping,
                  },
                  {
                    key: "2", label: pendingClose.stopping ? t("status.jobStopping") : t("runtime.stopAndClose"),
                    description: t("runtime.stopAndCloseDesc"), onClick: () => void resolvePendingClose("stop_and_close"),
                    danger: true, disabled: pendingClose.stopping,
                  },
                ]}
                secondaryAction={{
                  key: "Esc", label: t("runtime.returnToTask"), description: t("runtime.closeCancelDesc"),
                  onClick: () => setPendingClose(null), disabled: pendingClose.stopping,
                }}
              />
            )
            : visibleDecisionSurface === "clear_context" ? (
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
              className={[
                "composer-decision-host",
                runtimeTransitioning
                  ? "composer-decision-host--footprint-hidden"
                  : composerSurfaceHidden
                    ? "composer-decision-host--hidden"
                    : "",
                creationEmptyHero ? "composer-decision-host--creation-hero" : "",
              ].filter(Boolean).join(" ")}
              hidden={Boolean(decisionSurface) || undefined}
              inert={composerSurfaceHidden ? true : undefined}
              aria-hidden={composerSurfaceHidden ? true : undefined}
            >
            {creationEmptyHero && (
              <h2 className="welcome-creation__headline">{t("welcome.creation.title")}</h2>
            )}
            <Composer
              running={remoteSurfaceActive ? remoteSession.running : state.running || rewindCommitting}
              collaborationMode={collaborationMode}
              toolApprovalMode={toolApprovalMode}
              qualityFloor={composerProfile.qualityFloor}
              floorInferred={(activeTab?.floorInferred ?? false) && !composerProfile.pending.qualityFloor}
              onSetQualityFloor={applyQualityFloor}
              turnPhase={visibleRuntimeState.turnPhase}
              goal={goal}
              goalStatus={remoteSurfaceActive ? remoteSession.composerProfile?.goalStatus : state.meta?.goalStatus}
              goalRuntime={remoteSurfaceActive ? remoteSession.goalRuntime : state.meta?.goalRuntime}
              cwd={state.meta?.cwd}
              modelLabel={remoteSurfaceActive ? remoteSession.modelLabel || activeTab?.label || t("status.connecting") : state.meta?.label ?? t("status.connecting")}
              commandCatalog={remoteSurfaceActive ? remoteSession.commands : undefined}
              imageInputEnabled={!remoteSurfaceActive && state.meta?.imageInputEnabled !== false}
              imageUnderstandingEnabled={state.meta?.visionFallbackEnabled === true}
              attachmentInputEnabled={!remoteSurfaceActive} pinnedFiles={state.meta?.pinnedFiles}
              tabId={activeTabId} turnId={remoteSurfaceActive ? undefined : state.activeTurnId}
              effort={remoteSurfaceActive ? remoteSession.effort : state.effort}
              onSend={remoteSurfaceActive ? remoteComposerSend : handleSend}
              onInvocationMetadataChange={handleInvocationMetadataChange}
              onSteer={handleSteer}
              localDurableGuidance={!remoteSurfaceActive}
              onCancel={remoteSurfaceActive ? remoteCancel : cancel}
              onCycleMode={cycleMode}
              onSetMode={applyMode}
              onSetCollaborationMode={setCollaborationModeFromUi}
              onSetToolApprovalMode={applyToolApprovalMode}
              onToggleYoloApprovalMode={toggleYoloApprovalMode}
              onClearGoal={clearGoalFromUi}
              onPauseGoal={pauseGoalFromUi}
              onResumeGoal={resumeGoalFromUi}
              onSwitchModel={switchModelFromUi}
              onSetEffort={setEffortFromUi}
              insertRequest={composerInsertRequest}
              selectedTextRequest={selectedTextRequest}
              readOnly={Boolean(activeTab?.readOnly)}
              disabled={runtimeTransitioning || rewindCommitting || state.messageAction != null || Boolean(decisionSurface)}
              submitDisabled={remoteSurfaceActive ? !remoteComposerReady || !remoteComposerProfileReady : !controllerReady}
              decisionPending={rewindCommitting || state.messageAction != null || Boolean(decisionSurface)}
              ready={remoteSurfaceActive ? remoteComposerReady && remoteComposerProfileReady : controllerReady}
              turnStartAt={visibleRuntimeState.turnStartAt}
              turnWaitAccumMs={visibleRuntimeState.turnWaitAccumMs}
              promptWaitStartedAt={visibleRuntimeState.promptWaitStartedAt}
              turnTokens={visibleRuntimeState.turnTokens}
              turnOutputTokens={visibleRuntimeState.turnOutputTokens}
              turnOutputCharsAtUsage={visibleRuntimeState.turnOutputCharsAtUsage}
              turnModelActiveAt={visibleRuntimeState.turnModelActiveAt}
              turnModelActiveMs={visibleRuntimeState.turnModelActiveMs}
              liveStore={remoteSurfaceActive ? remoteSession.liveStore : liveStore}
              turnArgChars={visibleRuntimeState.turnArgChars}
              retry={visibleRuntimeState.retry}
              suspendedByDecision={Boolean(decisionSurface)}
              transientDismissSignal={transientOverlayDismissSignal}
              sessionKey={composerSessionKey}
              workspaceScopeKey={workspaceScopeKey}
              fileRefRefreshKey={composerFileRefRefreshKey}
              guidanceConsumedKey={latestGuidanceConsumed?.key}
              guidanceConsumedItemId={latestGuidanceConsumed?.itemId}
              guidanceConsumedText={latestGuidanceConsumed?.text}
              guidanceQueuePreviewItems={guidanceQueueMockItems}
              showContextWindowRing={sidebarCreation}
              heroMode={creationEmptyHero}
              context={visibleRuntimeState.context}
              turnCost={visibleRuntimeState.turnCost}
              turnRateBand={visibleRuntimeState.turnRateBand}
              currency={visibleRuntimeState.sessionCurrency}
              cacheHitTokens={visibleRuntimeState.usage?.cacheHitTokens}
              cacheMissTokens={visibleRuntimeState.usage?.cacheMissTokens}
              balance={visibleRuntimeState.balance}
            />
            </div>
          </footer>
          )}
          </>
          )}
        </section>

        {surfaceWorkspacePanelGridOpen && (
          <button
            className="workspace-panel-resizer"
            type="button"
            role="separator"
            aria-orientation="vertical"
            aria-label={t("rightDock.resize")}
            aria-valuemin={workspacePanelResizeMinWidth}
            aria-valuemax={Math.max(workspacePanelResizeMaxWidth, workspacePanelRenderWidth)}
            aria-valuenow={workspacePanelRenderWidth}
            onPointerDown={startWorkspacePanelResize}
            onKeyDown={resizeWorkspacePanelWithKeyboard}
            onDoubleClick={() => setSavedWorkspacePanelWidth(workspacePanelResetWidth)}
          />
        )}

        {surfaceWorkspacePanelRenderable && (
          <aside
            className={[
              "workbench-dock",
              `workbench-dock--${rightDockMode}`,
              workspacePanelOverlay ? "workbench-dock--overlay" : "",
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
                <Suspense fallback={null}>
                  <ContextPanel
                    tabId={remoteSurfaceActive ? undefined : activeTabId}
                    items={exportItems}
                    context={visibleRuntimeState.context}
                    usage={visibleRuntimeState.usage}
                    sessionTokens={visibleRuntimeState.sessionTokens}
                    sessionCost={visibleRuntimeState.sessionCost}
                    sessionCurrency={visibleRuntimeState.sessionCurrency}
                    sessionTurns={sessionTurns}
                    turnTokens={visibleRuntimeState.turnTotalTokens}
                    turnCost={visibleRuntimeState.turnCost}
                    turnRateBand={visibleRuntimeState.turnRateBand}
                    balance={visibleRuntimeState.balance}
                    sessionGen={visibleRuntimeState.sessionGen}
                    refreshKey={dockRefreshKey + visibleRuntimeState.contextPanelSeq}
                    usageSeq={visibleRuntimeState.usageSeq}
                  />
                </Suspense>
              ) : (
                <Suspense fallback={null}>
                  <WorkspacePanel
                    key={workspaceTreeMemoryKey}
                    open={surfaceWorkspacePanelRenderable}
                    tabId={activeTabId}
                    cwd={state.meta?.cwd}
                    workspaceScopeKey={workspaceScopeKey}
                    workspaceMemoryKey={workspaceTreeMemoryKey}
                    dockTreeWidth={rightDockTreeWidth}
                    dockPreviewWidth={rightDockPreviewWidth}
                    onRestoreDockWidths={restoreWorkspaceDockWidths}
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
                    onSessionRevertCommitted={handleSessionRevertCommitted}
                    onOpenInTerminal={remoteSurfaceActive ? undefined : openTerminalForPath}
                    initialViewMode={rightDockMode === "changed" ? "changed" : "files"}
                    completionSummary={state.completionSummary}
                    turnStartAt={state.turnStartAt}
                    verificationRevealRequest={verificationRevealRequest}
                    qualityFloor={composerProfile.qualityFloor}
                    showViewTabs={false}
                    creationMode={sidebarCreation}
                  />
                </Suspense>
              )}
            </div>
          </aside>
        )}
        <>
          <aside
            className="terminal-drawer"
            aria-label={t("terminal.title")}
            aria-hidden={!terminalSurfaceOpen} inert={!terminalSurfaceOpen ? true : undefined}
          >
            {!remoteSurfaceActive && terminalContentVisible && (
              <Suspense fallback={<div className="terminal-empty"><span className="terminal-empty__spinner" />{t("terminal.loading")}</div>}>
                <TerminalPanel
                  tabId={activeTabId ?? ""}
                  cwd={state.meta?.cwd}
                  readOnly={Boolean(activeTab?.readOnly)}
                  open={terminalSurfaceOpen} fitEnabled={terminalFitEnabled}
                  onClose={() => {
                    setTerminalPanelOpen(false);
                    saveTerminalPanelOpen(false);
                  }}
                  onAddOutput={(sessionId) => void addTerminalOutputToComposer(sessionId)}
                  onAddToChat={addTerminalSelectionToComposer}
                />
              </Suspense>
            )}
          </aside>
          <button
            className="terminal-drawer-resizer"
            type="button"
            role="separator"
            aria-orientation="horizontal"
            aria-label={t("terminal.resize")}
            aria-valuemin={TERMINAL_MIN_HEIGHT}
            aria-valuemax={terminalResizeMaxHeight}
            aria-valuenow={liveTerminalHeight ?? terminalRenderHeight}
            aria-hidden={!terminalSurfaceOpen}
            tabIndex={terminalSurfaceOpen ? 0 : -1}
            onPointerDown={startTerminalResize}
            onKeyDown={resizeTerminalWithKeyboard}
            onDoubleClick={() => {
              setSavedTerminalHeight(TERMINAL_DEFAULT_HEIGHT);
            }}
          />
        </>

        {!sidebarImDetailConnection && (
          <StatusBar
            context={visibleRuntimeState.context}
            usage={visibleRuntimeState.usage}
            balance={visibleRuntimeState.balance}
            running={visibleRuntimeState.running || (!remoteSurfaceActive && rewindCommitting)}
            jobs={visibleRuntimeState.jobs}
            onCancelJob={remoteSurfaceActive ? remoteSession.cancelJob : cancelJob}
            backgroundRuntimes={remoteSurfaceActive ? [] : backgroundRuntimes}
            onCancelRuntimeJob={cancelRuntimeJob}
            onRevealRuntime={revealBackgroundRuntime}
            sessionTurns={sessionTurns}
            sessionTokens={visibleRuntimeState.sessionTokens}
            turnTokens={visibleRuntimeState.turnTotalTokens}
            lastTurnOutputTokens={visibleRuntimeState.lastTurnOutputTokens}
            lastTurnModelMs={visibleRuntimeState.lastTurnModelMs}
            lastTurnOutputEstimated={visibleRuntimeState.lastTurnOutputEstimated}
            lastRequestTps={visibleRuntimeState.lastRequestTps}
            turnCost={visibleRuntimeState.turnCost}
            turnRateBand={visibleRuntimeState.turnRateBand}
            cost={visibleRuntimeState.sessionCost}
            currency={visibleRuntimeState.sessionCurrency}
            modelLabel={remoteSurfaceActive ? remoteSession.modelLabel || activeTab?.label : state.meta?.label}
            labelStyle={statusBarStyle}
            items={statusBarItems}
            extensionStatuses={extensionStatusList}
            workspacePath={remoteSurfaceActive ? activeTab?.remote?.workspace : state.meta?.workspacePath || state.meta?.workspaceRoot || state.meta?.cwd}
            workspaceName={remoteSurfaceActive ? activeTab?.workspaceName : state.meta?.workspaceName}
            gitBranch={remoteSurfaceActive ? undefined : state.meta?.gitBranch}
            onConnectRemote={connectAndOpenRemoteWorkspace}
            onDisconnectRemote={(hostId) => void app.DisconnectRemoteHost(hostId).catch(() => {})}
            onManageRemote={() => setSettingsTarget("remote")}
            onOpenRemote={requestRemoteExplorer}
            onOpenRemoteWorkspace={openRemoteWorkspaceFromStatus}
            remoteHosts={remoteHosts}
            remoteStatuses={remoteStatuses}
          />
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
            onRename={onRenameHistorySession}
            onRestore={onRestoreTrashedSession}
            onPurge={onPurgeTrashedSession}
            onPurgeAll={onPurgeAllTrashedSessions}
            onInspectVersions={requestSessionVersions}
            onClose={closeHistory}
          />
        </Suspense>
      )}

      <Suspense fallback={null}>
        <SessionRecoveryVersionsHost
          sessions={histView?.sessions}
          onResumeSession={onResumeSession}
          onRecoveryCreated={onRecoveryCreated}
          onLineageChanged={onRecoveryLineageChanged}
        />
      </Suspense>

      {settingsTarget !== null && (
        <Suspense fallback={null}>
          <SettingsPanel
            initialTab={settingsTarget}
            initialFocus={settingsFocus ?? undefined}
            agentRunning={state.running}
            desktopPlatform={desktopPlatform}
            activeWorkspaceKey={`${activeTab?.id ?? activeTabId ?? ""}\u0000${activeTab?.workspaceRoot ?? activeTab?.cwd ?? state.meta?.cwd ?? ""}`}
            onUseSubagent={prefillSubagentCommand}
            onClose={() => {
              setSettingsFocus(null);
              setSettingsTarget(null);
            }}
            onChanged={(settings) => {
              void refreshMeta();
              void refreshProviderSetupState().catch(() => {});
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
      <RemoteSecretDialog />

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

      {needsOnboarding && (
        <OnboardingOverlay
          onComplete={() => {
            setProviderSetupNeeded(false);
            setNeedsOnboarding(false);
          }}
          onChooseProvider={() => {
            setNeedsOnboarding(false);
            setSettingsFocus({ target: "model-access" });
            setSettingsTarget("models");
          }}
          onSkip={() => {
            dismissOnboarding();
            setNeedsOnboarding(false);
          }}
        />
      )}

      <Suspense fallback={null}>
        <TranscriptSelectionMenu
          enabled={Boolean(activeTabId && !activeTab?.readOnly && !decisionSurface && !sidebarImDetailConnection && !hydratePlaceholderActive)}
          resetKey={activeTabId ?? ""}
          onAddToChat={addSelectedTextToComposer}
        />
      </Suspense>
      {worktreeMergeTabId && (
        <Suspense fallback={null}>
          <WorktreeMergeModal
            tabId={worktreeMergeTabId}
            isOpen={Boolean(worktreeMergeTabId)}
            onClose={() => setWorktreeMergeTabId(null)}
            onMerged={async (res) => {
              const tabToClose = worktreeMergeTabId;
              if (!tabToClose || !res.sourceRoot || !res.worktreeRoot || !res.targetBranch || !res.mergedCommit || !res.worktreeBranch || !res.worktreeHead) {
                throw new Error(res.error || t("worktree.mergeReceiptInvalid"));
              }
              const navigationIntentSeq = noteNavigationIntent();
              try {
                const navigationIntentToken = await registeredNavigationIntent(navigationIntentSeq);
                if (!navigationIntentToken || !isNavigationIntentCurrent(navigationIntentSeq)) {
                  showToast(t("worktree.navigationChangedPreserved"), "error", { durationMs: 9000 });
                  return;
                }
                const lifecycle = await runWorktreeMergeLifecycle(res, tabToClose, navigationIntentToken, {
                  ensureSource: (sourceRoot) => singleSurfaceLayout
                    ? ensureBlankSurface("project", sourceRoot, navigationIntentSeq)
                    : ensureBlankTab("project", sourceRoot, navigationIntentSeq),
                  isNavigationCurrent: () => isNavigationIntentCurrent(navigationIntentSeq),
                  seedSource: seedActiveTabMeta,
                  listTabs: () => app.ListTabs(),
                  closeWorktree: (request) => app.CloseMergedWorktreeTab(request),
                  finalize: (request) => app.FinalizeWorktreeMerge(request),
                  onNavigationPreserved: () => showToast(t("worktree.navigationChangedPreserved"), "error", { durationMs: 9000 }),
                  onCloseBlocked: () => showToast(t("worktree.cleanupViewBlocked"), "error", { durationMs: 8000 }),
                });
                if (lifecycle.phase !== "finalized") return;
                showWorktreeCleanupNotice(lifecycle.cleanup, t, showToast);
              } catch (caught: unknown) {
                showToast(`${t("worktree.mergeDoneCleanupFailed")} ${caught instanceof Error ? caught.message : String(caught)}`, "error", { durationMs: 9000 });
              }
            }}
          />
        </Suspense>
      )}
      {windowsFramelessChrome && (
        <WindowsWindowControls
          maximised={mainWindowMaximised}
          syncMaximised={syncMainWindowMaximised}
        />
      )}
    </div>
    </UpdaterProvider>
    </ShellExpandProvider>
  );
}
