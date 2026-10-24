import { SseShell } from "./sse_shell";
import type { PresentFilesSettings } from "./boundary";
import type { AgentComposition, AgentPresetVersion, AgentPresetCatalog, CompositionSummary } from "./composition";
import type { ScheduleBoard, ScheduleDraft, SchedulePreview, ScheduleResultView, ScheduleView } from "./schedule";
import type { PersonaDraft, PersonaSettings } from "./boundary";
import type { Adjudications, BrowserToolsSettings, SubagentClaimsSettings, SubagentSteerSettings, WriteLeaseSettings, PlanApprovalSettings, DefaultModeSettings, ToolSettings, PeerReceipt, PeersState, PeersSettings, PeersSettingsEdit, PeerRemember, ConfigProblem, ConfigRepair, PermissionLists, PermissionRules, SandboxSettings } from "./port";
import type { AskTimeoutSettings, ModStatus, ModsSettings, ModsSettingsEdit, SessionRefSettings, AutoArchiveSettings, ChangedFilesSettings, FeedbackLevelNotices, FoldWorkSettings, TurnNarrationSettings, ObservabilityPatch, ObservabilitySettings, WebAccessPatch, WebAccessSettings, SearxngProbe, DisplayCurrencyMode, DisplayCurrencySettings, ProgressWatchSettings, WorkspaceRoots, ReceiptSettings } from "./boundary";

