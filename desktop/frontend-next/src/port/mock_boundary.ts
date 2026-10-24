import type { Adjudications, BrowserToolsSettings, SubagentClaimsSettings, SubagentSteerSettings, WriteLeaseSettings, PlanApprovalSettings, DefaultModeSettings, ToolSettings, PeerReceipt, PeersState, PeersSettings, PeersSettingsEdit, PeerRemember, ConfigProblem, ConfigRepair, PermissionLists, PermissionRules, SandboxSettings, UpdateMode } from "./port";
import type { AskTimeoutSettings, ModStatus, ModsSettings, ModsSettingsEdit, SessionRefSettings, AutoArchiveSettings, ChangedFilesSettings, FeedbackLevelNotices, FoldWorkSettings, TurnNarrationSettings, ObservabilityPatch, ObservabilitySettings, WebAccessPatch, WebAccessSettings, SearxngProbe, DisplayCurrencyMode, DisplayCurrencySettings, ProgressWatchSettings, WorkspaceRoots, ReceiptSettings } from "./boundary";
import { mockMods } from "./mock_mods";
import type { ScheduleBoard, ScheduleDraft, SchedulePreview, ScheduleResultView, ScheduleView } from "./schedule";
import { HttpError } from "./http_error";
import { MockShell } from "./mock_shell";
import { MockCompositions } from "./mock_composition";
import type { AgentComposition } from "./composition";
import type { PersonaDraft, PersonaSettings } from "./boundary";

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

// One file per machine, so every port of the demo reads the same answer.
let levelNotices: FeedbackLevelNotices = { enabled: false, path: "/Users/you/.reasonix/config.toml" };

