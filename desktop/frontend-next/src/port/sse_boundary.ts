import { SseShell } from "./sse_shell";
import type { Adjudications, BrowserToolsSettings, SubagentClaimsSettings, WriteLeaseSettings, PlanApprovalSettings, PeerReceipt, PeersState, ConfigProblem, ConfigRepair, PermissionLists, PermissionRules, SandboxSettings } from "./port";
import type { AskTimeoutSettings, AutoArchiveSettings, ChangedFilesSettings, FoldWorkSettings, TurnNarrationSettings, DisplayCurrencyMode, DisplayCurrencySettings, ProgressWatchSettings, WorkspaceRoots, ReceiptSettings, PersonaDraft, PersonaSettings } from "./boundary";

// Where the agent may reach: the permission rules a call is matched against and
// the sandbox the shell runs in.
export class SseBoundary extends SseShell {
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
  resolvePeerMessage(id: string, approve: boolean) {
    return this.post(`/peers/held/${encodeURIComponent(id)}`, { approve });
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
