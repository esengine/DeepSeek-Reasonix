// bridge is the single seam between the React app and the Go kernel. In the Wails
// shell it calls the bound App methods (window.go.main.App.*) and subscribes to
// the runtime event stream (window.runtime.EventsOn). In a plain browser (`pnpm
// dev` outside the shell) those globals are absent, so it falls back to a mock
// that streams a canned turn through the same contract — letting the whole UI be
// developed and laid out without rebuilding the Go side.

// @ts-ignore `wails generate module` creates this locally; fresh checkouts keep
// typecheck green by falling back to a disabled drift check below.
import type * as GeneratedApp from "../../wailsjs/go/main/App";
import type { InvocationRequest } from "./invocationDisplay";

import { addBreadcrumb } from "./breadcrumbs";
import { t } from "./i18n";
import { providerRequiresKey } from "./providerModels";
import { DEFAULT_STATUS_BAR_ITEMS, normalizeStatusBarItems } from "./statusBarItems";
import { registerTrustedThemeBackgroundURLs } from "./themePack";
import { modeHasAutoApproveTools, modeWithAutoApproveTools, modeWithPlan, normalizeCollaborationMode, normalizeMode, normalizeTokenMode, normalizeToolApprovalMode } from "./types";

import type {
  AutoResearchFindingView,
  AutoResearchEvidenceView,
  AutoResearchStatusView,
  RemoteHostView,
  RemoteHostInput,
  RemoteConnectionStatus,
  RemoteDirEntry,
  RemoteFilePreview,
  RemoteWriteResult,
  RemoteForwardInput,
  RemoteForwardView,
  RemoteServerView,
  RemoteForwardsEvent,
  BalanceInfo,
  BotConnectionDiagnostic,
  BotInstallPollResult,
  BotInstallStartResult,
  BotRuntimeStatusView,
  BotSettingsView,
  CapabilitiesView,
  CapabilityDiagnosticsReport,
  CheckpointMeta,
  CommandInfo,
  ContextInfo,
  ContextPanelInfo,
  DirEntry,
  DesktopStartupSettingsView,
  DeliveryWorktreeAvailability,
  DeliveryWorktreeOpenResult,
  DroppedItem,
  EffortInfo,
  FilePreview,
  ExternalOpenersView,
  HistoryMessage,
  HistoryPage,
  HookConfigView,
  HooksSettingsView,
  JobView,
  MCPServerInput,
  MCPToolView,
  MemorySuggestion,
  MemorySuggestionsView,
  MemoryView,
  Meta,
  Mode,
  ModelInfo,
  NetworkView,
  PluginInstallOptions,
  PluginView,
  ProjectNode,
  PromptHistoryEntry,
  PromptHistoryResult,
  ProviderPresetView,
  ProviderView,
  QuestionAnswer,
  ServerView,
  SessionMeta,
  SessionRecoveryFailedEvent,
  SessionRecoveryEvent,
  SettingsView,
  SkillsSettingsView,
  SkillRootView,
  SkillSuggestion,
  SkillView,
  SlashArgsResult,
  SubagentProfileInput,
  TabMeta,
  TopicMeta,
  ToolApprovalMode,
  UpdateDownloadResult,
  UpdateInfo,
  UpdateProgress,
  WireEvent,
  WorkspaceChangesView,
  GitCommitView,
  GitCommitDetailView,
  WorkspaceView,
} from "./types";

const GLOBAL_PROJECT_ORDER_KEY = "__global__";

function stripGoalResearchFlags(arg: string): string {
  const parts = arg.trim().split(/\s+/).filter(Boolean);
  while (parts.length > 0) {
    const flag = parts[0].toLowerCase();
    if (flag !== "--research" && flag !== "--auto-research" && flag !== "--deep" && flag !== "--simple" && flag !== "--no-research") break;
    parts.shift();
  }
  return parts.join(" ");
}

// AppBindings is derived from the Wails-generated Go → TS method signatures, so
// the compiler catches drift between the Go binding surface and the frontend mock.
// Run `wails generate module` after adding/renaming a bound method on App, then
// `pnpm typecheck` to verify the mock still satisfies the contract.
//
// Types for the new native-feel bindings — kept inline since they are
// bridge-specific and only used in AppBindings / the dev mock.
interface NativeConfirmRequest {
  title: string;
  message: string;
  detail: string;
  confirmLabel: string;
  cancelLabel: string;
  destructive: boolean;
}

interface DesktopWindowState {
  width: number;
  height: number;
  x: number;
  y: number;
  maximised: boolean;
}

// AppBindings is the hand-written contract between the React app and the Go
// kernel. It uses local types (types.ts) so components don't import generated
// model classes. _CheckGeneratedBindings catches drift: when a Go method is
// added or renamed, the generated types shift, and a key present in GeneratedApp
// but missing from AppBindings causes a type error here. Fix: add the new method
// to AppBindings, then run `pnpm typecheck` to verify.
export interface AppBindings {
  Platform(): Promise<string>;
  MinimiseMainWindow(): Promise<void>;
  ToggleMaximiseMainWindow(): Promise<void>;
  IsMainWindowMaximised(): Promise<boolean>;
  CloseMainWindow(): Promise<void>;
  // ── Heartbeat ──
  HeartbeatListTasks(): Promise<unknown>;
  HeartbeatReloadTasks(): Promise<unknown>;
  HeartbeatSaveTasks(tasks: unknown): Promise<void>;
  HeartbeatTriggerNow(id: string): Promise<void>;
  HeartbeatGenerateID(): Promise<string>;
  Submit(input: string): Promise<void>;
  SubmitToTab(tabID: string, input: string): Promise<void>;
  SubmitDisplay(display: string, input: string): Promise<void>;
  SubmitDisplayToTab(tabID: string, display: string, input: string): Promise<void>;
  SubmitDeliveryRecoveryToTab(tabID: string, display: string, input: string): Promise<void>;
  SubmitInvocationsToTab(tabID: string, display: string, input: string, invocations: InvocationRequest[]): Promise<void>;
  SubmitEditedDisplayToTab(tabID: string, display: string, input: string, original: string): Promise<void>;
  RunShell(command: string): Promise<void>;
  RunShellForTab(tabID: string, command: string): Promise<void>;
  Steer(text: string): Promise<void>;
  SteerForTab(tabID: string, text: string): Promise<void>;
  Cancel(): Promise<void>;
  CancelTab(tabID: string): Promise<void>;
  Approve(id: string, allow: boolean, session: boolean, persist: boolean): Promise<void>;
  ApproveTab(tabID: string, id: string, allow: boolean, session: boolean, persist: boolean): Promise<void>;
  AnswerQuestion(id: string, answers: QuestionAnswer[]): Promise<void>;
  AnswerQuestionForTab(tabID: string, id: string, answers: QuestionAnswer[]): Promise<void>;
  ReplayPendingPrompts(): Promise<void>;
  SetPlanMode(on: boolean): Promise<void>;
  SetMode(mode: string): Promise<void>;
  // Resolves with the pending approval prompt ids the switch auto-allowed
  // (drained); prompts not listed are still pending backend-side (#6432).
  SetModeForTab(tabID: string, mode: string): Promise<string[] | void>;
  SetAutoApproveTools(on: boolean): Promise<void>;
  SetCollaborationMode(mode: string): Promise<void>;
  SetCollaborationModeForTab(tabID: string, mode: string): Promise<void>;
  SetToolApprovalMode(mode: string): Promise<void>;
  // Same drained-prompt-id contract as SetModeForTab.
  SetToolApprovalModeForTab(tabID: string, mode: string): Promise<string[] | void>;
  SetGoal(goal: string): Promise<void>;
  SetGoalForTab(tabID: string, goal: string): Promise<void>;
  ResumeGoalForTab(tabID: string): Promise<boolean>;
  ClearGoal(): Promise<void>;
  ClearGoalForTab(tabID: string): Promise<void>;
  Compact(): Promise<void>;
  CompactForTab(tabID: string): Promise<void>;
  NewSession(): Promise<void>;
  NewSessionForTab(tabID: string): Promise<void>;
  ClearSession(): Promise<void>;
  ClearSessionForTab(tabID: string): Promise<void>;
  History(): Promise<HistoryMessage[]>;
  HistoryForTab(tabID: string): Promise<HistoryMessage[]>;
  HistoryPage(beforeTurn: number, limit: number): Promise<HistoryPage>;
  HistoryPageForTab(tabID: string, beforeTurn: number, limit: number): Promise<HistoryPage>;
  HistoryCheckpointTurnsForTab(tabID: string): Promise<number[]>;
  Checkpoints(): Promise<CheckpointMeta[]>;
  CheckpointsForTab(tabID: string): Promise<CheckpointMeta[]>;
  Rewind(turn: number, scope: string): Promise<void>;
  RewindForTab(tabID: string, turn: number, scope: string): Promise<void>;
  Fork(turn: number): Promise<TabMeta>;
  ForkForTab(tabID: string, turn: number): Promise<TabMeta>;
  SummarizeFrom(turn: number): Promise<void>;
  SummarizeFromForTab(tabID: string, turn: number): Promise<void>;
  SummarizeUpTo(turn: number): Promise<void>;
  SummarizeUpToForTab(tabID: string, turn: number): Promise<void>;
  ListSessions(): Promise<SessionMeta[]>;
  ListTrashedSessions(): Promise<SessionMeta[]>;
  ResumeSession(path: string): Promise<HistoryMessage[]>;
  ResumeSessionForTab(tabID: string, path: string): Promise<HistoryMessage[]>;
  ResumeSessionPage(path: string, limit: number): Promise<HistoryPage>;
  ResumeSessionPageForTab(tabID: string, path: string, limit: number): Promise<HistoryPage>;
  OpenChannelSessionForTab(tabID: string, path: string): Promise<HistoryMessage[]>;
  OpenChannelSessionPageForTab(tabID: string, path: string, limit: number): Promise<HistoryPage>;
  PreviewSession(path: string): Promise<HistoryMessage[]>;
  DeleteSession(path: string): Promise<void>;
  DeleteRecoveryCopy(path: string): Promise<void>;
  RestoreSession(path: string): Promise<void>;
  PurgeTrashedSession(path: string): Promise<void>;
  PurgeRecoveryCopy(path: string): Promise<void>;
  RenameSession(path: string, title: string): Promise<void>;
  ScanPromptHistory(nonce: string): Promise<PromptHistoryResult>;
  ListWorkspaces(): Promise<WorkspaceView[]>;
  PickWorkspace(): Promise<string>;
  SwitchWorkspace(path: string): Promise<string>;
  RemoveWorkspace(path: string): Promise<void>;
  ContextUsage(): Promise<ContextInfo>;
  ContextUsageForTab(tabID: string): Promise<ContextInfo>;
  Balance(): Promise<BalanceInfo>;
  BalanceForTab(tabID: string): Promise<BalanceInfo>;
  Jobs(): Promise<JobView[]>;
  JobsForTab(tabID: string): Promise<JobView[]>;
  ToolResultForTab(tabID: string, toolID: string): Promise<{ args: string; output: string } | null>;
  Meta(): Promise<Meta>;
  MetaForTab(tabID: string): Promise<Meta>;
  AutoResearchCurrent(): Promise<AutoResearchStatusView>;
  AutoResearchStatus(tabID: string): Promise<AutoResearchStatusView>;
  AutoResearchList(tabID: string): Promise<AutoResearchStatusView[]>;
  AutoResearchFindings(tabID: string, limit: number): Promise<AutoResearchFindingView[]>;
  AutoResearchOpenTask(tabID: string): Promise<void>;
  AutoResearchRecordEvidence(tabID: string, criterionID: string, input: AutoResearchEvidenceView): Promise<void>;
  Commands(): Promise<CommandInfo[]>;
  Capabilities(): Promise<CapabilitiesView>;
  MCPServers(): Promise<ServerView[]>;
  SkillsSettings(): Promise<SkillsSettingsView>;
  CapabilityDiagnostics(includeSessionRuntime: boolean): Promise<CapabilityDiagnosticsReport>;
  Plugins(): Promise<PluginView[]>;
  PlanPluginInstall(source: string, options: PluginInstallOptions): Promise<string>;
  InstallPlugin(source: string, options: PluginInstallOptions): Promise<string>;
  RemovePlugin(name: string): Promise<void>;
  SetPluginEnabled(name: string, enabled: boolean): Promise<void>;
  UpdatePlugin(name: string): Promise<string>;
  PluginDoctor(name: string): Promise<PluginView>;
  AddMCPServer(input: MCPServerInput): Promise<number>;
  UpdateMCPServer(name: string, input: MCPServerInput): Promise<void>;
  RemoveMCPServer(name: string): Promise<void>;
  AuthorizeAndConnectMCPServer(name: string): Promise<void>;
  ReconnectMCPServer(name: string): Promise<void>;
  ClearMCPServerAuthentication(name: string): Promise<void>;
  PickSkillFolder(): Promise<string>;
  PickPluginFolder(): Promise<string>;
  AddSkillPath(path: string): Promise<void>;
  RemoveSkillPath(path: string): Promise<void>;
  RefreshSkills(): Promise<void>;
  ReloadCommands(): Promise<void>;
  SetSkillEnabled(name: string, enabled: boolean): Promise<void>;
  AvailableSubagentTools(): Promise<MCPToolView[]>;
  CreateSubagentProfile(input: SubagentProfileInput): Promise<string>;
  UpdateSubagentProfile(name: string, scope: string, input: SubagentProfileInput): Promise<void>;
  DeleteSubagentProfile(name: string, scope: string): Promise<void>;
  SetSubagentProfileModel(name: string, ref: string): Promise<void>;
  SetSubagentProfileEffort(name: string, level: string): Promise<void>;
  TrySubagentProfile(input: SubagentProfileInput, task: string): Promise<string>;
  CancelTrySubagentProfile(): Promise<void>;
  SetMCPServerEnabled(name: string, enabled: boolean): Promise<void>;
  SetMCPServerTier(name: string, tier: string): Promise<void>;
  SlashArgs(input: string): Promise<SlashArgsResult>;
  ListDir(rel: string): Promise<DirEntry[]>;
  ListDirForTab(tabID: string, rel: string): Promise<DirEntry[]>;
  SearchFileRefs(query: string): Promise<DirEntry[]>;
  SearchFileRefsForTab(tabID: string, query: string): Promise<DirEntry[]>;
  ReadFile(rel: string): Promise<FilePreview>;
  ReadFileForTab(tabID: string, rel: string): Promise<FilePreview>;
  WorkspaceChanges(tabID: string): Promise<WorkspaceChangesView>;
  GitBranches(): Promise<string[]>;
  GitCheckout(branch: string): Promise<void>;
  WorkspaceGitHistory(tabID: string, path: string): Promise<GitCommitView[]>;
  WorkspaceGitCommitDetail(tabID: string, hash: string, path: string): Promise<GitCommitDetailView>;
  OpenWorkspacePath(rel: string): Promise<void>;
  OpenWorkspacePathForTab(tabID: string, rel: string): Promise<void>;
  ExternalOpeners(): Promise<ExternalOpenersView>;
  SetPreferredExternalOpener(id: string): Promise<void>;
  OpenWorkspaceInExternalOpener(id: string): Promise<void>;
  OpenWorkspaceInExternalOpenerForTab(tabID: string, id: string): Promise<void>;
  RevealWorkspacePath(rel: string): Promise<void>;
  RevealWorkspacePathForTab(tabID: string, rel: string): Promise<void>;
  RevealPath(path: string): Promise<void>;
  SavePastedImage(dataUrl: string): Promise<string>;
  SaveClipboardImage(): Promise<string>;
  SavePastedFile(name: string, dataUrl: string): Promise<string>;
  PickExportFile(defaultFilename: string, mimeType: string): Promise<string>;
  SaveExportFile(path: string, payload: string, base64Encoded: boolean): Promise<void>;
  SaveExportImageFiles(path: string, payloads: string[]): Promise<void>;
  AttachDropped(path: string): Promise<DroppedItem>;
  AttachmentDataURL(path: string): Promise<string>;
  Models(): Promise<ModelInfo[]>;
  SetModel(name: string): Promise<void>;
  ModelsForTab(tabID: string): Promise<ModelInfo[]>;
  SetModelForTab(tabID: string, name: string): Promise<void>;
  Effort(): Promise<EffortInfo>;
  SetEffort(level: string): Promise<void>;
  EffortForTab(tabID: string): Promise<EffortInfo>;
  SetEffortForTab(tabID: string, level: string): Promise<void>;
  SetTokenMode(mode: string): Promise<void>;
  SetTokenModeForTab(tabID: string, mode: string): Promise<void>;
  Memory(): Promise<MemoryView>;
  MemorySuggestions(): Promise<MemorySuggestionsView>;
  AcceptMemorySuggestion(suggestion: MemorySuggestion): Promise<string>;
  AcceptSkillSuggestion(suggestion: SkillSuggestion): Promise<string>;
  MemoryForTab(tabID: string): Promise<MemoryView>;
  MemorySuggestionsForTab(tabID: string): Promise<MemorySuggestionsView>;
  AcceptMemorySuggestionForTab(tabID: string, suggestion: MemorySuggestion): Promise<string>;
  AcceptSkillSuggestionForTab(tabID: string, suggestion: SkillSuggestion): Promise<string>;
  Remember(scope: string, note: string): Promise<string>;
  RememberForTab(tabID: string, scope: string, note: string): Promise<string>;
  Forget(name: string): Promise<void>;
  ForgetForTab(tabID: string, name: string): Promise<void>;
  SaveDoc(path: string, body: string): Promise<string>;
  SaveDocForTab(tabID: string, path: string, body: string): Promise<string>;
  DesktopStartupSettings(): Promise<DesktopStartupSettingsView>;
  Settings(): Promise<SettingsView>;
  HooksSettings(scope: string): Promise<HooksSettingsView>;
  SaveHooksSettings(scope: string, hooks: HookConfigView[]): Promise<void>;
  SaveHooksSettingsForRoot(scope: string, projectRoot: string, hooks: HookConfigView[]): Promise<void>;
  TrustProjectHooks(): Promise<void>;
  TrustProjectHooksForRoot(projectRoot: string): Promise<void>;
  SetDefaultModel(ref: string): Promise<void>;
  SetPlannerModel(ref: string): Promise<void>;
  SetSubagentModel(ref: string): Promise<void>;
  SetSubagentEffort(level: string): Promise<void>;
  SetMaxSubagentDepth(depth: number): Promise<void>;
  SetMaxSubagentConcurrency(n: number): Promise<void>;
  SetMaxParallelWriters(n: number): Promise<void>;
  SetAutoPlan(mode: string): Promise<void>;
  SetDefaultToolApprovalMode(mode: string): Promise<void>;
  SaveProvider(p: ProviderView): Promise<void>;
  SaveProviderWithKey(p: ProviderView, key: string): Promise<string>;
  AddOfficialProviderAccess(kind: string, key: string): Promise<string>;
  AddProviderPresetAccess(id: string, key: string): Promise<string>;
  ResetProviderPresetAccess(id: string): Promise<void>;
  FetchProviderModels(p: ProviderView): Promise<string[]>;
  DeleteProvider(name: string): Promise<void>;
  RemoveProviderAccess(name: string): Promise<void>;
  SaveProviderKey(apiKeyEnv: string, value: string): Promise<string>;
  SetProviderKey(apiKeyEnv: string, value: string): Promise<string>;
  ClearProviderKey(apiKeyEnv: string): Promise<void>;
  SetPermissionMode(mode: string): Promise<void>;
  AddPermissionRule(list: string, rule: string): Promise<void>;
  RemovePermissionRule(list: string, rule: string): Promise<void>;
  ReloadSettings(): Promise<void>;
  SetSandbox(bash: string, network: boolean, workspaceRoot: string, allowWrite: string[], shell: string): Promise<void>;
  SetNetwork(n: NetworkView): Promise<void>;
  SetBotSettings(b: BotSettingsView): Promise<void>;
  SetBotConnectionToolApprovalMode(connID: string, mode: string): Promise<void>;
  SetBotSecret(envName: string, value: string): Promise<void>;
  ClearBotSecret(envName: string): Promise<void>;
  StartBotConnectionInstall(provider: string, domain: string): Promise<BotInstallStartResult>;
  PollBotConnectionInstall(installID: string): Promise<BotInstallPollResult>;
  BotRuntimeStatus(): Promise<BotRuntimeStatusView>;
  DiagnoseBotConnection(id: string): Promise<BotConnectionDiagnostic>;
  TestBotConnection(id: string, target?: string): Promise<BotConnectionDiagnostic>;
  SetCloseBehavior(mode: string): Promise<void>;
  SetDisplayMode(mode: string): Promise<void>;
  SetStatusBarStyle(style: string): Promise<void>;
  SetStatusBarItems(items: string[]): Promise<void>;
  SetDesktopLanguage(lang: string): Promise<void>;
  SetDesktopAppearance(theme: string, style: string): Promise<void>;
  ListThemePacks(): Promise<import("./themePack").ThemePackView[]>;
  GetActiveThemePack(): Promise<import("./themePack").ThemeActiveView>;
  GetThemeExperience(): Promise<import("./themeExperience").ThemeExperienceView>;
  ActivateThemePack(id: string): Promise<void>;
  ActivateBaseStyle(style: string): Promise<void>;
  DisableThemePack(): Promise<void>;
  RestoreGraphiteAppearance(): Promise<void>;
  ResetThemePack(): Promise<void>;
  SaveThemePack(input: import("./themePack").ThemeSaveInput): Promise<import("./themePack").ThemePackView>;
  DeleteThemePack(id: string): Promise<void>;
  CopyThemePack(sourceID: string, newID: string, newName: string): Promise<import("./themePack").ThemePackView>;
  ImportThemePack(sourcePath: string, replace: boolean): Promise<import("./themePack").ThemeImportResult>;
  ExportThemePack(id: string, destPath: string): Promise<string>;
  PickThemeBackground(): Promise<string>;
  SetDesktopLayoutStyle(style: string): Promise<void>;
  SetDesktopZoomFactor(factor: number): Promise<void>;
  GetDesktopZoomFactor(): Promise<number>;
  RestartApplication(): Promise<void>;
  SetDesktopCheckUpdates(enabled: boolean): Promise<void>;
  SetDesktopTelemetry(enabled: boolean): Promise<void>;
  SetDesktopMetrics(enabled: boolean): Promise<void>;
  SetExpandThinking(on: boolean): Promise<void>;
  SetDesktopConversationWidth(width: string): Promise<void>;
  MigrateDesktopPreferences(language: string, theme: string, style: string): Promise<void>;
  SetAgentParams(temperature: number, maxSteps: number, plannerMaxSteps: number, systemPrompt: string): Promise<void>;
  SetColdResumePrune(enabled: boolean): Promise<void>;
  SetReasoningLanguage(lang: string): Promise<void>;
  SetTrayLocale(locale: "en" | "zh" | "zh-TW"): Promise<void>;
  // SetBypass is the legacy Wails name for YOLO/full-access tool auto-approval
  // (ask questions and plan approvals still wait; deny rules still apply).
  // Runtime-only.
  SetBypass(on: boolean): Promise<void>;
  Version(): Promise<string>;
  CheckUpdate(): Promise<UpdateInfo | null>;
  DownloadUpdate(): Promise<UpdateDownloadResult | null>;
  InstallUpdate(): Promise<void>;
  ApplyUpdate(): Promise<void>;
  OpenDownloadPage(): Promise<void>;
  NeedsOnboarding(): Promise<boolean>;
  ConnectKey(apiKey: string): Promise<string>;
  // Crash overlay "Send report" (desktop/crash_app.go): scrubs user paths, attaches
  // version/os/arch, POSTs to the collection endpoint. Only ever sent on user click.
  ReportCrash(kind: string, detail: string): Promise<void>;
  ListTabs(): Promise<TabMeta[]>;
  OpenProjectTab(workspaceRoot: string, topicID: string): Promise<TabMeta>;
  DeliveryWorktreeAvailability(workspaceRoot: string): Promise<DeliveryWorktreeAvailability>;
  CreateDeliveryWorktree(workspaceRoot: string): Promise<DeliveryWorktreeOpenResult>;
  OpenGlobalTab(topicID: string): Promise<TabMeta>;
  OpenTopicSession(scope: string, workspaceRoot: string, topicID: string, sessionPath: string): Promise<TabMeta>;
  EnsureBlankTab(scope: string, workspaceRoot: string): Promise<TabMeta>;
  ActivateTopic(scope: string, workspaceRoot: string, topicID: string, sessionPath: string): Promise<TabMeta>;
  EnsureBlankSurface(scope: string, workspaceRoot: string): Promise<TabMeta>;
  SetActiveTab(tabID: string): Promise<void>;
  ReorderTabs(tabIDs: string[]): Promise<void>;
  CloseTab(tabID: string): Promise<void>;
  ListProjectTree(): Promise<ProjectNode[]>;
  RenameProject(workspaceRoot: string, title: string): Promise<void>;
  SetProjectColor(workspaceRoot: string, color: string): Promise<void>;
  SetProjectPinned(workspaceRoot: string, pinned: boolean): Promise<void>;
  ReorderProjects(workspaceRoots: string[]): Promise<void>;
  CreateTopic(scope: string, workspaceRoot: string, title: string): Promise<TopicMeta>;
  RenameTopic(topicID: string, title: string): Promise<void>;
  DeleteTopic(topicID: string): Promise<void>;
  TrashTopic(topicID: string): Promise<void>;
  SetTopicPinned(topicID: string, pinned: boolean): Promise<void>;
  ContextPanel(tabID: string): Promise<ContextPanelInfo>;
  // New native-feel bindings (added with the desktop native-feel plan).
  ConfirmAction(req: NativeConfirmRequest): Promise<boolean>;
  SaveWindowState(state: DesktopWindowState): Promise<void>;
  // ── Remote (SSH) ──
  RemoteHosts(): Promise<RemoteHostView[]>;
  AddRemoteHost(input: RemoteHostInput): Promise<RemoteHostView>;
  UpdateRemoteHost(id: string, input: RemoteHostInput): Promise<RemoteHostView>;
  RemoveRemoteHost(id: string): Promise<void>;
  ScanSSHConfig(): Promise<RemoteHostInput[]>;
  ConnectRemoteHost(id: string): Promise<void>;
  DisconnectRemoteHost(id: string): Promise<void>;
  RemoteConnectionStatuses(): Promise<RemoteConnectionStatus[]>;
  ConfirmRemoteHostKey(hostId: string, accept: boolean): Promise<void>;
  ConfirmRemoteSecret(hostId: string, promptId: string, secret: string, accept: boolean): Promise<void>;
  ListRemoteDir(hostId: string, path: string): Promise<RemoteDirEntry[]>;
  ReadRemoteFile(hostId: string, path: string): Promise<RemoteFilePreview>;
  WriteRemoteFile(hostId: string, path: string, body: string, expectMtimeUnix: number): Promise<RemoteWriteResult>;
  MkdirRemote(hostId: string, path: string): Promise<void>;
  RenameRemotePath(hostId: string, oldPath: string, newPath: string): Promise<void>;
  DeleteRemotePath(hostId: string, path: string, recursive: boolean): Promise<void>;
  RemoteForwards(hostId: string): Promise<RemoteForwardView[]>;
  AddRemoteForward(hostId: string, input: RemoteForwardInput): Promise<RemoteForwardView>;
  RemoveRemoteForward(hostId: string, forwardId: string): Promise<void>;
  EnsureRemoteServer(hostId: string, workspace: string): Promise<void>;
  OpenRemoteWorkspace(hostId: string, workspace: string): Promise<void>;
  StopRemoteServer(hostId: string): Promise<void>;
  RemoteServerStatus(hostId: string): Promise<RemoteServerView>;
  RemoteServerLogs(hostId: string, tailLines: number): Promise<string>;
  RemoteLastWorkspace(hostId: string): Promise<string>;
}

// Compile-time drift check. Exclude<A, B> extracts keys in A that are missing
// from B. If that set is non-empty, AssertNever<non-never> fails with
// "Type 'X' does not satisfy the constraint 'never'".
// _CheckGenToApp errors mean a generated Go method has no TS counterpart.
// These compare method *names* only; full signature checking isn't possible here
// because local types (types.ts) use plain interfaces while generated types
// (models.ts) use classes with a convertValues prototype method. The structural
// mismatch would produce false positives. Method-arity and parameter-order drift
// are caught at the call sites by tsc when components invoke app.<method>(...).
type AssertNever<T extends never> = T;
type GeneratedAppKeys = keyof typeof GeneratedApp;
type GeneratedAppMissing =
  string extends GeneratedAppKeys ? true :
  number extends GeneratedAppKeys ? true :
  symbol extends GeneratedAppKeys ? true :
  false;
export type _CheckGenToApp = AssertNever<
  GeneratedAppMissing extends true ? never : Exclude<GeneratedAppKeys, keyof AppBindings>
>;

interface WailsRuntime {
  EventsOn(name: string, cb: (...data: unknown[]) => void): () => void;
  BrowserOpenURL(url: string): void;
  WindowSetSystemDefaultTheme?(): void;
  WindowSetLightTheme?(): void;
  WindowSetDarkTheme?(): void;
  WindowSetBackgroundColour?(r: number, g: number, b: number, a: number): void;
  WindowGetSize?(): Promise<{ w: number; h: number }>;
  WindowGetPosition?(): Promise<{ x: number; y: number }>;
  WindowIsMaximised?(): Promise<boolean>;
  ClipboardSetText?(text: string): Promise<boolean>;
  // Native OS file drop (desktop only); useDropTarget gates delivery to elements
  // carrying the --wails-drop-target CSS property. Absent in the browser dev mock.
  OnFileDrop?(cb: (x: number, y: number, paths: string[]) => void, useDropTarget: boolean): void;
  OnFileDropOff?(): void;
}

