import type { Adjudications, ConfigProblem, ConfigRepair, PermissionLists, PermissionRules, SandboxSettings } from "./port";
import { MockShell } from "./mock_shell";

// The boundary half of the fixture: what the agent is refused outright, and how
// far an approved write reaches. It carries one rule of each kind already, so
// the pane renders the three lists populated rather than as three empty boxes —
// an empty editor cannot show what a rule looks like.
// The kernel's platform resolution, so flipping the fixture's platform to
// "windows" shows the pane what that machine shows rather than what this file
// happens to store. Unset is not "off" anywhere but Windows.
function effectiveBash(s: SandboxSettings): string {
  if (s.platform === "windows") return "off";
  return s.bash.trim() === "off" ? "off" : "enforce";
}

export class MockBoundary extends MockShell {
  // One interrupted barrier and one that a later turn took over, so the card
  // and the reason it disappears are both reachable without a crash.
  private adjudicated: Adjudications = {
    schema_version: 1,
    active: [{
      barrier_id: "7", kind: "ask", state: "interrupted",
      question: "Should I replace the fixture with a generated one?",
      opened_at: "2026-09-04T09:12:00Z",
    }],
    history: [{
      barrier_id: "6", kind: "ask", state: "superseded",
      question: "Which side of the fold should I measure?",
      opened_at: "2026-09-04T08:55:00Z", settled_at: "2026-09-04T09:01:00Z",
      superseded_by: "turn-41",
    }],
  };

  async adjudications(): Promise<Adjudications> {
    return this.adjudicated;
  }

  private rules: PermissionRules = {
    mode: "ask",
    deny: ["bash(git push:*)", "file_mutation(*.env*)"],
    ask: ["bash(rm:*)"],
    allow: ["bash(go test:*)"],
    path: "/Users/you/.reasonix/config.toml",
  };

  private jail: SandboxSettings = {
    bash: "enforce",
    network: true,
    workspaceRoot: "",
    allowWrite: ["/tmp/scratch"],
    effectiveWriteRoots: ["/Users/you/code/site", "/tmp/scratch"],
    effectiveBash: "enforce",
    available: true,
    platform: "darwin",
    path: "/Users/you/.reasonix/config.toml",
  };

  async permissions(): Promise<PermissionRules> {
    return { ...this.rules };
  }

  // A rule the gate's own parser would reject is refused here the same way, so
  // the pane's error path is reachable from the fixture.
  async savePermissions(lists: PermissionLists): Promise<PermissionRules> {
    for (const rule of [...lists.deny, ...lists.ask, ...lists.allow]) {
      if (!/^[^()]+(\(.*\))?$/.test(rule.trim())) throw new Error(`invalid permission rule "${rule}"`);
    }
    this.rules = { ...this.rules, ...lists };
    return { ...this.rules };
  }

  async sandbox(): Promise<SandboxSettings> {
    return { ...this.jail };
  }

  async saveSandbox(s: SandboxSettings): Promise<SandboxSettings> {
    const roots = [s.workspaceRoot || "/Users/you/code/site", ...s.allowWrite.filter(Boolean)];
    this.jail = { ...this.jail, ...s, effectiveWriteRoots: roots, effectiveBash: effectiveBash(s) };
    return { ...this.jail };
  }

  // The fixture carries the broken file, because a banner nobody can reach is
  // a banner nobody designs. Repairing it here clears it, so the whole
  // interaction is reachable without a Go build.
  private broken: ConfigProblem | null = {
    path: "C:\\Users\\you\\AppData\\Roaming\\reasonix\\config.toml",
    line: 263,
    key: "plugins.command",
    excerpt: 'command = "C:\\Scripts\\mcp.exe"',
    repair: 'command = "C:\\\\Scripts\\\\mcp.exe"',
    recovered: "last-known-good",
  };

  async configProblem(): Promise<ConfigProblem | null> {
    return this.broken && { ...this.broken };
  }

  async repairConfig(): Promise<ConfigRepair> {
    const backup = (this.broken?.path ?? "") + ".broken-20260823-181500";
    this.broken = null;
    return { backup, problem: null };
  }
}
