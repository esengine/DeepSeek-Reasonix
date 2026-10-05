// Commands the user hung on the agent's own events. They run on this machine
// with the user's rights, so what they are is worth reading before saving one.
// One configured hook rule. blocking/usesMatch are the kernel's answer about
// the event, not the client's guess: whether exit 2 stops the agent is a
// property of the event, and issues are what Inspect already found wrong.
export interface HookEntry {
  event: string;
  match?: string;
  command: string;
  description?: string;
  timeout?: number;
  cwd?: string;
  scope: string;
  source?: string;
  blocking?: boolean;
  usesMatch?: boolean;
  readOnly?: boolean;
  issues?: string[];
}

export interface HookSource {
  scope: string;
  path: string;
  status: string;
  hookCount: number;
  parseError?: string;
}

export interface HookEventInfo {
  name: string;
  blocking: boolean;
  usesMatch: boolean;
}

// projectPath is empty when no project is open — project-scoped rules have
// nowhere to live, and the UI must not offer that scope.
export interface HookCatalog {
  hooks: HookEntry[];
  sources: HookSource[];
  events: HookEventInfo[];
  projectPath: string;
  globalPath: string;
}

// A real execution against a sample payload. blocks is the consequence on this
// event specifically — the same exit code stops one event and warns on another.
export interface HookDryRun {
  decision: string;
  exitCode: number;
  stdout?: string;
  stderr?: string;
  timedOut?: boolean;
  durationMs: number;
  blocks: boolean;
}