declare global {
  interface Window {
    runtime?: WailsRuntime;
    go?: { main?: { App?: AppBindings } };
  }
}

// Must match desktop/app.go's eventChannel constant.
const EVENT_CHANNEL = "agent:event";
const RECENT_NATIVE_FILE_DRAG_MS = 2000;
const WAILS_NON_FILE_DRAG_MESSAGE = "additional File object is not a file on the disk";
const UNCAUGHT_ERROR_PREFIX_RE = /^Uncaught(?:\s+\(in promise\))?(?:\s+\w*Error)?:\s*/i;
const WAILS_IPC_CONNECTING_RE = /Failed to execute 'send' on 'WebSocket': Still in CONNECTING state/i;
const WAILS_IPC_NULL_SEND_RE = /Cannot read properties of null \(reading 'send'\)/i;

// Resolve the Wails binding at CALL time, not module-load time: in dev the Wails
// runtime can inject window.go AFTER this module first evaluates, so snapshotting
// once would pin the browser mock for the whole session (and show fake data — the
// dev mock's model list leaking into the real app was exactly this bug).
function realApp(): AppBindings | undefined {
  return typeof window !== "undefined" ? window.go?.main?.App : undefined;
}

let mockSingleton: AppBindings | null = null;
function getMock(): AppBindings {
  if (!mockSingleton) mockSingleton = makeMockApp();
  return mockSingleton;
}

// onEvent subscribes to the agent's typed event stream; returns an unsubscribe.
export function onEvent(cb: (e: WireEvent) => void): () => void {
  if (realApp() && typeof window !== "undefined" && window.runtime) {
    return window.runtime.EventsOn(EVENT_CHANNEL, (payload) => cb(payload as WireEvent));
  }
  return mockSubscribe(cb);
}

// onUpdaterProgress subscribes to the auto-updater's progress events (a separate
// channel from the agent stream); returns an unsubscribe. Must match the event
// name emitted in desktop/updater_app.go.
export function onUpdaterProgress(cb: (p: UpdateProgress) => void): () => void {
  if (realApp() && typeof window !== "undefined" && window.runtime) {
    return window.runtime.EventsOn("updater:progress", (p) => cb(p as UpdateProgress));
  }
  updaterListeners.add(cb);
  return () => {
    updaterListeners.delete(cb);
  };
}

function errorMessage(err: unknown): string {
  if (err && typeof err === "object" && "message" in err) {
    const msg = (err as { message?: unknown }).message;
    if (typeof msg === "string") return msg;
  }
  return String(err);
}

export function isWailsNonFileDragError(err: unknown, recentNativeFileDrag = false): boolean {
  const msg = errorMessage(err).trim().replace(UNCAUGHT_ERROR_PREFIX_RE, "");
  if (msg.includes(WAILS_NON_FILE_DRAG_MESSAGE)) return true;
  return recentNativeFileDrag && msg.toLowerCase() === "invalid argument";
}

export function isWailsNonFileDragErrorEvent(
  event: Pick<ErrorEvent, "error" | "message">,
  recentNativeFileDrag = false,
): boolean {
  if (isWailsNonFileDragError(event.error ?? event.message, recentNativeFileDrag)) return true;
  return event.error != null && isWailsNonFileDragError(event.message, recentNativeFileDrag);
}

export function isTransientWailsIPCError(err: unknown): boolean {
  const msg = errorMessage(err).trim().replace(UNCAUGHT_ERROR_PREFIX_RE, "");
  return WAILS_IPC_CONNECTING_RE.test(msg) || WAILS_IPC_NULL_SEND_RE.test(msg);
}

function dataTransferLooksLikeFileDrag(dt: DataTransfer | null): boolean {
  if (!dt) return false;
  if (dt.files?.length > 0) return true;
  return Array.from(dt.types ?? []).includes("Files");
}

let wailsDragSuppressionRefs = 0;
let wailsDragSuppressionUninstall: (() => void) | null = null;
let lastNativeFileDragAt = 0;

export function installWailsNonFileDragErrorSuppression(): () => void {
  if (typeof window === "undefined") return () => {};

  wailsDragSuppressionRefs += 1;
  if (!wailsDragSuppressionUninstall) {
    const markNativeFileDrag = (e: DragEvent) => {
      if (dataTransferLooksLikeFileDrag(e.dataTransfer)) lastNativeFileDragAt = Date.now();
    };
    const hasRecentNativeFileDrag = () => Date.now() - lastNativeFileDragAt <= RECENT_NATIVE_FILE_DRAG_MS;
    const suppressNonFileDragError = (e: ErrorEvent) => {
      if (isWailsNonFileDragErrorEvent(e, hasRecentNativeFileDrag()) || isTransientWailsIPCError(e.error ?? e.message)) {
        e.preventDefault();
      }
    };
    const suppressNonFileDragRejection = (e: PromiseRejectionEvent) => {
      if (isWailsNonFileDragError(e.reason, hasRecentNativeFileDrag()) || isTransientWailsIPCError(e.reason)) {
        e.preventDefault();
      }
    };

    window.addEventListener("dragenter", markNativeFileDrag, true);
    window.addEventListener("dragover", markNativeFileDrag, true);
    window.addEventListener("drop", markNativeFileDrag, true);
    window.addEventListener("error", suppressNonFileDragError);
    window.addEventListener("unhandledrejection", suppressNonFileDragRejection);
    wailsDragSuppressionUninstall = () => {
      window.removeEventListener("dragenter", markNativeFileDrag, true);
      window.removeEventListener("dragover", markNativeFileDrag, true);
      window.removeEventListener("drop", markNativeFileDrag, true);
      window.removeEventListener("error", suppressNonFileDragError);
      window.removeEventListener("unhandledrejection", suppressNonFileDragRejection);
      lastNativeFileDragAt = 0;
    };
  }

  let disposed = false;
  return () => {
    if (disposed) return;
    disposed = true;
    wailsDragSuppressionRefs = Math.max(0, wailsDragSuppressionRefs - 1);
    if (wailsDragSuppressionRefs === 0 && wailsDragSuppressionUninstall) {
      wailsDragSuppressionUninstall();
      wailsDragSuppressionUninstall = null;
    }
  };
}

// onFilesDropped subscribes to native OS file drops landing on the composer (the
// --wails-drop-target element); the callback gets the dropped files' absolute
// paths. No-op in the browser dev mock, where the runtime is absent.
export function onFilesDropped(cb: (paths: string[]) => void): () => void {
  const rt = typeof window !== "undefined" ? window.runtime : undefined;
  if (!rt?.OnFileDrop) return () => {};

  // Wails' internal ResolveFilePaths throws when a non-file object (e.g. the
  // window icon) is dragged onto the webview. The error is uncaught and crashes
  // the app. Intercept it here so only real file drops reach the callback.
  const uninstallDragSuppression = installWailsNonFileDragErrorSuppression();

  rt.OnFileDrop((_x, _y, paths) => {
    if (Array.isArray(paths) && paths.length > 0) cb(paths);
  }, true);
  return () => {
    rt.OnFileDropOff?.();
    uninstallDragSuppression();
  };
}

// onReady subscribes to the agent:ready event fired when boot.Build completes.
// The frontend re-fetches Meta/Context/History when this lands.
// onRuntimeRebuilt fires when a tab's controller is replaced in place
// (model/effort/token-mode switch, clear-while-running). The rebuilt
// controller restarts prompt ids, so per-tab id-keyed state must reset.
export function onRuntimeRebuilt(cb: (tabId?: string) => void): () => void {
  if (realApp() && typeof window !== "undefined" && window.runtime) {
    return window.runtime.EventsOn("runtime:rebuilt", (tabId?: unknown) => cb(typeof tabId === "string" ? tabId : undefined));
  }
  return () => {};
}

export function onReady(cb: (tabId?: string) => void): () => void {
  if (realApp() && typeof window !== "undefined" && window.runtime) {
    return window.runtime.EventsOn("agent:ready", (tabId?: unknown) => cb(typeof tabId === "string" ? tabId : undefined));
  }
  // In dev mock, fire immediately since there's no real boot sequence.
  cb();
  return () => {};
}

export function onProjectTreeChanged(cb: () => void): () => void {
  if (realApp() && typeof window !== "undefined" && window.runtime) {
    return window.runtime.EventsOn("project-tree:changed", () => cb());
  }
  return () => {};
}

export function onSessionRecovered(cb: (payload: SessionRecoveryEvent) => void): () => void {
  if (realApp() && typeof window !== "undefined" && window.runtime) {
    return window.runtime.EventsOn("session:recovered", (payload?: unknown) => cb((payload ?? {}) as SessionRecoveryEvent));
  }
  return () => {};
}

export function onSessionRecoveryFailed(cb: (payload: SessionRecoveryFailedEvent) => void): () => void {
  if (realApp() && typeof window !== "undefined" && window.runtime) {
    return window.runtime.EventsOn("session:recovery-failed", (payload?: unknown) => cb((payload ?? {}) as SessionRecoveryFailedEvent));
  }
  return () => {};
}

export function onRemoteStatus(cb: (s: RemoteConnectionStatus) => void): () => void {
  if (realApp() && typeof window !== "undefined" && window.runtime) {
    return window.runtime.EventsOn("remote:status", (payload?: unknown) => cb((payload ?? {}) as RemoteConnectionStatus));
  }
  return registerMockRemoteListener("status", cb as (v: unknown) => void);
}

export function onRemoteForwards(cb: (e: RemoteForwardsEvent) => void): () => void {
  if (realApp() && typeof window !== "undefined" && window.runtime) {
    return window.runtime.EventsOn("remote:forwards", (payload?: unknown) => cb((payload ?? {}) as RemoteForwardsEvent));
  }
  return registerMockRemoteListener("forwards", cb as (v: unknown) => void);
}

export function onRemoteServer(cb: (s: RemoteServerView) => void): () => void {
  if (realApp() && typeof window !== "undefined" && window.runtime) {
    return window.runtime.EventsOn("remote:server", (payload?: unknown) => cb((payload ?? {}) as RemoteServerView));
  }
  return registerMockRemoteListener("server", cb as (v: unknown) => void);
}

// Mock event fan-out so browser-dev and tsx tests can drive remote:* events
// without a Wails runtime.
type MockRemoteChannel = "status" | "forwards" | "server";
const mockRemoteListeners: Record<MockRemoteChannel, Set<(v: unknown) => void>> = {
  status: new Set(),
  forwards: new Set(),
  server: new Set(),
};
function registerMockRemoteListener(ch: MockRemoteChannel, cb: (v: unknown) => void): () => void {
  mockRemoteListeners[ch].add(cb);
  return () => mockRemoteListeners[ch].delete(cb);
}
export function __emitMockRemote(ch: MockRemoteChannel, payload: unknown): void {
  for (const cb of mockRemoteListeners[ch]) cb(payload);
}

// app proxies each call to the live binding (or the dev mock only when truly
// outside the shell), so a late-injected window.go is picked up transparently.
function bridgeBreadcrumb(method: string): string {
  if (method === "ReportCrash") return "";
  if (/^(Submit|SubmitDisplay|RunShell|Steer|Cancel|Approve|AnswerQuestion|ReplayPendingPrompts)/.test(method))
    return `turn ${method}`;
  if (/^(SetModel|SetEffort|SetTokenMode|SetDefaultModel|SetPlannerModel|SetSubagentModel|SetSubagentEffort|SetMaxSubagentDepth|SetMaxSubagentConcurrency|SetMaxParallelWriters)/.test(method))
    return `model ${method}`;
  if (/^(SetDesktop|SetCloseBehavior|SetDisplayMode|SetStatusBar|SetExpandThinking|SetAutoPlan|SetDefaultToolApprovalMode|SetReasoningLanguage)/.test(method))
    return `settings ${method}`;
  if (/^(SaveProvider|AddOfficialProviderAccess|AddProviderPresetAccess|ResetProviderPresetAccess|RemoveProviderAccess|DeleteProvider|SaveProviderKey|SetProviderKey|ClearProviderKey|FetchProviderModels|ConnectKey)/.test(method))
    return `provider ${method}`;
  if (/^(CheckUpdate|DownloadUpdate|InstallUpdate|ApplyUpdate|OpenDownloadPage)/.test(method)) return `update ${method}`;
  if (/^(AddMCPServer|UpdateMCPServer|RemoveMCPServer|AuthorizeAndConnectMCPServer|ReconnectMCPServer|ClearMCPServerAuthentication|SetMCPServer)/.test(method))
    return `mcp ${method}`;
  if (/^(AddSkillPath|RemoveSkillPath|RefreshSkills|SetSkillEnabled|AcceptSkillSuggestion|AvailableSubagentTools|CreateSubagentProfile|UpdateSubagentProfile|DeleteSubagentProfile|SetSubagentProfileModel|SetSubagentProfileEffort|TrySubagentProfile|CancelTrySubagentProfile)/.test(method))
    return `skill ${method}`;
  if (/^(MinimiseMainWindow|ToggleMaximiseMainWindow|IsMainWindowMaximised|CloseMainWindow)$/.test(method)) return `window ${method}`;
  if (/^(OpenProjectTab|OpenGlobalTab|OpenTopicSession|EnsureBlankTab|ActivateTopic|EnsureBlankSurface|SetActiveTab|CloseTab|ReorderTabs|CreateTopic|RenameTopic|DeleteTopic|TrashTopic|RenameProject|RemoveWorkspace|SwitchWorkspace|PickWorkspace|DeliveryWorktreeAvailability|CreateDeliveryWorktree)/.test(method))
    return `nav ${method}`;
  return "";
}

function elapsedMs(startedAt: number): number {
  const now = typeof performance !== "undefined" ? performance.now() : Date.now();
  return Math.max(0, Math.round(now - startedAt));
}

export const app: AppBindings = new Proxy({} as AppBindings, {
  get(_t, prop) {
    const target = realApp() ?? getMock();
    const v = (target as unknown as Record<string, unknown>)[String(prop)];
    if (typeof v !== "function") return v;
    return (...args: unknown[]) => {
      const method = String(prop);
      const crumb = bridgeBreadcrumb(method);
      const startedAt = crumb ? (typeof performance !== "undefined" ? performance.now() : Date.now()) : 0;
      if (crumb) addBreadcrumb("bridge", crumb);
      try {
        const result = (v as (...a: unknown[]) => unknown).apply(target, args);
        if (result && typeof (result as Promise<unknown>).then === "function") {
          return (result as Promise<unknown>).then(
            (value) => {
              if (crumb) addBreadcrumb("bridge", `${crumb} done ms=${elapsedMs(startedAt)}`);
              return value;
            },
            (err) => {
              if (crumb) addBreadcrumb("bridge.error", `${method} ms=${elapsedMs(startedAt)}`);
              throw err;
            },
          );
        }
        if (crumb) addBreadcrumb("bridge", `${crumb} done ms=${elapsedMs(startedAt)}`);
        return result;
      } catch (err) {
        if (crumb) addBreadcrumb("bridge.error", `${method} ms=${elapsedMs(startedAt)}`);
        throw err;
      }
    };
  },
});

// openExternal opens a URL in the system browser (so links in rendered markdown
// don't navigate the webview away from the app). Falls back to window.open in the
// browser dev mock.
export function openExternal(url: string): void {
  if (typeof window !== "undefined" && window.runtime?.BrowserOpenURL) {
    window.runtime.BrowserOpenURL(url);
  } else if (typeof window !== "undefined") {
    window.open(url, "_blank", "noopener");
  }
}

// --- browser dev mock --------------------------------------------------------

const listeners = new Set<(e: WireEvent) => void>();
let mockScopedTabId: string | undefined;

function mockSubscribe(cb: (e: WireEvent) => void): () => void {
  listeners.add(cb);
  return () => {
    listeners.delete(cb);
  };
}

function emit(e: WireEvent) {
  const event = mockScopedTabId && !e.tabId ? { ...e, tabId: mockScopedTabId } : e;
  listeners.forEach((l) => l(event));
}

export function mockToolApprovalModeAfterModeChange(current: string | undefined, nextMode: Mode): ToolApprovalMode {
  if (modeHasAutoApproveTools(nextMode)) return "yolo";
  const currentMode = normalizeToolApprovalMode(current);
  return currentMode === "yolo" ? "ask" : currentMode;
}

async function withMockTabScope<T>(tabId: string, fn: () => Promise<T>): Promise<T> {
  const previous = mockScopedTabId;
  mockScopedTabId = tabId || previous;
  try {
    return await fn();
  } finally {
    mockScopedTabId = previous;
  }
}

// Updater progress has its own listener set so the browser dev mock can stream a
// fake download/install flow through onUpdaterProgress.
const updaterListeners = new Set<(p: UpdateProgress) => void>();

function emitUpdater(p: UpdateProgress) {
  updaterListeners.forEach((l) => l(p));
}

function delay(ms: number): Promise<void> {
  return new Promise((r) => setTimeout(r, ms));
}

function baseName(path: string): string {
  return path.replace(/[/\\]+$/, "").split(/[/\\]/).filter(Boolean).pop() ?? path;
}

function browserPlatformOverride(): "darwin" | "windows" | "linux" | "" {
  if (typeof window === "undefined" || window.runtime) return "";
  const value = new URLSearchParams(window.location.search).get("platform");
  return value === "darwin" || value === "windows" || value === "linux" ? value : "";
}

function browserPreviewBashSandboxMode(): "enforce" | "off" {
  return browserPlatformOverride() === "windows" ? "off" : "enforce";
}

function browserPreviewEffectiveShell(prefer = "auto"): "bash" | "git-bash" | "powershell" | "pwsh" {
  const normalized = prefer.trim().toLowerCase();
  if (normalized === "powershell" || normalized === "pwsh") return normalized;
  return browserPlatformOverride() === "windows" ? "git-bash" : "bash";
}

function mockScenario(): "demo" | "fresh" | "running" | "guidance" | "sandbox_escape" | "notice" {
  if (typeof window === "undefined") return "demo";
  const value = new URLSearchParams(window.location.search).get("mock")?.trim().toLowerCase();
  if (value === "fresh" || value === "empty" || value === "first-run") return "fresh";
  if (value === "guidance" || value === "guide" || value === "steer") return "guidance";
  if (value === "running" || value === "busy" || value === "streaming") return "running";
  if (value === "sandbox_escape" || value === "sandbox-escape" || value === "sandboxescape") return "sandbox_escape";
  if (value === "notice" || value === "notices" || value === "notice-preview") return "notice";
  return "demo";
}

type MockProviderPresetTemplate = {
  id: string;
  label: string;
  description: string;
  keyEnv: string;
  provider: ProviderView;
};

function mockProviderTemplate(p: Pick<ProviderView, "name" | "kind" | "baseUrl" | "models" | "default" | "apiKeyEnv"> & Partial<ProviderView>): ProviderView {
  return {
    name: p.name,
    builtIn: false,
    added: true,
    kind: p.kind,
    baseUrl: p.baseUrl,
    modelsUrl: p.modelsUrl ?? "",
    models: p.models,
    visionModels: p.visionModels ?? [],
    visionModelsConfigured: Boolean(p.visionModelsConfigured ?? ((p.visionModels ?? []).length > 0)),
    default: p.default,
    apiKeyEnv: p.apiKeyEnv,
    headers: p.headers,
    extraBody: p.extraBody,
    authHeader: p.authHeader,
    keySet: Boolean(p.keySet),
    balanceUrl: p.balanceUrl ?? "",
    contextWindow: p.contextWindow ?? 0,
    reasoningProtocol: p.reasoningProtocol ?? "",
    thinking: p.thinking ?? "",
    supportedEfforts: p.supportedEfforts ?? [],
    defaultEffort: p.defaultEffort ?? "",
    modelOverrides: p.modelOverrides,
  };
}

function mockPreset(id: string, label: string, description: string, keyEnv: string, provider: ProviderView): MockProviderPresetTemplate {
  return { id, label, description, keyEnv, provider };
}

const mockKimiAPIModels = ["kimi-k2.7-code", "kimi-k2.7-code-highspeed", "kimi-k2.6", "kimi-k2.5"];
const mockLongCatModels = ["LongCat-2.0"];
const mockMiMoV25Models = ["mimo-v2.5-pro", "mimo-v2.5"];
const mockMiniMaxModels = ["MiniMax-M3", "MiniMax-M2.7", "MiniMax-M2.7-highspeed"];
const mockGLMAPIModels = ["glm-5.2", "glm-5.1", "glm-5", "glm-5-turbo", "glm-5v-turbo", "glm-4.7", "glm-4.7-flash", "glm-4.7-flashx", "glm-4.6", "glm-4.5", "glm-4.5-air", "glm-4.5-flash"];
const mockGLMCodingModels = ["glm-5.2", "glm-5.1", "glm-5", "glm-4.7"];
const mockGLMAnthropicModels = ["glm-5.2[1m]", "glm-5.2", "glm-5.1", "glm-5", "glm-4.7", "glm-4.5-air"];
const mockQwenAPIModels = ["qwen3.7-plus", "qwen3.7-max", "qwen3.6-plus", "qwen3.5-plus", "qwen3-max-2026-01-23", "qwen3-coder-next", "qwen3-coder-plus", "MiniMax-M2.5", "glm-5", "glm-4.7", "kimi-k2.5"];
const mockQwenPlanModels = ["qwen3.7-plus", "qwen3.6-plus", "kimi-k2.5", "glm-5", "MiniMax-M2.5", "qwen3.5-plus", "qwen3-max-2026-01-23", "qwen3-coder-next", "qwen3-coder-plus", "glm-4.7"];
const mockQwenPlanVisionModels = ["qwen3.7-plus", "qwen3.6-plus", "qwen3.5-plus", "kimi-k2.5"];
const mockStepFunModels = ["step-3.7-flash", "step-3.5-flash", "step-3.5-flash-2603"];
const mockOpenCodeGoModels = ["glm-5.2", "glm-5.1", "kimi-k2.7-code", "kimi-k2.6", "deepseek-v4-pro", "deepseek-v4-flash", "mimo-v2.5-pro", "mimo-v2.5"];
const mockOpenCodeGoAnthropicModels = ["qwen3.7-plus", "qwen3.7-max", "qwen3.6-plus", "minimax-m3", "minimax-m2.7", "minimax-m2.5"];
const mockOpenCodeZenAnthropicModels = ["claude-sonnet-4-6", "claude-opus-4-8", "claude-haiku-4-5", "qwen3.6-plus", "qwen3.5-plus", "qwen3.6-plus-free"];
const mockNovitaModels = ["zai-org/glm-5.2", "moonshotai/kimi-k2.7-code", "minimax/minimax-m3", "deepseek/deepseek-v4-pro", "deepseek/deepseek-v4-flash", "qwen/qwen3.7-max", "qwen/qwen3.6-plus", "zai-org/glm-5v-turbo"];
const mockGMIModels = ["zai-org/GLM-5.2-FP8", "deepseek-ai/DeepSeek-V4-Pro", "deepseek-ai/DeepSeek-V4-Flash", "moonshotai/Kimi-K2.7-Code", "anthropic/claude-sonnet-4.6", "openai/gpt-5.5"];
const mockVercelModels = ["anthropic/claude-sonnet-4.6", "anthropic/claude-opus-4.8", "openai/gpt-5.4", "openai/gpt-5.4-pro", "moonshotai/kimi-k2.7-code", "zai/glm-5.2", "deepseek/deepseek-v4-pro"];
const mockOllamaCloudModels = ["glm-5.2", "kimi-k2.7-code", "deepseek-v4-pro", "deepseek-v4-flash", "minimax-m3", "nemotron-3-nano:30b", "qwen3-coder-next"];

