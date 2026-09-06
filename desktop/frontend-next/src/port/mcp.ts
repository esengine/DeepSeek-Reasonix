// External services and the project they were switched on for. A scope is which
// project's answer a call is asking for; absent means the running one.
export interface McpEntry {
  name: string;
  state: string;
  enabled: boolean;
  transport?: string;
  source?: string;
  description?: string; // the server's own words, from the handshake; often absent
  tools: number;
  prompts?: number;
  resources?: number;
  toolList?: McpTool[];
  remembered?: boolean; // recovered from the last handshake, nothing is connected
  stale?: boolean; // and the declaration changed since that cache was written
  error?: string;
  // What the endpoint answered, when the failure was an HTTP one. A fact and
  // not a verdict: whether a 401 means the user has to authorise again depends
  // on whether the automatic recovery ran, which nothing here knows yet.
  httpStatus?: number;
  // This project decided the switch for itself instead of inheriting it.
  localOverride?: boolean;
}

// One tool as its own server describes it. error carries a schema this host
// rejected: listed but not callable, which is not the same as absent.
export interface McpTool {
  name: string;
  description?: string;
  readOnly?: boolean;
  destructive?: boolean;
  error?: string;
}

// Where a capability decision applies: this folder alone, or every project.
// The same two words for servers and for skills.
export type ScopeLayer = "project" | "global";

// GET /capability-scope, and carried on both listings. The shell holds several
// projects at once, so a pane that silently follows the active tab would show
// different content under an unchanged heading. Every listing says which folder
// it answered for, and trees says how many working trees share that answer —
// worktrees of one repository are one project, not several.
export interface CapabilityScope {
  root: string;
  name: string;
  // Set only when another project on offer shares this name: the shortest
  // trailing path that tells them apart.
  label?: string;
  key: string;
  repo: boolean;
  trees?: number;
  branch?: string;
  overrides: number;
  current?: boolean;
}

export interface McpCatalog {
  servers: McpEntry[];
  scope: CapabilityScope;
  // Absent for a project the session is not pointed at: only the folder being
  // driven knows whether its servers actually came up.
  live?: boolean;
}

// How far a newly installed server reaches. user is every project; local is
// this project only — installing is a global act either way, so local declares
// the server globally and switches it off everywhere but here; project writes
// the declaration into the repository, reaching everyone who clones it.
export type McpInstallScope = "user" | "local" | "project";

// One server a paste resolved to, before anything has been installed.
export interface McpDraftServer {
  name: string;
  transport: string;
  command?: string;
  args?: string[];
  url?: string;
  env?: Record<string, string>;
  headers?: Record<string, string>;
}

// What the confirmation card must show: shell is the command that will run,
// unknown-host the endpoint it will talk to, secret a credential written out in
// full. None of them block the install — they are what the user is agreeing to.
export interface McpRisk {
  server: string;
  kind: "secret" | "shell" | "unknown-host";
  field: string;
  detail: string;
}

export interface McpDraft {
  servers: McpDraftServer[];
  risks: McpRisk[];
}

// state is ready | action_required | issue. action_required means the config was
// kept because finishing OAuth is impossible once the entry is gone.
export interface McpInstallResult {
  name: string;
  state: string;
  toolCount: number;
  action: string;
  message: string;
}
