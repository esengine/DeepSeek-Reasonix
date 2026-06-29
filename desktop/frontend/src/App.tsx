import { lazy, Suspense, useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { CSSProperties, KeyboardEvent, PointerEvent as ReactPointerEvent } from "react";
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
  Download,
  Search,
  SquarePen,
  PanelLeft,
  PanelRight,
  FileDown,
  FileImage,
  FileText,
  FileJson,
  GitBranch,
  History,
  MessageSquare,
  Settings as SettingsIcon,
  Pencil,
  Trash2,
  AlarmClock,
  Brain,
  Cpu,
  Palette,
} from "lucide-react";
import { useToast } from "./lib/toast";
import { asArray } from "./lib/array";
import { clearLegacyLangPref, normalizeLangPref, readLegacyLangPref, useI18n, useT, type Translator } from "./lib/i18n";
import { useController, type Item, type LiveStream } from "./lib/useController";
import { app, onEvent, onProjectTreeChanged } from "./lib/bridge";
import { generativeMusic, isGenerativeMusicEnabled } from "./lib/generative-music";
import { playSuccessChime } from "./lib/sound";
import { Transcript } from "./components/Transcript";
import { Composer } from "./components/Composer";
import { TodoPanel } from "./components/TodoPanel";
import { ApprovalModal } from "./components/ApprovalModal";
import { AskCard } from "./components/AskCard";
import { UndoRewindBanner } from "./components/UndoRewindBanner";
import { ClearContextCard } from "./components/ClearContextCard";
import { StatusBar } from "./components/StatusBar";
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
import { HeartbeatPanel } from "./custom/features/heartbeat/HeartbeatPanel";
import "./custom/features/heartbeat/heartbeat.css";
import { CopyButton } from "./components/CopyButton";
import { parseTodos } from "./lib/tools";
import { shouldShowTodoPanel, todoDismissalKey } from "./lib/todoVisibility";
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
  type SessionMeta,
  type SettingsView,
  type TabMeta,
  type TokenMode,
  type ToolApprovalMode,
} from "./lib/types";
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
  clampCreationSidebarWidth,
  clampRightDockPreviewWidth,
  clampRightDockTreeWidth,
  clampSidebarWidth,
  defaultRightDockTreeWidth,
  defaultSidebarWidth,
  saveRightDockPreviewWidth,
  saveRightDockTreeWidth,
  saveSidebarCollapsed,
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
import { applyTextSize, DEFAULT_TEXT_SIZE, getTextSize, nextTextSize } from "./lib/textSize";
import { useViewportHeightVar, useWindowStatePersistence } from "./lib/windowState";
import { availableWorkspacePanelWidth, resolveLiveWorkspacePanelWidth, resolveWorkspacePanelWidth, workspacePanelAriaMinWidth } from "./lib/workspaceLayout";
import { createRafResizeUpdater } from "./lib/resizeDrag";
import { useGlobalShortcut } from "./lib/keyboardShortcuts";
import { topicShortcutIndexFromEvent, useTopicShortcuts, type TopicShortcutEntry } from "./lib/topicShortcuts";
import { composerDraftKeyForTab } from "./lib/composerDraftKey";
import logoWordmark from "./assets/logo-wordmark.svg";

