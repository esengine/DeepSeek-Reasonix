import { MockMcp } from "./mock_mcp";
import { HttpError, type HookCatalog, type HookDryRun, type HookEntry } from "./port";
import type { SubagentRun, SubagentTranscript } from "./subagent";

// Hooks, and what a dry run of one reports back.
export class MockHook extends MockMcp {
  private hookList: HookEntry[] = [
    { event: "PostToolUse", match: "edit_file|write_file", command: "gofmt -w .", scope: "global", usesMatch: true },
    { event: "PreToolUse", match: "bash", command: "./scripts/guard.sh", scope: "global", blocking: true, usesMatch: true },
  ];

  async stopToolCall(_callId: string): Promise<void> {
    throw new HttpError(409, "no running tool call with this id", { code: "tool_call.not_running" });
  }

  async subagents(): Promise<SubagentRun[]> {
    return [];
  }
  async fleetRecoveryPlans() { return []; }
  async requestFleetRecovery() { throw new Error("Recovery is unavailable in this preview."); }

  async steerSubagent(_ref: string, _text: string): Promise<void> {
    throw new HttpError(404, "no sub-agent run answers to that reference", { code: "subagent.not_found" });
  }

  async subagentTranscript(_ref: string): Promise<SubagentTranscript> {
    throw new HttpError(404, "no sub-agent run answers to that reference", { code: "subagent.not_found" });
  }

  async hooks(): Promise<HookCatalog> {
    return {
      hooks: this.hookList.map((h) => ({ ...h })),
      sources: [{ scope: "global", path: "~/.reasonix/settings.json", status: "ok", hookCount: this.hookList.length }],
      events: [
        { name: "PreToolUse", blocking: true, usesMatch: true },
        { name: "PostToolUse", blocking: false, usesMatch: true },
        { name: "PostToolUseFailure", blocking: false, usesMatch: true },
        { name: "PermissionRequest", blocking: false, usesMatch: true },
        { name: "UserPromptSubmit", blocking: true, usesMatch: false },
        { name: "Stop", blocking: false, usesMatch: false },
        { name: "SessionStart", blocking: false, usesMatch: false },
        { name: "Notification", blocking: false, usesMatch: false },
      ],
      projectPath: "~/projects/DeepSeek-Reasonix/.reasonix/settings.json",
      globalPath: "~/.reasonix/settings.json",
    };
  }

  async saveHooks(scope: "user" | "project", hooks: HookEntry[]) {
    this.hookList = hooks.map((h) => ({ ...h, scope: scope === "user" ? "global" : "project" }));
  }

  // Stands in for a real execution: a command mentioning "guard" exits 2 so the
  // blocking verdict is reachable in dev.

  // Stands in for a real execution: a command mentioning "guard" exits 2 so the
  // blocking verdict is reachable in dev.
  async dryRunHook(h: HookEntry): Promise<HookDryRun> {
    const bad = /guard|exit 2/.test(h.command);
    return {
      decision: bad ? "block" : "pass",
      exitCode: bad ? 2 : 0,
      stdout: bad ? "" : "ok",
      stderr: bad ? "refusing: .env is protected" : "",
      durationMs: 120,
      blocks: bad && !!h.blocking,
    };
  }
}
