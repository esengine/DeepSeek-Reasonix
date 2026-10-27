// A plugin package as the settings pane and the install flow read it: what
// one contributes, and the plan that describes letting it in.
// One thing a package contributes. invocation is how the user reaches it,
// already namespaced by the package — empty where there is no way to call it
// by name (a prompt template, a theme).
export interface PluginItem {
  name: string;
  description?: string;
  invocation?: string;
}

export interface PluginHook {
  event: string;
  match?: string;
  command?: string;
  contextFile?: string;
  description?: string;
}

export interface PluginServer {
  name: string;
  displayName?: string;
  description?: string;
  transport?: string;
  command?: string;
  url?: string;
  autoStart?: boolean;
}

// A runtime process runs inside Reasonix with the user's full trust: it reads
// the session, and what it intercepts or replaces it decides for itself.
export interface PluginRuntime {
  command: string;
  args?: string[];
  intercepts?: string[];
  replaces?: string[];
  capabilities?: string[];
  // Tools the process serves to the agent, by the name its manifest gives.
  tools?: string[];
}

// What the install preview shows about a Claude Code mod's hooks module, read
// from its syntax tree. A listing, not a guarantee.
export interface ModPreviewEvent {
  name: string;
  // Not a literal: the name cannot be listed.
  dynamic?: boolean;
}

export interface ModPreviewCall {
  namespace: string;
  method?: string;
  dynamic?: boolean;
}

// Code the module pulls in that the listing cannot see.
export interface ModPreviewImport {
  spec: string;
  kind: "static" | "require" | "dynamic";
}

// eval, new Function, a computed member of the API: parts of the module are unlistable.
export interface ModPreviewFinding {
  kind: string;
}

export interface ModPreview {
  plugin: string;
  // The module's path inside the package, and the hash of the bytes previewed.
  module: string;
  sha256: string;
  // False when the module could not be read (typescript | too_large | unparseable | timeout |
  // unsupported_module): say "not previewed", never "clean".
  listed: boolean;
  reason?: string;
  events: ModPreviewEvent[];
  api: ModPreviewCall[];
  imports: ModPreviewImport[];
  findings: ModPreviewFinding[];
  // What the module takes from the session or queues into it, by literal call:
  // "messages" and "usage" read it, "submit" queues a prompt.
  session: string[];
  // Literal events this host never raises.
  unserved: string[];
}

export interface PluginSkipped {
  capability: string;
  path?: string;
  reason: string;
}

// One installed package. The first five lists are contributions — things the
// package adds to a menu. hooks, mcpServers and runtime are code it brought
// with it, and the difference is the whole reason they are separate fields.
export interface PluginPackage {
  name: string;
  version?: string;
  description?: string;
  source?: string;
  root: string;
  manifestKind?: string;
  enabled: boolean;
  status?: string;
  statusReason?: string;
  compatibility?: string;
  skipped?: PluginSkipped[];
  skills?: PluginItem[];
  agents?: PluginItem[];
  commands?: PluginItem[];
  prompts?: PluginItem[];
  themes?: PluginItem[];
  hooks?: PluginHook[];
  mcpServers?: PluginServer[];
  runtime?: PluginRuntime;
  warnings?: string[];
  error?: string;
}

// A frame document a package ships, as the plan lists it before anything is written.
export interface PluginFrame {
  id: string;
  title: string;
  entry: string;
  bytes: number;
  sha256?: string;
  error?: string;
}

// One step of an install plan. riskLevel is the kernel's own grading — the
// confirmation page groups by it rather than deciding for itself what is
// dangerous, so the two can never disagree.
export interface PluginAction {
  kind: string;
  action: string;
  status: string;
  riskLevel: string;
  riskReasons?: string[];
  name?: string;
  source?: string;
  commit?: string;
  target?: string;
  version?: string;
  manifestKind?: string;
  compatibility?: string;
  skippedCapabilities?: PluginSkipped[];
  skills?: string[];
  skillCount?: number;
  agents?: string[];
  agentCount?: number;
  commands?: string[];
  commandCount?: number;
  hookCount?: number;
  promptCount?: number;
  themeCount?: number;
  toolCount?: number;
  transport?: string;
  url?: string;
  command?: string;
  args?: string[];
  env?: Record<string, string>;
  headers?: Record<string, string>;
  runtime?: PluginRuntime & { fullTrust: boolean };
  mod?: ModPreview;
  frames?: PluginFrame[];
  warnings?: string[];
  error?: string;
  next?: string;
  errorCode?: string;
  // The kernel shortened a field or list of this step; what is shown is not all of it.
  previewTruncated?: boolean;
}

// The same object answers a preview and an apply; status says which happened —
// planned | done | partial | failed | blocked | denied.
export interface PluginPlan {
  ok: boolean;
  status: string;
  applied: boolean;
  source?: string;
  planId?: string;
  actions?: PluginAction[];
  warnings?: string[];
  error?: string;
  next?: string;
  previewTruncated?: boolean;
  // Steps the plan holds beyond the ones listed; none of them is high risk.
  hiddenActions?: number;
  // The plan's real tally per kind, whatever the list shows.
  kinds?: { skill: number; mcp: number; plugin: number };
  // The package landed but the running session could not be rebuilt — almost
  // always because a turn is in flight. Not a failed install.
  reloadError?: string;
}

// What an install asks for. Updating an installed package is the same request
// with its recorded source and replace set — not a third verb — so what the
// user confirms is a plan in the same shape either way.
export interface PluginInstallRequest {
  source: string;
  name?: string;
  replace?: boolean;
  // Present only on an apply: the plan the user just read.
  planId?: string;
}

// required names the environment variables the archive no longer carries: an
// export strips every literal value out of a package's MCP and runtime config,
// so whoever installs it supplies these. savedTo is where the shell put the
// file; empty in a browser tab, which hands it to the download list instead.
export interface PluginExport {
  required: string[];
  savedTo?: string;
}