const HistoryPanel = lazy(() => import("./components/HistoryPanel").then((module) => ({ default: module.HistoryPanel })));
const SettingsPanel = lazy(() => import("./components/SettingsPanel").then((module) => ({ default: module.SettingsPanel })));

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
type HistoryScopeFilter = { scope: "global" | "project"; workspaceRoot: string };
type WorkspaceInsertTarget = "composer" | "planRevision";
type DesktopPlatform = "darwin" | "windows" | "linux";
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
    send,
    sendToTab,
    runShell,
    steer,
    notice,
    cancel,
    approve,
    answerQuestion,
    setControllerMode,
    setCollaborationMode: setControllerCollaborationMode,
    setToolApprovalMode: setControllerToolApprovalMode,
    setGoal: setControllerGoal,
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
    rewind,
    setModel,
    setEffort,
    setTokenMode,
    switchTab,
    openProjectTab,
    openGlobalTab,
    closeTab,
    reorderTabs,
    openTopicSession,
    activateTopic,
    syncActiveTab,
    ensureBlankTab,
    ensureBlankSurface,
  } = useController();
  const { locale, setPref: setLocalePref } = useI18n();
  const t = useT();
  const [composerProfilesByTab, setComposerProfilesByTab] = useState<Record<string, ComposerProfile>>({});
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
  const singleSurfaceLayout = desktopLayoutStyle === "workbench" || desktopLayoutStyle === "creation";
  const [startupUpdateChecksEnabled, setStartupUpdateChecksEnabled] = useState<boolean | null>(null);
  const [histView, setHistView] = useState<HistoryViewState | null>(null);
  const paletteOpen = useOverlayStore((s) => s.paletteOpen);
  const setPaletteOpen = useOverlayStore((s) => s.setPaletteOpen);
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
  // Bump dockRefreshKey after each turn so WorkspacePanel/ContextPanel re-fetch
  // workspace changes, git history, and session metadata after AI tool writes.
  useEffect(() => {
    const unsub = onEvent((e) => {
      if (e.kind === "turn_done") {
        setDockRefreshKey((v) => v + 1);
        if (!e.err) playSuccessChime();
      }
    });
    return unsub;
  }, []);

  const [workspacePanelResizing, setWorkspacePanelResizing] = useState(false);
  const [liveWorkspacePanelRenderWidth, setLiveWorkspacePanelRenderWidth] = useState<number | null>(null);
  const workspacePanelMaximized = useLayoutStore((s) => s.workspacePanelMaximized);
  const setWorkspacePanelMaximized = useLayoutStore((s) => s.setWorkspacePanelMaximized);
  const rightDockMode = useLayoutStore((s) => s.rightDockMode);
  const setRightDockMode = useLayoutStore((s) => s.setRightDockMode);
  const [dockRefreshKey, setDockRefreshKey] = useState(0);
  const [projectRevision, setProjectRevision] = useState(0);
  const [activeTopicTurns, setActiveTopicTurns] = useState<number | undefined>(undefined);
  const [composerInsertRequest, setComposerInsertRequest] = useState<ComposerInsertRequest | null>(null);
  const [planRevisionInsertRequest, setPlanRevisionInsertRequest] = useState<ComposerInsertRequest | null>(null);
  const [workspaceInsertTarget, setWorkspaceInsertTarget] = useState<WorkspaceInsertTarget>("composer");
  const transientOverlayDismissSignal = useOverlayStore((s) => s.transientOverlayDismissSignal);
  const setTransientOverlayDismissSignal = useOverlayStore((s) => s.setTransientOverlayDismissSignal);
  const [desktopPlatform, setDesktopPlatform] = useState<DesktopPlatform>(detectBrowserPlatform);
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
    (settings: Pick<SettingsView, "desktopTheme" | "desktopThemeStyle" | "desktopLayoutStyle" | "desktopLanguage" | "checkUpdates" | "statusBarStyle" | "statusBarItems">) => {
      const nextTheme = normalizeThemePreference(settings.desktopTheme);
      const nextStyle = normalizeThemeStyleForTheme(settings.desktopThemeStyle, nextTheme);
      applyTheme(nextTheme, nextStyle, { persist: false });
      setDesktopLayoutStyle(normalizeDesktopLayoutStyle(settings.desktopLayoutStyle));
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
      hydrateDisplayMode(settings.displayMode);
      setSidebarImConnections(sidebarImConnectionsFromBot(settings.bot, t, runtimeStatus));
      setImTopicSources(sidebarImTopicSourcesFromBot(settings.bot, t));
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

  const [pendingPlanRevision, setPendingPlanRevision] = useState<string | null>(null);
  const [footerHeight, setFooterHeight] = useState(0);
  const footerHeightRef = useRef(0);
  const footerRef = useRef<HTMLElement>(null);
  const runningRef = useRef(state.running);
  const activeTabIdRef = useRef(activeTabId);
  const commitThenSendRef = useRef<(displayText: string, submitText?: string) => Promise<void>>(async () => {});
  const rightDockDetailActive = rightDockMode !== "context" && workspacePreviewActive;
  const preferredWorkspacePanelWidth = rightDockDetailActive ? rightDockPreviewWidth : rightDockTreeWidth;
  const workspacePanelMinWidth = rightDockDetailActive ? RIGHT_DOCK_PREVIEW_MIN_WIDTH : RIGHT_DOCK_TREE_MIN_WIDTH;
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
    workspacePanelOpen && (workspacePanelMaximized || workspacePanelRenderWidth >= RIGHT_DOCK_MIN_RENDER_WIDTH);
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
  const composerSessionKey = useMemo(() => {
    return composerDraftKeyForTab(activeTab, activeTabId);
  }, [activeTab, activeTabId]);
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
  const controllerReady = state.meta?.ready === true && !state.backendActivationPending;
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
    (m: CollaborationMode): Promise<void> => {
      userPlanModeByTabRef.current = updateUserPlanModeIntent(userPlanModeByTabRef.current, activeTabId, m === "plan");
      if (m === "goal") {
        patchActiveComposerProfile({ collaborationMode: "normal", goalDraftMode: true, goal: "" }, ["collaborationMode", "goal"]);
        return setControllerCollaborationMode("normal");
      }
      patchActiveComposerProfile({ collaborationMode: m, goalDraftMode: false, goal: "" }, ["collaborationMode", "goal"]);
      return setControllerCollaborationMode(m);
    },
    [activeTabId, patchActiveComposerProfile, setControllerCollaborationMode],
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
    (nextGoal: string) => {
      userPlanModeByTabRef.current = updateUserPlanModeIntent(userPlanModeByTabRef.current, activeTabId, false);
      const trimmed = nextGoal.trim();
      patchActiveComposerProfile({
        collaborationMode: trimmed ? "goal" : "normal",
        goalDraftMode: false,
        goal: trimmed,
      }, ["collaborationMode", "goal"]);
      void (trimmed ? setControllerGoal(trimmed) : clearControllerGoal());
    },
    [activeTabId, clearControllerGoal, patchActiveComposerProfile, setControllerGoal],
  );
  const applyTokenMode = useCallback(
    (m: TokenMode) => {
      patchActiveComposerProfile({ tokenMode: m }, ["tokenMode"]);
      void setTokenMode(m);
    },
    [patchActiveComposerProfile, setTokenMode],
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
  // completed lists collapse automatically and can then be dismissed. The
  // dismissal key is still based on stable todo content/state so history reloads
  // do not resurrect the same finished list under a different event id.
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
  const [dismissedTodo, setDismissedTodo] = useState<string | null>(null);
  const todoKey = useMemo(() => todoDismissalKey(todos), [todos]);
  const showTodos = shouldShowTodoPanel(todoKey, dismissedTodo, todos);

  const sessionTitle = topicTitle(activeTab);
  const sessionHasContent = state.items.length > 0 || Boolean(state.live?.text || state.live?.reasoning);
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
          if (path) await app.SaveExportFile(path, getSessionJson(), false);
        } else if (format === "pdf") {
          const path = await app.PickExportFile(`${base}.pdf`, "application/pdf");
          if (!path) return;
          const { blobToBase64, renderSessionPdfBlob } = await import("./lib/sessionExport");
          const blob = await renderSessionPdfBlob(getSessionMarkdown(), sessionTitle);
          await app.SaveExportFile(path, await blobToBase64(blob), true);
        } else if (format === "image") {
          const path = await app.PickExportFile(`${base}.png`, "image/png");
          if (!path) return;
          const { blobToBase64, renderSessionImageBlob } = await import("./lib/sessionExport");
          const blob = await renderSessionImageBlob(getSessionMarkdown());
          await app.SaveExportFile(path, await blobToBase64(blob), true);
        } else {
          const path = await app.PickExportFile(`${base}.md`, "text/markdown");
          if (path) await app.SaveExportFile(path, getSessionMarkdown(), false);
        }
      } catch (err) {
        console.error("Failed to export session", err);
      }
    },
    [getSessionJson, getSessionMarkdown, sessionTitle],
  );

  useEffect(() => {
    if (!pendingPlanRevision || state.running) return;
    const text = pendingPlanRevision;
    setPendingPlanRevision(null);
    void commitThenSendRef.current(text).catch((err) => {
      console.warn("Failed to submit pending plan revision", err);
    });
  }, [pendingPlanRevision, state.running]);

  useEffect(() => {
    setClearContextPending(false);
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

  // Keep runningRef in sync so handleSend sees the latest running value
  // even inside a stale closure.
  useEffect(() => {
    runningRef.current = state.running;
  }, [state.running]);

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
    async (displayText: string, submitText = displayText) => {
      const trimmed = displayText.trim();
      // "!<cmd>" runs a shell command directly, bypassing the model.
      if (trimmed.startsWith("!")) {
        const cmd = trimmed.slice(1).trim();
        if (!cmd) {
          notice("usage: !<command>  (e.g. !ls -la)");
          return;
        }
        await runShell(cmd);
        return;
      }
      const model = /^\/model\s+(\S+)$/.exec(trimmed);
      if (model) {
        void switchModel(model[1]);
        return;
      }
      if (trimmed === "/memory") {
        closeTransientOverlays();
        setSettingsTarget("memory");
        return;
      }
      if (trimmed === "/clear") {
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
            applyGoal(displayGoal);
          }
        } else if (["clear", "off", "stop", "done"].includes(displayGoal.toLowerCase())) {
          applyGoal("");
        }
        if (!controllerReady) return;
        await commitThenSendRef.current(trimmed, submitText.trim());
        return;
      }
      if (collaborationMode === "goal" && !goal.trim()) {
        if (!controllerReady) return;
        applyGoal(trimmed);
        await commitThenSendRef.current(trimmed, `/goal ${submitText.trim()}`);
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
      if (runningRef.current) { await steer(submitText.trim()); return; }
      if (!controllerReady) return;
      await setControllerCollaborationMode(controllerComposerProfileCollaborationMode(composerProfile));
      await setControllerToolApprovalMode(toolApprovalMode);
      if (goal.trim()) await setControllerGoal(goal);
      await commitThenSendRef.current(trimmed, submitText.trim());
    },
    [activeTabId, applyGoal, closeTransientOverlays, collaborationMode, composerProfile, controllerReady, goal, send, runShell, notice, setControllerCollaborationMode, setControllerGoal, setControllerToolApprovalMode, steer, switchModel, t, toolApprovalMode, showToast],
  );

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
      const next = clampRightDockTreeWidth(width);
      setRightDockTreeWidth(next);
      saveRightDockTreeWidth(next);
    },
    [closeTransientOverlays, rightDockDetailActive],
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
          nextDockWidth = clampRightDockTreeWidth(nextDockWidth);
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
    [closeTransientOverlays, resolveLiveWorkspacePanelRenderWidth, rightDockDetailActive, setSavedWorkspacePanelWidth, workspacePanelOpen, workspacePanelRenderWidth],
  );

  const resizeWorkspacePanelWithKeyboard = useCallback(
    (event: KeyboardEvent<HTMLButtonElement>) => {
      if (event.key === "ArrowLeft" || event.key === "ArrowRight") {
        event.preventDefault();
        setSavedWorkspacePanelWidth(workspacePanelRenderWidth + (event.key === "ArrowLeft" ? 16 : -16));
      } else if (event.key === "Home") {
        event.preventDefault();
        setSavedWorkspacePanelWidth(rightDockDetailActive ? RIGHT_DOCK_PREVIEW_MIN_WIDTH : RIGHT_DOCK_TREE_MIN_WIDTH);
      } else if (event.key === "End") {
        event.preventDefault();
        setSavedWorkspacePanelWidth(rightDockDetailActive ? RIGHT_DOCK_MAX_WIDTH : RIGHT_DOCK_TREE_MAX_WIDTH);
      }
    },
    [rightDockDetailActive, setSavedWorkspacePanelWidth, workspacePanelRenderWidth],
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
  }, [closeTransientOverlays, workspacePanelOpen]);

  const toggleWorkspacePanel = useCallback(() => {
    pulseWorkspaceToggle();
    if (workspacePanelRenderable) {
      closeWorkspacePanel();
      return;
    }
    openWorkspacePanel("context");
  }, [closeWorkspacePanel, openWorkspacePanel, pulseWorkspaceToggle, workspacePanelRenderable]);

  const openRightDockMode = useCallback(
    (mode: RightDockMode) => {
      openWorkspacePanel(mode);
    },
    [openWorkspacePanel],
  );

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
    if (workspaceInsertTarget === "planRevision" && state.approval?.tool === "exit_plan_mode") {
      setPlanRevisionInsertRequest({ id: Date.now(), text });
      return;
    }
    setComposerInsertRequest({ id: Date.now(), text });
  }, [state.approval?.tool, workspaceInsertTarget]);

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
  const [rewindState, setRewindState] = useState<RewindState | null>(null);
  const [rewindCommitting, setRewindCommitting] = useState(false);
  const rewindStateRef = useRef(rewindState);
  rewindStateRef.current = rewindState;

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

  // send wrapper: commits any pending optimistic rewind before sending.
  const commitThenSend = useCallback(async (displayText: string, submitText?: string) => {
    if (activeTab?.readOnly) throw new Error("channel session is read-only");
    if (!controllerReady) throw new Error("workspace is still starting");
    const rs = rewindStateRef.current;
    if (rs) {
      rewindStateRef.current = null;
      setRewindState(null);
      setRewindCommitting(true);
      let ok = false;
      try {
        ok = await rewind(rs.turn, rs.scope);
      } finally {
        setRewindCommitting(false);
      }
      if (!ok) {
        // Rewind failed: the Go conversation is intact. Do not send; the
        // controller emits a notice with the reason.
        setRewindState(null);
        throw new Error("rewind failed");
      }
      setRewindSignal((v) => v + 1);
      if (rs.scope === "both") {
        // Code was only reverted now (deferred), so refresh the dock here.
        setDockRefreshKey((v) => v + 1);
        setProjectRevision((v) => v + 1);
      }
    }
    await send(displayText, submitText);
  }, [activeTab?.readOnly, controllerReady, send, rewind]);

  const handleTranscriptPrompt = useCallback((text: string) => {
    if (!controllerReady) return;
    void commitThenSend(text).catch((err) => {
      console.warn("Failed to submit transcript prompt", err);
    });
  }, [commitThenSend, controllerReady]);
  commitThenSendRef.current = commitThenSend;

  const handleMessageAction = useCallback((turn: number, scope: string) => {
    if (activeTab?.readOnly) return;
    if (hydratePlaceholderActive) return;
    if (scope === "fork") {
      // Fork still goes through the controller (not optimistic).
      rewind(turn, scope).then((ok) => {
        if (!ok) return;
        refreshTabMetas();
        setProjectRevision((v) => v + 1);
      });
      return;
    }

    // Code-only rewind only affects files — no message truncation,
    // no optimistic UI needed.  Execute immediately.
    if (scope === "code") {
      rewind(turn, scope).then((ok) => {
        if (!ok) return;
        setDockRefreshKey((v) => v + 1);
        setProjectRevision((v) => v + 1);
      });
      return;
    }

    // Summarize only compresses the conversation log — no files touched,
    // no optimistic UI needed. Execute immediately like code-only rewind.
    if (scope === "summ-from" || scope === "summ-upto") {
      rewind(turn, scope).then((ok) => {
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
      rewind(turn, scope).then((ok) => {
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
    setRewindState({
      turn,
      scope,
      fullItems: items,
      boundaryIdx,
      turnDiff,
      prompt,
    });

    // Fill composer with the rewound-to user message.
    const insertId = Date.now();
    setComposerInsertRequest({ id: insertId, text: prompt, mode: "replace" });

    setRewindSignal((v) => v + 1);
  }, [activeTab?.readOnly, hydratePlaceholderActive, state.items, rewind, refreshTabMetas, setComposerInsertRequest]);

  const handleEditPrompt = useCallback(async (turn: number, displayText: string, submitText?: string): Promise<boolean> => {
    const sourceTabId = activeTabId;
    if (!sourceTabId || activeTab?.readOnly || !controllerReady || hydratePlaceholderActive || rewindStateRef.current || state.running || state.messageAction != null || state.approval != null || state.ask != null || clearContextPending) return false;
    const next = displayText.trim();
    if (!next) return false;
    const submit = (submitText ?? displayText).trim();
    const ok = await rewind(turn, "conversation");
    if (!ok) return false;
    setRewindSignal((v) => v + 1);
    try {
      await sendToTab(sourceTabId, next, submit);
      return true;
    } catch {
      return false;
    }
  }, [activeTab?.readOnly, activeTabId, clearContextPending, controllerReady, hydratePlaceholderActive, sendToTab, state.approval, state.ask, state.messageAction, state.running, rewind]);

  // History drawer: project menus can open a scoped saved-session list. Idle row
  // clicks resume; running row clicks only preview through PreviewSession.
  const openProjectHistory = useCallback(async (scope: "global" | "project", workspaceRoot: string) => {
    closeTransientOverlays();
    const filter = { scope, workspaceRoot };
    setHistView({ kind: "history", source: "scope", filter, sessions: sessionsForScope(await listSessions(), filter) });
  }, [closeTransientOverlays, listSessions]);
  const openAllHistory = useCallback(async () => {
    closeTransientOverlays();
    setHistView({ kind: "history", source: "all", sessions: await listSessions() });
  }, [closeTransientOverlays, listSessions]);
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
          : (session.topicId ? "Missing workspaceRoot" : t("history.failedOpenSession")));
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
  }, [activateTopic, ensureBlankSurface, ensureBlankTab, openChannelSession, openGlobalTab, openProjectTab, openTopicSession, refreshHistoryView, resumeSession, seedActiveTabMeta, showToast, singleSurfaceLayout, t]);

  const enqueueNavigation = useCallback((input: DesktopNavigationInput): Promise<void> => enqueueNavigationRequest(
    { seqRef: navigationSeqRef, runningRef: navigationRunningRef, pendingRef: navigationPendingRef },
    input,
    runNavigationRequest,
  ), [runNavigationRequest]);

  const openBlankSession = useCallback((scope: string, workspaceRoot: string): Promise<void> =>
    enqueueNavigation({ kind: "blank", scope, workspaceRoot: scope === "project" ? workspaceRoot : "" }),
  [enqueueNavigation]);

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
      { id: "cmd-history", group: t("palette.group.commands"), title: t("palette.cmd.history"), icon: <History size={15} />, compact: true, keywords: ["history", "历史"], run: () => void openAllHistory() },
      { id: "cmd-trash", group: t("palette.group.commands"), title: t("palette.cmd.trash"), icon: <Trash2 size={15} />, compact: true, keywords: ["trash", "回收站"], run: () => void openTrash() },
      { id: "cmd-settings", group: t("palette.group.commands"), title: t("palette.cmd.settings"), icon: <SettingsIcon size={15} />, compact: true, keywords: ["settings", "设置"], run: () => setSettingsTarget("general") },
      { id: "cmd-appearance", group: t("palette.group.commands"), title: t("palette.cmd.appearance"), icon: <Palette size={15} />, compact: true, keywords: ["theme", "appearance", "外观", "主题"], run: () => setSettingsTarget("appearance") },
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
    return [...cmds, ...sessionItems];
  }, [t, paletteSessions, handleNewTab, openAllHistory, openTrash, onResumeSession]);
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
      await syncActiveTab(true);
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
  const workspacePanelResetWidth = rightDockDetailActive
    ? RIGHT_DOCK_PREVIEW_DEFAULT_WIDTH
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
      className={[
        "app",
        `app--${desktopPlatform}`,
        browserPreviewChrome ? "app--browser-preview" : "",
        sidebarWorkbench ? "app--workbench" : "",
        sidebarCreation ? "app--creation" : "",
      ].filter(Boolean).join(" ")}
    >
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
              onOpenProjectHistory={openProjectHistory}
              onCreateTopic={(scope, workspaceRoot) => openBlankSession(scope, scope === "project" ? workspaceRoot : "")}
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
                <Tooltip label={t("sidebar.allHistory")} fill side="top">
                  <button
                    className="sidebar__utility-button"
                    type="button"
                    onClick={() => void openAllHistory()}
                  >
                    <History size={16} aria-hidden="true" />
                    <span className="sr-only">{t("sidebar.allHistory")}</span>
                  </button>
                </Tooltip>
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
              <Tooltip label={t("sidebar.allHistory")} fill side="right" disabled={sidebarNavTooltipDisabled}>
                <button
                  className="sidebar__navitem"
                  onClick={() => void openAllHistory()}
                >
                  <History size={15} />
                  <span>{t("sidebar.allHistory")}</span>
                </button>
              </Tooltip>
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
          onDoubleClick={() => setExpandedSidebarWidth(defaultSidebarWidth())}
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
                  className="topicbar__action-btn topicbar__action-btn--label topicbar__action-btn--accent"
                  type="button"
                  aria-label={t("topicBar.command")}
                  onClick={() => void openPalette()}
                >
                  <Command size={14} />
                  <span>{t("topicBar.command")}</span>
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

          <UpdateBanner enabled={startupUpdateChecksEnabled === true} />

          <main className="main">
            {sidebarImDetailConnection ? (
              <SidebarImConnectionDetail
                connection={sidebarImDetailConnection}
                onClose={() => setSidebarImDetailConnectionId("")}
                onOpenSettings={openBotSettings}
                onManageAllowlist={() => openBotAllowlistSettings(sidebarImDetailConnection.connectionId)}
                onOpenSession={() => void openSidebarImConnectionSession(sidebarImDetailConnection)}
              />
            ) : (
              <Transcript
                items={displayItems}
                live={state.live}
                tabId={activeTabId}
                footerHeight={footerHeight}
                onPrompt={handleTranscriptPrompt}
                onEditPrompt={handleEditPrompt}
                onRewind={handleMessageAction}
                checkpoints={state.checkpoints}
                actionPending={state.messageAction != null}
                rewindDisabled={Boolean(activeTab?.readOnly) || !controllerReady || hydratePlaceholderActive || rewindState != null || rewindCommitting || state.running || state.messageAction != null || state.approval != null || state.ask != null || clearContextPending}
                running={state.running || rewindCommitting}
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
              />
            )}
          </main>

          {!sidebarImDetailConnection && (
          <footer className="footer" ref={footerRef}>
            {showTodos && <TodoPanel todos={todos} onDismiss={() => setDismissedTodo(todoKey)} />}
            {rewindState && (
              <UndoRewindBanner
                meta={{
                  turns: rewindState.turnDiff,
                  filesRestored: [], // optimistic: files haven't changed yet
                  filesRemoved: [],
                  onUndo: () => {
                    setRewindState(null);
                    setComposerInsertRequest({ id: Date.now(), text: "", mode: "replace" });
                  },
                }}
              />
            )}
            {state.approval && (
              <ApprovalModal
                key={state.approval.id}
                approval={state.approval}
                cwd={state.meta?.cwd}
                insertRequest={planRevisionInsertRequest}
                onRevisionActiveChange={(active) => setWorkspaceInsertTarget(active ? "planRevision" : "composer")}
                onAnswer={async (allow, session, persist) => {
                  // Approving an exit_plan_mode plan leaves plan mode; await the
                  // mode switch before sending the approval so the controller
                  // observes the updated state before it unblocks.
                  if (state.approval!.tool === "exit_plan_mode" && allow) await applyCollaborationMode("normal");
                  approve(state.approval!.id, allow, session, persist);
                }}
                onRevisePlan={(text) => {
                  setPendingPlanRevision(text);
                  approve(state.approval!.id, false, false, false);
                }}
                onExitPlan={async () => {
                  await applyCollaborationMode("normal");
                  approve(state.approval!.id, false, false, false);
                }}
                onStop={() => {
                  cancel();
                }}
              />
            )}
            {state.ask && (
              <AskCard
                ask={state.ask}
                onAnswer={answerQuestion}
                onDismiss={() => answerQuestion(state.ask!.id, [])}
                onStop={() => {
                  cancel();
                }}
              />
            )}
            {clearContextPending && (
              <ClearContextCard
                onCancel={cancelClearContext}
                onConfirm={() => {
                  void confirmClearContext();
                }}
              />
            )}
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
              readOnly={Boolean(activeTab?.readOnly)}
              disabled={rewindCommitting || state.messageAction != null || state.approval != null || state.ask != null || clearContextPending}
              submitDisabled={!controllerReady}
              decisionPending={rewindCommitting || state.messageAction != null || state.approval != null || state.ask != null || clearContextPending}
              ready={controllerReady}
              turnStartAt={state.turnStartAt}
              turnTokens={state.turnTokens}
              retry={state.retry}
              transientDismissSignal={transientOverlayDismissSignal}
              sessionKey={composerSessionKey}
            />
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
                {SHOW_CONTEXT_DOCK && (
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
              </div>
            </div>
            <div className="workbench-dock__body">
              {rightDockMode === "context" ? (
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
                />
              ) : (
                <WorkspacePanel
                  open={workspacePanelRenderable}
                  tabId={activeTabId}
                  cwd={state.meta?.cwd}
                  maximized={workspacePanelMaximized}
                  panelWidth={workspacePanelRenderWidth}
                  onClose={() => setWorkspacePanel(false)}
                  onToggleMaximized={() => {
                    closeTransientOverlays();
                    setWorkspacePanelMaximized((value) => !value);
                  }}
                  onPreviewModeChange={handleWorkspacePreviewModeChange}
                  onAddToChat={addWorkspaceTextToComposer}
                  onRequestPanelWidth={ensureWorkspacePanelWidth}
                  refreshKey={dockRefreshKey}
                  initialViewMode={rightDockMode === "changed" ? "changed" : "files"}
                  showViewTabs={false}
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
    </div>
    </ShellExpandProvider>
  );
}
