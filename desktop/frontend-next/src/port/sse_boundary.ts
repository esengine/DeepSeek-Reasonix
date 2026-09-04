import { SseShell } from "./sse_shell";
import type { Adjudications, ConfigProblem, ConfigRepair, PermissionLists, PermissionRules, SandboxSettings } from "./port";

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
  sandbox() {
    return this.get<SandboxSettings>("/sandbox");
  }
  saveSandbox(s: SandboxSettings) {
    return this.post0<SandboxSettings>("/sandbox", s);
  }
  configProblem() {
    return this.get<ConfigProblem | null>("/config/problem");
  }
  repairConfig() {
    return this.post0<ConfigRepair>("/config/repair");
  }
}