const mockProviderPresetTemplates: MockProviderPresetTemplate[] = [
  mockPreset("longcat-openai", "LongCat OpenAI", "LongCat Platform OpenAI-compatible endpoint for LongCat-2.0.", "LONGCAT_API_KEY", mockProviderTemplate({ name: "longcat-openai", kind: "openai", baseUrl: "https://api.longcat.chat/openai/v1", modelsUrl: "https://api.longcat.chat/openai/v1/models", models: mockLongCatModels, default: "LongCat-2.0", apiKeyEnv: "LONGCAT_API_KEY", contextWindow: 131072, thinking: "enabled", supportedEfforts: ["enabled", "disabled"], defaultEffort: "enabled" })),
  mockPreset("longcat-anthropic", "LongCat Anthropic", "LongCat Platform Anthropic-compatible Messages endpoint for LongCat-2.0.", "LONGCAT_API_KEY", mockProviderTemplate({ name: "longcat-anthropic", kind: "anthropic", baseUrl: "https://api.longcat.chat/anthropic", modelsUrl: "https://api.longcat.chat/anthropic/v1/models", models: mockLongCatModels, default: "LongCat-2.0", apiKeyEnv: "LONGCAT_API_KEY", authHeader: true, contextWindow: 131072, thinking: "enabled", supportedEfforts: ["enabled", "disabled"], defaultEffort: "enabled" })),
  mockPreset("kimi-cn", "Kimi CN API", "Moonshot Kimi China OpenAI-compatible API.", "KIMI_API_KEY", mockProviderTemplate({ name: "kimi-cn", kind: "openai", baseUrl: "https://api.moonshot.cn/v1", models: mockKimiAPIModels, visionModels: mockKimiAPIModels, default: "kimi-k2.7-code", apiKeyEnv: "KIMI_API_KEY", balanceUrl: "https://api.moonshot.cn/v1/users/me/balance", contextWindow: 262144, reasoningProtocol: "none" })),
  mockPreset("kimi-global", "Kimi Global API", "Moonshot Kimi international OpenAI-compatible API.", "MOONSHOT_API_KEY", mockProviderTemplate({ name: "kimi-global", kind: "openai", baseUrl: "https://api.moonshot.ai/v1", models: mockKimiAPIModels, visionModels: mockKimiAPIModels, default: "kimi-k2.7-code", apiKeyEnv: "MOONSHOT_API_KEY", balanceUrl: "https://api.moonshot.ai/v1/users/me/balance", contextWindow: 262144, reasoningProtocol: "none" })),
  mockPreset("kimi-coding-plan", "Kimi Coding Plan", "Kimi Coding Plan via its dedicated Anthropic-compatible endpoint.", "KIMI_CODING_API_KEY", mockProviderTemplate({ name: "kimi-coding-plan", kind: "anthropic", baseUrl: "https://api.kimi.com/coding/", models: ["kimi-for-coding"], visionModels: ["kimi-for-coding"], default: "kimi-for-coding", apiKeyEnv: "KIMI_CODING_API_KEY", headers: { "User-Agent": "claude-code/0.1.0" }, thinking: "adaptive", contextWindow: 262144 })),
  mockPreset("mimo-api", "MiMo API", "Xiaomi MiMo direct API with text and vision-capable models.", "MIMO_API_KEY", mockProviderTemplate({ name: "mimo-api", kind: "openai", baseUrl: "https://api.xiaomimimo.com/v1", models: mockMiMoV25Models, visionModels: ["mimo-v2.5"], default: "mimo-v2.5-pro", apiKeyEnv: "MIMO_API_KEY", contextWindow: 1048576 })),
  mockPreset("mimo-anthropic", "MiMo Anthropic", "Xiaomi MiMo direct Anthropic-compatible endpoint.", "MIMO_API_KEY", mockProviderTemplate({ name: "mimo-anthropic", kind: "anthropic", baseUrl: "https://api.xiaomimimo.com/anthropic", models: mockMiMoV25Models, visionModels: ["mimo-v2.5"], default: "mimo-v2.5-pro", apiKeyEnv: "MIMO_API_KEY", thinking: "adaptive", contextWindow: 1048576 })),
  mockPreset("mimo-token-plan-cn", "MiMo Token Plan CN", "Xiaomi MiMo token-plan China endpoint.", "MIMO_TOKEN_PLAN_API_KEY", mockProviderTemplate({ name: "mimo-token-plan-cn", kind: "openai", baseUrl: "https://token-plan-cn.xiaomimimo.com/v1", models: mockMiMoV25Models, visionModels: ["mimo-v2.5"], default: "mimo-v2.5-pro", apiKeyEnv: "MIMO_TOKEN_PLAN_API_KEY", contextWindow: 1048576 })),
  mockPreset("mimo-token-plan-cn-anthropic", "MiMo Token Plan CN Anthropic", "Xiaomi MiMo token-plan China Anthropic-compatible endpoint.", "MIMO_TOKEN_PLAN_API_KEY", mockProviderTemplate({ name: "mimo-token-plan-cn-anthropic", kind: "anthropic", baseUrl: "https://token-plan-cn.xiaomimimo.com/anthropic", models: mockMiMoV25Models, visionModels: ["mimo-v2.5"], default: "mimo-v2.5-pro", apiKeyEnv: "MIMO_TOKEN_PLAN_API_KEY", thinking: "adaptive", contextWindow: 1048576 })),
  mockPreset("mimo-token-plan-sgp", "MiMo Token Plan SGP", "Xiaomi MiMo token-plan Singapore endpoint.", "MIMO_TOKEN_PLAN_API_KEY", mockProviderTemplate({ name: "mimo-token-plan-sgp", kind: "openai", baseUrl: "https://token-plan-sgp.xiaomimimo.com/v1", models: mockMiMoV25Models, visionModels: ["mimo-v2.5"], default: "mimo-v2.5-pro", apiKeyEnv: "MIMO_TOKEN_PLAN_API_KEY", contextWindow: 1048576 })),
  mockPreset("mimo-token-plan-sgp-anthropic", "MiMo Token Plan SGP Anthropic", "Xiaomi MiMo token-plan Singapore Anthropic-compatible endpoint.", "MIMO_TOKEN_PLAN_API_KEY", mockProviderTemplate({ name: "mimo-token-plan-sgp-anthropic", kind: "anthropic", baseUrl: "https://token-plan-sgp.xiaomimimo.com/anthropic", models: mockMiMoV25Models, visionModels: ["mimo-v2.5"], default: "mimo-v2.5-pro", apiKeyEnv: "MIMO_TOKEN_PLAN_API_KEY", thinking: "adaptive", contextWindow: 1048576 })),
  mockPreset("mimo-token-plan-ams", "MiMo Token Plan AMS", "Xiaomi MiMo token-plan Amsterdam endpoint.", "MIMO_TOKEN_PLAN_API_KEY", mockProviderTemplate({ name: "mimo-token-plan-ams", kind: "openai", baseUrl: "https://token-plan-ams.xiaomimimo.com/v1", models: mockMiMoV25Models, visionModels: ["mimo-v2.5"], default: "mimo-v2.5-pro", apiKeyEnv: "MIMO_TOKEN_PLAN_API_KEY", contextWindow: 1048576 })),
  mockPreset("mimo-token-plan-ams-anthropic", "MiMo Token Plan AMS Anthropic", "Xiaomi MiMo token-plan Amsterdam Anthropic-compatible endpoint.", "MIMO_TOKEN_PLAN_API_KEY", mockProviderTemplate({ name: "mimo-token-plan-ams-anthropic", kind: "anthropic", baseUrl: "https://token-plan-ams.xiaomimimo.com/anthropic", models: mockMiMoV25Models, visionModels: ["mimo-v2.5"], default: "mimo-v2.5-pro", apiKeyEnv: "MIMO_TOKEN_PLAN_API_KEY", thinking: "adaptive", contextWindow: 1048576 })),
  mockPreset("minimax-cn-api", "MiniMax CN API", "MiniMax China OpenAI-compatible M-series API endpoint.", "MINIMAX_API_KEY", mockProviderTemplate({ name: "minimax-cn-api", kind: "openai", baseUrl: "https://api.minimaxi.com/v1", models: mockMiniMaxModels, visionModels: ["MiniMax-M3"], default: "MiniMax-M3", apiKeyEnv: "MINIMAX_API_KEY", extraBody: { reasoning_split: true }, contextWindow: 1048576, thinking: "adaptive", supportedEfforts: ["disabled", "adaptive"], defaultEffort: "adaptive" })),
  mockPreset("minimax-global-api", "MiniMax Global API", "MiniMax international OpenAI-compatible M-series API endpoint.", "MINIMAX_API_KEY", mockProviderTemplate({ name: "minimax-global-api", kind: "openai", baseUrl: "https://api.minimax.io/v1", models: mockMiniMaxModels, visionModels: ["MiniMax-M3"], default: "MiniMax-M3", apiKeyEnv: "MINIMAX_API_KEY", extraBody: { reasoning_split: true }, contextWindow: 1048576, thinking: "adaptive", supportedEfforts: ["disabled", "adaptive"], defaultEffort: "adaptive" })),
  mockPreset("minimax-cn-anthropic", "MiniMax CN Anthropic", "MiniMax China Anthropic-compatible M-series endpoint.", "MINIMAX_PLAN_API_KEY", mockProviderTemplate({ name: "minimax-cn-anthropic", kind: "anthropic", baseUrl: "https://api.minimaxi.com/anthropic", models: mockMiniMaxModels, visionModels: ["MiniMax-M3"], default: "MiniMax-M3", apiKeyEnv: "MINIMAX_PLAN_API_KEY", authHeader: true, contextWindow: 1048576, thinking: "adaptive", supportedEfforts: ["disabled", "adaptive"], defaultEffort: "adaptive" })),
  mockPreset("minimax-global-anthropic", "MiniMax Global Anthropic", "MiniMax international Anthropic-compatible endpoint with Bearer auth.", "MINIMAX_API_KEY", mockProviderTemplate({ name: "minimax-global-anthropic", kind: "anthropic", baseUrl: "https://api.minimax.io/anthropic", models: mockMiniMaxModels, visionModels: ["MiniMax-M3"], default: "MiniMax-M3", apiKeyEnv: "MINIMAX_API_KEY", authHeader: true, contextWindow: 1048576, thinking: "adaptive", supportedEfforts: ["disabled", "adaptive"], defaultEffort: "adaptive" })),
  mockPreset("glm-cn", "GLM CN API", "Zhipu GLM China OpenAI-compatible API with thinking controls.", "GLM_API_KEY", mockProviderTemplate({ name: "glm-cn", kind: "openai", baseUrl: "https://open.bigmodel.cn/api/paas/v4", models: mockGLMAPIModels, visionModels: ["glm-5v-turbo"], default: "glm-5.2", apiKeyEnv: "GLM_API_KEY", contextWindow: 1000000, thinking: "enabled", supportedEfforts: ["enabled", "disabled"], defaultEffort: "enabled" })),
  mockPreset("zai-global", "Z.AI Global API", "Z.AI international OpenAI-compatible GLM API.", "ZAI_API_KEY", mockProviderTemplate({ name: "zai-global", kind: "openai", baseUrl: "https://api.z.ai/api/paas/v4", models: mockGLMAPIModels, visionModels: ["glm-5v-turbo"], default: "glm-5.2", apiKeyEnv: "ZAI_API_KEY", contextWindow: 1000000, thinking: "enabled", supportedEfforts: ["enabled", "disabled"], defaultEffort: "enabled" })),
  mockPreset("glm-coding-plan-cn", "GLM Coding Plan CN", "Zhipu GLM China coding-plan endpoint.", "GLM_PLAN_API_KEY", mockProviderTemplate({ name: "glm-coding-plan-cn", kind: "openai", baseUrl: "https://open.bigmodel.cn/api/coding/paas/v4", models: mockGLMCodingModels, default: "glm-5.2", apiKeyEnv: "GLM_PLAN_API_KEY", contextWindow: 1000000, thinking: "enabled", supportedEfforts: ["enabled", "disabled"], defaultEffort: "enabled" })),
  mockPreset("glm-coding-plan-cn-anthropic", "GLM Coding Plan CN Anthropic", "Zhipu GLM China coding-plan Anthropic-compatible endpoint.", "GLM_PLAN_API_KEY", mockProviderTemplate({ name: "glm-coding-plan-cn-anthropic", kind: "anthropic", baseUrl: "https://open.bigmodel.cn/api/anthropic", models: mockGLMAnthropicModels, default: "glm-5.2[1m]", apiKeyEnv: "GLM_PLAN_API_KEY", authHeader: true, thinking: "adaptive", contextWindow: 1000000 })),
  mockPreset("zai-coding-plan-global", "Z.AI Coding Plan Global", "Z.AI international coding-plan endpoint.", "ZAI_CODING_API_KEY", mockProviderTemplate({ name: "zai-coding-plan-global", kind: "openai", baseUrl: "https://api.z.ai/api/coding/paas/v4", models: mockGLMCodingModels, default: "glm-5.2", apiKeyEnv: "ZAI_CODING_API_KEY", contextWindow: 1000000, thinking: "enabled", supportedEfforts: ["enabled", "disabled"], defaultEffort: "enabled" })),
  mockPreset("zai-coding-plan-global-anthropic", "Z.AI Coding Plan Global Anthropic", "Z.AI international coding-plan Anthropic-compatible endpoint.", "ZAI_CODING_API_KEY", mockProviderTemplate({ name: "zai-coding-plan-global-anthropic", kind: "anthropic", baseUrl: "https://api.z.ai/api/anthropic", models: mockGLMAnthropicModels, default: "glm-5.2[1m]", apiKeyEnv: "ZAI_CODING_API_KEY", authHeader: true, thinking: "adaptive", contextWindow: 1000000 })),
  mockPreset("opencode-go", "OpenCode Go", "OpenCode Go relay with per-model capability overrides.", "OPENCODE_GO_API_KEY", mockProviderTemplate({ name: "opencode-go", kind: "openai", baseUrl: "https://opencode.ai/zen/go/v1", models: mockOpenCodeGoModels, default: "glm-5.2", apiKeyEnv: "OPENCODE_GO_API_KEY", contextWindow: 128000 })),
  mockPreset("opencode-go-anthropic", "OpenCode Go Anthropic", "OpenCode Go subscription Anthropic-compatible route for Qwen and MiniMax models.", "OPENCODE_GO_API_KEY", mockProviderTemplate({ name: "opencode-go-anthropic", kind: "anthropic", baseUrl: "https://opencode.ai/zen/go", models: mockOpenCodeGoAnthropicModels, visionModels: ["qwen3.7-plus", "qwen3.6-plus"], default: "qwen3.7-plus", apiKeyEnv: "OPENCODE_GO_API_KEY", thinking: "adaptive", contextWindow: 262144 })),
  mockPreset("opencode-zen-anthropic", "OpenCode Zen Anthropic", "OpenCode Zen Anthropic-compatible route for Claude and Qwen models.", "OPENCODE_API_KEY", mockProviderTemplate({ name: "opencode-zen-anthropic", kind: "anthropic", baseUrl: "https://opencode.ai/zen", models: mockOpenCodeZenAnthropicModels, visionModels: ["claude-sonnet-4-6", "claude-opus-4-8", "claude-haiku-4-5"], default: "claude-sonnet-4-6", apiKeyEnv: "OPENCODE_API_KEY", contextWindow: 262144 })),
  mockPreset("qwen-cn", "Qwen CN API", "Alibaba DashScope China standard OpenAI-compatible endpoint.", "QWEN_API_KEY", mockProviderTemplate({ name: "qwen-cn", kind: "openai", baseUrl: "https://dashscope.aliyuncs.com/compatible-mode/v1", models: mockQwenAPIModels, visionModels: ["qwen3.7-plus", "qwen3.6-plus", "qwen3.5-plus", "kimi-k2.5"], default: "qwen3.7-plus", apiKeyEnv: "QWEN_API_KEY" })),
  mockPreset("qwen-global", "Qwen Global API", "Alibaba DashScope international standard OpenAI-compatible endpoint.", "QWEN_API_KEY", mockProviderTemplate({ name: "qwen-global", kind: "openai", baseUrl: "https://dashscope-intl.aliyuncs.com/compatible-mode/v1", models: mockQwenAPIModels, visionModels: ["qwen3.7-plus", "qwen3.6-plus", "qwen3.5-plus", "kimi-k2.5"], default: "qwen3.7-plus", apiKeyEnv: "QWEN_API_KEY" })),
  mockPreset("qwen-coding-plan-cn", "Qwen Coding Plan CN", "Alibaba Cloud Qwen Coding Plan China endpoint.", "QWEN_CODING_API_KEY", mockProviderTemplate({ name: "qwen-coding-plan-cn", kind: "openai", baseUrl: "https://coding.dashscope.aliyuncs.com/v1", models: mockQwenPlanModels, visionModels: mockQwenPlanVisionModels, default: "qwen3.7-plus", apiKeyEnv: "QWEN_CODING_API_KEY" })),
  mockPreset("qwen-coding-plan-cn-anthropic", "Qwen Coding Plan CN Anthropic", "Alibaba Cloud Qwen Coding Plan China Anthropic-compatible endpoint.", "QWEN_CODING_API_KEY", mockProviderTemplate({ name: "qwen-coding-plan-cn-anthropic", kind: "anthropic", baseUrl: "https://coding.dashscope.aliyuncs.com/apps/anthropic", models: mockQwenPlanModels, visionModels: mockQwenPlanVisionModels, default: "qwen3.7-plus", apiKeyEnv: "QWEN_CODING_API_KEY", thinking: "adaptive" })),
  mockPreset("qwen-coding-plan-global", "Qwen Coding Plan Global", "Alibaba Cloud Qwen Coding Plan international endpoint.", "QWEN_CODING_API_KEY", mockProviderTemplate({ name: "qwen-coding-plan-global", kind: "openai", baseUrl: "https://coding-intl.dashscope.aliyuncs.com/v1", models: mockQwenPlanModels, visionModels: mockQwenPlanVisionModels, default: "qwen3.7-plus", apiKeyEnv: "QWEN_CODING_API_KEY" })),
  mockPreset("qwen-coding-plan-global-anthropic", "Qwen Coding Plan Global Anthropic", "Alibaba Cloud Qwen Coding Plan international Anthropic-compatible endpoint.", "QWEN_CODING_API_KEY", mockProviderTemplate({ name: "qwen-coding-plan-global-anthropic", kind: "anthropic", baseUrl: "https://coding-intl.dashscope.aliyuncs.com/apps/anthropic", models: mockQwenPlanModels, visionModels: mockQwenPlanVisionModels, default: "qwen3.7-plus", apiKeyEnv: "QWEN_CODING_API_KEY", thinking: "adaptive" })),
  mockPreset("stepfun", "StepFun", "StepFun coding-plan OpenAI-compatible endpoint.", "STEPFUN_API_KEY", mockProviderTemplate({ name: "stepfun", kind: "openai", baseUrl: "https://api.stepfun.com/step_plan/v1", models: mockStepFunModels, default: "step-3.7-flash", apiKeyEnv: "STEPFUN_API_KEY", supportedEfforts: ["low", "medium", "high"], defaultEffort: "medium" })),
  mockPreset("stepfun-anthropic", "StepFun Anthropic", "StepFun coding-plan Anthropic-compatible endpoint.", "STEPFUN_API_KEY", mockProviderTemplate({ name: "stepfun-anthropic", kind: "anthropic", baseUrl: "https://api.stepfun.com/step_plan", models: mockStepFunModels, default: "step-3.7-flash", apiKeyEnv: "STEPFUN_API_KEY", thinking: "adaptive", supportedEfforts: ["low", "medium", "high"], defaultEffort: "medium" })),
  mockPreset("novita", "NovitaAI", "NovitaAI OpenAI-compatible multi-model gateway.", "NOVITA_API_KEY", mockProviderTemplate({ name: "novita", kind: "openai", baseUrl: "https://api.novita.ai/openai/v1", models: mockNovitaModels, default: "zai-org/glm-5.2", apiKeyEnv: "NOVITA_API_KEY" })),
  mockPreset("gmi", "GMI Cloud", "GMI Cloud direct multi-model OpenAI-compatible gateway.", "GMI_API_KEY", mockProviderTemplate({ name: "gmi", kind: "openai", baseUrl: "https://api.gmi-serving.com/v1", models: mockGMIModels, default: "zai-org/GLM-5.2-FP8", apiKeyEnv: "GMI_API_KEY", headers: { "User-Agent": "Reasonix" } })),
  mockPreset("vercel-ai-gateway", "Vercel AI Gateway", "Vercel AI Gateway via Anthropic-compatible Messages API.", "AI_GATEWAY_API_KEY", mockProviderTemplate({ name: "vercel-ai-gateway", kind: "anthropic", baseUrl: "https://ai-gateway.vercel.sh", models: mockVercelModels, visionModels: ["anthropic/claude-sonnet-4.6", "anthropic/claude-opus-4.8", "openai/gpt-5.4", "openai/gpt-5.4-pro", "moonshotai/kimi-k2.7-code"], default: "anthropic/claude-sonnet-4.6", apiKeyEnv: "AI_GATEWAY_API_KEY", authHeader: true, contextWindow: 1000000 })),
  mockPreset("huggingface", "HuggingFace Router", "HuggingFace Inference Router OpenAI-compatible endpoint.", "HF_TOKEN", mockProviderTemplate({ name: "huggingface", kind: "openai", baseUrl: "https://router.huggingface.co/v1", models: ["zai-org/GLM-5.2", "deepseek-ai/DeepSeek-V3.2", "Qwen/Qwen3.5-72B-Instruct"], default: "zai-org/GLM-5.2", apiKeyEnv: "HF_TOKEN" })),
  mockPreset("nvidia", "NVIDIA NIM", "NVIDIA NIM OpenAI-compatible accelerated inference endpoint.", "NVIDIA_API_KEY", mockProviderTemplate({ name: "nvidia", kind: "openai", baseUrl: "https://integrate.api.nvidia.com/v1", models: ["nvidia/nemotron-3-nano-30b-a3b", "nvidia/nemotron-3-super-120b-a12b", "nvidia/nemotron-3-ultra-550b-a55b", "deepseek-ai/deepseek-v4-pro", "qwen/qwen3.5-397b-a17b"], default: "nvidia/nemotron-3-nano-30b-a3b", apiKeyEnv: "NVIDIA_API_KEY" })),
  mockPreset("kilocode", "KiloCode", "Kilo Code gateway OpenAI-compatible endpoint.", "KILOCODE_API_KEY", mockProviderTemplate({ name: "kilocode", kind: "openai", baseUrl: "https://api.kilo.ai/api/gateway", models: ["kilo/auto"], default: "kilo/auto", apiKeyEnv: "KILOCODE_API_KEY" })),
  mockPreset("ollama-cloud", "Ollama Cloud", "Hosted Ollama Cloud OpenAI-compatible endpoint with max reasoning effort.", "OLLAMA_API_KEY", mockProviderTemplate({ name: "ollama-cloud", kind: "openai", baseUrl: "https://ollama.com/v1", models: mockOllamaCloudModels, default: "glm-5.2", apiKeyEnv: "OLLAMA_API_KEY" })),
];

function mockProviderPresetViews(): ProviderPresetView[] {
  return [...mockProviderPresetTemplates].sort((a, b) => mockProviderPresetDisplayRank(a.id) - mockProviderPresetDisplayRank(b.id)).map((template) => ({
    id: template.id,
    label: template.label,
    description: template.description,
    keyEnv: template.keyEnv,
    providerNames: [template.provider.name],
    models: [...template.provider.models],
    added: false,
    status: "available",
    statusProviderNames: [],
    keySet: false,
    requiresKey: true,
    configured: false,
  }));
}

function mockProviderPresetDisplayRank(id: string): number {
  if (id === "glm-cn" || id === "zai-global" || id.startsWith("glm-coding-plan-") || id.startsWith("zai-coding-plan-")) return 0;
  if (id.startsWith("longcat-")) return 1;
  if (id.startsWith("kimi-")) return 2;
  if (id.startsWith("minimax-")) return 3;
  return 4;
}

function cloneMockProviderTemplate(id: string, key: string): ProviderView | undefined {
  const template = mockProviderPresetTemplates.find((candidate) => candidate.id === id);
  if (!template) return undefined;
  return {
    ...JSON.parse(JSON.stringify(template.provider)) as ProviderView,
    keySet: Boolean(key.trim()),
  };
}

const mockPreviewImageDataURL =
  "data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='160' height='120' viewBox='0 0 160 120'%3E%3Cdefs%3E%3ClinearGradient id='g' x1='0' y1='0' x2='1' y2='1'%3E%3Cstop offset='0' stop-color='%23f97316'/%3E%3Cstop offset='1' stop-color='%232563eb'/%3E%3C/linearGradient%3E%3C/defs%3E%3Crect width='160' height='120' rx='14' fill='url(%23g)'/%3E%3Ccircle cx='44' cy='38' r='16' fill='%23fff7ed' opacity='.9'/%3E%3Cpath d='M18 96 62 58l24 22 18-16 38 32z' fill='%23ffffff' opacity='.9'/%3E%3C/svg%3E";