export class MockBoundary extends MockShell {
  protected compositions = new MockCompositions();
  shareAgentPresetsFrom(other: MockBoundary, workspace:string) { this.compositions=other.compositions.fork(workspace); }
  agentPresets() { return this.compositions.agentPresets(); }
  saveAgentPreset(d: AgentComposition, expected: string) { return this.compositions.saveAgentPreset(d, expected); }
  deleteAgentPreset(id: string, revision: string) { return this.compositions.deleteAgentPreset(id, revision); }
  setDefaultAgentPreset(id: string, workspace: boolean) { return this.compositions.setDefaultAgentPreset(id, workspace); }
  exportAgentPreset(id: string) { return this.compositions.exportAgentPreset(id); }
  importAgentPreset(d: AgentComposition) { return this.compositions.importAgentPreset(d); }
  agentComposition() { return this.compositions.agentComposition(); }
  selectAgentComposition(id: string) { return this.compositions.selectAgentComposition(id); }
  async decideWorkspaceTrust(_trust: "trusted" | "declined") {}
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
    protectChangedFiles: true,
    effectiveWriteRoots: ["/Users/you/code/site", "/tmp/scratch"],
    effectiveBash: "enforce",
    available: true,
    platform: "darwin",
    path: "/Users/you/.reasonix/config.toml",
  };

  private browser: BrowserToolsSettings = { enabled: true, effective: true, path: "/Users/you/.reasonix/config.toml" };

  async permissions(): Promise<PermissionRules> {
    return { ...this.rules };
  }

  // A rule the gate's own parser would reject is refused here the same way, so
  // the pane's error path is reachable from the fixture.
  async savePermissions(lists: PermissionLists): Promise<PermissionRules> {
    for (const rule of [...lists.deny, ...lists.ask, ...lists.allow]) {
      if (!/^[^()]+(\(.*\))?$/.test(rule.trim())) throw new Error(`invalid permission rule "${rule}"`);
    }
    // An absent switch is left as it is, the way the kernel treats a save without it.
    const { askOnExternalOrigin, ...rest } = lists;
    this.rules = { ...this.rules, ...rest, askOnExternalOrigin: askOnExternalOrigin ?? this.rules.askOnExternalOrigin ?? false };
    return { ...this.rules };
  }

  async revokeSessionGrant(rule: string): Promise<PermissionRules> {
    const granted = this.rules.granted ?? [];
    this.rules = { ...this.rules, granted: rule ? granted.filter((g) => g !== rule) : [] };
    return { ...this.rules };
  }

  async revokeRememberedProjectRule(rule: string): Promise<PermissionRules> {
    this.rules = { ...this.rules, remembered: (this.rules.remembered ?? []).filter((saved) => saved !== rule) };
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

  private roots: WorkspaceRoots = { primary: "/Users/you/code/site", extra: [] };

  // The kernel's refusals, by the same codes: a path that is not absolute, one
  // already inside the workspace, and one it will not grant.
  private refuseRoot(code: string): never {
    throw new HttpError(400, "this folder cannot be added", { code });
  }

  async workspaceRoots(): Promise<WorkspaceRoots> {
    return { ...this.roots, extra: [...this.roots.extra] };
  }

  async addWorkspaceRoot(path: string): Promise<WorkspaceRoots> {
    const dir = path.trim();
    if (!/^([a-zA-Z]:)?[\\/]/.test(dir)) this.refuseRoot("workspace_roots.not_directory");
    if (dir === this.roots.primary || dir.startsWith(this.roots.primary + "/")) this.refuseRoot("workspace_roots.covered_by_primary");
    if (dir === "/" || dir === "/Users/you") this.refuseRoot("workspace_roots.too_broad");
    if (dir.includes(".ssh")) this.refuseRoot("workspace_roots.protected");
    if (dir.startsWith("/var/root")) this.refuseRoot("workspace_roots.not_owned");
    if (dir === "/tmp" || this.roots.extra.some((p) => dir.startsWith(p + "/"))) this.refuseRoot("workspace_roots.overlaps_write_root");
    // The kernel records the resolved path, not the spelling: a trailing slash
    // goes and a symlink is followed.
    const resolved = (dir.length > 1 ? dir.replace(/[\\/]+$/, "") : dir).replace("/Users/you/link", "/Users/you/real");
    if (!this.roots.extra.includes(resolved)) this.roots = { ...this.roots, extra: [...this.roots.extra, resolved] };
    return this.workspaceRoots();
  }

  async removeWorkspaceRoot(path: string): Promise<WorkspaceRoots> {
    this.roots = { ...this.roots, extra: this.roots.extra.filter((p) => p !== path) };
    return this.workspaceRoots();
  }

  async browserTools(): Promise<BrowserToolsSettings> {
    return { ...this.browser };
  }

  async saveBrowserTools(enabled: boolean): Promise<BrowserToolsSettings> {
    this.browser = { ...this.browser, enabled, effective: enabled };
    return { ...this.browser };
  }

  private claims: SubagentClaimsSettings = { optimistic: true, path: "/Users/you/.reasonix/config.toml" };

  async subagentClaims(): Promise<SubagentClaimsSettings> {
    return { ...this.claims };
  }

  async saveSubagentClaims(optimistic: boolean): Promise<SubagentClaimsSettings> {
    this.claims = { ...this.claims, optimistic };
    return { ...this.claims };
  }

  private steerLimits: SubagentSteerSettings = { maxBytes: 16384, perMinute: 30, defaultMaxBytes: 16384, defaultPerMinute: 30, path: "/Users/you/.reasonix/config.toml" };

  async subagentSteerSettings(): Promise<SubagentSteerSettings> {
    return { ...this.steerLimits };
  }

  async saveSubagentSteerSettings(maxBytes: number, perMinute: number): Promise<SubagentSteerSettings> {
    this.steerLimits = { ...this.steerLimits, maxBytes, perMinute };
    return { ...this.steerLimits };
  }

  private plans: PlanApprovalSettings = { auto: false, path: "/Users/you/.reasonix/config.toml" };

  async planApproval(): Promise<PlanApprovalSettings> {
    return { ...this.plans };
  }

  async savePlanApproval(auto: boolean): Promise<PlanApprovalSettings> {
    this.plans = { ...this.plans, auto };
    return { ...this.plans };
  }

  private deliveries = { enabled: false, path: "/Users/you/.reasonix/config.toml" };

  async presentFiles() {
    return { ...this.deliveries };
  }

  async savePresentFiles(enabled: boolean) {
    this.deliveries = { ...this.deliveries, enabled };
    return { ...this.deliveries };
  }

  private modes: DefaultModeSettings = { plan: false, path: "/Users/you/.reasonix/config.toml" };

  async defaultMode(): Promise<DefaultModeSettings> {
    return { ...this.modes };
  }

  async saveDefaultMode(plan: boolean): Promise<DefaultModeSettings> {
    this.modes = { ...this.modes, plan };
    return { ...this.modes };
  }

  private toolList: ToolSettings = {
    tools: [
      { name: "bash", description: "Run a shell command in the workspace.", disabled: false },
      { name: "read_file", description: "Read a file, or a line range of it.", disabled: false },
      { name: "recall", description: "Search what earlier turns folded away.", disabled: false },
      { name: "todo_write", description: "Keep a checklist for a multi-step task.", disabled: false, requiredBy: "plan_steps" },
      { name: "web_search", description: "Search the web.", disabled: true, byProject: true },
      { name: "update_goal", description: "Report a goal as complete, blocked or stopped.", disabled: false, requiredBy: "goal_mode" },
      { name: "use_capability", description: "Reach a deferred MCP tool.", disabled: false, requiredBy: "deferred_mcp_tools" },
      { name: "web_fetch", description: "Fetch a web page.", disabled: false },
    ],
    path: "/Users/you/.reasonix/config.toml",
  };

  async toolSettings(): Promise<ToolSettings> {
    return structuredClone(this.toolList);
  }

  async saveToolDisabled(name: string, disabled: boolean): Promise<ToolSettings> {
    this.toolList = { ...this.toolList, tools: this.toolList.tools.map((x) => (x.name === name ? { ...x, disabled } : x)) };
    return structuredClone(this.toolList);
  }

  private lease: WriteLeaseSettings = { optimistic: true, maxWaitMinutes: 0, path: "/Users/you/.reasonix/config.toml" };

  async writeLease(): Promise<WriteLeaseSettings> {
    return { ...this.lease };
  }

  async saveWriteLease(edit: { optimistic?: boolean; maxWaitMinutes?: number }): Promise<WriteLeaseSettings> {
    this.lease = { ...this.lease, ...edit };
    return { ...this.lease };
  }

  protected peerState: PeersState = { enabled: false, peers: [], held: [] };

  private scheduled: ScheduleBoard = {
    pausedAll: false,
    policy: { minIntervalMinutes: 60, maxSchedules: 8, globalTokensDay: 1_200_000, expiryDays: 30 },
    folders: [{ path: "/Users/you/src/app", name: "app" }, { path: "/Users/you/src/docs", name: "docs" }],
    schedules: [
      { id: "sch_a1b2c3d4", label: "Morning briefing", status: "active", everySec: 86_400, workspace: "/Users/you/src/app", model: "deepseek/deepseek-v4-pro",
        prompt: "Summarize what changed in the repository since yesterday and list open TODOs.", perRunTokens: 250_000, lifetimeTokens: 3_000_000, spentLifetimeTokens: 410_000,
        expiresAtUnix: 1_793_000_000 },
      { id: "sch_e5f6a7b8", label: "Docs drift", status: "paused", pausedReason: "consecutive_failures", everySec: 604_800, workspace: "/Users/you/src/docs", model: "deepseek/deepseek-v4-pro",
        prompt: "Check the docs against the code and report what is out of date.", perRunTokens: 250_000, lifetimeTokens: 3_000_000, spentLifetimeTokens: 1_240_000,
        consecutiveFailures: 3, expiresAtUnix: 1_793_000_000 },
    ],
    runs: [
      { triggerId: "sch_a1b2c3d4/1790000000", scheduleId: "sch_a1b2c3d4", slotUnix: 1_790_000_000, state: "succeeded", chargedTokens: 118_400, endedUnix: 1_790_000_260 },
      { triggerId: "sch_a1b2c3d4/1789913600", scheduleId: "sch_a1b2c3d4", slotUnix: 1_789_913_600, state: "blocked", chargedTokens: 64_000, endedUnix: 1_789_913_700 },
      { triggerId: "sch_e5f6a7b8/1789400000", scheduleId: "sch_e5f6a7b8", slotUnix: 1_789_400_000, state: "skipped", skipReason: "missed", chargedTokens: 0 },
    ],
  };

  async schedules(): Promise<ScheduleBoard> {
    return structuredClone(this.scheduled);
  }

  async previewSchedule(d: ScheduleDraft): Promise<SchedulePreview> {
    return { firstRunUnix: 1_790_100_000, workspace: d.workspace, model: d.model || "deepseek/deepseek-v4-pro", prompt: d.prompt, perRunTokens: 250_000, perRunSteps: 40,
      perRunWallSec: 900, perDayMax: 1_000_000, lifetimeTokens: 3_000_000, expiresAtUnix: 1_792_700_000, confirm: "mock-confirm" };
  }

  async createSchedule(d: ScheduleDraft, confirm: string): Promise<ScheduleView> {
    // A refused write is the only thing that shows the read-only state, so a rendered check reaches it by this label.
    if (d.label === "no-edit-grant") throw new HttpError(403, "x", { code: "schedule.editing_disabled", error: "this window has no edit grant" });
    if (confirm !== "mock-confirm") throw new HttpError(409, "x", { code: "schedule.digest_mismatch", error: "what is being created is not what was previewed" });
    const sc: ScheduleView = { id: "sch_" + Math.random().toString(16).slice(2, 10), label: d.label, status: "active", everySec: d.everySec, atUnix: d.atUnix, workspace: d.workspace,
      model: d.model || "deepseek/deepseek-v4-pro", prompt: d.prompt, perRunTokens: 250_000, lifetimeTokens: 3_000_000, spentLifetimeTokens: 0, expiresAtUnix: 1_792_700_000 };
    this.scheduled.schedules = [...this.scheduled.schedules, sc];
    return sc;
  }

  async scheduleAction(id: string, verb: "pause" | "resume" | "renew" | "delete"): Promise<ScheduleBoard> {
    const sc = this.scheduled.schedules.find((s) => s.id === id);
    if (!sc) throw new HttpError(404, "x", { code: "schedule.not_found", error: "no such schedule" });
    if (verb === "delete") this.scheduled.schedules = this.scheduled.schedules.filter((s) => s.id !== id);
    else if (verb === "pause") { sc.status = "paused"; sc.pausedReason = "user"; }
    else if (verb === "resume") { sc.status = "active"; sc.pausedReason = undefined; sc.consecutiveFailures = undefined; }
    return structuredClone(this.scheduled);
  }

  async setSchedulesPaused(paused: boolean): Promise<ScheduleBoard> {
    this.scheduled.pausedAll = paused;
    return structuredClone(this.scheduled);
  }

  async scheduleResult(triggerId: string): Promise<ScheduleResultView> {
    if (!this.scheduled.runs.some((r) => r.triggerId === triggerId)) throw new HttpError(404, "x", { code: "schedule.not_found", error: "no such result" });
    return { triggerId, report: "Three files changed since yesterday. Two TODOs were added in `internal/store`.", pending: [{ kind: "approval", summary: "Wanted to run a shell command", detail: "git log --stat" }], enforcement: "tool-filter", recordedUnix: 1_790_000_260 };
  }

  async peers(): Promise<PeersState> {
    return { ...this.peerState, peers: [...this.peerState.peers], held: [...this.peerState.held], threads: [...(this.peerState.threads ?? [])] };
  }

  async sendPeerMessage(to: string, text: string): Promise<PeerReceipt> {
    const target = this.peerState.peers.find((p) => p.key === to || p.title === to);
    if (!target) throw new Error("no session answers to that name");
    return { id: `m-${text.length}`, to: target };
  }

  async resolvePeerMessage(id: string, _approve: boolean, _remember: PeerRemember = "none"): Promise<void> {
    this.peerState = { ...this.peerState, held: this.peerState.held.filter((h) => h.id !== id) };
  }

  protected modConfig: ModsSettings = { enabled: false, hook_budget_ms: 0, path: "/Users/you/.reasonix/config.toml", available: false, applies: "next_session" };

  async modsSettings(): Promise<ModsSettings> {
    return { ...this.modConfig };
  }

  async saveModsSettings(edit: ModsSettingsEdit): Promise<ModsSettings> {
    if (edit.hook_budget_ms !== undefined && (edit.hook_budget_ms < 0 || edit.hook_budget_ms > 55000)) throw new Error("a hook budget is 0 to 55000 milliseconds");
    this.modConfig = { ...this.modConfig, enabled: edit.enabled ?? this.modConfig.enabled, hook_budget_ms: edit.hook_budget_ms ?? this.modConfig.hook_budget_ms };
    return this.modsSettings();
  }

  async mods(): Promise<ModStatus[]> {
    return mockMods(this.modConfig.enabled);
  }

  protected peerSettings: PeersSettings = { enabled: false, dispatch: false, release_projects: [], max_dispatch_depth: 1, max_dispatched_sessions: 8, path: "/Users/you/.reasonix/config.toml", applies: "next_turn", pending: false, available: true };

  async peersSettings(): Promise<PeersSettings> {
    return { ...this.peerSettings, release_projects: [...this.peerSettings.release_projects] };
  }

  async savePeersSettings(edit: PeersSettingsEdit): Promise<PeersSettings> {
    const keep = edit.release_projects;
    this.peerSettings = {
      ...this.peerSettings,
      enabled: edit.enabled ?? this.peerSettings.enabled,
      dispatch: edit.dispatch ?? this.peerSettings.dispatch,
      max_dispatch_depth: edit.max_dispatch_depth ?? this.peerSettings.max_dispatch_depth,
      max_dispatched_sessions: edit.max_dispatched_sessions ?? this.peerSettings.max_dispatched_sessions,
      release_projects: keep ? keep.map((path) => this.peerSettings.release_projects.find((p) => p.path === path) ?? { path, name: path.split(/[\\/]/).filter(Boolean).at(-1) ?? path }) : this.peerSettings.release_projects,
      pending: true,
    };
    return this.peersSettings();
  }

  private archive: AutoArchiveSettings = { enabled: false, days: 30, defaultDays: 30, path: "/Users/you/.reasonix/config.toml" };

  async autoArchive(): Promise<AutoArchiveSettings> {
    return { ...this.archive };
  }

  async saveAutoArchive(s: Pick<AutoArchiveSettings, "enabled" | "days">): Promise<AutoArchiveSettings> {
    if (s.days < 1 || s.days > 3650) throw new Error("auto archive setting out of range");
    this.archive = { ...this.archive, ...s };
    return { ...this.archive };
  }
  private personaState: PersonaSettings = {
    prefix: "", suffix: "", include_runtime_context: true,
    path: "/Users/you/.reasonix/config.toml",
    presets: [
      {
        id: "engineer",
        prefix: "You are a rigorous staff engineer. Verify before claiming, make the smallest correct change, and state the residual risk.",
        suffix: "", include_runtime_context: true,
      },
      {
        id: "concise",
        prefix: "",
        suffix: "Be terse. Skip the preamble; prefer code and bullets.",
        include_runtime_context: false,
      },
    ],
  };

  async persona(): Promise<PersonaSettings> {
    return { ...this.personaState, presets: this.personaState.presets.map((p) => ({ ...p })) };
  }

  async savePersonaSettings(s: PersonaDraft): Promise<PersonaSettings> {
    this.personaState = { ...this.personaState, ...s };
    return this.persona();
  }

  private currency: DisplayCurrencySettings = { mode: "auto", path: "/Users/you/.reasonix/config.toml", unconverted: [] };

  async displayCurrency(): Promise<DisplayCurrencySettings> {
    return { ...this.currency };
  }

  async saveDisplayCurrency(mode: DisplayCurrencyMode): Promise<DisplayCurrencySettings> {
    this.currency = { ...this.currency, mode };
    return { ...this.currency };
  }

  private changedFiles: ChangedFilesSettings = { show: true, path: "/Users/you/.reasonix/config.toml" };

  async changedFilesSettings(): Promise<ChangedFilesSettings> {
    return { ...this.changedFiles };
  }

  async saveChangedFilesSettings(show: boolean): Promise<ChangedFilesSettings> {
    this.changedFiles = { ...this.changedFiles, show };
    return { ...this.changedFiles };
  }

  async feedbackLevelNotices(): Promise<FeedbackLevelNotices> {
    return { ...levelNotices };
  }

  async saveFeedbackLevelNotices(enabled: boolean): Promise<FeedbackLevelNotices> {
    levelNotices = { ...levelNotices, enabled };
    return { ...levelNotices };
  }

  private foldWork: FoldWorkSettings = { fold: false, path: "/Users/you/.reasonix/config.toml" };

  async foldWorkSettings(): Promise<FoldWorkSettings> {
    return { ...this.foldWork };
  }

  private narration: TurnNarrationSettings = { level: "brief", path: "/Users/you/.reasonix/config.toml" };

  async turnNarrationSettings(): Promise<TurnNarrationSettings> {
    return { ...this.narration };
  }

  async saveTurnNarrationSettings(level: TurnNarrationSettings["level"]): Promise<TurnNarrationSettings> {
    this.narration = { ...this.narration, level };
    return { ...this.narration };
  }

  private observability: ObservabilitySettings = {
    enabled: false, endpoint: "", headerName: "", headerEnv: "", serviceName: "", headerEnvSet: false,
    path: "/Users/you/.reasonix/config.toml", applies: "next_session",
  };

  async observabilitySettings(): Promise<ObservabilitySettings> {
    return { ...this.observability };
  }

  async saveObservabilitySettings(patch: ObservabilityPatch): Promise<ObservabilitySettings> {
    this.observability = { ...this.observability, ...patch };
    return { ...this.observability };
  }

  private webAccess: WebAccessSettings = {
    search_backends: [], searxng_url: "", search_paid_allowed: [], fetch_backends: [], fetch_paid_allowed: [],
    known_search_backends: ["searxng", "deepseek_api"], known_fetch_backends: ["local", "deepseek_api"],
    paid_backends: ["deepseek_api"], deepseek_api_ready: false, video_url_passthrough: false, path: "/Users/you/.reasonix/config.toml",
  };

  async webAccessSettings(): Promise<WebAccessSettings> {
    return { ...this.webAccess };
  }

  async saveWebAccessSettings(patch: WebAccessPatch): Promise<WebAccessSettings> {
    this.webAccess = { ...this.webAccess, ...patch };
    return { ...this.webAccess };
  }

  async probeSearxng(): Promise<SearxngProbe> {
    return this.webAccess.searxng_url.trim() ? { ok: true } : { ok: false, code: "search.searxng_unreachable" };
  }

  async saveFoldWorkSettings(fold: boolean): Promise<FoldWorkSettings> {
    this.foldWork = { ...this.foldWork, fold };
    return { ...this.foldWork };
  }

  private watch: ProgressWatchSettings = {
    pause: false, rounds: 20, tokenMultiple: 8, defaultRounds: 20, defaultTokenMultiple: 8,
    path: "/Users/you/.reasonix/config.toml",
  };

  private ask: AskTimeoutSettings = { seconds: 0, defaultSeconds: 300, minSeconds: 10, maxSeconds: 86400, path: "/Users/you/.reasonix/config.toml" };

  async askTimeout(): Promise<AskTimeoutSettings> {
    return { ...this.ask };
  }

  async saveAskTimeout(seconds: number): Promise<AskTimeoutSettings> {
    if (seconds !== 0 && (seconds < this.ask.minSeconds || seconds > this.ask.maxSeconds)) throw new Error("ask timeout out of range");
    this.ask = { ...this.ask, seconds };
    return { ...this.ask };
  }
  private sessionRef: SessionRefSettings = { tokens: 8000, defaultTokens: 8000, minTokens: 500, maxTokens: 200000, path: "/Users/you/.reasonix/config.toml" };

  async sessionRefSettings(): Promise<SessionRefSettings> {
    return { ...this.sessionRef };
  }

  async saveSessionRefSettings(tokens: number): Promise<SessionRefSettings> {
    if (tokens !== 0 && (tokens < this.sessionRef.minTokens || tokens > this.sessionRef.maxTokens)) throw new Error("session reference limit out of range");
    this.sessionRef = { ...this.sessionRef, tokens: tokens || this.sessionRef.defaultTokens };
    return { ...this.sessionRef };
  }

  private receipt: ReceiptSettings = { show: false, path: "/Users/you/.reasonix/config.toml" };

  async receiptSettings(): Promise<ReceiptSettings> {
    return { ...this.receipt };
  }

  async saveReceiptSettings(show: boolean): Promise<ReceiptSettings> {
    this.receipt = { ...this.receipt, show };
    return { ...this.receipt };
  }

  async progressWatch(): Promise<ProgressWatchSettings> {
    return { ...this.watch };
  }

  async saveProgressWatch(s: Pick<ProgressWatchSettings, "pause" | "rounds" | "tokenMultiple">): Promise<ProgressWatchSettings> {
    if (s.rounds < 1 || s.rounds > 1000 || s.tokenMultiple < 1 || s.tokenMultiple > 1000) throw new Error("progress watch setting out of range");
    this.watch = { ...this.watch, ...s };
    return { ...this.watch };
  }

  private broken: ConfigProblem | null = null;

  async configProblem(): Promise<ConfigProblem | null> {
    return this.broken && { ...this.broken };
  }

  async repairConfig(): Promise<ConfigRepair> {
    const backup = (this.broken?.path ?? "") + ".broken-20260823-181500";
    this.broken = null;
    return { backup, problem: null };
  }

  private mode: UpdateMode = "manual";
  async updateMode(): Promise<UpdateMode> { return this.mode; }
  async setUpdateMode(mode: UpdateMode): Promise<UpdateMode> { this.mode = mode; return mode; }
}
