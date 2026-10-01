import { SseShell } from "./sse_shell";
import type { Adjudications, BrowserToolsSettings, ConfigProblem, ConfigRepair, PermissionLists, PermissionRules, SandboxSettings } from "./port";
import type { AutoArchiveSettings, ProgressWatchSettings } from "./boundary";

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
  browserTools() {
    return this.get<BrowserToolsSettings>("/browser-tools");
  }
  saveBrowserTools(enabled: boolean) {
    return this.post0<BrowserToolsSettings>("/browser-tools", { enabled });
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
  configProblem() {
    return this.get<ConfigProblem | null>("/config/problem");
  }
  repairConfig() {
    return this.post0<ConfigRepair>("/config/repair");
  }
}