function mockExternalOpenerIconDataURL(color: string, label: string): string {
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><rect width="64" height="64" rx="14" fill="${color}"/><text x="32" y="40" text-anchor="middle" font-family="system-ui" font-size="25" font-weight="700" fill="white">${label}</text></svg>`;
  return `data:image/svg+xml,${encodeURIComponent(svg)}`;
}

function makeMockApp(): AppBindings {
  const scenario = mockScenario();
  const freshMock = scenario === "fresh";
  const guidanceMock = scenario === "guidance";
  const runningMock = scenario === "running" || guidanceMock;
  const sandboxEscapeMock = scenario === "sandbox_escape";
  const noticePreviewMock = scenario === "notice";
  const mockAttachmentDataURLs = new Map<string, string>();
  let cancelled = false;
  let pendingAskPreview = false;
  let pendingApprovalPreview = false;
  // Mirrors the last emitted approval preview so mode switches can mirror the
  // backend drain contract: only non-fresh tools auto-allow; plan/sandbox
  // escape prompts stay pending and visible.
  let pendingApprovalPreviewPrompt: { id: string; tool: string } | undefined;
  const globalWorkspaceRoot = "~/Library/Application Support/reasonix/global-workspace";
  let cwd = freshMock ? globalWorkspaceRoot : "~/projects/joyquant-db"; // mutable so PickWorkspace is visible in dev
  let workspaces = freshMock ? [] : ["~/projects/joyquant-db", "~/projects/joyquant-sys", "~/projects/reasonix", "~/projects/blade"];
  let mockEffort = "auto";
  let mockDesktopZoomFactor = 1.0;
  let mockActiveThemeId = "";
  let mockBaseStyle = "graphite";
  let mockThemeMode: "auto" | "light" | "dark" = "dark";
  // Vite rewrites these literal asset URLs in both dev and production builds.
  // Keeping them on the browser mock makes local visual acceptance match the
  // Wails bridge, whose ListThemePacks response carries the same two URLs.
  const mockOfficialThemeAssets = {
    "official-rose-dawn": {
      previewUrl: new URL("../../../themes/official/official-rose-dawn/preview.webp", import.meta.url).href,
      backgroundUrl: new URL("../../../themes/official/official-rose-dawn/background.webp", import.meta.url).href,
    },
    "official-fortune-forge": {
      previewUrl: new URL("../../../themes/official/official-fortune-forge/preview.webp", import.meta.url).href,
      backgroundUrl: new URL("../../../themes/official/official-fortune-forge/background.webp", import.meta.url).href,
    },
    "official-crimson-horizon": {
      previewUrl: new URL("../../../themes/official/official-crimson-horizon/preview.webp", import.meta.url).href,
      backgroundUrl: new URL("../../../themes/official/official-crimson-horizon/background.webp", import.meta.url).href,
    },
    "official-sage-breeze": {
      previewUrl: new URL("../../../themes/official/official-sage-breeze/preview.webp", import.meta.url).href,
      backgroundUrl: new URL("../../../themes/official/official-sage-breeze/background.webp", import.meta.url).href,
    },
    "official-spark-notebook": {
      previewUrl: new URL("../../../themes/official/official-spark-notebook/preview.webp", import.meta.url).href,
      backgroundUrl: new URL("../../../themes/official/official-spark-notebook/background.webp", import.meta.url).href,
    },
    "official-violet-starlight": {
      previewUrl: new URL("../../../themes/official/official-violet-starlight/preview.webp", import.meta.url).href,
      backgroundUrl: new URL("../../../themes/official/official-violet-starlight/background.webp", import.meta.url).href,
    },
    "official-cyan-stage": {
      previewUrl: new URL("../../../themes/official/official-cyan-stage/preview.webp", import.meta.url).href,
      backgroundUrl: new URL("../../../themes/official/official-cyan-stage/background.webp", import.meta.url).href,
    },
    "official-noir-gold": {
      previewUrl: new URL("../../../themes/official/official-noir-gold/preview.webp", import.meta.url).href,
      backgroundUrl: new URL("../../../themes/official/official-noir-gold/background.webp", import.meta.url).href,
    },
  } as const;
  registerTrustedThemeBackgroundURLs(Object.values(mockOfficialThemeAssets).map((asset) => asset.backgroundUrl));
  let mockThemePacks: import("./themePack").ThemePackView[] = [
    { id: "graphite", name: "Graphite", author: "Reasonix", baseStyle: "graphite", builtin: true, kind: "base", active: false, hasBackground: false, tokens: {}, recipes: { density: "comfortable", corners: "soft" } },
    { id: "aurora", name: "Aurora", author: "Reasonix", baseStyle: "aurora", builtin: true, kind: "base", active: false, hasBackground: false, tokens: {}, recipes: { density: "comfortable", corners: "soft" } },
    { id: "slate", name: "Slate", author: "Reasonix", baseStyle: "slate", builtin: true, kind: "base", active: false, hasBackground: false, tokens: {}, recipes: { density: "comfortable", corners: "soft" } },
    { id: "carbon", name: "Carbon", author: "Reasonix", baseStyle: "carbon", builtin: true, kind: "base", active: false, hasBackground: false, tokens: {}, recipes: { density: "comfortable", corners: "soft" } },
    { id: "nocturne", name: "Nocturne", author: "Reasonix", baseStyle: "nocturne", builtin: true, kind: "base", active: false, hasBackground: false, tokens: {}, recipes: { density: "comfortable", corners: "soft" } },
    { id: "amber", name: "Amber", author: "Reasonix", baseStyle: "amber", builtin: true, kind: "base", active: false, hasBackground: false, tokens: {}, recipes: { density: "comfortable", corners: "soft" } },
    { ...mockOfficialThemeAssets["official-rose-dawn"], id: "official-rose-dawn", name: "Rose Dawn", author: "Reasonix Contributors", license: "MIT", baseStyle: "graphite", builtin: true, kind: "official", active: false, hasBackground: true, nameKey: "settings.themes.official.official-rose-dawn.name", descriptionKey: "settings.themes.official.official-rose-dawn.description", tokens: { light: { bg: "#FFF7F8", fg: "#3A252C", accent: "#B43F65" }, dark: { bg: "#1E1419", fg: "#FFF3F6", accent: "#E26D91" } }, recipes: { density: "comfortable", corners: "round" }, background: { focusX: 0.72, focusY: 0.43, safeArea: "left", homeOpacity: 1, taskOpacity: 0.2, overlayStrength: 0.68, paneOpacity: 0.50 } },
    { ...mockOfficialThemeAssets["official-fortune-forge"], id: "official-fortune-forge", name: "Fortune Forge", author: "Reasonix Contributors", license: "MIT", baseStyle: "amber", builtin: true, kind: "official", active: false, hasBackground: true, nameKey: "settings.themes.official.official-fortune-forge.name", descriptionKey: "settings.themes.official.official-fortune-forge.description", tokens: { light: { bg: "#FFF8E8", fg: "#382116", accent: "#A92D22" }, dark: { bg: "#1D140D", fg: "#FFF2D1", accent: "#E8AD38" } }, recipes: { density: "comfortable", corners: "soft" }, background: { focusX: 0.74, focusY: 0.44, safeArea: "left", homeOpacity: 1, taskOpacity: 0.2, overlayStrength: 0.7, paneOpacity: 0.50 } },
    { ...mockOfficialThemeAssets["official-crimson-horizon"], id: "official-crimson-horizon", name: "Crimson Horizon", author: "Reasonix Contributors", license: "MIT", baseStyle: "graphite", builtin: true, kind: "official", active: false, hasBackground: true, nameKey: "settings.themes.official.official-crimson-horizon.name", descriptionKey: "settings.themes.official.official-crimson-horizon.description", tokens: { light: { bg: "#FFF8F7", fg: "#301D1D", accent: "#B92B38" }, dark: { bg: "#190D11", fg: "#FFF1F2", accent: "#FF6772" } }, recipes: { density: "comfortable", corners: "soft" }, background: { focusX: 0.75, focusY: 0.45, safeArea: "left", homeOpacity: 0.98, taskOpacity: 0.22, overlayStrength: 0.66, paneOpacity: 0.50 } },
    { ...mockOfficialThemeAssets["official-sage-breeze"], id: "official-sage-breeze", name: "Sage Breeze", author: "Reasonix Contributors", license: "MIT", baseStyle: "slate", builtin: true, kind: "official", active: false, hasBackground: true, nameKey: "settings.themes.official.official-sage-breeze.name", descriptionKey: "settings.themes.official.official-sage-breeze.description", tokens: { light: { bg: "#F7F7EF", fg: "#26332D", accent: "#47735F" }, dark: { bg: "#101814", fg: "#EEF6F0", accent: "#84CBA7" } }, recipes: { density: "comfortable", corners: "soft" }, background: { focusX: 0.73, focusY: 0.44, safeArea: "left", homeOpacity: 1, taskOpacity: 0.2, overlayStrength: 0.68, paneOpacity: 0.50 } },
    { ...mockOfficialThemeAssets["official-spark-notebook"], id: "official-spark-notebook", name: "Spark Notebook", author: "Reasonix Contributors", license: "MIT", baseStyle: "aurora", builtin: true, kind: "official", active: false, hasBackground: true, nameKey: "settings.themes.official.official-spark-notebook.name", descriptionKey: "settings.themes.official.official-spark-notebook.description", tokens: { light: { bg: "#FFF9ED", fg: "#2B2F35", accent: "#007B78" }, dark: { bg: "#14171A", fg: "#F8F5E9", accent: "#42D1C6" } }, recipes: { density: "comfortable", corners: "round" }, background: { focusX: 0.74, focusY: 0.46, safeArea: "left", homeOpacity: 0.98, taskOpacity: 0.2, overlayStrength: 0.68, paneOpacity: 0.50 } },
    { ...mockOfficialThemeAssets["official-violet-starlight"], id: "official-violet-starlight", name: "Violet Starlight", author: "Reasonix Contributors", license: "MIT", baseStyle: "nocturne", builtin: true, kind: "official", active: false, hasBackground: true, nameKey: "settings.themes.official.official-violet-starlight.name", descriptionKey: "settings.themes.official.official-violet-starlight.description", tokens: { light: { bg: "#F7F4FF", fg: "#251F3C", accent: "#6242C7" }, dark: { bg: "#0C1022", fg: "#F4F2FF", accent: "#9B86FF" } }, recipes: { density: "comfortable", corners: "round" }, background: { focusX: 0.73, focusY: 0.44, safeArea: "left", homeOpacity: 0.96, taskOpacity: 0.18, overlayStrength: 0.72, paneOpacity: 0.50 } },
    { ...mockOfficialThemeAssets["official-cyan-stage"], id: "official-cyan-stage", name: "Cyan Stage", author: "Reasonix Contributors", license: "MIT", baseStyle: "carbon", builtin: true, kind: "official", active: false, hasBackground: true, nameKey: "settings.themes.official.official-cyan-stage.name", descriptionKey: "settings.themes.official.official-cyan-stage.description", tokens: { light: { bg: "#F1FCFD", fg: "#173238", accent: "#007C92" }, dark: { bg: "#07181D", fg: "#E9FCFF", accent: "#37D7E4" } }, recipes: { density: "comfortable", corners: "round" }, background: { focusX: 0.74, focusY: 0.45, safeArea: "left", homeOpacity: 0.96, taskOpacity: 0.18, overlayStrength: 0.72, paneOpacity: 0.50 } },
    { ...mockOfficialThemeAssets["official-noir-gold"], id: "official-noir-gold", name: "Noir Gold", author: "Reasonix Contributors", license: "MIT", baseStyle: "carbon", builtin: true, kind: "official", active: false, hasBackground: true, nameKey: "settings.themes.official.official-noir-gold.name", descriptionKey: "settings.themes.official.official-noir-gold.description", tokens: { light: { bg: "#FCF8EE", fg: "#2A241B", accent: "#7A5A16" }, dark: { bg: "#0D0B09", fg: "#F8F1DF", accent: "#D9B45B" } }, recipes: { density: "comfortable", corners: "soft" }, background: { focusX: 0.73, focusY: 0.43, safeArea: "left", homeOpacity: 0.94, taskOpacity: 0.18, overlayStrength: 0.74, paneOpacity: 0.50 } },
  ];
  const day = 86_400_000;
  const t0 = Date.now();
  // Mutable so MCP add/remove/retry are observable in browser dev.
  let capServers: ServerView[] = [
    {
      name: "github",
      transport: "stdio",
      status: "connected",
      configured: true,
      autoStart: true,
      tier: "background",
      command: "npx",
      args: ["-y", "@modelcontextprotocol/server-github"],
      tools: 4,
      prompts: 2,
      resources: 0,
      trustedReadOnlyTools: ["pull_request_read"],
      toolList: [
        { name: "issue_read", description: "Read GitHub issue details and comments.", readOnlyHint: true },
        { name: "pull_request_read", description: "Read pull request metadata, files, and review threads.", readOnlyHint: true },
        { name: "search_issues", description: "Search issues and pull requests.", readOnlyHint: true },
        { name: "issue_write", description: "Create or update GitHub issues." },
      ],
    },
    {
      name: "linear",
      transport: "http",
      status: "initializing",
      configured: true,
      autoStart: true,
      tier: "background",
      url: "https://mcp.linear.app/mcp",
      authStatus: "possible",
      authUrl: "https://mcp.linear.app/mcp",
      tools: 8,
      prompts: 0,
      resources: 0,
      toolList: [
        { name: "list_issues", description: "List and filter Linear issues." },
        { name: "get_issue", description: "Fetch a Linear issue by id or key." },
        { name: "create_issue", description: "Create a Linear issue." },
        { name: "update_issue", description: "Update status, assignee, priority, or labels." },
        { name: "list_projects", description: "List Linear projects." },
        { name: "get_project", description: "Fetch project details." },
        { name: "list_teams", description: "List Linear teams." },
        { name: "search", description: "Search Linear workspace objects." },
      ],
    },
    { name: "figma", transport: "http", status: "failed", configured: true, autoStart: true, tier: "background", url: "https://mcp.figma.com/mcp", authStatus: "required", authUrl: "https://mcp.figma.com/mcp", tools: 0, prompts: 0, resources: 0, error: "connect: 401 unauthorized" },
  ];
  const capSkills: SkillView[] = [
    {
      name: "explore", description: "Investigate the codebase in an isolated subagent", scope: "builtin", runAs: "subagent", enabled: true,
      allowedTools: ["read_file", "ls", "glob", "grep", "code_index"], invocation: "/explore", invocationMode: "auto",
      configuredModel: "deepseek/deepseek-v4-pro", configuredEffort: "high",
    },
    { name: "research", description: "Combine web_fetch + code reading in an isolated subagent", scope: "builtin", runAs: "subagent", enabled: true, allowedTools: ["read_file", "ls", "glob", "grep", "code_index", "web_fetch"], invocation: "/research", invocationMode: "auto" },
    { name: "review", description: "Review the staged diff", scope: "project", runAs: "inline", enabled: false, invocation: "/review" },
    { name: "init", description: "Scaffold a REASONIX.md for this repo", scope: "builtin", runAs: "inline", enabled: true, invocation: "/init" },
    {
      name: "my-formatter", description: "Formats code the way I like it", scope: "global", runAs: "subagent", enabled: true,
      model: "deepseek-pro", effort: "high", allowedTools: ["read_file", "edit_file"], color: "amber", invocation: "/my-formatter", invocationMode: "manual",
      body: "You are a code formatting assistant. Reformat the given file to match project style without changing behavior.",
    },
  ];
  let capSkillRoots: SkillRootView[] = [
    { dir: "~/projects/reasonix/.reasonix/skills", scope: "project", priority: 1, status: "missing", configured: false, removable: true, skills: 0 },
    {
      dir: "~/my-skills",
      scope: "custom",
      priority: 5,
      status: "ok",
      configured: true,
      removable: true,
      skills: 1,
      skillItems: [{ name: "review", description: "Review the staged diff", scope: "custom", runAs: "inline" }],
    },
    {
      dir: "~/.reasonix/skills",
      scope: "global",
      priority: 6,
      status: "ok",
      configured: false,
      removable: true,
      skills: 2,
      skillItems: [
        { name: "explore", description: "Investigate the codebase in an isolated subagent", scope: "global", runAs: "subagent" },
        { name: "init", description: "Scaffold a REASONIX.md for this repo", scope: "global", runAs: "inline" },
      ],
    },
  ];
  let capPlugins: PluginView[] = [];
  const mockSwitchWorkspace = async (path: string) => {
    cwd = path || "~";
    workspaces = [cwd, ...workspaces.filter((p) => p !== cwd)].slice(0, 12);
    if (!mockProjectTree.some((node) => node.kind === "project" && node.root === cwd)) {
      mockProjectTree.unshift({
        key: `project_${cwd}`,
        kind: "project",
        label: baseName(cwd),
        root: cwd,
        children: [],
      });
    }
    return cwd;
  };
  // Mutable so delete/rename are observable in browser dev.
  const sessions: SessionMeta[] = [
    { path: "/mock/sessions/a.jsonl", preview: "fix the login bug in auth.go", turns: 12, createdAt: t0 - 2 * day, lastActivityAt: t0 - 3_600_000, modTime: t0 - 3_600_000, current: true, open: true },
    { path: "/mock/sessions/b-recovery-0123456789abcdef.jsonl", preview: "refactor the payment module", turns: 5, createdAt: t0 - 3 * day, lastActivityAt: t0 - 6 * 3_600_000, modTime: t0 - 6 * 3_600_000, current: false, open: true, recovered: true, recoveryCopy: true },
    { path: "/mock/sessions/c.jsonl", preview: "write the README and badges", turns: 8, createdAt: t0 - 4 * day, lastActivityAt: t0 - day - 3_600_000, modTime: t0 - day - 3_600_000, current: false, open: false },
    { path: "/mock/sessions/d.jsonl", preview: "explain the plugin host design", turns: 3, createdAt: t0 - 5 * day, lastActivityAt: t0 - 4 * day, modTime: t0 - 4 * day, current: false, open: false },
  ];
  const trashedSessions: SessionMeta[] = [
    {
      path: "/mock/sessions/.trash/trash-dev-standard.jsonl",
      title: t("mock.trashDevStandardTitle"),
      preview: t("mock.trashDevStandardPreview"),
      turns: 4,
      createdAt: t0 - 8 * day,
      lastActivityAt: t0 - 7 * day,
      modTime: t0 - 7 * day,
      deletedAt: t0 - 20 * 60_000,
      current: false,
      open: false,
      scope: "project",
      workspaceRoot: "~/projects/joyquant-db",
      topicId: "topic_dev_standard",
      topicTitle: t("mock.trashDevStandardTitle"),
    },
    {
      path: "/mock/sessions/.trash/trash-p3a-review.jsonl",
      title: t("mock.trashP3aTitle"),
      preview: t("mock.trashP3aPreview"),
      turns: 7,
      createdAt: t0 - 6 * day,
      lastActivityAt: t0 - 5 * day,
      modTime: t0 - 5 * day,
      deletedAt: t0 - 2 * 3_600_000,
      current: false,
      open: false,
      scope: "project",
      workspaceRoot: "~/projects/joyquant-sys",
      topicId: "topic_p3a_pd",
      topicTitle: t("mock.trashP3aTitle"),
    },
    {
      path: "/mock/sessions/.trash/trash-global-product.jsonl",
      title: t("mock.trashGlobalProductTitle"),
      preview: t("mock.trashGlobalProductPreview"),
      turns: 2,
      createdAt: t0 - 4 * day,
      lastActivityAt: t0 - 3 * day,
      modTime: t0 - 3 * day,
      deletedAt: t0 - day,
      current: false,
      open: false,
      scope: "global",
      topicId: "topic_product",
      topicTitle: t("mock.trashGlobalProductTitle"),
      recovered: true,
      recoveryCopy: true,
    },
  ];
  if (freshMock) {
    sessions.splice(0);
    trashedSessions.splice(0);
  }
  // Mutable settings so the Settings panel's edits are observable in browser dev.
  const settings: SettingsView = {
    defaultModel: "deepseek",
    plannerModel: "",
    subagentModel: "",
    subagentEffort: "",
    autoPlan: "off",
    providers: [
      { name: "deepseek", builtIn: true, added: false, kind: "openai", baseUrl: "https://api.deepseek.com", modelsUrl: "", models: ["deepseek-v4-flash"], visionModels: [], visionModelsConfigured: false, default: "deepseek-v4-flash", apiKeyEnv: "DEEPSEEK_API_KEY", keySet: true, balanceUrl: "https://api.deepseek.com/user/balance", contextWindow: 1_000_000, reasoningProtocol: "", thinking: "", supportedEfforts: [], defaultEffort: "" },
    ],
    officialProviders: [
      { name: "deepseek", builtIn: true, added: false, kind: "openai", baseUrl: "https://api.deepseek.com", modelsUrl: "", models: ["deepseek-v4-flash", "deepseek-v4-pro"], visionModels: [], visionModelsConfigured: false, default: "deepseek-v4-flash", apiKeyEnv: "DEEPSEEK_API_KEY", keySet: true, balanceUrl: "https://api.deepseek.com/user/balance", contextWindow: 1_000_000, reasoningProtocol: "", thinking: "", supportedEfforts: [], defaultEffort: "" },
    ],
    providerPresets: mockProviderPresetViews(),
    permissions: { mode: "ask", allow: ["ls", "read_file"], ask: [], deny: ["Bash(rm:*)"] },
    sandbox: { bash: browserPreviewBashSandboxMode(), network: true, workspaceRoot: "", allowWrite: [], effectiveWorkspaceRoot: cwd, effectiveWriteRoots: [cwd], shell: "auto", effectiveShell: browserPreviewEffectiveShell("auto") },
    network: {
      proxyMode: "auto",
      proxyUrl: "",
      noProxy: "",
      proxy: { type: "socks5", server: "127.0.0.1", port: 7890, username: "", password: "" },
    },
    agent: { temperature: 0.2, maxSteps: 0, plannerMaxSteps: 0, maxSubagentDepth: 2, maxSubagentConcurrency: 6, maxParallelWriters: 3, systemPrompt: "You are Reasonix, a coding agent.", coldResumePrune: true, reasoningLanguage: "auto" },
    bot: {
      enabled: !freshMock,
      model: "",
      toolApprovalMode: "ask",
      maxSteps: 25,
      debounceMs: 1500,
      queueMode: "steer",
      queueCap: 20,
      queueDrop: "summarize",
      ignoreSelfMessages: true,
      selfUserIds: {
        qq: [],
        feishu: [],
        weixin: [],
      },
      control: {
        enabled: false,
        addr: "127.0.0.1:37913",
        tokenEnv: "REASONIX_BOT_CONTROL_TOKEN",
      },
      pairing: {
        enabled: true,
        requestTtlMinutes: 60,
        maxPendingPerPlatform: 3,
      },
      routes: [],
      allowlist: {
        enabled: true,
        allowAll: false,
        qqUsers: [],
        feishuUsers: freshMock ? [] : ["ou_mock_user_001"],
        weixinUsers: freshMock ? [] : ["wxid_mock_user_001"],
        qqApprovers: [],
        feishuApprovers: [],
        weixinApprovers: [],
        qqAdmins: [],
        feishuAdmins: [],
        weixinAdmins: [],
        qqGroups: [],
        feishuGroups: [],
        weixinGroups: [],
      },
      qq: { enabled: false, appId: "", appSecretEnv: "QQ_BOT_APP_SECRET", secretSet: false, sandbox: false, model: "", toolApprovalMode: "ask", workspaceRoot: "", access: { enabled: true, allowAll: false, pairingEnabled: true, users: [], groups: [], approvers: [], admins: [] } },
      feishu: {
        enabled: false,
        domain: "feishu",
        appId: "",
        appSecretEnv: "FEISHU_BOT_APP_SECRET",
        secretSet: false,
        verificationToken: "",
        mode: "webhook",
        webhookPort: 8080,
        requireMention: true,
      },
      weixin: {
        enabled: false,
        accountId: "default",
        tokenEnv: "WEIXIN_BOT_TOKEN",
        tokenSet: false,
        apiBase: "https://ilinkai.weixin.qq.com",
      },
      connections: freshMock ? [] : [
        {
          id: "mock-lark-kun",
          provider: "feishu",
          domain: "lark",
          label: "kun",
          enabled: true,
          status: "connected",
	          model: "",
	          toolApprovalMode: "",
	          workspaceRoot: "",
	          access: { enabled: true, allowAll: false, pairingEnabled: true, users: ["ou_mock_user_001"], groups: [], approvers: [], admins: [] },
	          credential: {
            appId: "cli_mock_lark",
            appSecretEnv: "FEISHU_BOT_APP_SECRET",
            accountId: "",
            tokenEnv: "",
            secretSet: true,
          },
          sessionMappings: [
            {
              remoteId: "ou_mock_user_001",
              sessionId: "topic:topic_product",
              sessionSource: "",
              chatType: "",
              userId: "",
              threadId: "",
              scope: "global",
              workspaceRoot: "",
              updatedAt: new Date(Date.now() - 4 * 60_000).toISOString(),
            },
          ],
          lastError: "",
          createdAt: new Date(Date.now() - 86_400_000).toISOString(),
          updatedAt: new Date(Date.now() - 4 * 60_000).toISOString(),
        },
        {
          id: "mock-weixin-kun",
          provider: "weixin",
          domain: "weixin",
          label: "kun",
          enabled: true,
          status: "connected",
	          model: "",
	          toolApprovalMode: "",
	          workspaceRoot: "",
	          access: { enabled: true, allowAll: false, pairingEnabled: true, users: ["wxid_mock_user_001"], groups: [], approvers: [], admins: [] },
	          credential: {
            appId: "",
            appSecretEnv: "",
            accountId: "default",
            tokenEnv: "WEIXIN_BOT_TOKEN",
            secretSet: true,
          },
          sessionMappings: [
            {
              remoteId: "wxid_mock_user_001",
              sessionId: "topic:topic_ai",
              sessionSource: "",
              chatType: "",
              userId: "",
              threadId: "",
              scope: "global",
              workspaceRoot: "",
              updatedAt: new Date(Date.now() - 12 * 60_000).toISOString(),
            },
          ],
          lastError: "",
          createdAt: new Date(Date.now() - 86_400_000).toISOString(),
          updatedAt: new Date(Date.now() - 12 * 60_000).toISOString(),
        },
      ],
    },
    desktopLanguage: "",
    desktopLayoutStyle: "workbench",
    desktopTheme: "auto",
    desktopThemeStyle: "graphite",
    conversationWidth: "standard",
    closeBehavior: "background",
    displayMode: "compact",
    statusBarStyle: "text",
    statusBarItems: [...DEFAULT_STATUS_BAR_ITEMS],
    defaultToolApprovalMode: "auto",
    checkUpdates: true,
    telemetry: true,
    metrics: true,
    configPath: "~/projects/reasonix/reasonix.toml",
    providerKinds: ["openai", "anthropic"],
    autoApproveTools: false,
    bypass: false,
  };
  const hookEvents = ["PreToolUse", "PostToolUse", "UserPromptSubmit", "Stop", "PostLLMCall", "SessionStart", "SessionEnd", "SubagentStop", "Notification", "PreCompact"];
  const hookSettings: Record<string, HooksSettingsView> = {
    global: {
      scope: "global",
      path: "~/.reasonix/settings.json",
      projectRoot: "",
      trusted: true,
      events: hookEvents,
      hooks: [
        { event: "Stop", command: "echo turn done", description: "Notify after each turn" },
      ],
    },
    project: {
      scope: "project",
      path: "./.reasonix/settings.json",
      projectRoot: "/mock/project",
      trusted: false,
      events: hookEvents,
      hooks: [],
    },
  };
  settings.providers = settings.providers.map((provider) =>
    provider.apiKeyEnv === "DEEPSEEK_API_KEY" ? { ...provider, keySet: !freshMock } : provider,
  );
  if (freshMock) {
    settings.configPath = "~/.config/reasonix/config.toml";
  }
  const mockNow = Date.now();
  const mockProjectTree: ProjectNode[] = freshMock ? [] : [
    {
      key: "project_~/projects/joyquant-db",
      kind: "project",
      label: t("mock.projectJoyquantDb"),
      root: "~/projects/joyquant-db",
      projectColor: "blue",
      children: [
        { key: "topic_dev_standard", kind: "topic", label: `● ${t("mock.topicDevStandard")}`, root: "~/projects/joyquant-db", topicId: "topic_dev_standard", projectColor: "blue", turns: 18, lastActivityAt: mockNow - 8 * 60_000, open: true, running: runningMock },
        { key: "topic_db_maint", kind: "topic", label: t("mock.topicDbMaint"), root: "~/projects/joyquant-db", topicId: "topic_db_maint", projectColor: "blue", turns: 7, lastActivityAt: mockNow - 2 * 60 * 60_000 },
        { key: "topic_env", kind: "topic", label: t("mock.topicEnv"), root: "~/projects/joyquant-db", topicId: "topic_env", projectColor: "blue", turns: 3, lastActivityAt: mockNow - 26 * 60 * 60_000 },
      ],
    },
    {
      key: "project_~/projects/joyquant-sys",
      kind: "project",
      label: t("mock.projectJoyquantSys"),
      root: "~/projects/joyquant-sys",
      projectColor: "purple",
      children: [
        { key: "topic_p3b_pd", kind: "topic", label: `● ${t("mock.topicP3b")}`, root: "~/projects/joyquant-sys", topicId: "topic_p3b_pd", projectColor: "purple", turns: 11, lastActivityAt: mockNow - 3 * 24 * 60 * 60_000, status: runningMock ? "streaming" : undefined },
        { key: "topic_p3a_pd", kind: "topic", label: t("mock.topicP3a"), root: "~/projects/joyquant-sys", topicId: "topic_p3a_pd", projectColor: "purple", turns: 9, lastActivityAt: mockNow - 4 * 24 * 60 * 60_000, status: runningMock ? "thinking" : undefined },
        { key: "topic_hotfix", kind: "topic", label: t("mock.topicHotfix"), root: "~/projects/joyquant-sys", topicId: "topic_hotfix", projectColor: "purple", turns: 4, lastActivityAt: mockNow - 5 * 24 * 60 * 60_000, status: runningMock ? "thinking" : undefined },
        { key: "topic_sys_coord", kind: "topic", label: t("mock.topicSysCoord"), root: "~/projects/joyquant-sys", topicId: "topic_sys_coord", projectColor: "purple", turns: 14, lastActivityAt: mockNow - 6 * 24 * 60 * 60_000, status: runningMock ? "waiting_confirmation" : undefined },
        { key: "topic_sys_standard", kind: "topic", label: t("mock.topicSysStandard"), root: "~/projects/joyquant-sys", topicId: "topic_sys_standard", projectColor: "purple", turns: 6, lastActivityAt: mockNow - 7 * 24 * 60 * 60_000, status: "paused" },
        { key: "topic_sys_exception", kind: "topic", label: t("mock.topicSysException"), root: "~/projects/joyquant-sys", topicId: "topic_sys_exception", projectColor: "purple", turns: 2, lastActivityAt: mockNow - 8 * 24 * 60 * 60_000, status: "error" },
      ],
    },
    {
      key: "global_folder",
      kind: "global_folder",
      label: "Global",
      root: globalWorkspaceRoot,
      children: [
        { key: "global_topic_product", kind: "global_topic", label: t("mock.topicProduct"), topicId: "topic_product", turns: 5, lastActivityAt: mockNow - 8 * 24 * 60 * 60_000 },
        { key: "global_topic_ai", kind: "global_topic", label: t("mock.topicAi"), topicId: "topic_ai", turns: 8, lastActivityAt: mockNow - 10 * 24 * 60 * 60_000 },
        { key: "global_topic_lab", kind: "global_topic", label: t("mock.topicLab"), topicId: "topic_lab", turns: 2, lastActivityAt: mockNow - 12 * 24 * 60 * 60_000 },
      ],
    },
  ];
  const ensureMockGlobalFolder = (): ProjectNode => {
    let node = mockProjectTree.find((item) => item.kind === "global_folder");
    if (!node) {
      node = {
        key: "global_folder",
        kind: "global_folder",
        label: "Global",
        root: globalWorkspaceRoot,
        children: [],
      };
      mockProjectTree.push(node);
    }
    return node;
  };
  const mockProjectTreeForDisplay = () => {
    const pinnedProjects = mockProjectTree.filter((node) => node.kind === "project" && node.pinned);
    if (pinnedProjects.length === 0) return mockProjectTree;
    const rest = mockProjectTree.filter((node) => !(node.kind === "project" && node.pinned));
    return [...pinnedProjects, ...rest];
  };
  const cloneProjectTree = () => {
    if (mockProjectTree.length === 0) ensureMockGlobalFolder();
    return JSON.parse(JSON.stringify(mockProjectTreeForDisplay())) as ProjectNode[];
  };
  const projectChildren = (node: ProjectNode): ProjectNode[] => Array.isArray(node.children) ? node.children : [];
  const findMockTopic = (topicId: string): ProjectNode | null => {
    for (const parent of mockProjectTree) {
      const found = projectChildren(parent).find((child) => child.topicId === topicId);
      if (found) return found;
    }
    return null;
  };
  const setMockTopicPinned = (topicId: string, pinned: boolean) => {
    for (const parent of mockProjectTree) {
      const children = projectChildren(parent);
      const index = children.findIndex((child) => child.topicId === topicId);
      if (index < 0) continue;
      const topic = { ...children[index], pinned: pinned || undefined };
      if (!pinned) {
        parent.children = children.map((child, i) => (i === index ? topic : child));
        return;
      }
      const remaining = children.filter((_, i) => i !== index);
      parent.children = [topic, ...remaining];
      return;
    }
  };
  const setMockProjectPinned = (workspaceRoot: string, pinned: boolean) => {
    const index = mockProjectTree.findIndex((node) => node.kind === "project" && node.root === workspaceRoot);
    if (index < 0) return;
    mockProjectTree[index] = { ...mockProjectTree[index], pinned: pinned || undefined };
  };
  const deleteMockTopic = (topicId: string) => {
    for (const parent of mockProjectTree) {
      parent.children = projectChildren(parent).filter((child) => child.topicId !== topicId);
    }
  };
  const topicLabel = (topicId: string, fallback: string) => (findMockTopic(topicId)?.label || fallback).replace(/^●\s*/, "");
  const mockTopicStatus = (topicId: string) => findMockTopic(topicId)?.status ?? "";
  const mockTopicIsRunning = (topicId: string) => {
    const status = mockTopicStatus(topicId);
    return status === "streaming" || status === "thinking" || status === "waiting_confirmation";
  };
  const mockTopicIsBlank = (topicId: string) => {
    const topic = findMockTopic(topicId);
    return Boolean(topic && topic.label === t("mock.newSession") && !topic.turns && !topic.lastActivityAt && !topic.status);
  };
  const mockTopicRunsInScenario = (topicId: string) => runningMock && mockTopicIsRunning(topicId);
  const mockLongTranscriptHistory = (): HistoryMessage[] => {
    const out: HistoryMessage[] = [];
    for (let i = 1; i <= 18; i++) {
      out.push({
        role: "user",
        content: `第 ${i} 轮：检查聊天滚动定位，切换会话后应该自动停在最新消息底部。`,
        createdAt: t0 - (19 - i) * 15 * 60_000,
      });
      if (i === 4) {
        out.push({ role: "phase", content: "复现切换会话后的滚动位置" });
      }
      if (i === 8) {
        const toolID = "mock-scroll-layout-check";
        out.push({
          role: "assistant",
          content: "我会先读取滚动容器尺寸，再确认是否存在动态高度变化导致的底部偏移。",
          reasoning: "旧实现只重置 stick 标志，没有主动等待布局稳定；AskCard、Approval、Todo 这类卡片可能在下一帧改变高度。",
          toolCalls: [{ id: toolID, name: "bash", arguments: JSON.stringify({ command: "npm run check:css && pnpm typecheck" }) }],
        });
        out.push({
          role: "tool",
          toolCallId: toolID,
          toolName: "bash",
          content: "CSS syntax check passed\nz-index token check passed\ntsc --noEmit passed\n",
        });
        continue;
      }
      if (i === 13) {
        out.push({ role: "notice", level: "info", content: "模拟提示：用户向上查看历史后，右下角应出现跳到底部按钮。" });
      }
      out.push({
        role: "assistant",
        content: [
          `第 ${i} 轮结果：当前滚动契约会在切换会话或 reveal 信号到达后执行强制贴底。`,
          "它会先立即设置 scrollTop 到 scrollHeight，再连续几个 animation frame 复查，避免动态内容把底部再次推走。",
          "如果用户主动向上滚动，普通 streaming 不会强行拉回；只有点击跳到底部按钮或显式切换会话才会重新贴底。",
        ].join("\n\n"),
      });
    }
    out.push({
      role: "compaction",
      content: "",
      trigger: "manual",
      messages: 36,
      summary: "Mock 长会话用于验证桌面端 Transcript 自动贴底、多帧布局修正和跳到底部按钮。",
      archive: "mock-scroll-preview",
    });
    out.push({
      role: "assistant",
      content: "最终状态：这条消息应该位于真实底部。向上滚动后，右下角会显示跳到底部按钮；点击按钮后应回到这里。",
    });
    return out;
  };
	  const mockTopicHistory = (topicId: string): HistoryMessage[] => {
	    switch (topicId) {
      case "topic_product":
        return [
          {
            role: "user",
            content: [
              "[[reasonix-im]]",
              "provider=lark",
              "label=Feishu / Lark",
              "sender=ou_mock_user_001",
              "chat=p2p 会话",
              "[[/reasonix-im]]",
              "你可以做什么",
            ].join("\n"),
          },
          {
            role: "assistant",
            content: "这是 Global 范围下的 IM 会话。我可以先处理不依赖项目文件的问答、计划和信息整理；需要进入项目时，再由桌面端显式绑定或迁移到项目话题。",
          },
        ];
      case "topic_ai":
        return [
          {
            role: "user",
            content: [
              "[[reasonix-im]]",
              "provider=weixin",
              "label=微信",
              "sender=wxid_mock_user_001",
              "chat=单聊",
              "[[/reasonix-im]]",
              "帮我整理一下今天要做的事",
            ].join("\n"),
          },
          {
            role: "assistant",
            content: "可以。我会先在 Global 范围里整理任务清单；如果某条任务需要读取项目文件，再切到你授权的项目话题处理。",
          },
        ];
      case "topic_dev_standard":
        return mockLongTranscriptHistory();
      case "topic_p3b_pd":
        return [
          { role: "user", content: "把 p3b P&D 的范围和风险重新整理成可执行计划。" },
          { role: "phase", content: "分析需求范围" },
        ];
      case "topic_p3a_pd":
        return [
          { role: "user", content: "复盘 p3a 的技术方案，先不要写文件，先说明你的判断。" },
        ];
      case "topic_hotfix":
        return [
          { role: "user", content: "检查 post-p3-hotfix 的回归风险，重点看最近的 shell 输出和 git 改动。" },
          { role: "assistant", content: "", reasoning: "我先定位最近一次 hotfix 的上下文，然后用只读命令检查状态；左侧保持“思考中”，工具细节在这里展开。" },
        ];
      case "topic_sys_coord":
        return [
          { role: "user", content: "准备执行 joyquant-sys 的同步脚本，但需要我确认后再运行。" },
          { role: "assistant", content: "", reasoning: "这个动作会运行脚本并可能刷新本地缓存，所以需要先等用户确认。" },
        ];
      case "topic_sys_standard":
        return [
          { role: "user", content: "继续制定 SYS 项目开发规范，先停在当前检查点。" },
          { role: "assistant", content: "已暂停在规范整理阶段。当前保留了目录约定、分支策略和待确认的发布检查项；继续时可以从这里恢复。" },
          { role: "notice", level: "info", content: "会话已暂停：未继续执行命令，等待用户恢复或切换任务。" },
        ];
      case "topic_sys_exception":
        return [
          { role: "user", content: "演练异常处理流程，看看失败时界面怎么提示。" },
          { role: "assistant", content: "我尝试校验恢复脚本时遇到异常，已停止继续执行。" },
          { role: "notice", level: "warn", content: "运行异常：恢复脚本缺少必要环境变量 JOYQUANT_SYS_TOKEN。请补齐配置后重试。" },
        ];
      default:
        return [];
	    }
	  };
	  const mockHistoryPage = (messages: HistoryMessage[], beforeTurn = 0, limit = 60): HistoryPage => {
	    const totalTurns = messages.reduce((count, message) => count + (message.role === "user" ? 1 : 0), 0);
	    const safeLimit = Math.max(1, Math.min(200, Math.floor(limit || 60)));
	    const endTurn = beforeTurn > 0 && beforeTurn <= totalTurns ? beforeTurn : totalTurns;
	    const startTurn = Math.max(0, endTurn - safeLimit);
	    let turn = -1;
	    const pageMessages = messages.filter((message) => {
	      if (message.role === "user") turn += 1;
	      if (turn < 0) return startTurn === 0;
	      return turn >= startTurn && turn < endTurn;
	    });
	    return { messages: pageMessages, startTurn, endTurn, totalTurns, hasOlder: startTurn > 0 };
	  };
	  const mockRuntimeInjected = new Set<string>();
  const queueMockTopicRuntime = (tab: TabMeta) => {
    if (!runningMock) return;
    const status = mockTopicStatus(tab.topicId);
    if (status !== "streaming" && status !== "thinking" && status !== "waiting_confirmation") return;
    const key = `${tab.id}:${tab.topicId}:${status}`;
    if (mockRuntimeInjected.has(key)) return;
    mockRuntimeInjected.add(key);
    window.setTimeout(() => {
      void withMockTabScope(tab.id, async () => {
        emitMockTurnStarted();
        await delay(120);
        if (tab.topicId === "topic_p3b_pd") {
          const text = "我会先把范围拆成三层：目标、依赖、风险。当前已经确认 p3b 的交付边界，接下来补充每个模块的验收口径...";
          for (const ch of text) {
            emit({ kind: "text", text: ch });
            await delay(5);
          }
          return;
        }
        if (tab.topicId === "topic_p3a_pd") {
          emit({ kind: "reasoning", text: "我正在对比 p3a 和 p3b 的差异：先看约束，再看变更风险，最后判断是否需要拆成独立任务。\n\n" });
          await delay(220);
          emit({ kind: "reasoning", text: "当前倾向：先保留 p3a 的兼容路径，不急于删除旧逻辑。" });
          return;
        }
        if (tab.topicId === "topic_hotfix") {
          const id = "mock-hotfix-shell";
          emit({ kind: "tool_dispatch", tool: { id, name: "bash", args: JSON.stringify({ command: "git status --short && npm test" }), readOnly: true } });
          await delay(180);
          emit({ kind: "tool_progress", tool: { id, name: "bash", readOnly: true, output: "$ git status --short\n M internal/sys/runner.go\n\n$ npm test\nrunning targeted regression tests...\n" } });
          return;
        }
        if (tab.topicId === "topic_sys_coord") {
          pendingApprovalPreview = true;
          pendingApprovalPreviewPrompt = { id: "mock-sys-confirm", tool: "bash" };
          emit({ kind: "reasoning", text: "我已经准备好执行同步脚本，但这个操作会影响本地 workspace，需要用户确认。" });
          await delay(160);
          emit({
            kind: "approval_request",
            approval: {
              id: "mock-sys-confirm",
              tool: "bash",
              subject: "npm run sync:joyquant-sys\n\n该命令会同步 SYS 项目配置并刷新本地缓存。",
            },
          });
        }
      });
    }, 180);
  };
  const setMockActiveTab = (tabId: string) => {
    mockTabs = mockTabs.map((tab) => ({ ...tab, active: tab.id === tabId }));
  };
  const currentMockTurnTabId = () => mockScopedTabId || mockTabs.find((tab) => tab.active)?.id;
  const setMockTabRunning = (tabId: string | undefined, running: boolean) => {
    if (!tabId) return;
    mockTabs = mockTabs.map((tab) => (tab.id === tabId ? { ...tab, running } : tab));
  };
  const emitMockTurnStarted = () => {
    setMockTabRunning(currentMockTurnTabId(), true);
    emit({ kind: "turn_started" });
  };
  const emitMockTurnDone = () => {
    setMockTabRunning(currentMockTurnTabId(), false);
    emit({ kind: "turn_done" });
  };
  // Fresh user decisions never auto-allow on a posture switch (mirrors the
  // backend's requiresFreshApprovalTool set).
  const mockFreshApprovalTools = new Set(["exit_plan_mode", "sandbox_escape", "memory_remember", "memory_forget", "managed_config_write"]);
  // Mirrors the backend drain contract for the mode-switch bindings: returns
  // the prompt ids the new posture auto-allowed; fresh prompts stay pending.
  const drainMockApprovalPreviews = (toolApprovalMode: string): string[] => {
    if (toolApprovalMode !== "auto" && toolApprovalMode !== "yolo") return [];
    const prompt = pendingApprovalPreviewPrompt;
    if (!pendingApprovalPreview || !prompt || mockFreshApprovalTools.has(prompt.tool)) return [];
    pendingApprovalPreview = false;
    pendingApprovalPreviewPrompt = undefined;
    emit({ kind: "message", text: `approval preview auto-allowed (${toolApprovalMode})` });
    emitMockTurnDone();
    return [prompt.id];
  };
  let mockTabs: TabMeta[] = noticePreviewMock ? [
    {
      id: "tab_notice_preview",
      scope: "project",
      workspaceRoot: "~/projects/reasonix",
      workspaceName: "reasonix",
      workspacePath: "~/projects/reasonix",
      gitBranch: "codex/compact-chat-notices-i18n",
      topicId: "topic_notice_preview",
      topicTitle: "Compact notice preview",
      projectColor: "green",
      label: "DeepSeek-R1",
      ready: true,
      running: false,
      mode: "normal",
      collaborationMode: "normal",
      toolApprovalMode: "ask",
      tokenMode: "full",
      active: true,
      cwd: "~/projects/reasonix",
    },
  ] : freshMock ? [
    {
      id: "tab_global",
      scope: "global",
      workspaceRoot: globalWorkspaceRoot,
      workspaceName: "Global",
      workspacePath: globalWorkspaceRoot,
      topicId: "",
      topicTitle: "Global",
      label: "DeepSeek-R1",
      ready: true,
      running: false,
      mode: "normal",
      collaborationMode: "normal",
      toolApprovalMode: "ask",
      tokenMode: "full",
      active: true,
      cwd: globalWorkspaceRoot,
    },
  ] : [
    {
      id: "tab_joyquant_db",
      scope: "project",
      workspaceRoot: "~/projects/joyquant-db",
      workspaceName: "joyquant-db",
      workspacePath: "~/projects/joyquant-db",
      gitBranch: "main",
      topicId: "topic_dev_standard",
      topicTitle: t("mock.trashDevStandardTitle"),
      projectColor: "blue",
      label: "DeepSeek-R1",
      ready: true,
      running: false,
      mode: "normal",
      collaborationMode: "normal",
      toolApprovalMode: "ask",
      tokenMode: "full",
      active: !guidanceMock,
      cwd: "~/projects/joyquant-db",
    },
    {
      id: "tab_joyquant_sys",
      scope: "project",
      workspaceRoot: "~/projects/joyquant-sys",
      workspaceName: "joyquant-sys",
      workspacePath: "~/projects/joyquant-sys",
      gitBranch: "feature/p3b",
      topicId: "topic_p3b_pd",
      topicTitle: "p3b P&D",
      projectColor: "purple",
      label: "DeepSeek-R1",
      ready: true,
      running: runningMock && mockTopicIsRunning("topic_p3b_pd"),
      mode: "normal",
      collaborationMode: "normal",
      toolApprovalMode: "ask",
      tokenMode: "full",
      active: guidanceMock,
      cwd: "~/projects/joyquant-sys",
    },
    {
      id: "tab_global",
      scope: "global",
      workspaceRoot: "",
      workspaceName: "Global",
      workspacePath: "~/projects/joyquant-db",
      topicId: "topic_global",
      topicTitle: "Global",
      label: "DeepSeek-R1",
      ready: true,
      running: false,
      mode: "normal",
      collaborationMode: "normal",
      toolApprovalMode: "ask",
      tokenMode: "full",
      active: false,
      cwd: "~/projects/joyquant-db",
    },
  ];
  if (sandboxEscapeMock) {
    window.setTimeout(() => {
      if (pendingApprovalPreview) return;
      pendingApprovalPreview = true;
      pendingApprovalPreviewPrompt = { id: "mock-sandbox-escape-preview", tool: "sandbox_escape" };
      emitMockTurnStarted();
      emit({ kind: "reasoning", text: t("mock.sandboxEscapeReasoning") });
      emit({
        kind: "approval_request",
        approval: {
          id: "mock-sandbox-escape-preview",
          tool: "sandbox_escape",
          subject: t("mock.sandboxEscapeSubject"),
          reason: t("mock.sandboxEscapeReason"),
        },
      });
    }, 800);
  }
  const mockModelCatalog = [
    { ref: "deepseek/deepseek-v4-flash", provider: "deepseek", model: "deepseek-v4-flash" },
    { ref: "deepseek/deepseek-v4-pro", provider: "deepseek", model: "deepseek-v4-pro" },
  ];
  const defaultMockModelRef = mockModelCatalog[0].ref;
  const mockModelRef = (name: string): string => {
    const trimmed = name.trim();
    if (!trimmed || trimmed === "DeepSeek-R1") return defaultMockModelRef;
    const exact = mockModelCatalog.find((model) => model.ref === trimmed);
    if (exact) return exact.ref;
    const byModel = mockModelCatalog.find((model) => model.model === trimmed);
    return byModel?.ref ?? trimmed;
  };
  const mockModelLabel = (ref: string): string => mockModelCatalog.find((model) => model.ref === mockModelRef(ref))?.model ?? ref.split("/").pop() ?? ref;
  const mockTabModelRef = (tab?: TabMeta): string => mockModelRef(tab?.label ?? "");
  const setMockTabModel = (tabID: string | undefined, name: string) => {
    const ref = mockModelRef(name);
    const label = mockModelLabel(ref);
    let applied = false;
    mockTabs = mockTabs.map((tab) => {
      const match = tabID ? tab.id === tabID : tab.active;
      if (!match) return tab;
      applied = true;
      return { ...tab, label };
    });
    if (!applied && mockTabs.length > 0) {
      mockTabs = mockTabs.map((tab, index) => (index === 0 ? { ...tab, label } : tab));
    }
  };
  return {
    async MinimiseMainWindow() {
      console.info("mock MinimiseMainWindow");
    },
    async ToggleMaximiseMainWindow() {
      console.info("mock ToggleMaximiseMainWindow");
    },
    async IsMainWindowMaximised() {
      return false;
    },
    async CloseMainWindow() {
      console.info("mock CloseMainWindow");
    },
    async Platform() {
      const override = browserPlatformOverride();
      if (override) return override;
      // Mirror the OS the browser dev mock runs on.
      const ua = typeof navigator !== "undefined" ? navigator.userAgent : "";
      if (/Win/i.test(ua)) return "windows";
      if (/Mac/i.test(ua)) return "darwin";
      return "linux";
    },
        async Submit(input) {
          cancelled = false;
      emitMockTurnStarted();
      const trimmedInput = input.trim().toLowerCase();
      const goalMatch = /^\/goal(?:\s+([\s\S]*))?$/.exec(input.trim());
      if (goalMatch) {
        const arg = stripGoalResearchFlags((goalMatch[1] ?? "").trim());
        const lowered = arg.toLowerCase();
        const active = mockTabs.find((tab) => tab.active);
        if (!arg || lowered === "status") {
          emit({ kind: "notice", level: "info", text: active?.goal ? `goal: ${active.goal}` : "goal: none" });
          emitMockTurnDone();
          return;
        }
        if (["clear", "off", "stop", "done"].includes(lowered)) {
          mockTabs = mockTabs.map((tab) => (tab.active ? { ...tab, goal: "", goalStatus: "stopped", collaborationMode: "normal" } : tab));
          emit({ kind: "notice", level: "info", text: "goal cleared" });
          emitMockTurnDone();
          return;
        }
        mockTabs = mockTabs.map((tab) => (tab.active ? { ...tab, goal: arg, goalStatus: "running", collaborationMode: "goal" } : tab));
        emit({ kind: "notice", level: "info", text: `goal set: ${arg}` });
        await delay(350);
        if (cancelled) return;
        const reply = `Autonomous goal run started for: **${arg}**\n\nMock run completed.\n\n[goal:complete]`;
        emit({ kind: "message", text: reply });
        mockTabs = mockTabs.map((tab) => (tab.active ? { ...tab, goal: "", goalStatus: "complete", collaborationMode: "normal" } : tab));
        emit({ kind: "notice", level: "info", text: "goal complete" });
        emitMockTurnDone();
        return;
      }
      if (trimmedInput === "/approve-preview" || trimmedInput === "approve preview" || trimmedInput === "approve预览") {
        pendingApprovalPreview = true;
        pendingApprovalPreviewPrompt = { id: "mock-approval-preview", tool: "bash" };
        await delay(250);
        if (cancelled) return;
        emit({
          kind: "approval_request",
          approval: {
            id: "mock-approval-preview",
            tool: "bash",
            subject: t("mock.approvalSubject"),
          },
        });
        return;
      }
      if (
        trimmedInput === "/sandbox-escape-preview" ||
        trimmedInput === "sandbox escape preview" ||
        trimmedInput === "sandbox_escape preview" ||
        trimmedInput === "sandbox escape预览"
      ) {
        pendingApprovalPreview = true;
        pendingApprovalPreviewPrompt = { id: "mock-sandbox-escape-preview", tool: "sandbox_escape" };
        await delay(250);
        if (cancelled) return;
        emit({
          kind: "approval_request",
          approval: {
            id: "mock-sandbox-escape-preview",
            tool: "sandbox_escape",
            subject: t("mock.sandboxEscapeSubject"),
            reason: t("mock.sandboxEscapeReason"),
          },
        });
        return;
      }
      if (
        trimmedInput === "/plan-approve-preview" ||
        trimmedInput === "plan approve preview" ||
        trimmedInput === "plan approve预览"
      ) {
        pendingApprovalPreview = true;
        pendingApprovalPreviewPrompt = { id: "mock-plan-approval-preview", tool: "exit_plan_mode" };
        await delay(250);
        if (cancelled) return;
        emit({
          kind: "approval_request",
          approval: {
            id: "mock-plan-approval-preview",
            tool: "exit_plan_mode",
            subject: "",
          },
        });
        return;
      }
      if (trimmedInput === "/ask-preview" || trimmedInput === "ask preview" || trimmedInput === "ask预览") {
        pendingAskPreview = true;
        await delay(250);
        if (cancelled) return;
        emit({
          kind: "ask_request",
          ask: {
            id: "mock-ask-preview",
            questions: [
              {
                id: "q1",
                header: t("mock.askQ1Header"),
                prompt: t("mock.askQ1Prompt"),
                options: [
                  { label: t("mock.askQ1Opt1Label"), description: t("mock.askQ1Opt1Desc") },
                  { label: t("mock.askQ1Opt2Label"), description: t("mock.askQ1Opt2Desc") },
                  { label: t("mock.askQ1Opt3Label"), description: t("mock.askQ1Opt3Desc") },
                ],
              },
              {
                id: "q2",
                header: t("mock.askQ2Header"),
                prompt: t("mock.askQ2Prompt"),
                options: [
                  { label: t("mock.askQ2Opt1Label"), description: t("mock.askQ2Opt1Desc") },
                  { label: t("mock.askQ2Opt2Label"), description: t("mock.askQ2Opt2Desc") },
                  { label: t("mock.askQ2Opt3Label"), description: t("mock.askQ2Opt3Desc") },
                ],
              },
            ],
          },
        });
        return;
      }
      if (trimmedInput === "/todo-preview" || trimmedInput === "todo preview" || trimmedInput === "todo预览") {
        await delay(250);
        if (cancelled) return;
        emit({
          kind: "tool_dispatch",
          tool: {
            id: "mock-todo-preview",
            name: "todo_write",
            args: JSON.stringify({
              todos: [
                { content: t("mock.todo1"), status: "completed" },
                { content: t("mock.todo2"), activeForm: t("mock.todo2ActiveForm"), status: "in_progress" },
                { content: t("mock.todo3"), status: "pending" },
              ],
            }),
            readOnly: false,
          },
        });
        await delay(150);
        emit({
          kind: "tool_result",
          tool: {
            id: "mock-todo-preview",
            name: "todo_write",
            args: JSON.stringify({
              todos: [
                { content: t("mock.todo1"), status: "completed" },
                { content: t("mock.todo2"), activeForm: t("mock.todo2ActiveForm"), status: "in_progress" },
                { content: t("mock.todo3"), status: "pending" },
              ],
            }),
            output: "todo list updated",
            readOnly: false,
            durationMs: 150,
          },
        });
        emitMockTurnDone();
        return;
      }
      if (trimmedInput === "/process-preview" || trimmedInput === "process preview" || trimmedInput === "过程预览") {
        await delay(200);
        if (cancelled) return;
        emit({ kind: "phase", text: "Preparing context" });
        await delay(120);
        emit({ kind: "notice", level: "info", text: "Loaded project instructions from AGENTS.md." });
        await delay(120);
        emit({ kind: "notice", level: "warn", text: "Network access is enabled; external results may change over time." });
        await delay(120);
        emit({ kind: "compaction_started", compaction: { trigger: "manual" } });
        await delay(320);
        emit({
          kind: "compaction_done",
          compaction: {
            trigger: "manual",
            messages: 6,
            summary: "Preserved the active task, relevant files, and UI decisions while trimming earlier exploratory context.",
          },
        });
        emit({ kind: "message", text: "Process card preview complete." });
        emitMockTurnDone();
        return;
      }
      if (trimmedInput === "/nested-preview" || trimmedInput === "nested preview" || trimmedInput === "嵌套预览") {
        const parentId = "mock-nested-explore";
        await delay(180);
        if (cancelled) return;
        emit({
          kind: "reasoning",
          text: "我先快速探索相关文件，再整理这个工具行的视觉层级。",
        });
        emit({
          kind: "message",
          text: "",
          reasoning: "我先快速探索相关文件，再整理这个工具行的视觉层级。",
        });
        emit({
          kind: "tool_dispatch",
          tool: {
            id: parentId,
            name: "explore",
            args: JSON.stringify({ task: "在 Reasonix 前端中检查工具调用图标和嵌套调用展示" }),
            readOnly: true,
            profile: { model: "mock-reasonix", effort: "high" },
          },
        });
        for (let i = 1; i <= 30; i += 1) {
          if (cancelled) return;
          const id = `mock-nested-${i}`;
          const isSearch = i % 3 === 0;
          const name = isSearch ? "grep" : "read_file";
          const args = isSearch
            ? { pattern: i % 2 === 0 ? "tool__nested-count" : "explore", path: "desktop/frontend/src" }
            : { path: `desktop/frontend/src/${i % 2 === 0 ? "components/ToolCard.tsx" : "styles.css"}`, offset: i * 10, limit: 40 };
          emit({ kind: "tool_dispatch", tool: { id, name, args: JSON.stringify(args), readOnly: true, parentId } });
          emit({
            kind: "tool_result",
            tool: {
              id,
              name,
              readOnly: true,
              output: isSearch ? "3 matches" : "read 40 lines",
              durationMs: 24 + i,
            },
          });
          await delay(18);
        }
        emit({
          kind: "tool_result",
          tool: {
            id: parentId,
            name: "explore",
            readOnly: true,
            output: "已读 20 个文件 · 搜索 10 个文件",
            durationMs: 61510,
          },
        });
        emit({
          kind: "message",
          text: "Mock nested tool preview complete. The explore row now shows the compass count marker.",
        });
        emitMockTurnDone();
        return;
      }
      // Simulate the server's pre-first-token latency so the deferred user bubble
      // and the "un-send on Esc before any reply" path are observable in browser
      // dev. Bail if cancelled during the wait — nothing was streamed yet.
      await delay(700);
      if (cancelled) return;
      const reasoningChunks = [
        "我先判断这是浏览器预览环境，所以不会调用真实 kernel。\n",
        "接着模拟 provider 的 reasoning delta：先展示思考过程，再切到正式回复。\n",
        "完成后前端应该把过程区折叠成“已工作 N 秒”。\n",
      ];
      for (const chunk of reasoningChunks) {
        if (cancelled) return;
        emit({ kind: "reasoning", reasoning: chunk });
        await delay(520);
      }
      if (cancelled) return;
      await delay(260);
      const reply =
        `You said: **${input}**\n\n` +
        "This is the browser dev mock — the real reply comes from the kernel " +
        "inside the Wails shell. Here's a fenced block to exercise the editor seam:\n\n" +
        "```go\nfunc main() {\n    println(\"hello from the mock\")\n}\n```\n";
      for (const ch of reply) {
        if (cancelled) break;
        emit({ kind: "text", text: ch });
        await delay(6);
      }
      emit({ kind: "message", text: reply });
      emit({
        kind: "tool_dispatch",
        tool: {
          id: "t1",
          name: "edit_file",
          args: '{"path":"main.go","old_string":"println(\\"hi\\")","new_string":"println(\\"hello\\")"}',
          readOnly: false,
        },
      });
      await delay(350);
      emit({
        kind: "tool_result",
        tool: { id: "t1", name: "edit_file", output: "edited main.go", readOnly: false, durationMs: 350 },
      });
      emit({
        kind: "usage",
        usage: {
          promptTokens: 1280,
          completionTokens: 64,
          totalTokens: 1344,
          cacheHitTokens: 1024,
          cacheMissTokens: 256,
          sessionCacheHitTokens: 1024,
          sessionCacheMissTokens: 256,
        },
      });
          emitMockTurnDone();
        },
        async SubmitToTab(_tabID, input) {
          await withMockTabScope(_tabID, () => this.Submit(input));
        },
        async SubmitDisplay(_display, input) {
          await this.Submit(input);
        },
        async SubmitDisplayToTab(_tabID, display, input) {
          await withMockTabScope(_tabID, () => this.SubmitDisplay(display, input));
        },
        async SubmitDeliveryRecoveryToTab(_tabID, display, input) {
          await withMockTabScope(_tabID, () => this.SubmitDisplay(display, input));
        },
        async SubmitInvocationsToTab(_tabID, display, input, _invocations) {
          await withMockTabScope(_tabID, () => this.SubmitDisplay(display, input));
        },
        async SubmitEditedDisplayToTab(_tabID, display, input, _original) {
          await withMockTabScope(_tabID, () => this.SubmitDisplay(display, input));
        },
        async RunShell(command) {
          cancelled = false;
          emitMockTurnStarted();
          await delay(100);
          if (cancelled) return;
          const id = `shell-${command.slice(0, 32)}`;
          emit({ kind: "tool_dispatch", tool: { id, name: "bash", args: JSON.stringify({ command }), readOnly: false } });
          await delay(200);
          if (cancelled) return;
          emit({ kind: "tool_progress", tool: { id, name: "bash", output: `$ ${command}\n(mock output)\n`, readOnly: false } });
          await delay(100);
          if (cancelled) return;
          emit({ kind: "tool_result", tool: { id, name: "bash", output: `$ ${command}\n(mock output)\n`, readOnly: false, durationMs: 300 } });
          emitMockTurnDone();
        },
        async RunShellForTab(_tabID, command) {
          await withMockTabScope(_tabID, () => this.RunShell(command));
        },
        async Steer(_text) {
          // Mock: emit a steer event as confirmation in the transcript.
          emit({ kind: "steer", text: _text });
        },
        async SteerForTab(_tabID, _text) {
          await this.Steer(_text);
        },
        async Cancel() {
          cancelled = true;
          emitMockTurnDone();
        },
        async CancelTab(_tabID) {
          await withMockTabScope(_tabID, () => this.Cancel());
        },
        async Approve(_id, allow, session, persist) {
          if (!pendingApprovalPreview) return;
          pendingApprovalPreview = false;
          pendingApprovalPreviewPrompt = undefined;
          const suffix = persist ? "grant saved" : session ? "grant active this session" : "allowed once";
          emit({
            kind: "message",
            text: `approval preview answered: ${allow ? suffix : "denied"}`,
          });
          emitMockTurnDone();
        },
        async ApproveTab(_tabID, id, allow, session, persist) {
          await withMockTabScope(_tabID, () => this.Approve(id, allow, session, persist));
        },
        async AnswerQuestion(_id, answers) {
      if (!pendingAskPreview) return;
      pendingAskPreview = false;
      const summary = answers
        .map((answer) => `${answer.questionId}: ${(answer.selected ?? []).join(", ") || "(no answer)"}`)
        .join("\n");
      emit({ kind: "message", text: `ask preview answered:\n\n${summary}` });
          emitMockTurnDone();
        },
        async AnswerQuestionForTab(_tabID, id, answers) {
          await withMockTabScope(_tabID, () => this.AnswerQuestion(id, answers));
        },
        async ReplayPendingPrompts() {},
        async ConfirmAction(req) {
          void req;
          return false;
        },
        async SetPlanMode(on) {
          const active = mockTabs.find((tab) => tab.active);
          if (active) await this.SetModeForTab(active.id, modeWithPlan(normalizeMode(active.mode), on));
        },
        async SetMode(mode) {
          const active = mockTabs.find((tab) => tab.active);
          if (active) await this.SetModeForTab(active.id, mode);
        },
        async SetModeForTab(tabID, mode) {
          const nextMode = normalizeMode(mode);
          let nextToolApprovalMode: ToolApprovalMode | "" = "";
          mockTabs = mockTabs.map((tab) => {
            if (tab.id !== tabID) return tab;
            nextToolApprovalMode = mockToolApprovalModeAfterModeChange(tab.toolApprovalMode, nextMode);
            return {
              ...tab,
              mode: nextMode,
              collaborationMode: normalizeCollaborationMode(undefined, tab.goal, nextMode),
              toolApprovalMode: nextToolApprovalMode,
            };
          });
          return drainMockApprovalPreviews(nextToolApprovalMode);
        },
        async SetCollaborationMode(mode) {
          const active = mockTabs.find((tab) => tab.active);
          if (active) await this.SetCollaborationModeForTab(active.id, mode);
        },
        async SetCollaborationModeForTab(tabID, mode) {
          const next = normalizeCollaborationMode(mode);
          mockTabs = mockTabs.map((tab) => {
            if (tab.id !== tabID) return tab;
            const toolMode = normalizeToolApprovalMode(tab.toolApprovalMode, normalizeMode(tab.mode));
            return {
              ...tab,
              collaborationMode: next,
              goal: next === "normal" || next === "plan" ? "" : tab.goal,
              mode: modeWithPlan(modeWithAutoApproveTools(normalizeMode(tab.mode), toolMode === "yolo"), next === "plan"),
            };
          });
        },
        async SetToolApprovalMode(mode) {
          const active = mockTabs.find((tab) => tab.active);
          if (active) await this.SetToolApprovalModeForTab(active.id, mode);
        },
        async SetToolApprovalModeForTab(tabID, mode) {
          const next = normalizeToolApprovalMode(mode);
          settings.autoApproveTools = next === "yolo";
          settings.bypass = next === "yolo";
          mockTabs = mockTabs.map((tab) =>
            tab.id === tabID
              ? {
                  ...tab,
                  toolApprovalMode: next,
                  mode: modeWithAutoApproveTools(normalizeMode(tab.mode), next === "yolo"),
                }
              : tab,
          );
          return drainMockApprovalPreviews(next);
        },
        async SetGoal(goal) {
          const active = mockTabs.find((tab) => tab.active);
          if (active) await this.SetGoalForTab(active.id, goal);
        },
        async SetGoalForTab(tabID, goal) {
          const nextGoal = goal.trim();
          mockTabs = mockTabs.map((tab) =>
            tab.id === tabID
              ? {
                  ...tab,
                  goal: nextGoal,
                  goalStatus: nextGoal ? "running" : "stopped",
                  collaborationMode: nextGoal ? "goal" : "normal",
                  mode: modeWithPlan(normalizeMode(tab.mode), false),
                }
              : tab,
          );
        },
        async ResumeGoalForTab(tabID) {
          let resumed = false;
          mockTabs = mockTabs.map((tab) => {
            if (tab.id !== tabID || !tab.goal || tab.goalStatus === "complete") return tab;
            resumed = true;
            return { ...tab, goalStatus: "running", collaborationMode: "goal" };
          });
          return resumed;
        },
        async ClearGoal() {
          await this.SetGoal("");
        },
        async ClearGoalForTab(tabID) {
          await this.SetGoalForTab(tabID, "");
        },
        async Compact() {},
        async CompactForTab() {},
        async NewSession() {},
        async NewSessionForTab() {},
        async ClearSession() {},
        async ClearSessionForTab() {},
    async Checkpoints() {
      return [
        { turn: 0, prompt: "你好呀", files: ["src/App.tsx"], fileCount: 1, turnFileCount: 1, time: Date.now() - 30_000, canCode: true, canConversation: true },
      ];
    },
    async CheckpointsForTab() {
      return this.Checkpoints();
    },
    async Rewind() {},
    async RewindForTab() {},
    async Fork() {
      const active = mockTabs.find((tab) => tab.active) ?? mockTabs[0];
      const tab: TabMeta = {
        ...active,
        id: "tab_fork_" + Date.now(),
        topicId: "topic_fork_" + Date.now(),
        topicTitle: `${active.topicTitle || t("rewind.fork")} · fork`,
        active: true,
        running: false,
      };
      mockTabs = [...mockTabs.map((item) => ({ ...item, active: false })), tab];
      return { ...tab };
    },
    async ForkForTab(tabID, turn) {
      mockTabs = mockTabs.map((tab) => ({ ...tab, active: tab.id === tabID }));
      return this.Fork(turn);
    },
    async SummarizeFrom() {},
    async SummarizeFromForTab() {},
    async SummarizeUpTo() {},
    async SummarizeUpToForTab() {},
        async History() {
          return [];
        },
        async HistoryForTab(tabID?: string) {
          const tab = mockTabs.find((item) => item.id === tabID) ?? mockTabs.find((item) => item.active);
          if (tab?.topicId) {
            queueMockTopicRuntime(tab);
            return mockTopicHistory(tab.topicId);
          }
          return this.History();
        },
        async HistoryPage(beforeTurn = 0, limit = 60) {
          return mockHistoryPage(await this.History(), beforeTurn, limit);
        },
        async HistoryPageForTab(tabID: string, beforeTurn = 0, limit = 60) {
          return mockHistoryPage(await this.HistoryForTab(tabID), beforeTurn, limit);
        },
        async HistoryCheckpointTurnsForTab(tabID: string) {
          const turns: number[] = [];
          for (const message of await this.HistoryForTab(tabID)) {
            if (message.role !== "user") continue;
            turns.push(message.checkpointTurn ?? turns.length);
          }
          return turns;
        },
    async ListSessions() {
      return sessions.map((s) => ({ ...s }));
    },
    async ListTrashedSessions() {
      return trashedSessions.map((s) => ({ ...s }));
    },
    async ResumeSession(path: string) {
      sessions.forEach((s) => {
        s.current = s.path === path;
        s.open = s.open || s.path === path;
      });
      return [
        { role: "user", content: `(mock) resumed ${path}` },
        { role: "assistant", content: "This is a mock resumed transcript — the real one comes from the kernel." },
      ];
    },
	    async ResumeSessionForTab(_tabID: string, path: string) {
	      return this.ResumeSession(path);
	    },
	    async ResumeSessionPage(path: string, limit = 60) {
	      return mockHistoryPage(await this.ResumeSession(path), 0, limit);
	    },
	    async ResumeSessionPageForTab(_tabID: string, path: string, limit = 60) {
	      return this.ResumeSessionPage(path, limit);
	    },
	    async OpenChannelSessionForTab(tabID: string, path: string) {
	      mockTabs = mockTabs.map((tab) => tab.id === tabID ? { ...tab, sessionPath: path, readOnly: true } : tab);
	      return this.ResumeSession(path);
	    },
	    async OpenChannelSessionPageForTab(tabID: string, path: string, limit = 60) {
	      return mockHistoryPage(await this.OpenChannelSessionForTab(tabID, path), 0, limit);
	    },
	    async PreviewSession(path: string) {
      const s = sessions.find((x) => x.path === path) ?? trashedSessions.find((x) => x.path === path);
      return [
        { role: "user", content: s?.preview || `(mock) preview ${path}` },
        { role: "phase", content: "Preparing read-only preview" },
        {
          role: "assistant",
          content: "This is a read-only mock preview. The active conversation is unchanged.",
          reasoning: "Preview reads the saved session without resuming it.",
        },
        { role: "notice", level: "info", content: "Preview mode keeps the active conversation untouched." },
        { role: "compaction", content: "", trigger: "manual", messages: 3, summary: "Mock preview preserved the latest task, tool result, and answer summary." },
      ];
    },
    async DeleteSession(path: string) {
      const i = sessions.findIndex((s) => s.path === path);
      if (i >= 0) {
        const [s] = sessions.splice(i, 1);
        trashedSessions.unshift({
          ...s,
          current: false,
          open: false,
          path: s.path.replace("/mock/sessions/", "/mock/sessions/.trash/"),
          deletedAt: Date.now(),
        });
      }
    },
    async DeleteRecoveryCopy(path: string) {
      return this.DeleteSession(path);
    },
    async RestoreSession(path: string) {
      const i = trashedSessions.findIndex((s) => s.path === path);
      if (i >= 0) {
        const [s] = trashedSessions.splice(i, 1);
        sessions.unshift({
          ...s,
          path: s.path.replace("/mock/sessions/.trash/", "/mock/sessions/"),
          deletedAt: undefined,
        });
      }
    },
    async PurgeTrashedSession(path: string) {
      const i = trashedSessions.findIndex((s) => s.path === path);
      if (i >= 0) trashedSessions.splice(i, 1);
    },
    async PurgeRecoveryCopy(path: string) {
      return this.PurgeTrashedSession(path);
    },
    async RenameSession(path: string, title: string) {
      const s = sessions.find((x) => x.path === path);
      if (s) s.title = title.trim() || undefined;
    },
	    async ScanPromptHistory(nonce: string) {
	      // Dev mock returns a static set of sample prompts for UI development.
	      const entries: PromptHistoryEntry[] = [
	        { text: "Explain the architecture of this project", at: Date.now() - 60000, sessionPath: "/mock/sessions/arch.jsonl", turn: 0 },
	        { text: "Fix the login button styling", at: Date.now() - 120000, sessionPath: "/mock/sessions/arch.jsonl", turn: 1 },
	        { text: "What is the capital of France?", at: Date.now() - 300000, sessionPath: "/mock/sessions/general.jsonl", turn: 0 },
	      ];
	      return { entries, nonce: "mock-" + nonce, olderCursor: "", hasOlder: false };
	    },
    async ListWorkspaces() {
      return mockProjectTree
        .filter((node) => node.kind === "project" && node.root)
        .map((node) => ({
          path: node.root!,
          name: node.label || baseName(node.root!),
          current: node.root === cwd,
        }));
    },
    async PickWorkspace() {
      // Browser dev has no native dialog; simulate picking a folder and re-root so
      // the topbar folder chip visibly changes.
      return mockSwitchWorkspace(cwd.endsWith("another-project") ? "~/projects/reasonix" : "~/projects/another-project");
    },
    async SwitchWorkspace(path: string) {
      return mockSwitchWorkspace(path);
    },
    async RemoveWorkspace(path: string) {
      workspaces = workspaces.filter((p) => p !== path);
      const index = mockProjectTree.findIndex((node) => node.root === path);
      if (index >= 0) mockProjectTree.splice(index, 1);
    },
        async ContextUsage() {
          return { used: 42124, window: 128000, sessionTokens: 34479, compactRatio: 0.8 };
        },
        async ContextUsageForTab() {
          return this.ContextUsage();
        },
        async Balance() {
      // Mirror the active mock provider: deepseek-flash carries a balance_url.
      const p = settings.providers.find((x) => x.name === settings.defaultModel);
      if (!p?.balanceUrl) return { available: false, display: "" };
          return { available: true, display: "¥128.50" };
        },
        async BalanceForTab() {
          return this.Balance();
        },
        async Jobs() {
          return []; // browser dev mock has no background jobs
        },
        async JobsForTab() {
          return this.Jobs();
        },
        async ToolResultForTab() {
          return null;
        },
        async Meta() {
          const active = mockTabs.find((tab) => tab.active) ?? mockTabs[0];
          const toolApprovalMode = normalizeToolApprovalMode(active?.toolApprovalMode, active ? normalizeMode(active.mode) : "normal", settings.autoApproveTools);
          const autoApproveTools = toolApprovalMode === "yolo";
          const collaborationMode = normalizeCollaborationMode(active?.collaborationMode, active?.goal, active ? normalizeMode(active.mode) : "normal");
          const workspacePath = active?.workspacePath || active?.workspaceRoot || active?.cwd || cwd;
          return {
            label: active?.label ?? "DeepSeek-R1",
            ready: active?.ready ?? true,
            eventChannel: EVENT_CHANNEL,
            cwd: active?.cwd || cwd,
            workspaceRoot: active?.workspaceRoot || workspacePath,
            workspaceName: active?.workspaceName,
            workspacePath,
            sandboxPath: settings.sandbox.workspaceRoot,
            gitBranch: active?.gitBranch || (active?.scope === "project" ? "main" : ""),
            imageInputEnabled: true,
            autoApproveTools,
            bypass: autoApproveTools,
            collaborationMode,
            toolApprovalMode,
            tokenMode: normalizeTokenMode(active?.tokenMode),
            goal: active?.goal ?? "",
            goalStatus: active?.goalStatus ?? (active?.goal ? "running" : "stopped"),
            autoResearch: active?.goal ? { taskId: "mock-autoresearch", status: "running", iteration: 4, pivotRequired: false, staleCount: 0 } : undefined,
          };
        },
        async MetaForTab(tabID) {
          const tab = mockTabs.find((item) => item.id === tabID) ?? mockTabs.find((item) => item.active) ?? mockTabs[0];
          const toolApprovalMode = normalizeToolApprovalMode(tab?.toolApprovalMode, tab ? normalizeMode(tab.mode) : "normal", settings.autoApproveTools);
          const autoApproveTools = toolApprovalMode === "yolo";
          const collaborationMode = normalizeCollaborationMode(tab?.collaborationMode, tab?.goal, tab ? normalizeMode(tab.mode) : "normal");
          const workspacePath = tab?.workspacePath || tab?.workspaceRoot || tab?.cwd || cwd;
          return {
            label: tab?.label ?? "DeepSeek-R1",
            ready: tab?.ready ?? true,
            eventChannel: EVENT_CHANNEL,
            cwd: tab?.cwd || cwd,
            workspaceRoot: tab?.workspaceRoot || workspacePath,
            workspaceName: tab?.workspaceName,
            workspacePath,
            sandboxPath: settings.sandbox.workspaceRoot,
            gitBranch: tab?.gitBranch || (tab?.scope === "project" ? "main" : ""),
            autoApproveTools,
            bypass: autoApproveTools,
            collaborationMode,
            toolApprovalMode,
            tokenMode: normalizeTokenMode(tab?.tokenMode),
            goal: tab?.goal ?? "",
            goalStatus: tab?.goalStatus ?? (tab?.goal ? "running" : "stopped"),
            autoResearch: tab?.goal ? { taskId: "mock-autoresearch", status: "running", iteration: 4, pivotRequired: false, staleCount: 0 } : undefined,
          };
        },
        async AutoResearchCurrent() {
          return {
            taskId: "mock-autoresearch",
            goal: "Mock long-running research",
            status: "running",
            iteration: 4,
            currentDirection: "Inspect status chip",
            staleCount: 0,
            pivotCount: 0,
            pivotRequired: false,
            lastHeartbeatAt: "2026-06-29T00:00:00Z",
            findingCount: 1,
            openCriteria: [],
            blocker: "",
            taskPath: "/tmp/mock/.reasonix/autoresearch/mock-autoresearch",
            nextRequiredAction: "continue with the next evidence-producing step",
          };
        },
        async AutoResearchStatus(_tabID) {
          return {
            taskId: "mock-autoresearch",
            goal: "Mock long-running research",
            status: "running",
            iteration: 4,
            currentDirection: "Inspect status chip",
            staleCount: 0,
            pivotCount: 0,
            pivotRequired: false,
            lastHeartbeatAt: "2026-06-29T00:00:00Z",
            findingCount: 1,
            openCriteria: [],
            blocker: "",
            taskPath: "/tmp/mock/.reasonix/autoresearch/mock-autoresearch",
            nextRequiredAction: "continue with the next evidence-producing step",
          };
        },
        async AutoResearchList(_tabID) {
          return [{
            taskId: "mock-autoresearch",
            goal: "Mock long-running research",
            status: "running",
            iteration: 4,
            currentDirection: "Inspect status chip",
            staleCount: 0,
            pivotCount: 0,
            pivotRequired: false,
            lastHeartbeatAt: "2026-06-29T00:00:00Z",
            findingCount: 1,
            openCriteria: [],
            blocker: "",
            taskPath: "/tmp/mock/.reasonix/autoresearch/mock-autoresearch",
            nextRequiredAction: "continue with the next evidence-producing step",
          }];
        },
        async AutoResearchFindings(_tabID, limit) {
          return [{
            id: "f1",
            kind: "test",
            summary: "Mock accepted finding",
            source: "command",
            command: "go test ./...",
            accepted: true,
            createdAt: "2026-06-29T00:00:00Z",
          }].slice(0, Math.max(0, limit || 1));
        },
        async AutoResearchOpenTask(_tabID) {
          console.info("mock AutoResearchOpenTask");
        },
        async AutoResearchRecordEvidence(_tabID, _criterionID, _input) {
          console.info("mock AutoResearchRecordEvidence");
        },
    async Commands() {
      const commands: CommandInfo[] = [
        { name: "new", description: "start new session; save transcript", kind: "builtin" as const, group: "actions" },
        { name: "clear", description: "discard current context", kind: "builtin" as const, group: "actions" },
        { name: "compact", description: "Summarize older history to free up context", kind: "builtin" as const, group: "actions" },
        { name: "model", description: "Switch model", kind: "builtin" as const, group: "actions" },
        { name: "effort", description: "Set reasoning effort", kind: "builtin" as const, group: "actions" },
        { name: "skill", description: "List skills", kind: "builtin" as const, group: "skills" },
        { name: "mcp", description: "Manage MCP servers", kind: "builtin" as const, group: "integrations" },
        { name: "plugins", description: "Manage plugin packages", kind: "builtin" as const, group: "integrations" },
        { name: "review", description: "Review the staged diff", hint: "[focus]", kind: "custom" as const, group: "skills" },
      ];
      const seen = new Set(commands.map((command) => command.name));
      for (const skill of capSkills) {
        if (skill.enabled === false) continue;
        const name = (skill.invocation || `/${skill.name}`).replace(/^\/+/, "");
        if (!name || seen.has(name)) continue;
        seen.add(name);
        commands.push({
          name,
          description: skill.description,
          kind: skill.runAs === "subagent" ? "subagent" : "skill",
          group: skill.runAs === "subagent" ? "subagents" : "skills",
          color: skill.color,
        });
      }
      return commands;
    },
    async Capabilities() {
      return {
        servers: capServers.map((s) => ({ ...s })),
        skills: capSkills.map((s) => ({ ...s })),
        skillRoots: capSkillRoots.map((s) => ({ ...s })),
        plugins: capPlugins.map((p) => ({ ...p })),
      };
    },
    async MCPServers() {
      return capServers.map((s) => ({ ...s }));
    },
    async SkillsSettings() {
      return {
        skills: capSkills.map((s) => ({ ...s })),
        skillRoots: capSkillRoots.map((s) => ({ ...s })),
      };
    },
    async CapabilityDiagnostics(includeSessionRuntime: boolean) {
      const report: CapabilityDiagnosticsReport = {
        schema_version: 1,
        root: "<workspace>",
        live: false,
        summary: {
          errors: 0,
          warnings: 1,
          infos: includeSessionRuntime ? 1 : 0,
          instructions: 1,
          skills: capSkills.length,
          commands: 0,
          hooks: 0,
          plugins: capPlugins.length,
          mcp_servers: capServers.length,
        },
        instructions: { docs: [{ path: "<workspace>/AGENTS.md", scope: "project", order: 1 }] },
        skills: {
          roots: [{ path: "<workspace>/.reasonix/skills", scope: "project", status: "ok" }],
          entries: capSkills.map((s) => ({
            name: s.name,
            description: s.description,
            scope: s.scope,
            path: "(mock)",
            status: "winner",
            run_as: s.runAs,
          })),
          winners: capSkills.length,
          shadowed: 0,
        },
        commands: { roots: [], entries: [], winners: 0, shadowed: 0 },
        hooks: { trusted_project: true, project_defines_hooks: false, sources: [], entries: [] },
        plugins: {
          packages: capPlugins.map((p) => ({
            name: p.name,
            enabled: p.enabled,
            root: p.root || "<external>/plugin",
            skills: p.skills ?? 0,
            commands: 0,
            hooks: p.hooks ?? 0,
            mcp_servers: p.mcpServers ?? 0,
            status: p.enabled ? "ok" : "disabled",
          })),
        },
        mcp: {
          servers: capServers.map((s) => ({
            name: s.name,
            transport: s.transport || "stdio",
            start_intent: s.startIntent === "off" ? "off" : "automatic",
            source: "toml",
            runtime_status: includeSessionRuntime ? s.status || "connected" : undefined,
            tool_count: s.tools,
            env_keys: s.envKeys ?? [],
            header_keys: s.headerKeys ?? [],
          })),
        },
        issues: [
          {
            severity: "warning",
            code: "skill.missing_description",
            subsystem: "skills",
            name: "example",
            message: "mock warning for browser harness",
            remediation: "Add a description frontmatter field",
            settings_tab: "skills",
          },
          ...(includeSessionRuntime
            ? [{
                severity: "info" as const,
                code: "mcp.runtime_unavailable",
                subsystem: "mcp",
                message: "browser mock has no live Host; runtime fields are synthetic",
                settings_tab: "mcp",
              }]
            : []),
        ],
      };
      return JSON.parse(JSON.stringify(report)) as CapabilityDiagnosticsReport;
    },
    async Plugins() {
      return capPlugins.map((p) => ({ ...p }));
    },
    async PlanPluginInstall(source: string, options: PluginInstallOptions) {
      const name = options.name || source.split("/").filter(Boolean).pop()?.replace(/\.git$/, "") || "plugin";
      return JSON.stringify({
        ok: true,
        status: "planned",
        kind: "plugin",
        actions: [{ kind: "plugin", action: "install_plugin_package", name, source, status: "planned" }],
      });
    },
    async InstallPlugin(source: string, options: PluginInstallOptions) {
      const name = options.name || source.split("/").filter(Boolean).pop()?.replace(/\.git$/, "") || "plugin";
      const existing = capPlugins.findIndex((p) => p.name === name);
      const view: PluginView = {
        name,
        version: "dev",
        description: "Mock plugin",
        source,
        root: `~/.reasonix/plugins/${name}`,
        manifestKind: "reasonix",
        enabled: true,
        skills: 1,
        hooks: 0,
        mcpServers: 0,
        skillDetails: [{ name: "plan", description: "Plan work before implementation", invocation: "/plan", runAs: "inline" }],
      };
      if (existing >= 0) capPlugins[existing] = view;
      else capPlugins.push(view);
      return JSON.stringify({ ok: true, status: "done", kind: "plugin", actions: [{ kind: "plugin", name }] });
    },
    async RemovePlugin(name: string) {
      capPlugins = capPlugins.filter((p) => p.name !== name);
    },
    async SetPluginEnabled(name: string, enabled: boolean) {
      capPlugins = capPlugins.map((p) => p.name === name ? { ...p, enabled } : p);
    },
    async UpdatePlugin(name: string) {
      capPlugins = capPlugins.map((p) => p.name === name ? { ...p, version: p.version || "dev" } : p);
      return JSON.stringify({ ok: true, status: "done", kind: "plugin", name });
    },
    async PluginDoctor(name: string) {
      return capPlugins.find((p) => p.name === name) || {
        name,
        root: "",
        enabled: false,
        skills: 0,
        hooks: 0,
        mcpServers: 0,
        error: "plugin is not installed",
      };
    },
    async AddMCPServer(input: MCPServerInput) {
      const tools = input.transport === "stdio" ? 3 : 5;
      capServers.push({
        name: input.name,
        transport: input.transport,
        status: "connected",
        configured: true,
        autoStart: true,
        tier: "background",
        command: input.command,
        args: input.args,
        url: input.url,
        envKeys: input.env ? Object.keys(input.env).sort() : undefined,
        headerKeys: input.headers ? Object.keys(input.headers).sort() : undefined,
        tools,
        prompts: 0,
        resources: 0,
        toolList: Array.from({ length: tools }, (_, i) => ({
          name: `${input.name}_tool_${i + 1}`,
          description: `Mock tool ${i + 1} exposed by ${input.name}.`,
        })),
      });
      return tools;
    },
    async UpdateMCPServer(name: string, input: MCPServerInput) {
      capServers = capServers.map((s) => {
        if (s.name !== name) return s;
        const connected = s.status === "connected" || s.status === "failed" || s.autoStart !== false;
        const nextStatus = s.status === "disabled" ? "disabled" : connected ? "connected" : "deferred";
        const nextTools = nextStatus === "connected" ? s.tools || (input.transport === "stdio" ? 3 : 5) : 0;
        return {
          ...s,
          transport: input.transport,
          status: nextStatus,
          command: input.transport === "stdio" ? input.command : "",
          args: input.transport === "stdio" ? input.args : [],
          url: input.transport === "stdio" ? "" : input.url,
          envKeys: input.env ? Object.keys(input.env).sort() : s.envKeys,
          headerKeys: input.headers ? Object.keys(input.headers).sort() : s.headerKeys,
          trustedReadOnlyTools: input.trustedReadOnlyTools ?? s.trustedReadOnlyTools,
          tools: nextTools,
          error: undefined,
          authStatus: nextStatus !== "connected" && input.transport !== "stdio" ? "possible" : undefined,
          authUrl: nextStatus !== "connected" && input.transport !== "stdio" ? input.url : undefined,
        };
      });
    },
    async RemoveMCPServer(name: string) {
      capServers = capServers.filter((s) => s.name !== name);
    },
    async AuthorizeAndConnectMCPServer(name: string) {
      capServers = capServers.map((s) =>
        s.name === name
          ? {
              ...s,
              status: "connected",
              runtimeState: "ready",
              tools: s.tools || 4,
              error: undefined,
              requiresLaunchApproval: false,
            }
          : s,
      );
    },
    async ReconnectMCPServer(name: string) {
      capServers = capServers.map((s) =>
        s.name === name
          ? { ...s, status: "initializing", error: undefined, authStatus: undefined, authUrl: undefined }
          : s,
      );
      await new Promise((r) => setTimeout(r, 400));
      capServers = capServers.map((s) =>
        s.name === name ? { ...s, status: "connected", tools: s.tools || 4 } : s,
      );
    },
    async ClearMCPServerAuthentication(name: string) {
      capServers = capServers.map((s) =>
        s.name === name
          ? {
              ...s,
              status: s.autoStart === false ? "disabled" : "initializing",
              tools: 0,
              error: undefined,
              authStatus: s.transport !== "stdio" ? "possible" : undefined,
              authUrl: s.transport !== "stdio" ? s.url : undefined,
              authConfigured: undefined,
            }
          : s,
      );
    },
    async PickSkillFolder() {
      return "~/my-skills";
    },
    async PickPluginFolder() {
      return "~/plugins/superpowers";
    },
    async AddSkillPath(path: string) {
      const dir = path.trim() || "~/my-skills";
      if (!capSkillRoots.some((r) => r.scope === "custom" && r.dir === dir)) {
        capSkillRoots.push({
          dir,
          scope: "custom",
          priority: capSkillRoots.length + 1,
          status: "ok",
          configured: true,
          removable: true,
          skills: 1,
          skillItems: [{ name: "local-dev", description: "Local custom development workflow", scope: "custom", runAs: "inline" }],
        });
      }
      if (!capSkills.some((s) => s.name === "local-dev")) {
        capSkills.push({ name: "local-dev", description: "Local custom development workflow", scope: "custom", runAs: "inline", enabled: true });
      }
    },
    async RemoveSkillPath(path: string) {
      capSkillRoots = capSkillRoots.filter((r) => r.dir !== path);
      if (!capSkillRoots.some((r) => r.scope === "custom")) {
        const idx = capSkills.findIndex((s) => s.name === "local-dev");
        if (idx >= 0) capSkills.splice(idx, 1);
      }
    },
    async RefreshSkills() {},
    async ReloadCommands() {},
    async SetSkillEnabled(name: string, enabled: boolean) {
      const skill = capSkills.find((s) => s.name === name);
      if (skill) skill.enabled = enabled;
    },
    async AvailableSubagentTools() {
      return [
        { name: "read_file", description: "Read a file's contents", readOnlyHint: true },
        { name: "ls", description: "List a directory", readOnlyHint: true },
        { name: "glob", description: "Find files by name pattern", readOnlyHint: true },
        { name: "grep", description: "Search file contents", readOnlyHint: true },
        { name: "code_index", description: "Look up symbol definitions and file outlines", readOnlyHint: true },
        { name: "edit_file", description: "Edit an existing file" },
        { name: "write_file", description: "Write a new file" },
        { name: "bash", description: "Run a shell command" },
        { name: "web_fetch", description: "Fetch a URL" },
      ];
    },
    async CreateSubagentProfile(input: SubagentProfileInput) {
      const name = input.name.trim();
      const builtinNames = ["init", "explore", "research", "install-capability", "review", "security-review", "test"];
      if (builtinNames.includes(name)) throw new Error(`"${name}" is a built-in subagent name and cannot be reused`);
      if (capSkills.some((s) => s.name === name)) throw new Error(`"${name}" already exists`);
      capSkills.push({
        name, description: input.description, scope: input.scope === "project" ? "project" : "global",
        runAs: "subagent", enabled: true, model: input.model, effort: input.effort,
        allowedTools: input.allowedTools, color: input.color, invocation: `/${name}`, invocationMode: "manual",
      });
      return `~/.reasonix/skills/${name}/SKILL.md`;
    },
    async UpdateSubagentProfile(name: string, scope: string, input: SubagentProfileInput) {
      const skill = capSkills.find((s) => s.name === name && s.scope === scope);
      if (!skill) throw new Error(`"${name}" resolves at a different scope — refusing to update`);
      skill.description = input.description;
      skill.color = input.color;
      skill.model = input.model;
      skill.effort = input.effort;
      skill.allowedTools = input.allowedTools;
    },
    async DeleteSubagentProfile(name: string, scope: string) {
      const idx = capSkills.findIndex((s) => s.name === name && s.scope === scope);
      if (idx < 0) throw new Error(`"${name}" resolves at a different scope — refusing to delete`);
      capSkills.splice(idx, 1);
    },
    async SetSubagentProfileModel(name: string, ref: string) {
      const skill = capSkills.find((s) => s.name === name);
      if (skill) skill.configuredModel = ref || undefined;
    },
    async SetSubagentProfileEffort(name: string, level: string) {
      const skill = capSkills.find((s) => s.name === name);
      if (skill) skill.configuredEffort = level || undefined;
    },
    async CancelTrySubagentProfile() {},
    async TrySubagentProfile(input: SubagentProfileInput, task: string) {
      if (!task.trim()) throw new Error("task is required");
      if (!input.systemPrompt.trim()) throw new Error("system prompt is required");
      await new Promise((resolve) => setTimeout(resolve, 400));
      return `[mock run of "${input.name || "draft"}"]\n\nTask: ${task}\n\n(This is a dev-mode mock response — the real backend runs an isolated subagent loop against your configured model.)`;
    },
    async SetMCPServerEnabled(name: string, enabled: boolean) {
      capServers = capServers.map((s) =>
        s.name === name
          ? {
              ...s,
              status: enabled ? "connected" : "disabled",
              autoStart: s.builtIn ? enabled : s.autoStart,
              tools: enabled ? s.tools || 4 : 0,
              error: undefined,
              authStatus: !enabled && s.transport !== "stdio" ? "possible" : undefined,
              authUrl: !enabled && s.transport !== "stdio" ? s.url : undefined,
            }
          : s,
      );
    },
    async SetMCPServerTier(name: string, tier: string) {
      capServers = capServers.map((s) => {
        if (s.name !== name) return s;
        const tools = s.tools || (s.transport === "stdio" ? 3 : 5);
        return { ...s, tier, autoStart: true, status: "connected", tools, error: undefined, authStatus: undefined, authUrl: undefined };
      });
    },
    async SlashArgs(input: string) {
      // Mirror a slice of the real arg hints so the menu is exercisable in browser dev.
      const from = input.lastIndexOf(" ") + 1;
      const cur = input.slice(from);
      const cmd = input.slice(0, input.indexOf(" ") < 0 ? input.length : input.indexOf(" "));
      const subs: Record<string, { label: string; insert: string; hint: string; descend?: boolean }[]> = {
        "/skill": [
          { label: "list", insert: "list", hint: "list skills" },
          { label: "show", insert: "show ", hint: "show a skill's body", descend: true },
          { label: "enable", insert: "enable ", hint: "enable a disabled skill", descend: true },
          { label: "disable", insert: "disable ", hint: "disable an enabled skill", descend: true },
          { label: "new", insert: "new ", hint: "scaffold a new skill" },
          { label: "paths", insert: "paths", hint: "show discovery paths" },
        ],
        "/hooks": [
          { label: "list", insert: "list", hint: "list active hooks" },
          { label: "trust", insert: "trust", hint: "trust this project's hooks" },
        ],
        "/model": [
          { label: "deepseek/deepseek-v4-flash", insert: "deepseek/deepseek-v4-flash", hint: "current" },
          { label: "deepseek/deepseek-v4-pro", insert: "deepseek/deepseek-v4-pro", hint: "" },
        ],
        "/effort": [
          { label: "auto", insert: "auto", hint: "use the model default" },
          { label: "high", insert: "high", hint: "deeper reasoning" },
          { label: "max", insert: "max", hint: "maximum reasoning" },
        ],
      };
      const items = (subs[cmd] ?? [])
        .filter((it) => it.label.toLowerCase().startsWith(cur.toLowerCase()))
        .map((it) => ({ label: it.label, insert: it.insert, hint: it.hint, descend: it.descend ?? false }));
      return { items, from };
    },
    async ListDir(rel: string) {
      // A tiny fake tree so the @ menu is navigable in browser dev.
      if (rel === "" || rel === "./") {
        return [
          { name: "internal", isDir: true },
          { name: "desktop", isDir: true },
          { name: "README.md", isDir: false },
          { name: "go.mod", isDir: false },
        ];
      }
      if (rel === "internal/") {
        return [
          { name: "control", isDir: true },
          { name: "boot", isDir: true },
          { name: "event.go", isDir: false },
        ];
      }
      return [{ name: "file.go", isDir: false }];
    },
    async ListDirForTab(_tabID: string, rel: string) {
      return this.ListDir(rel);
    },
    async SearchFileRefs(query: string) {
      const q = query.toLowerCase();
      return ["desktop/frontend/src/lib/bridge.ts", "frontend/wailsjs/runtime/runtime.js", "internal/control/refs.go"]
        .filter((path) => path.split("/").pop()?.toLowerCase().includes(q))
        .map((name) => ({ name, isDir: false }));
    },
    async SearchFileRefsForTab(_tabID: string, query: string) {
      return this.SearchFileRefs(query);
    },
    async ReadFile(rel: string) {
      const samples: Record<string, string> = {
        "README.md": "# Reasonix\n\nBrowser-dev workspace preview.\n\n- Chat in the center\n- Browse files on the right\n- Keep sessions on the left\n",
        "go.mod": "module reasonix\n\ngo 1.23\n",
        "desktop/file.go": "package desktop\n\nfunc main() {\n\tprintln(\"workspace preview\")\n}\n",
        "internal/event.go": "package internal\n\n// mock file used by the browser dev seam\n",
      };
      return {
        path: rel,
        body: samples[rel] ?? `// ${rel}\n\nMock file body from browser dev.`,
        size: samples[rel]?.length ?? 42,
        truncated: false,
        binary: false,
      };
    },
    async ReadFileForTab(_tabID: string, rel: string) {
      return this.ReadFile(rel);
    },
    async WorkspaceChanges(_tabID: string) {
      return {
        gitAvailable: true,
        gitBranch: "main",
        files: [
          {
            path: "desktop/frontend/src/components/WorkspacePanel.tsx",
            sources: ["session", "git"],
            gitStatus: "M",
            turns: [0, 2],
            latestPrompt: "Mock session edited the workspace panel.",
            latestTime: Date.now() - 60_000,
          },
          { path: "README.md", sources: ["git"], gitStatus: "??" },
          { path: "internal/control/controller.go", sources: ["session"], turns: [1], latestTime: Date.now() - 120_000 },
        ],
      };
    },
    async GitBranches() {
      return ["main", "dev", "feature/branch-switcher"];
    },
    async GitCheckout(_branch: string) {
      console.info("mock GitCheckout", _branch);
    },
    async WorkspaceGitHistory(_tabID: string, path: string) {
      return [
        { hash: "abcdef123456", author: "Mock Author", date: new Date().toISOString(), message: "Mock commit message for " + path },
      ];
    },
    async WorkspaceGitCommitDetail(_tabID: string, _hash: string, path: string) {
      if (path) {
        return { diff: "--- a/mock\n+++ b/mock\n@@ -1,1 +1,1 @@\n-mock\n+mock diff" };
      }
      return { files: ["mock_file_1.ts", "mock_file_2.ts"] };
    },
    async OpenWorkspacePath(rel: string) {
      console.info("mock OpenWorkspacePath", rel);
    },
    async OpenWorkspacePathForTab(_tabID: string, rel: string) {
      await this.OpenWorkspacePath(rel);
    },
    async ExternalOpeners() {
      return {
        openers: [
          { id: "vscode", name: "VS Code", kind: "editor", iconDataUrl: mockExternalOpenerIconDataURL("#1684d6", "V") },
          { id: "cursor", name: "Cursor", kind: "editor", iconDataUrl: mockExternalOpenerIconDataURL("#25262a", "C") },
          { id: "finder", name: "Finder", kind: "file-manager", iconDataUrl: mockExternalOpenerIconDataURL("#36aaf4", "F") },
          { id: "ghostty", name: "Ghostty", kind: "terminal", iconDataUrl: mockExternalOpenerIconDataURL("#264db6", ">") },
        ],
        preferred: "vscode",
      } as ExternalOpenersView;
    },
    async SetPreferredExternalOpener(_id: string) {},
    async OpenWorkspaceInExternalOpener(_id: string) {},
    async OpenWorkspaceInExternalOpenerForTab(_tabID: string, id: string) {
      await this.OpenWorkspaceInExternalOpener(id);
    },
    async RevealWorkspacePath(rel: string) {
      console.info("mock RevealWorkspacePath", rel);
    },
    async RevealWorkspacePathForTab(_tabID: string, rel: string) {
      await this.RevealWorkspacePath(rel);
    },
    async RevealPath(path: string) {
      console.info("mock RevealPath", path);
    },
    async SavePastedImage(dataUrl: string) {
      const path = `.reasonix/attachments/mock-${mockAttachmentDataURLs.size + 1}.png`;
      mockAttachmentDataURLs.set(path, dataUrl);
      return path;
    },
    async SaveClipboardImage() {
      const path = `.reasonix/attachments/mock-clipboard-${mockAttachmentDataURLs.size + 1}.png`;
      mockAttachmentDataURLs.set(path, mockPreviewImageDataURL);
      return path;
    },
    async SavePastedFile(name: string, dataUrl: string) {
      const path = `.reasonix/attachments/mock-${name}`;
      mockAttachmentDataURLs.set(path, dataUrl);
      return path;
    },
    async PickExportFile(defaultFilename: string, _mimeType: string) {
      return defaultFilename;
    },
    async SaveExportFile(path: string, payload: string, base64Encoded: boolean) {
      const a = document.createElement("a");
      let url = "";
      if (base64Encoded) {
        url = `data:application/octet-stream;base64,${payload}`;
      } else {
        url = URL.createObjectURL(new Blob([payload], { type: "text/plain;charset=utf-8" }));
      }
      a.href = url;
      a.download = path;
      document.body.appendChild(a);
      a.click();
      a.remove();
      if (!base64Encoded) URL.revokeObjectURL(url);
    },
    async SaveExportImageFiles(path: string, payloads: string[]) {
      if (payloads.length === 0) throw new Error("No image payloads to export");
      const slash = Math.max(path.lastIndexOf("/"), path.lastIndexOf("\\"));
      const dot = path.lastIndexOf(".");
      const extensionStart = dot > slash ? dot : path.length;
      const stem = path.slice(0, extensionStart);
      const extension = path.slice(extensionStart);
      for (let index = 0; index < payloads.length; index++) {
        const partPath = payloads.length > 1
          ? `${stem}-${index + 1}-of-${payloads.length}${extension}`
          : path;
        await this.SaveExportFile(partPath, payloads[index], true);
      }
    },
    async AttachDropped(path: string) {
      const name = path.split(/[/\\]/).filter(Boolean).pop() ?? path;
      const hasExt = /\.\w{1,6}$/i.test(name);
      if (!hasExt) {
        const tokenName = name.replace(/[^\w.-]+/g, "-") || "folder";
        return { kind: "workspace" as const, path: `__reasonix_external_folder/mock/${tokenName}`, isDir: true, displayPath: path };
      }
      const attachmentPath = `.reasonix/attachments/mock-${name}`;
      mockAttachmentDataURLs.set(attachmentPath, mockPreviewImageDataURL);
      return { kind: "attachment" as const, path: attachmentPath };
    },
    async AttachmentDataURL(path: string) {
      return mockAttachmentDataURLs.get(path) ?? mockPreviewImageDataURL;
    },
        async Models() {
          const active = mockTabs.find((tab) => tab.active) ?? mockTabs[0];
          const current = mockTabModelRef(active);
          return mockModelCatalog.map((model) => ({ ...model, current: model.ref === current }));
        },
        async ModelsForTab(tabID) {
          const tab = mockTabs.find((item) => item.id === tabID) ?? mockTabs.find((item) => item.active) ?? mockTabs[0];
          const current = mockTabModelRef(tab);
          return mockModelCatalog.map((model) => ({ ...model, current: model.ref === current }));
        },
        async SetModel(name) {
          setMockTabModel(undefined, name);
        },
        async SetModelForTab(tabID, name) {
          setMockTabModel(tabID, name);
        },
        async Effort() {
          return { supported: true, current: mockEffort, default: "high", levels: ["auto", "high", "max"] };
        },
        async EffortForTab() {
          return this.Effort();
        },
        async SetEffort(level: string) {
          mockEffort = level || "auto";
        },
        async SetEffortForTab(_tabID, level) {
          await this.SetEffort(level);
        },
        async SetTokenMode(mode: string) {
          const active = mockTabs.find((tab) => tab.active);
          if (active) await this.SetTokenModeForTab(active.id, mode);
        },
        async SetTokenModeForTab(tabID, mode) {
          const tokenMode = normalizeTokenMode(mode);
          mockTabs = mockTabs.map((tab) => (tab.id === tabID ? { ...tab, tokenMode } : tab));
        },
    async Memory() {
      return {
        available: true,
        storeDir: "~/.config/reasonix/projects/-mock/memory",
        storeGlobalDir: "~/.config/reasonix/memory/global",
        docs: [
          {
            path: "REASONIX.md",
            scope: "project",
            body: "# Reasonix project memory\n\nMock doc shown in the browser dev seam.\n\n## Notes\n\n- prefers concise replies",
          },
          {
            path: "~/.config/reasonix/REASONIX.md",
            scope: "user",
            body: t("mock.memoryBody"),
          },
        ],
        facts: [
          {
            name: "prefers-tabs",
            description: "User prefers tabs",
            type: "user",
            body: "Indent with tabs.",
          },
        ],
        archives: [
          {
            name: "old-plan",
            description: "Superseded planning note",
            type: "project",
            body: "This plan was archived after the implementation changed.",
            path: "~/.config/reasonix/projects/-mock/memory/.archive/20260612-021500.000-old-plan.md",
            archivedAt: "2026-06-12T02:15:00Z",
          },
        ],
        scopes: [
          { scope: "user", path: "~/.config/reasonix/REASONIX.md" },
          { scope: "project", path: "REASONIX.md" },
          { scope: "local", path: "REASONIX.local.md" },
        ],
      };
    },
    async MemorySuggestions() {
      return {
        memories: [
          {
            id: "memory-prefers-concise-replies",
            name: "prefers-concise-replies",
            title: "Prefers concise replies",
            description: "User prefers concise replies unless detail is requested.",
            type: "user",
            body: "User prefers concise replies unless detail is requested.\n\n**Why:** Suggested from recent local history.\n**How to apply:** Keep answers brief by default.",
            reason: "future-facing preference",
            evidence: ["mock-session: always keep replies concise"],
          },
        ],
        skills: [
          {
            id: "skill-reasonix-pr-followup",
            name: "reasonix-pr-followup",
            description: "Review or update a Reasonix GitHub PR, address feedback, verify, and publish safely.",
            scope: "project",
            body: "# Reasonix PR Followup\n\nUse this skill for repeated Reasonix PR work.\n\n## Workflow\n\n1. Confirm branch and PR state.\n2. Inspect the diff.\n3. Fix actionable feedback.\n4. Verify and update the PR.\n",
            reason: "recent history repeatedly touched PR workflows",
            evidence: ["mock-pr-session: 提交到pr，并更新内容", "mock-review-session: 解决该pr下机器人提出来的问题"],
          },
        ],
        generatedAt: new Date().toISOString(),
        available: true,
        source: "mock",
      };
    },
    async AcceptMemorySuggestion(suggestion: MemorySuggestion) {
      emit({ kind: "notice", level: "info", text: `saved suggested memory → ${suggestion.name}` });
      return `${suggestion.name}.md`;
    },
    async AcceptSkillSuggestion(suggestion: SkillSuggestion) {
      emit({ kind: "notice", level: "info", text: `created suggested skill → ${suggestion.name}` });
      return `.reasonix/skills/${suggestion.name}/SKILL.md`;
    },
    async MemorySuggestionsForTab(_tabID: string) {
      return this.MemorySuggestions();
    },
    async AcceptMemorySuggestionForTab(_tabID: string, suggestion: MemorySuggestion) {
      return this.AcceptMemorySuggestion(suggestion);
    },
    async AcceptSkillSuggestionForTab(_tabID: string, suggestion: SkillSuggestion) {
      return this.AcceptSkillSuggestion(suggestion);
    },
    async MemoryForTab(_tabID: string) {
      return this.Memory();
    },
    async Remember(_scope: string, _note: string) {
      emit({ kind: "notice", level: "info", text: `remembered → ${_scope}` });
      return `${_scope} REASONIX.md (mock): ${_note}`;
    },
    async RememberForTab(_tabID: string, scope: string, note: string) {
      return this.Remember(scope, note);
    },
    async Forget(_name: string) {
      emit({ kind: "notice", level: "info", text: `forgot → ${_name}` });
    },
    async ForgetForTab(_tabID: string, name: string) {
      return this.Forget(name);
    },
    async SaveDoc(_path: string, _body: string) {
      emit({ kind: "notice", level: "info", text: `saved → ${_path}` });
      return _path;
    },
    async SaveDocForTab(_tabID: string, path: string, body: string) {
      return this.SaveDoc(path, body);
    },
    async DesktopStartupSettings() {
      const { bot, desktopLanguage, desktopLayoutStyle, desktopTheme, desktopThemeStyle, displayMode, statusBarStyle, statusBarItems, checkUpdates, conversationWidth } = settings;
      return JSON.parse(JSON.stringify({
        bot,
        desktopLanguage,
        desktopLayoutStyle,
        desktopTheme,
        desktopThemeStyle,
        displayMode,
        statusBarStyle,
        statusBarItems,
        checkUpdates,
        conversationWidth,
      })) as DesktopStartupSettingsView;
    },
    async Settings() {
      return JSON.parse(JSON.stringify(settings)) as SettingsView;
    },
    async HooksSettings(scope: string) {
      const key = scope === "project" ? "project" : "global";
      return JSON.parse(JSON.stringify(hookSettings[key])) as HooksSettingsView;
    },
    async SaveHooksSettings(scope: string, hooks: HookConfigView[]) {
      const key = scope === "project" ? "project" : "global";
      hookSettings[key].hooks = JSON.parse(JSON.stringify(hooks)) as HookConfigView[];
    },
    async SaveHooksSettingsForRoot(scope: string, _projectRoot: string, hooks: HookConfigView[]) {
      const key = scope === "project" ? "project" : "global";
      hookSettings[key].hooks = JSON.parse(JSON.stringify(hooks)) as HookConfigView[];
    },
    async TrustProjectHooks() {
      hookSettings.project.trusted = true;
    },
    async TrustProjectHooksForRoot(projectRoot: string) {
      if (projectRoot && projectRoot === hookSettings.project.projectRoot) {
        hookSettings.project.trusted = true;
      }
    },
    async SetDefaultModel(ref: string) {
      settings.defaultModel = ref;
    },
    async SetPlannerModel(ref: string) {
      settings.plannerModel = ref;
    },
    async SetSubagentModel(ref: string) {
      settings.subagentModel = ref;
    },
    async SetSubagentEffort(level: string) {
      settings.subagentEffort = level;
    },
    async SetMaxSubagentDepth(depth: number) {
      settings.agent = { ...settings.agent, maxSubagentDepth: depth <= 1 ? 1 : 2 };
    },
    async SetMaxSubagentConcurrency(n: number) {
      const total = Math.max(1, Math.min(32, Math.floor(n) || 6));
      const writers = Math.min(total, Math.max(1, settings.agent.maxParallelWriters || 3));
      settings.agent = { ...settings.agent, maxSubagentConcurrency: total, maxParallelWriters: writers };
    },
    async SetMaxParallelWriters(n: number) {
      const total = Math.max(1, Math.min(32, settings.agent.maxSubagentConcurrency || 6));
      const writers = Math.max(1, Math.min(total, Math.floor(n) || 3));
      settings.agent = { ...settings.agent, maxParallelWriters: writers };
    },
    async SetAutoPlan(mode: string) {
      settings.autoPlan = mode;
    },
    async SetDefaultToolApprovalMode(mode: string) {
      settings.defaultToolApprovalMode = normalizeToolApprovalMode(mode);
    },
    async SaveProvider(p: ProviderView) {
      p.added = true;
      const i = settings.providers.findIndex((x) => x.name === p.name);
      if (i >= 0) settings.providers[i] = p;
      else settings.providers.push(p);
    },
    async SaveProviderWithKey(p: ProviderView, key: string) {
      p.added = true;
      p.keySet = Boolean(key.trim()) || p.keySet;
      const i = settings.providers.findIndex((x) => x.name === p.name);
      if (i >= 0) settings.providers[i] = p;
      else settings.providers.push(p);
      return "";
    },
    async AddOfficialProviderAccess(kind: string, key: string) {
      const templates: Record<string, ProviderView> = {
        deepseek: { name: "deepseek", builtIn: true, added: true, kind: "openai", baseUrl: "https://api.deepseek.com", modelsUrl: "", models: ["deepseek-v4-flash", "deepseek-v4-pro"], visionModels: [], visionModelsConfigured: false, default: "deepseek-v4-flash", apiKeyEnv: "DEEPSEEK_API_KEY", keySet: !!key.trim(), balanceUrl: "https://api.deepseek.com/user/balance", contextWindow: 1_000_000, reasoningProtocol: "", thinking: "", supportedEfforts: [], defaultEffort: "" },
      };
      const next = templates[kind];
      if (!next) throw new Error(`unknown official provider template ${kind}`);
      const i = settings.providers.findIndex((x) => x.name === next.name);
      if (i >= 0) settings.providers[i] = { ...settings.providers[i], ...next, keySet: next.keySet || settings.providers[i].keySet };
      else settings.providers.push(next);
      return "";
    },
    async AddProviderPresetAccess(id: string, key: string) {
      const preset = settings.providerPresets.find((p) => p.id === id);
      if (!preset) throw new Error(`unknown provider preset ${id}`);
      const next = cloneMockProviderTemplate(id, key);
      if (!next) throw new Error(`unknown provider preset ${id}`);
      const i = settings.providers.findIndex((x) => x.name === next.name);
      if (i >= 0) settings.providers[i] = { ...settings.providers[i], ...next, keySet: next.keySet || settings.providers[i].keySet };
      else settings.providers.push(next);
      preset.added = true;
      preset.status = "installed";
      preset.statusProviderNames = [...preset.providerNames];
      preset.keySet = preset.keySet || !!key.trim();
      preset.configured = !preset.requiresKey || preset.keySet;
      return "";
    },
    async ResetProviderPresetAccess(id: string) {
      const preset = settings.providerPresets.find((p) => p.id === id);
      if (!preset) throw new Error(`unknown provider preset ${id}`);
      const next = cloneMockProviderTemplate(id, "");
      if (!next) throw new Error(`unknown provider preset ${id}`);
      const i = settings.providers.findIndex((x) => x.name === next.name);
      if (i < 0) throw new Error(`provider preset ${id} cannot be reset because no same-name provider exists`);
      const existing = settings.providers[i];
      settings.providers[i] = {
        ...next,
        added: true,
        keySet: existing.apiKeyEnv === next.apiKeyEnv ? existing.keySet : next.keySet,
      };
      preset.added = true;
      preset.status = "installed";
      preset.statusProviderNames = [...preset.providerNames];
      preset.keySet = preset.keySet || settings.providers[i].keySet;
      preset.configured = !preset.requiresKey || preset.keySet;
    },
    async FetchProviderModels(p: ProviderView) {
      if (!p.baseUrl.trim()) throw new Error(t("settings.fetchModelsMissingBaseUrl"));
      if (providerRequiresKey(p) && !p.apiKeyEnv.trim()) throw new Error(t("settings.fetchModelsMissingKeyEnv"));
      await delay(350);
      if (p.baseUrl.includes("deepseek")) return ["deepseek-v4-flash", "deepseek-v4-pro"];
      if (p.baseUrl.includes("token-plan")) return ["mimo-v2.5", "mimo-v2.5-pro"];
      if (p.baseUrl.includes("xiaomimimo")) return ["mimo-v2.5-pro", "mimo-v2.5"];
      return ["gpt-5", "gpt-5-mini", "qwen3-coder"];
    },
    async DeleteProvider(name: string) {
      settings.providers = settings.providers.filter((p) => p.name !== name);
    },
    async RemoveProviderAccess(name: string) {
      const p = settings.providers.find((x) => x.name === name);
      if (p?.builtIn) p.added = false;
      else settings.providers = settings.providers.filter((x) => x.name !== name);
    },
    async SaveProviderKey(apiKeyEnv: string, _value: string) {
      settings.providers.forEach((p) => {
        if (p.apiKeyEnv === apiKeyEnv) p.keySet = true;
      });
      return "";
    },
    async SetProviderKey(apiKeyEnv: string, _value: string) {
      settings.providers.forEach((p) => {
        if (p.apiKeyEnv === apiKeyEnv) p.keySet = true;
      });
      return "";
    },
    async ClearProviderKey(apiKeyEnv: string) {
      settings.providers.forEach((p) => {
        if (p.apiKeyEnv === apiKeyEnv) p.keySet = false;
      });
    },
    async SetPermissionMode(mode: string) {
      settings.permissions.mode = mode;
    },
    async AddPermissionRule(list: string, rule: string) {
      const k = list as "allow" | "ask" | "deny";
      if (settings.permissions[k] && !settings.permissions[k].includes(rule)) settings.permissions[k].push(rule);
    },
    async RemovePermissionRule(list: string, rule: string) {
      const k = list as "allow" | "ask" | "deny";
      settings.permissions[k] = settings.permissions[k].filter((r) => r !== rule);
    },
        async ReloadSettings() {},
        async SetSandbox(bash: string, network: boolean, workspaceRoot: string, allowWrite: string[], shell: string) {
          const effectiveWorkspaceRoot = workspaceRoot.trim() || cwd;
          settings.sandbox = { bash, network, workspaceRoot, allowWrite, effectiveWorkspaceRoot, effectiveWriteRoots: [effectiveWorkspaceRoot, ...allowWrite], shell, effectiveShell: browserPreviewEffectiveShell(shell) };
        },
        async SetNetwork(n: NetworkView) {
          settings.network = n;
        },
        async SetBotSettings(b: BotSettingsView) {
          settings.bot = JSON.parse(JSON.stringify(b)) as BotSettingsView;
        },
        async SetBotConnectionToolApprovalMode(connID, mode) {
          const conn = settings.bot.connections.find((c) => c.id === connID);
          if (conn) conn.toolApprovalMode = mode as any;
        },
        async SetBotSecret(envName: string, _value: string) {
          const name = envName.trim();
          if (settings.bot.qq.appSecretEnv === name) settings.bot.qq.secretSet = true;
          if (settings.bot.feishu.appSecretEnv === name) settings.bot.feishu.secretSet = true;
          if (settings.bot.weixin.tokenEnv === name) settings.bot.weixin.tokenSet = true;
          settings.bot.connections = settings.bot.connections.map((connection) => ({
            ...connection,
            credential: connection.credential.appSecretEnv === name || connection.credential.tokenEnv === name
              ? { ...connection.credential, secretSet: true }
              : connection.credential,
          }));
        },
        async ClearBotSecret(envName: string) {
          const name = envName.trim();
          if (settings.bot.qq.appSecretEnv === name) settings.bot.qq.secretSet = false;
          if (settings.bot.feishu.appSecretEnv === name) settings.bot.feishu.secretSet = false;
          if (settings.bot.weixin.tokenEnv === name) settings.bot.weixin.tokenSet = false;
          settings.bot.connections = settings.bot.connections.map((connection) => ({
            ...connection,
            credential: connection.credential.appSecretEnv === name || connection.credential.tokenEnv === name
              ? { ...connection.credential, secretSet: false }
              : connection.credential,
          }));
        },
        async BotRuntimeStatus() {
          const qqRunning = settings.bot.qq.enabled && settings.bot.qq.appId.trim() && settings.bot.qq.secretSet;
          const runningConnections = (qqRunning ? 1 : 0) + settings.bot.connections.filter((connection) => connection.enabled && connection.status === "connected").length;
          return {
            running: settings.bot.enabled && runningConnections > 0,
            status: settings.bot.enabled && runningConnections > 0 ? "running" : "stopped",
            message: settings.bot.enabled && runningConnections > 0 ? `${runningConnections} bot connection(s) running` : "bot runtime is not started",
            connections: runningConnections,
            startedAt: settings.bot.enabled && runningConnections > 0 ? new Date(t0).toISOString() : "",
          };
        },
        async StartBotConnectionInstall(provider: string, domain: string) {
          const normalizedProvider = provider === "weixin" ? "weixin" : "feishu";
          const normalizedDomain = normalizedProvider === "weixin" ? "weixin" : domain === "lark" ? "lark" : "feishu";
          return {
            ok: true,
            provider: normalizedProvider,
            domain: normalizedDomain,
            installId: `mock-${normalizedProvider}-${normalizedDomain}`,
            url: "https://example.com/reasonix-bot-qr",
            deviceCode: "MOCKDEVICE",
            userCode: normalizedProvider === "weixin" ? "" : "MOCK-CODE",
            interval: 3,
            expireIn: 300,
            message: "",
          };
        },
        async PollBotConnectionInstall(installID: string) {
          const isWeixin = installID.includes("weixin");
          const domain = installID.includes("lark") ? "lark" : isWeixin ? "weixin" : "feishu";
          const provider = isWeixin ? "weixin" : "feishu";
          const connection = {
            id: `${provider}-${domain}`,
            provider,
            domain,
            label: domain === "lark" ? "Lark" : domain === "weixin" ? "微信" : "飞书",
            enabled: true,
            status: "connected",
	            model: "",
	            toolApprovalMode: "",
	            workspaceRoot: "",
	            access: { enabled: true, allowAll: false, pairingEnabled: true, users: [provider === "weixin" ? "wxid_mock_user_001" : "ou_mock_user_001"], groups: [], approvers: [], admins: [] },
	            credential: {
              appId: provider === "feishu" ? "cli_mock" : "",
              appSecretEnv: provider === "feishu" ? (domain === "lark" ? "LARK_BOT_APP_SECRET" : "FEISHU_BOT_APP_SECRET") : "",
              accountId: provider === "weixin" ? "mock-account" : "",
              tokenEnv: provider === "weixin" ? "WEIXIN_BOT_TOKEN" : "",
              secretSet: true,
            },
            sessionMappings: [],
            lastError: "",
            createdAt: new Date().toISOString(),
            updatedAt: new Date().toISOString(),
          };
          settings.bot.connections = [...settings.bot.connections.filter((c) => c.id !== connection.id), connection];
          return { done: true, connection, status: "connected", message: "connected", error: "" };
        },
        async DiagnoseBotConnection(id: string) {
          const connection = settings.bot.connections.find((c) => c.id === id);
          const occurredAt = new Date().toISOString();
          return connection
            ? { id, label: connection.label, status: connection.enabled ? "ok" : "disabled", message: connection.enabled ? "连接配置已保存。" : "连接已保存但未启用。", messageId: "", phase: "config", code: connection.enabled ? "config_ok" : "connection_disabled", reportKind: "", reportDetail: "", occurredAt }
            : { id, label: "", status: "missing", message: "未找到连接。", messageId: "", phase: "config", code: "connection_missing", reportKind: "bot", reportDetail: JSON.stringify({ schemaVersion: 2, kind: "bot", source: "bot.runtime", label: "bot.mock.config", message: "mock missing bot connection", errorType: "BotConnectionDiagnostic", errorMessage: "bot connection record was not found", topFrame: "bot.config", occurredAt }), occurredAt };
        },
        async TestBotConnection(id: string, target?: string) {
          const diag = await this.DiagnoseBotConnection(id);
          if (target?.trim()) return { ...diag, message: `Mock test sent to ${target.trim()}`, messageId: "mock-message-id" };
          return diag;
        },
        async SetCloseBehavior(mode: string) {
          settings.closeBehavior = mode === "quit" ? "quit" : "background";
        },
        async SetDisplayMode(mode: string) {
          settings.displayMode = mode;
        },
        async SetStatusBarStyle(style: string) {
          settings.statusBarStyle = style === "text" ? "text" : "icon";
        },
        async SetStatusBarItems(items: string[]) {
          settings.statusBarItems = normalizeStatusBarItems(items);
        },
        async SetDesktopLanguage(lang: string) {
          settings.desktopLanguage = lang === "en" || lang === "zh" ? lang : "";
        },
        async SetDesktopAppearance(theme: string, style: string) {
          settings.desktopTheme = theme === "auto" || theme === "light" ? theme : "dark";
          settings.desktopThemeStyle = style;
          mockThemeMode = settings.desktopTheme as "auto" | "light" | "dark";
          if (["graphite","aurora","slate","carbon","nocturne","amber"].includes(style)) {
            mockBaseStyle = style;
          }
        },
        async ListThemePacks() {
          const baseActive = !mockActiveThemeId;
          return mockThemePacks.map((p) => {
            const kind = p.kind || (p.builtin ? "base" : "user");
            let active = false;
            if (kind === "base") active = baseActive && p.id === mockBaseStyle;
            else active = p.id === mockActiveThemeId;
            return { ...p, active, tokens: { light: { ...(p.tokens.light || {}) }, dark: { ...(p.tokens.dark || {}) } }, recipes: { ...p.recipes } };
          });
        },
        async GetActiveThemePack() {
          // Base style ids are never active packs in the redesigned model.
          const pack = mockActiveThemeId && !["graphite","aurora","slate","carbon","nocturne","amber"].includes(mockActiveThemeId)
            ? mockThemePacks.find((p) => p.id === mockActiveThemeId)
            : null;
          return { activeThemeId: pack ? mockActiveThemeId : "", pack: pack ? { ...pack, active: true } : null, safeMode: false };
        },
        async GetThemeExperience() {
          const pack = mockActiveThemeId && !["graphite","aurora","slate","carbon","nocturne","amber"].includes(mockActiveThemeId)
            ? mockThemePacks.find((p) => p.id === mockActiveThemeId)
            : null;
          return {
            themeMode: mockThemeMode,
            baseStyle: mockBaseStyle,
            effectiveStyle: pack?.baseStyle || mockBaseStyle,
            activeThemeId: pack ? mockActiveThemeId : "",
            activePack: pack ? { ...pack, active: true } : null,
            safeMode: false,
          };
        },
        async ActivateThemePack(id: string) {
          const next = String(id || "").trim();
          if (["graphite","aurora","slate","carbon","nocturne","amber"].includes(next)) {
            throw new Error(`base style ${next} is not a theme pack; use ActivateBaseStyle`);
          }
          mockActiveThemeId = next;
        },
        async ActivateBaseStyle(style: string) {
          const s = String(style || "").trim().toLowerCase();
          if (!["graphite","aurora","slate","carbon","nocturne","amber"].includes(s)) {
            throw new Error(`unknown base style ${s}`);
          }
          mockBaseStyle = s;
          mockActiveThemeId = "";
        },
        async DisableThemePack() {
          mockActiveThemeId = "";
        },
        async RestoreGraphiteAppearance() {
          mockBaseStyle = "graphite";
          mockActiveThemeId = "";
        },
        async ResetThemePack() {
          mockActiveThemeId = "";
        },
        async SaveThemePack(input: import("./themePack").ThemeSaveInput) {
          const pack: import("./themePack").ThemePackView = {
            id: input.id,
            name: input.name,
            author: input.author,
            description: input.description,
            license: input.license,
            baseStyle: input.baseStyle,
            builtin: false,
            kind: "user",
            active: Boolean(input.activate),
            hasBackground: Boolean(
              (input.background && (input.backgroundDataUrl || input.background.image)) ||
              (input.taskBackground && (input.taskBackgroundDataUrl || input.taskBackground.image)),
            ),
            backgroundUrl: input.backgroundDataUrl || "",
            taskBackgroundUrl: input.taskBackgroundDataUrl || "",
            tokens: input.tokens || {},
            recipes: input.recipes || { density: "comfortable", corners: "soft" },
            background: input.background ?? undefined,
            taskBackground: input.taskBackground ?? undefined,
          };
          const idx = mockThemePacks.findIndex((p) => p.id === pack.id);
          if (idx >= 0) mockThemePacks[idx] = pack;
          else mockThemePacks.push(pack);
          if (input.activate) mockActiveThemeId = pack.id;
          return pack;
        },
        async DeleteThemePack(id: string) {
          mockThemePacks = mockThemePacks.filter((p) => p.id !== id || p.builtin);
          if (mockActiveThemeId === id) mockActiveThemeId = "";
        },
        async CopyThemePack(sourceID: string, newID: string, newName: string) {
          const src = mockThemePacks.find((p) => p.id === sourceID);
          if (!src) throw new Error("source theme not found");
          const pack: import("./themePack").ThemePackView = {
            ...src,
            id: newID,
            name: newName || `${src.name} Copy`,
            builtin: false,
            kind: "user",
            nameKey: undefined,
            descriptionKey: undefined,
            active: false,
          };
          mockThemePacks.push(pack);
          return pack;
        },
        async ImportThemePack(_sourcePath: string, replace: boolean) {
          if (replace) {
            return { pack: mockThemePacks[0], replaced: true };
          }
          // Simulate conflict path without re-prompting for a file on confirm.
          return { pack: mockThemePacks[0], replaced: false, needsReplace: true, pendingId: "pending-mock" };
        },
        async ExportThemePack(_id: string, _destPath: string) {
          return "";
        },
        async PickThemeBackground() {
          return "";
        },
        async SetDesktopLayoutStyle(style: string) {
          settings.desktopLayoutStyle = style === "workbench" || style === "creation" ? style : "classic";
        },
        async SetDesktopZoomFactor(factor: number) {
          mockDesktopZoomFactor = Math.min(2.0, Math.max(0.5, Number.isFinite(factor) ? factor : 1.0));
        },
        async GetDesktopZoomFactor() {
          return mockDesktopZoomFactor;
        },
        async RestartApplication() {
          // no-op in mock
        },
        async SetDesktopCheckUpdates(enabled: boolean) {
          settings.checkUpdates = enabled;
        },
        async SetDesktopTelemetry(enabled: boolean) {
          settings.telemetry = enabled;
        },
        async SetDesktopMetrics(enabled: boolean) {
          settings.metrics = enabled;
        },
        async SetDesktopConversationWidth(width: string) {
          settings.conversationWidth = width;
        },
        async SetExpandThinking(_on: boolean) {},
        async MigrateDesktopPreferences(language: string, theme: string, style: string) {
          if (!settings.desktopLanguage) settings.desktopLanguage = language === "en" || language === "zh" || language === "zh-TW" ? language : "";
          if (!settings.desktopTheme && !settings.desktopThemeStyle) {
            settings.desktopTheme = theme === "auto" || theme === "light" ? theme : "dark";
            settings.desktopThemeStyle = style;
          }
        },
    async SetAgentParams(temperature: number, maxSteps: number, plannerMaxSteps: number, systemPrompt: string) {
      settings.agent = { ...settings.agent, temperature, maxSteps, plannerMaxSteps, systemPrompt };
    },
    async SetColdResumePrune(enabled: boolean) {
      settings.agent = { ...settings.agent, coldResumePrune: enabled };
    },
    async SetReasoningLanguage(lang: string) {
      const normalized = lang === "zh" || lang === "en" ? lang : "auto";
      settings.agent = { ...settings.agent, reasoningLanguage: normalized };
    },
    // ── Heartbeat mock ──
    async HeartbeatListTasks() { return []; },
    async HeartbeatReloadTasks() { return []; },
    async HeartbeatSaveTasks(_tasks: unknown) {},
    async HeartbeatTriggerNow(_id: string) {},
    async HeartbeatGenerateID() { return "mock-" + Date.now().toString(36); },
    async SetTrayLocale(_locale: "en" | "zh" | "zh-TW") {},
    async SetAutoApproveTools(on: boolean) {
      await this.SetToolApprovalMode(on ? "yolo" : "ask");
    },
    async SetBypass(on: boolean) {
      await this.SetAutoApproveTools(on);
    },
    async Version() {
      return "v1.0.0 (browser dev)";
    },
    async CheckUpdate() {
      // Keep the default browser preview focused on the primary product surface.
      // DownloadUpdate/InstallUpdate remain mocked for explicit updater-flow tests.
      return {
        available: false,
        current: "v1.0.0",
        latest: "v1.0.0",
        notes: "",
        channel: "stable",
        canSelfUpdate: false,
        manualOnly: true,
        manualReason: "browser preview",
        downloaded: false,
        downloadUrl: "",
        assetSize: 0,
      };
    },
    async DownloadUpdate() {
      const total = 12_345_678;
      for (let r = 0; r <= total; r += 1_800_000) {
        emitUpdater({ phase: "downloading", received: Math.min(r, total), total });
        await delay(120);
      }
      emitUpdater({ phase: "verifying", received: total, total });
      await delay(500);
      emitUpdater({ phase: "downloaded", received: total, total });
      return { version: "v1.1.0", channel: "stable", path: "/tmp/reasonix-update", size: total, sha256: "mock" };
    },
    async InstallUpdate() {
      const total = 12_345_678;
      emitUpdater({ phase: "installing", received: total, total });
      await delay(500);
      emitUpdater({ phase: "done", received: total, total });
      // The real shell relaunches here; the mock just stops.
    },
    async ApplyUpdate() {
      await this.DownloadUpdate();
      await this.InstallUpdate();
    },
    async OpenDownloadPage() {
      if (typeof window !== "undefined") {
        window.open("https://reasonix.io/?download=desktop#start", "_blank", "noopener");
      }
    },
    // Dev seam: drives the overlay flow in the browser until ConnectKey sets the
    // key. Matches ConnectKey on apiKeyEnv so the two stay in sync.
    async NeedsOnboarding() {
      return !settings.providers.find((p) => p.apiKeyEnv === "DEEPSEEK_API_KEY")?.keySet;
    },
    async ConnectKey(apiKey: string) {
      if (!apiKey.trim()) throw new Error("key is required");
      settings.providers.forEach((p) => {
        if (p.apiKeyEnv === "DEEPSEEK_API_KEY") p.keySet = true;
      });
      await delay(300);
      return "";
    },
    async ReportCrash() {
      await delay(300);
    },
    // Tab management mocks.
    async ListTabs() {
      return mockTabs.map((tab) => ({ ...tab }));
    },
    async OpenProjectTab(workspaceRoot: string, _topicID: string) {
      const existing = mockTabs.find((tab) => tab.scope === "project" && tab.workspaceRoot === workspaceRoot && tab.topicId === _topicID);
      if (existing) {
        const active = { ...existing, active: true, running: mockTopicRunsInScenario(_topicID) };
        mockTabs = mockTabs.map((tab) => (tab.id === existing.id ? active : { ...tab, active: false }));
        return { ...active };
      }
      const defaultToolApprovalMode = normalizeToolApprovalMode(settings.defaultToolApprovalMode);
      const tab: TabMeta = {
        id: "tab_" + Date.now(),
        scope: "project",
        workspaceRoot,
        workspaceName: workspaceRoot.split("/").filter(Boolean).pop() ?? workspaceRoot,
        workspacePath: workspaceRoot,
        gitBranch: "main",
        topicId: _topicID,
        topicTitle: topicLabel(_topicID, t("mock.newSession")),
        sessionPath: `/mock/sessions/${_topicID}.jsonl`,
        projectColor: mockProjectTree.find((node) => node.root === workspaceRoot)?.projectColor,
        label: mockModelLabel(settings.defaultModel),
        ready: true,
        running: mockTopicRunsInScenario(_topicID),
        mode: modeWithAutoApproveTools("normal", defaultToolApprovalMode === "yolo"),
        collaborationMode: "normal",
        toolApprovalMode: defaultToolApprovalMode,
        tokenMode: "full",
        active: true,
        cwd: workspaceRoot,
      };
      mockTabs = [...mockTabs.map((item) => ({ ...item, active: false })), tab];
      return { ...tab };
    },
    async DeliveryWorktreeAvailability(workspaceRoot: string) {
      return workspaceRoot
        ? { available: true, repoRoot: workspaceRoot, branch: "main", sourceDirty: false }
        : { available: false, reason: "project folder is required" };
    },
    async CreateDeliveryWorktree(workspaceRoot: string) {
      if (!workspaceRoot) throw new Error("project folder is required");
      const suffix = Date.now().toString(36);
      const isolatedRoot = `/mock/reasonix-worktrees/${suffix}/${workspaceRoot.split("/").filter(Boolean).pop() ?? "project"}`;
      const topicID = `topic_worktree_${suffix}`;
      const tab = await this.OpenProjectTab(isolatedRoot, topicID);
      tab.isolatedWorktree = true;
      tab.gitBranch = `reasonix/delivery-${suffix}`;
      mockTabs = mockTabs.map((candidate) => candidate.id === tab.id ? { ...tab } : candidate);
      return {
        workspaceRoot: isolatedRoot,
        worktreeRoot: isolatedRoot,
        sourceRoot: workspaceRoot,
        branch: tab.gitBranch,
        sourceDirty: false,
        tab,
      };
    },
    async OpenGlobalTab(_topicID: string) {
      const existing = mockTabs.find((tab) => tab.scope === "global" && tab.topicId === _topicID);
      if (existing) {
        setMockActiveTab(existing.id);
        return { ...existing, active: true };
      }
      const defaultToolApprovalMode = normalizeToolApprovalMode(settings.defaultToolApprovalMode);
      const tab: TabMeta = {
        id: "tab_" + Date.now(),
        scope: "global",
        workspaceRoot: "",
        workspaceName: "Global",
        workspacePath: cwd,
        topicId: _topicID,
        topicTitle: topicLabel(_topicID, "Global"),
        sessionPath: `/mock/sessions/${_topicID}.jsonl`,
        label: mockModelLabel(settings.defaultModel),
        ready: true,
        running: false,
        mode: modeWithAutoApproveTools("normal", defaultToolApprovalMode === "yolo"),
        collaborationMode: "normal",
        toolApprovalMode: defaultToolApprovalMode,
        tokenMode: "full",
        active: true,
        cwd: "",
      };
      mockTabs = [...mockTabs.map((item) => ({ ...item, active: false })), tab];
      return { ...tab };
    },
    async OpenTopicSession(scope: string, workspaceRoot: string, topicID: string, sessionPath: string) {
      const tab = scope === "project"
        ? await this.OpenProjectTab(workspaceRoot, topicID)
        : await this.OpenGlobalTab(topicID);
      const active = { ...tab, sessionPath };
      mockTabs = mockTabs.map((item) => (item.id === tab.id ? active : item));
      return { ...active };
    },
    async EnsureBlankTab(scope: string, workspaceRoot: string) {
      const targetScope = scope === "project" && workspaceRoot ? "project" : "global";
      const targetRoot = targetScope === "project" ? workspaceRoot : "";
      const existing = mockTabs.find((tab) =>
        tab.scope === targetScope &&
        (targetScope === "global" || tab.workspaceRoot === targetRoot) &&
        !tab.running &&
        mockTopicIsBlank(tab.topicId)
      );
      if (existing) {
        setMockActiveTab(existing.id);
        return { ...existing, active: true };
      }
      const topic = await this.CreateTopic(targetScope, targetRoot, "");
      return targetScope === "global" ? this.OpenGlobalTab(topic.id) : this.OpenProjectTab(targetRoot, topic.id);
    },
    async ActivateTopic(scope: string, workspaceRoot: string, topicID: string, sessionPath: string) {
      const tab = sessionPath
        ? await this.OpenTopicSession(scope, workspaceRoot, topicID, sessionPath)
        : scope === "project"
          ? await this.OpenProjectTab(workspaceRoot, topicID)
          : await this.OpenGlobalTab(topicID);
      mockTabs = mockTabs.filter((item) => item.id === tab.id).map((item) => ({ ...item, active: true }));
      return { ...mockTabs[0] };
    },
    async EnsureBlankSurface(scope: string, workspaceRoot: string) {
      const tab = await this.EnsureBlankTab(scope, workspaceRoot);
      mockTabs = mockTabs.filter((item) => item.id === tab.id).map((item) => ({ ...item, active: true }));
      return { ...mockTabs[0] };
    },
    async SetActiveTab(_tabID: string) {
      setMockActiveTab(_tabID);
      const tab = mockTabs.find((item) => item.id === _tabID);
      if (tab) queueMockTopicRuntime(tab);
    },
    async ReorderTabs(_tabIDs: string[]) {
      const byId = new Map(mockTabs.map((tab) => [tab.id, tab]));
      const ordered = _tabIDs.map((id) => byId.get(id)).filter((tab): tab is TabMeta => Boolean(tab));
      if (ordered.length === mockTabs.length) mockTabs = ordered;
    },
    async CloseTab(_tabID: string) {
      if (mockTabs.length <= 1) return;
      const wasActive = mockTabs.some((tab) => tab.id === _tabID && tab.active);
      mockTabs = mockTabs.filter((tab) => tab.id !== _tabID);
      if (wasActive && mockTabs.length > 0 && !mockTabs.some((tab) => tab.active)) {
        mockTabs[mockTabs.length - 1] = { ...mockTabs[mockTabs.length - 1], active: true };
      }
    },
    async ListProjectTree() {
      return cloneProjectTree();
    },
    async RenameProject(workspaceRoot: string, title: string) {
      const node = workspaceRoot
        ? mockProjectTree.find((item) => item.root === workspaceRoot)
        : mockProjectTree.find((item) => item.kind === "global_folder");
      if (node) node.label = title.trim() || (node.kind === "global_folder" ? "Global" : node.label);
    },
    async SetProjectColor(workspaceRoot: string, color: string) {
      const node = workspaceRoot
        ? mockProjectTree.find((item) => item.root === workspaceRoot)
        : mockProjectTree.find((item) => item.kind === "global_folder");
      if (!node) return;
      node.projectColor = color || undefined;
      for (const child of projectChildren(node)) child.projectColor = node.projectColor;
      mockTabs = mockTabs.map((tab) =>
        (workspaceRoot ? tab.workspaceRoot === workspaceRoot : tab.scope === "global")
          ? { ...tab, projectColor: node.projectColor }
        : tab,
      );
    },
    async SetProjectPinned(workspaceRoot: string, pinned: boolean) {
      setMockProjectPinned(workspaceRoot, pinned);
    },
    async ReorderProjects(workspaceRoots: string[]) {
      const projects = mockProjectTree.filter((node) => node.kind === "project");
      const globals = mockProjectTree.filter((node) => node.kind === "global_folder");
      if (!workspaceRoots.includes(GLOBAL_PROJECT_ORDER_KEY)) {
        if (workspaceRoots.length !== projects.length) return;
        const byRoot = new Map(projects.map((node) => [node.root, node]));
        const ordered = workspaceRoots.map((root) => byRoot.get(root)).filter((node): node is ProjectNode => Boolean(node));
        if (ordered.length !== projects.length) return;
        mockProjectTree.splice(0, mockProjectTree.length, ...globals, ...ordered);
        return;
      }
      const byKey = new Map<string, ProjectNode>();
      for (const node of projects) {
        if (node.root) byKey.set(node.root, node);
      }
      for (const node of globals) byKey.set(GLOBAL_PROJECT_ORDER_KEY, node);
      const seen = new Set<string>();
      const ordered: ProjectNode[] = [];
      for (const key of workspaceRoots) {
        if (seen.has(key)) return;
        const node = byKey.get(key);
        if (!node) return;
        seen.add(key);
        ordered.push(node);
      }
      if (ordered.length !== projects.length + globals.length) return;
      mockProjectTree.splice(0, mockProjectTree.length, ...ordered);
    },
    async CreateTopic(_scope: string, _workspaceRoot: string, title: string) {
      const now = Date.now();
      const id = "topic_" + now;
      const topicTitle = title.trim() || t("mock.newSession");
      const parent = _scope === "global"
        ? ensureMockGlobalFolder()
        : mockProjectTree.find((node) => node.root === _workspaceRoot);
      if (parent) {
        const global = parent.kind === "global_folder";
        parent.children = [{
          key: parent.kind === "global_folder" ? "global_topic_" + id : "topic_" + id,
          kind: global ? "global_topic" : "topic",
          label: topicTitle,
          root: parent.root,
          topicId: id,
          projectColor: parent.projectColor,
          createdAt: now,
        }, ...projectChildren(parent)];
      }
      return { id, title: topicTitle, createdAt: now };
    },
    async RenameTopic(topicID: string, title: string) {
      const topic = findMockTopic(topicID);
      const nextTitle = title.trim();
      if (!topic || !nextTitle) return;
      const activePrefix = topic.label?.startsWith("● ") ? "● " : "";
      topic.label = `${activePrefix}${nextTitle}`;
      mockTabs = mockTabs.map((tab) =>
        tab.topicId === topicID ? { ...tab, topicTitle: nextTitle } : tab,
      );
    },
    async DeleteTopic(topicID: string) {
      deleteMockTopic(topicID);
    },
    async TrashTopic(topicID: string) {
      deleteMockTopic(topicID);
    },
    async SetTopicPinned(topicID: string, pinned: boolean) {
      setMockTopicPinned(topicID, pinned);
    },
    async SaveWindowState(_state) {
      // no-op in browser dev — no real window geometry to persist
    },
    async ContextPanel(_tabID: string) {
      const now = Date.now();
      const currency = "¥";
      const cost = (usd: number) => currency === "¥" ? Number((usd * 7.15).toFixed(4)) : usd;
      return {
        usedTokens: 42124,
        windowTokens: 128000,
        promptTokens: 22134,
        completionTokens: 12345,
        totalTokens: 34479,
        reasoningTokens: 7521,
        cacheHitTokens: 87000,
        cacheMissTokens: 13000,
        sessionCacheHitTokens: 87000,
        sessionCacheMissTokens: 13000,
        sessionCompletionTokens: 12345,
        requestCount: 10,
        elapsedMs: 33 * 60 * 1000,
        sessionCost: cost(0.018),
        sessionCurrency: currency,
        sessionCostUsd: cost(0.018),
        sources: {
          executor: {
            promptTokens: 24100,
            completionTokens: 8300,
            totalTokens: 32400,
            reasoningTokens: 5200,
            cacheHitTokens: 76000,
            cacheMissTokens: 9000,
            requestCount: 4,
            sessionCost: cost(0.0124),
            sessionCurrency: currency,
            sessionCostUsd: cost(0.0124),
          },
          planner: {
            promptTokens: 1800,
            completionTokens: 600,
            totalTokens: 2400,
            reasoningTokens: 420,
            cacheHitTokens: 3400,
            cacheMissTokens: 700,
            requestCount: 1,
            sessionCost: cost(0.0011),
            sessionCurrency: currency,
            sessionCostUsd: cost(0.0011),
          },
          subagent: {
            promptTokens: 4200,
            completionTokens: 2100,
            totalTokens: 6300,
            reasoningTokens: 1500,
            cacheHitTokens: 6100,
            cacheMissTokens: 2100,
            requestCount: 2,
            sessionCost: cost(0.0032),
            sessionCurrency: currency,
            sessionCostUsd: cost(0.0032),
          },
          compaction: {
            promptTokens: 2600,
            completionTokens: 700,
            totalTokens: 3300,
            reasoningTokens: 260,
            cacheHitTokens: 1100,
            cacheMissTokens: 900,
            requestCount: 1,
            sessionCost: cost(0.0009),
            sessionCurrency: currency,
            sessionCostUsd: cost(0.0009),
          },
          classifier: {
            promptTokens: 900,
            completionTokens: 120,
            totalTokens: 1020,
            reasoningTokens: 70,
            cacheHitTokens: 300,
            cacheMissTokens: 250,
            requestCount: 1,
            sessionCost: cost(0.0003),
            sessionCurrency: currency,
            sessionCostUsd: cost(0.0003),
          },
          title: {
            promptTokens: 420,
            completionTokens: 80,
            totalTokens: 500,
            reasoningTokens: 20,
            cacheHitTokens: 100,
            cacheMissTokens: 50,
            requestCount: 1,
            sessionCost: cost(0.0001),
            sessionCurrency: currency,
            sessionCostUsd: cost(0.0001),
          },
        },
        mock: true,
        readFiles: [
          { path: "README.md", turn: 2, time: now - 34 * 60 * 1000 },
          { path: "go.mod", turn: 3, time: now - 30 * 60 * 1000 },
          { path: "desktop/file.go", turn: 5, time: now - 13 * 60 * 1000, offset: 0, limit: 180 },
          { path: "internal/event.go", turn: 6, time: now - 4 * 60 * 1000, offset: 120, limit: 80, truncated: true },
        ],
        changedFiles: [
          { path: t("mock.changedFile1Path"), sources: ["session"], gitStatus: "modified", turns: [5, 6], latestPrompt: t("mock.changedFile1Prompt"), latestTime: now - 2 * 60 * 1000 },
          { path: t("mock.changedFile2Path"), sources: ["session"], gitStatus: "added", turns: [6], latestPrompt: t("mock.changedFile2Prompt"), latestTime: now - 60 * 1000 },
        ],
      };
    },

    // ── Remote (SSH) mock ──
    async RemoteHosts() {
      return mockRemoteHosts.slice();
    },
    async AddRemoteHost(input) {
      const view = mockRemoteHostView(input.label, input);
      mockRemoteHosts = [...mockRemoteHosts.filter((h) => h.id !== view.id), view];
      return view;
    },
    async UpdateRemoteHost(id, input) {
      const previous = mockRemoteHosts.find((h) => h.id === id);
      const view = mockRemoteHostView(id, input, previous);
      mockRemoteHosts = mockRemoteHosts.map((h) => (h.id === id ? view : h));
      return view;
    },
    async RemoveRemoteHost(id) {
      mockRemoteHosts = mockRemoteHosts.filter((h) => h.id !== id);
      delete mockRemoteConn[id];
    },
    async ScanSSHConfig() {
      return [
        { label: "gpu-box", host: "gpu-box", port: 0, user: "", identityFile: "", proxyJump: "", defaultWorkspace: "", serveInstall: "auto", useSSHConfig: true, preserveExistingSettings: true },
      ];
    },
    async ConnectRemoteHost(id) {
      mockRemoteConn[id] = "connecting";
      __emitMockRemote("status", { hostId: id, state: "connecting" });
      setTimeout(() => {
        mockRemoteConn[id] = "connected";
        __emitMockRemote("status", { hostId: id, state: "connected" });
      }, 300);
    },
    async DisconnectRemoteHost(id) {
      mockRemoteConn[id] = "stopped";
      __emitMockRemote("status", { hostId: id, state: "stopped" });
    },
    async RemoteConnectionStatuses() {
      return Object.entries(mockRemoteConn).map(([hostId, state]) => ({ hostId, state: state as RemoteConnectionStatus["state"] }));
    },
    async ConfirmRemoteHostKey(hostId, accept) {
      mockRemoteConn[hostId] = accept ? "connected" : "stopped";
      __emitMockRemote("status", { hostId, state: mockRemoteConn[hostId] });
    },
    async ConfirmRemoteSecret(hostId, _promptId, _secret, accept) {
      mockRemoteConn[hostId] = accept ? "connected" : "stopped";
      __emitMockRemote("status", { hostId, state: mockRemoteConn[hostId] });
    },
    async ListRemoteDir(_hostId, path) {
      const base = path.replace(/\/$/, "");
      return [
        { name: "src", path: `${base}/src`, isDir: true, size: 0, mtimeUnix: 1_700_000_000, symlink: false },
        { name: "README.md", path: `${base}/README.md`, isDir: false, size: 1024, mtimeUnix: 1_700_000_500, symlink: false },
      ];
    },
    async ReadRemoteFile(_hostId, path) {
      return { path, body: `# Mock remote file\n${path}\n`, size: 40, mtimeUnix: 1_700_000_500, truncated: false, binary: false };
    },
    async WriteRemoteFile(_hostId, _path, _body, _expectMtimeUnix) {
      return { ok: true, conflict: false, newMtimeUnix: 1_700_000_900 };
    },
    async MkdirRemote() {},
    async RenameRemotePath() {},
    async DeleteRemotePath() {},
    async RemoteForwards(hostId) {
      return mockRemoteForwards[hostId] ?? [];
    },
    async AddRemoteForward(hostId, input) {
      const view: RemoteForwardView = { id: `L:${input.localPort}`, hostId, ...input, state: "active" };
      mockRemoteForwards[hostId] = [...(mockRemoteForwards[hostId] ?? []), view];
      __emitMockRemote("forwards", { hostId, forwards: mockRemoteForwards[hostId] });
      return view;
    },
    async RemoveRemoteForward(hostId, forwardId) {
      mockRemoteForwards[hostId] = (mockRemoteForwards[hostId] ?? []).filter((f) => f.id !== forwardId);
      __emitMockRemote("forwards", { hostId, forwards: mockRemoteForwards[hostId] });
    },
    async EnsureRemoteServer(hostId, workspace) {
      for (const state of ["starting", "detect", "launch", "ready"] as const) {
        __emitMockRemote("server", { hostId, workspace, state, localUrl: state === "ready" ? "http://127.0.0.1:44321/" : "" });
      }
    },
    async OpenRemoteWorkspace() {},
    async StopRemoteServer(hostId) {
      __emitMockRemote("server", { hostId, workspace: "", state: "stopped" });
    },
    async RemoteServerStatus(hostId) {
      return { hostId, workspace: "~/app", state: "stopped" };
    },
    async RemoteServerLogs() {
      return "mock serve log line 1\nmock serve log line 2\n";
    },
    async RemoteLastWorkspace() {
      return "~/app";
    },
  };
}

// Mock remote state, module-scoped so it survives across mock method calls.
function mockRemoteHostView(id: string, input: RemoteHostInput, previous?: RemoteHostView): RemoteHostView {
  return {
    id,
    label: input.label,
    host: input.host,
    port: input.port,
    user: input.user,
    identityFile: input.identityFile,
    proxyJump: input.proxyJump,
    defaultWorkspace: input.defaultWorkspace,
    serveInstall: input.serveInstall,
    useSSHConfig: input.useSSHConfig,
    passwordSet: input.password ? true : input.clearPassword ? false : previous?.passwordSet,
    keyPassphraseSet: input.keyPassphrase ? true : input.clearPassphrase ? false : previous?.keyPassphraseSet,
  };
}

let mockRemoteHosts: RemoteHostView[] = [
  { id: "demo", label: "demo", host: "192.168.1.10", port: 22, user: "dev", identityFile: "", proxyJump: "", defaultWorkspace: "~/app", serveInstall: "auto", useSSHConfig: false },
];
const mockRemoteConn: Record<string, RemoteConnectionStatus["state"]> = {};
const mockRemoteForwards: Record<string, RemoteForwardView[]> = {};
