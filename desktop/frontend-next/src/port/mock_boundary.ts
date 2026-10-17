import type { Adjudications, BrowserToolsSettings, SubagentClaimsSettings, WriteLeaseSettings, PlanApprovalSettings, PeerReceipt, PeersState, ConfigProblem, ConfigRepair, PermissionLists, PermissionRules, SandboxSettings, UpdateMode, PeersSettings, PeersSettingsEdit, PeerRemember } from "./port";
import type { AskTimeoutSettings, AutoArchiveSettings, ChangedFilesSettings, FoldWorkSettings, TurnNarrationSettings, DisplayCurrencyMode, DisplayCurrencySettings, ProgressWatchSettings, WorkspaceRoots, ReceiptSettings, PersonaDraft, PersonaSettings, WebAccessPatch, WebAccessSettings } from "./boundary";
import { HttpError } from "./http_error";
import { MockShell } from "./mock_shell";
import { MockCompositions } from "./mock_composition";
import type { AgentComposition } from "./composition";

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
    this.rules = { ...this.rules, ...lists };
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

  private claims: SubagentClaimsSettings = { optimistic: false, path: "/Users/you/.reasonix/config.toml" };

  async subagentClaims(): Promise<SubagentClaimsSettings> {
    return { ...this.claims };
  }

  async saveSubagentClaims(optimistic: boolean): Promise<SubagentClaimsSettings> {
    this.claims = { ...this.claims, optimistic };
    return { ...this.claims };
  }

  private plans: PlanApprovalSettings = { auto: false, path: "/Users/you/.reasonix/config.toml" };

  async planApproval(): Promise<PlanApprovalSettings> {
    return { ...this.plans };
  }

  async savePlanApproval(auto: boolean): Promise<PlanApprovalSettings> {
    this.plans = { ...this.plans, auto };
    return { ...this.plans };
  }

  private lease: WriteLeaseSettings = { optimistic: false, path: "/Users/you/.reasonix/config.toml" };

  async writeLease(): Promise<WriteLeaseSettings> {
    return { ...this.lease };
  }

  async saveWriteLease(optimistic: boolean): Promise<WriteLeaseSettings> {
    this.lease = { ...this.lease, optimistic };
    return { ...this.lease };
  }

  protected peerState: PeersState = { enabled: false, peers: [], held: [] };

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

  protected peerSettings: PeersSettings = { enabled: false, dispatch: false, release_projects: [], path: "/Users/you/.reasonix/config.toml", applies: "next_turn", pending: false, available: true };

  async peersSettings(): Promise<PeersSettings> {
    return { ...this.peerSettings, release_projects: [...this.peerSettings.release_projects] };
  }

  async savePeersSettings(edit: PeersSettingsEdit): Promise<PeersSettings> {
    const keep = edit.release_projects;
    this.peerSettings = {
      ...this.peerSettings,
      enabled: edit.enabled ?? this.peerSettings.enabled,
      dispatch: edit.dispatch ?? this.peerSettings.dispatch,
      release_projects: keep ? this.peerSettings.release_projects.filter((p) => keep.includes(p.path)) : this.peerSettings.release_projects,
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

  // Two starting points are enough for the page's layout: one that is a whole
  // prefix (complete) and one that only trails the built-in guidance.
  private personaState: PersonaSettings = {
    prefix: "", suffix: "", complete: false, include_runtime_context: true,
    path: "/Users/you/.reasonix/config.toml",
    presets: [
      {
        id: "engineer",
        prefix: "You are a rigorous staff engineer. Verify before claiming, make the smallest correct change, and state the residual risk.",
        suffix: "", complete: true, include_runtime_context: true,
      },
      {
        id: "concise",
        prefix: "",
        suffix: "Be terse. Skip the preamble; prefer code and bullets.",
        complete: false, include_runtime_context: false,
      },
    ],
  };

  async persona(): Promise<PersonaSettings> {
    const catalog=await this.agentPresets();
    const definition=catalog.presets.find((v)=>v.definition.id===catalog.default)!.definition;
    return { ...this.personaState,...definition.persona, presets: this.personaState.presets.map((p) => ({ ...p })) };
  }

  async savePersonaSettings(s: PersonaDraft): Promise<PersonaSettings> {
    // The same rule the kernel enforces, so the fixture cannot show a screen
    // accepting something the real save refuses.
    if (s.complete && !s.prefix.trim()) throw new Error("complete requires a prefix");
    const catalog=await this.agentPresets();
    const old=catalog.presets.find((v)=>v.definition.id===catalog.default)!;
    const copied=old.definition.id.startsWith("builtin:");
    const definition={...old.definition,id:copied ? "user:persona" : old.definition.id,persona:{...s}};
    await this.saveAgentPreset(definition,copied ? "" : old.revision);
    await this.setDefaultAgentPreset(definition.id,false);
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

  private webAccess: WebAccessSettings = {
    search_backends: [], searxng_url: "", search_paid_allowed: [], fetch_backends: [], fetch_paid_allowed: [],
    known_search_backends: ["searxng", "deepseek_api"], known_fetch_backends: ["local", "deepseek_api"],
    paid_backends: ["deepseek_api"], deepseek_api_ready: false, path: "/Users/you/.reasonix/config.toml",
  };

  async webAccessSettings(): Promise<WebAccessSettings> {
    return { ...this.webAccess };
  }

  async saveWebAccessSettings(patch: WebAccessPatch): Promise<WebAccessSettings> {
    this.webAccess = { ...this.webAccess, ...patch };
    return { ...this.webAccess };
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
