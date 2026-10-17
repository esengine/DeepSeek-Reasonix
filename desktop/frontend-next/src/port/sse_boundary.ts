import { SseShell } from "./sse_shell";
import type { AgentComposition, AgentPresetVersion, AgentPresetCatalog, CompositionSummary } from "./composition";
import type { Adjudications, BrowserToolsSettings, SubagentClaimsSettings, WriteLeaseSettings, PlanApprovalSettings, PeerReceipt, PeersState, ConfigProblem, ConfigRepair, PermissionLists, PermissionRules, SandboxSettings, PeersSettings, PeersSettingsEdit, PeerRemember } from "./port";
import type { AskTimeoutSettings, AutoArchiveSettings, ChangedFilesSettings, FoldWorkSettings, TurnNarrationSettings, DisplayCurrencyMode, DisplayCurrencySettings, ProgressWatchSettings, WorkspaceRoots, ReceiptSettings, PersonaDraft, PersonaSettings, WebAccessPatch, WebAccessSettings } from "./boundary";

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
  selectAgentComposition(id: string) { return this.post0<CompositionSummary>("/agent-composition", { id }); }
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
  writeLease() {
    return this.get<WriteLeaseSettings>("/write-lease");
  }
  saveWriteLease(optimistic: boolean) {
    return this.post0<WriteLeaseSettings>("/write-lease", { optimistic });
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
  saveSubagentClaims(optimistic: boolean) {
    return this.post0<SubagentClaimsSettings>("/subagent-claims", { optimistic });
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
  webAccessSettings() {
    return this.get<WebAccessSettings>("/web-access-settings");
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