// Where the agent may reach: the permission rules a call is matched against and
// the sandbox the shell runs in.
export class SseBoundary extends SseShell {
  agentPresets() { return this.get<AgentPresetCatalog>("/agent-presets"); }
  saveAgentPreset(definition: AgentComposition, expectedRevision: string) { return this.post0<AgentPresetVersion>("/agent-presets", { definition, expectedRevision }); }
  deleteAgentPreset(id: string, revision: string) { return this.del(`/agent-presets/${encodeURIComponent(id)}?revision=${encodeURIComponent(revision)}`); }
  setDefaultAgentPreset(id: string, workspace: boolean) { return this.post0<void>("/agent-presets/default", { id, workspace }); }
  exportAgentPreset(id: string) { return this.get<AgentComposition>(`/agent-presets/${encodeURIComponent(id)}/export`); }
  importAgentPreset(content: AgentComposition) { return this.post0<{definition:AgentComposition;diagnostics:AgentPresetCatalog["diagnostics"]}>("/agent-presets/import", { content }); }
  agentComposition() { return this.get<CompositionSummary | null>("/agent-composition"); }
  selectAgentComposition(id: string) { return this.post0<CompositionSummary | null>("/agent-composition", { id }); }
  adjudications() {
    return this.get<Adjudications>("/adjudications");
  }
  permissions() {
    return this.get<PermissionRules>("/permissions");
  }
  savePermissions(lists: PermissionLists) {
    return this.post0<PermissionRules>("/permissions", lists);
  }
  revokeSessionGrant(rule: string) {
    return this.post0<PermissionRules>("/permissions/revoke", { rule });
  }
  revokeRememberedProjectRule(rule: string) {
    return this.post0<PermissionRules>("/permissions/remembered/revoke", { rule });
  }
  sandbox() {
    return this.get<SandboxSettings>("/sandbox");
  }
  saveSandbox(s: SandboxSettings) {
    return this.post0<SandboxSettings>("/sandbox", s);
  }
  workspaceRoots() {
    return this.get<WorkspaceRoots>("/workspace/roots");
  }
  addWorkspaceRoot(path: string) {
    return this.post0<WorkspaceRoots>("/workspace/roots", { path });
  }
  removeWorkspaceRoot(path: string) {
    return this.post0<WorkspaceRoots>("/workspace/roots/remove", { path });
  }
  browserTools() {
    return this.get<BrowserToolsSettings>("/browser-tools");
  }
  presentFiles() {
    return this.get<PresentFilesSettings>("/present-files");
  }
  savePresentFiles(enabled: boolean) {
    return this.post0<PresentFilesSettings>("/present-files", { enabled });
  }
  saveBrowserTools(enabled: boolean) {
    return this.post0<BrowserToolsSettings>("/browser-tools", { enabled });
  }
  subagentClaims() {
    return this.get<SubagentClaimsSettings>("/subagent-claims");
  }
  planApproval() {
    return this.get<PlanApprovalSettings>("/plan-approval");
  }
  savePlanApproval(auto: boolean) {
    return this.post0<PlanApprovalSettings>("/plan-approval", { auto });
  }
  defaultMode() {
    return this.get<DefaultModeSettings>("/default-mode");
  }
  saveDefaultMode(plan: boolean) {
    return this.post0<DefaultModeSettings>("/default-mode", { plan });
  }
  toolSettings() {
    return this.get<ToolSettings>("/tools");
  }
  saveToolDisabled(name: string, disabled: boolean) {
    return this.post0<ToolSettings>("/tools/disabled", { name, disabled });
  }
  writeLease() {
    return this.get<WriteLeaseSettings>("/write-lease");
  }
  saveWriteLease(edit: { optimistic?: boolean; maxWaitMinutes?: number }) {
    return this.post0<WriteLeaseSettings>("/write-lease", edit);
  }
  schedules() {
    return this.get<ScheduleBoard>("/schedules");
  }
  previewSchedule(draft: ScheduleDraft) {
    return this.post0<SchedulePreview>("/schedules/preview", draft);
  }
  createSchedule(draft: ScheduleDraft, confirm: string) {
    return this.post0<ScheduleView>("/schedules", { ...draft, confirm });
  }
  scheduleAction(id: string, verb: "pause" | "resume" | "renew" | "delete") {
    return this.post0<ScheduleBoard>(`/schedules/${encodeURIComponent(id)}/${verb}`, {});
  }
  setSchedulesPaused(paused: boolean) {
    return this.post0<ScheduleBoard>(paused ? "/schedules/pause-all" : "/schedules/resume-all", {});
  }
  scheduleResult(triggerId: string) {
    return this.get<ScheduleResultView>(`/schedules/result?trigger=${encodeURIComponent(triggerId)}`);
  }
  peers() {
    return this.get<PeersState>("/peers");
  }
  sendPeerMessage(to: string, text: string) {
    return this.post0<PeerReceipt>("/peers/send", { to, text });
  }
  resolvePeerMessage(id: string, approve: boolean, remember: PeerRemember = "none") {
    return this.post(`/peers/held/${encodeURIComponent(id)}`, { approve, remember });
  }
  peersSettings() {
    return this.get<PeersSettings>("/peers/settings");
  }
  savePeersSettings(edit: PeersSettingsEdit) {
    return this.post0<PeersSettings>("/peers/settings", edit);
  }
  async mods() {
    return (await this.get<{ mods: ModStatus[] }>("/mods")).mods ?? [];
  }
  modsSettings() {
    return this.get<ModsSettings>("/mods/settings");
  }
  saveModsSettings(edit: ModsSettingsEdit) {
    return this.post0<ModsSettings>("/mods/settings", edit);
  }
  saveSubagentClaims(optimistic: boolean) {
    return this.post0<SubagentClaimsSettings>("/subagent-claims", { optimistic });
  }
  subagentSteerSettings() {
    return this.get<SubagentSteerSettings>("/subagent-steer-limits");
  }
  saveSubagentSteerSettings(maxBytes: number, perMinute: number) {
    return this.post0<SubagentSteerSettings>("/subagent-steer-limits", { maxBytes, perMinute });
  }
  displayCurrency() {
    return this.get<DisplayCurrencySettings>("/display-currency");
  }
  saveDisplayCurrency(mode: DisplayCurrencyMode) {
    return this.post0<DisplayCurrencySettings>("/display-currency", { mode });
  }
  changedFilesSettings() {
    return this.get<ChangedFilesSettings>("/changed-files-settings");
  }
  saveChangedFilesSettings(show: boolean) {
    return this.post0<ChangedFilesSettings>("/changed-files-settings", { show });
  }
  feedbackLevelNotices() {
    return this.get<FeedbackLevelNotices>("/feedback-level-notices");
  }
  saveFeedbackLevelNotices(enabled: boolean) {
    return this.post0<FeedbackLevelNotices>("/feedback-level-notices", { enabled });
  }
  foldWorkSettings() {
    return this.get<FoldWorkSettings>("/fold-work-settings");
  }
  saveFoldWorkSettings(fold: boolean) {
    return this.post0<FoldWorkSettings>("/fold-work-settings", { fold });
  }
  turnNarrationSettings() {
    return this.get<TurnNarrationSettings>("/turn-narration-settings");
  }
  saveTurnNarrationSettings(level: TurnNarrationSettings["level"]) {
    return this.post0<TurnNarrationSettings>("/turn-narration-settings", { level });
  }
  observabilitySettings() {
    return this.get<ObservabilitySettings>("/observability");
  }
  saveObservabilitySettings(patch: ObservabilityPatch) {
    return this.post0<ObservabilitySettings>("/observability", patch);
  }
  webAccessSettings() {
    return this.get<WebAccessSettings>("/web-access-settings");
  }
  probeSearxng() {
    return this.post0<SearxngProbe>("/web-access-settings/probe", {});
  }
  saveWebAccessSettings(patch: WebAccessPatch) {
    return this.post0<WebAccessSettings>("/web-access-settings", patch);
  }
  progressWatch() {
    return this.get<ProgressWatchSettings>("/progress-watch");
  }
  saveProgressWatch(s: Pick<ProgressWatchSettings, "pause" | "rounds" | "tokenMultiple">) {
    return this.post0<ProgressWatchSettings>("/progress-watch", s);
  }
  autoArchive() {
    return this.get<AutoArchiveSettings>("/auto-archive");
  }
  saveAutoArchive(s: Pick<AutoArchiveSettings, "enabled" | "days">) {
    return this.post0<AutoArchiveSettings>("/auto-archive", s);
  }
  persona() {
    return this.get<PersonaSettings>("/persona");
  }
  savePersonaSettings(s: PersonaDraft) {
    return this.post0<PersonaSettings>("/persona", s);
  }
  askTimeout() {
    return this.get<AskTimeoutSettings>("/ask-timeout");
  }
  saveAskTimeout(seconds: number) {
    return this.post0<AskTimeoutSettings>("/ask-timeout", { seconds });
  }
  sessionRefSettings() {
    return this.get<SessionRefSettings>("/session-ref-settings");
  }
  saveSessionRefSettings(tokens: number) {
    return this.post0<SessionRefSettings>("/session-ref-settings", { tokens });
  }
  receiptSettings() {
    return this.get<ReceiptSettings>("/receipt-settings");
  }
  saveReceiptSettings(show: boolean) {
    return this.post0<ReceiptSettings>("/receipt-settings", { show });
  }
  configProblem() {
    return this.get<ConfigProblem | null>("/config/problem");
  }
  repairConfig() {
    return this.post0<ConfigRepair>("/config/repair");
  }
}
