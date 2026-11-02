import type { RuntimeView } from "./hub";
import { HttpError } from "./http_error";
import type { AgentEdit, GroupEdit, GroupLine, SecretSave, SharedMemory, TeamAgent, TeamExchange, TeamGroup, TeamPort, TeamPrefs, TeamSchedule, TeamSecrets, TeamView } from "./team";

// An in-memory team for the mock hub: enough of the kernel's rules to draw
// every state the window has to, with no kernel behind it.
export class MockTeam implements TeamPort {
  private revision = 0;
  private seq = 0;
  private agents: TeamAgent[] = [];
  private groups: TeamGroup[] = [];
  private readonly logs = new Map<string, GroupLine[]>();
  private book: TeamExchange[] = [];
  private readonly shared = new Map<string, SharedMemory[]>();
  calls: { method: string; args: unknown[] }[] = [];

  constructor(
    private readonly adopt: (view: RuntimeView) => void,
    private readonly changed: () => void,
  ) {}

  private prefs: TeamPrefs = { memoryReview: false };
  private tasks: TeamSchedule[] = [];
  private vault: TeamSecrets = { secrets: [], requests: [], retired: [], guarded: true };

  async secrets(): Promise<TeamSecrets> {
    return structuredClone(this.vault);
  }

  async saveSecret(s: SecretSave) {
    this.record("saveSecret", s);
    if (this.vault.secrets.some((x) => x.name === s.name)) this.vault.retired.push({ name: s.name });
    this.vault.secrets = [...this.vault.secrets.filter((x) => x.name !== s.name), { name: s.name, purpose: s.purpose, agents: s.agents, requestedBy: s.requester }];
    this.vault.requests = this.vault.requests.filter((q) => q.name !== s.name);
    this.bump();
  }

  async cancelSecret(name: string, agent: string) {
    this.record("cancelSecret", name, agent);
    this.vault.requests = this.vault.requests.filter((q) => !(q.name === name && q.agent === agent));
    this.bump();
  }

  async deleteSecret(name: string) {
    this.record("deleteSecret", name);
    this.vault.retired.push({ name });
    this.vault.secrets = this.vault.secrets.filter((x) => x.name !== name);
    this.bump();
  }

  async forgetRetiredSecrets() {
    this.record("forgetRetiredSecrets");
    this.vault.retired = [];
    this.bump();
  }

  /** Says the host does not deny agents' commands the secrets file, for tests. */
  unguardSecrets() {
    this.vault.guarded = false;
    this.bump();
  }

  /** Records an agent's request for a secret, as its tool would, for tests. */
  askSecret(name: string, purpose: string, agent: string) {
    this.vault.requests.push({ name, purpose, agent });
    this.bump();
  }

  async schedules(): Promise<TeamSchedule[]> {
    return structuredClone(this.tasks);
  }

  async saveSchedule(s: TeamSchedule): Promise<TeamSchedule> {
    this.record("saveSchedule", s);
    const saved = { ...s, id: s.id ?? `s-${++this.seq}`, next: s.enabled ? new Date(0).toISOString() : undefined };
    this.tasks = s.id ? this.tasks.map((x) => (x.id === s.id ? saved : x)) : [...this.tasks, saved];
    this.bump();
    return structuredClone(saved);
  }

  async deleteSchedule(id: string) {
    this.record("deleteSchedule", id);
    this.tasks = this.tasks.filter((x) => x.id !== id);
    this.bump();
  }

  private snapshot(): TeamView {
    return { revision: this.revision, agents: structuredClone(this.agents), groups: structuredClone(this.groups), prefs: { ...this.prefs } };
  }

  async setPrefs(prefs: Partial<TeamPrefs>) {
    this.record("setPrefs", prefs);
    this.prefs = { ...this.prefs, ...prefs };
    return this.bump();
  }

  private bump(): TeamView {
    this.revision++;
    this.changed();
    return this.snapshot();
  }

  private record(method: string, ...args: unknown[]) {
    this.calls.push({ method, args });
  }

  private agent(id: string): TeamAgent {
    const a = this.agents.find((x) => x.id === id);
    if (!a) throw new HttpError(404, "no such agent", { code: "team.unknown_agent" });
    return a;
  }

  private group(id: string): TeamGroup {
    const g = this.groups.find((x) => x.id === id);
    if (!g) throw new HttpError(404, "no such group", { code: "team.unknown_group" });
    return g;
  }

  private named(name: string | undefined, except = ""): void {
    if (!name?.trim()) throw new HttpError(400, "name is empty", { code: "team.invalid", params: { field: "name" } });
    if (name.length > 40) throw new HttpError(400, "name is too long", { code: "team.too_long", params: { field: "name" } });
    if (this.agents.some((a) => a.id !== except && a.name.toLowerCase() === name.trim().toLowerCase())) {
      throw new HttpError(400, "name is already taken", { code: "team.name_taken", params: { field: "name" } });
    }
  }

  view(): Promise<TeamView | null> {
    return Promise.resolve(this.snapshot());
  }

  async bootstrap(name: string, role?: string) {
    this.record("bootstrap", name, role);
    if (this.agents.length === 0) {
      this.agents.push({ id: "a-chief", name, role, main: true, avatar: { glyph: "whale" }, activity: { state: "idle" } });
    }
    return this.bump();
  }

  async createAgent(edit: AgentEdit) {
    this.record("createAgent", edit);
    this.named(edit.name);
    this.seq++;
    this.agents.push({
      id: `a-${this.seq}`, name: edit.name!.trim(), role: edit.role, instructions: edit.instructions,
      avatar: edit.avatar ?? {}, permission: edit.permission, model: edit.model, folder: edit.folder, main: false, activity: { state: "idle" },
    });
    return this.bump();
  }

  async setPhoto(id: string, image: string) {
    this.record("setPhoto", id, image);
    this.agent(id).avatar = { ...this.agent(id).avatar, image };
    return this.bump();
  }

  async clearPhoto(id: string) {
    this.record("clearPhoto", id);
    const { image: _, ...rest } = this.agent(id).avatar;
    this.agent(id).avatar = rest;
    return this.bump();
  }

  async updateAgent(id: string, edit: AgentEdit) {
    this.record("updateAgent", id, edit);
    const a = this.agent(id);
    if (edit.name !== undefined) this.named(edit.name, id);
    if (edit.main === false && a.main && this.agents.filter((x) => x.main).length === 1) {
      throw new HttpError(400, "the team needs at least one main agent", { code: "team.last_main" });
    }
    Object.assign(a, { ...edit, name: edit.name?.trim() ?? a.name });
    return this.bump();
  }

  async deleteAgent(id: string) {
    this.record("deleteAgent", id);
    this.agent(id);
    this.agents = this.agents.filter((a) => a.id !== id);
    this.groups = this.groups.filter((g) => !g.members.includes(id) || g.members.length > 2);
    for (const g of this.groups) g.members = g.members.filter((m) => m !== id);
    return this.bump();
  }

  async openAgent(id: string) {
    this.record("openAgent", id);
    const a = this.agent(id);
    const view: RuntimeView = { id: `team-${id}`, base: `/rt/team-${id}`, root: `~/Reasonix Team/${id}`, name: a.name, sessionPath: `/team/${id}.jsonl`, team: { agent: id } };
    a.runtime = view.id;
    this.adopt(view);
    this.bump();
    return view;
  }

  async stopAgent(id: string) {
    this.record("stopAgent", id);
  }

  async createGroup(edit: GroupEdit) {
    this.record("createGroup", edit);
    const members = edit.members ?? [];
    if (members.length < 2) throw new HttpError(400, "group size", { code: "team.group_size" });
    this.seq++;
    const id = `g-${this.seq}`;
    this.groups.push({
      id, name: edit.name, notice: edit.notice, members, admin: edit.admin,
      mode: edit.mode ?? (edit.admin ? "admin" : "everyone"), relayLimit: edit.relayLimit,
      round: { queue: [], working: [], limit: edit.relayLimit || members.length * 3 },
    });
    this.logs.set(id, []);
    return this.bump();
  }

  async updateGroup(id: string, edit: GroupEdit) {
    this.record("updateGroup", id, edit);
    Object.assign(this.group(id), edit);
    return this.bump();
  }

  async deleteGroup(id: string) {
    this.record("deleteGroup", id);
    this.group(id);
    this.groups = this.groups.filter((g) => g.id !== id);
    return this.bump();
  }

  async groupLog(id: string, page: { after?: number; before?: number; limit?: number }) {
    this.group(id);
    const lines = this.logs.get(id) ?? [];
    const after = lines.filter((l) => l.seq > (page.after ?? 0) && (!page.before || l.seq < page.before));
    return page.limit ? after.slice(-page.limit) : after;
  }

  async postGroup(id: string, text: string, mentions: string[], everyone = false) {
    this.record("postGroup", id, text, mentions, everyone);
    const g = this.group(id);
    if (!text.trim()) throw new HttpError(400, "message is empty", { code: "team.invalid" });
    const lines = this.logs.get(id) ?? [];
    const line: GroupLine = { seq: lines.length + 1, at: new Date(0).toISOString(), from: "user", kind: "post", text, mentions: mentions.length ? mentions : undefined };
    lines.push(line);
    this.logs.set(id, lines);
    g.round.speaking = mentions[0] ?? g.admin ?? g.members[0];
    g.preview = text;
    g.lastSeq = line.seq;
    this.bump();
    return line;
  }

  /** Puts a line in a group's log as if the kernel had, for tests. */
  addLine(id: string, line: Omit<GroupLine, "seq" | "at">) {
    const g = this.group(id);
    const lines = this.logs.get(id) ?? [];
    lines.push({ ...line, seq: lines.length + 1, at: new Date(0).toISOString() });
    this.logs.set(id, lines);
    g.lastSeq = lines.length;
    if (line.text) g.preview = line.text;
    g.lastEvent = line.kind === "event" ? lines.at(-1) : undefined;
    this.bump();
  }

  /** Sets what an agent is doing, as the kernel would report it, for tests. */
  setActivity(id: string, activity: TeamAgent["activity"]) {
    this.agent(id).activity = activity;
    this.bump();
  }

  /** Moves an agent's or a group's last activity, as a turn would, for tests. */
  touch(id: string, at: number) {
    const a = this.agents.find((x) => x.id === id);
    if (a) a.lastActivityAt = at;
    else this.group(id).lastActivityAt = at;
    this.bump();
  }

  /** Sets a group's round, as the kernel would report it, for tests. */
  setRound(id: string, round: Partial<TeamGroup["round"]>) {
    Object.assign(this.group(id).round, round);
    this.bump();
  }

  async continueGroup(id: string) {
    this.record("continueGroup", id);
    this.group(id).round.parked = 0;
    this.bump();
  }

  async stopMember(group: string, agent: string) {
    this.record("stopMember", group, agent);
  }

  async openMember(group: string, agent: string) {
    this.record("openMember", group, agent);
    const a = this.agent(agent);
    const view: RuntimeView = { id: `team-${group}-${agent}`, base: `/rt/team-${group}-${agent}`, root: `~/Reasonix Team/${agent}`, name: a.name, team: { agent, group } };
    this.adopt(view);
    return view;
  }

  async exchanges(agent: string): Promise<TeamExchange[]> {
    return structuredClone(this.book.filter((x) => !agent || x.from === agent || x.to === agent));
  }

  async sharedMemory(tier: "team" | "group", group?: string): Promise<SharedMemory[]> {
    return structuredClone(this.shared.get(`${tier}:${group ?? ""}`) ?? []);
  }

  async forgetShared(tier: "team" | "group", name: string, group?: string) {
    this.record("forgetShared", tier, name, group);
    const key = `${tier}:${group ?? ""}`;
    this.shared.set(key, (this.shared.get(key) ?? []).filter((m) => m.name !== name));
    this.bump();
  }

  /** Puts a memory in a shared store as an agent's tool would, for tests. */
  addShared(tier: "team" | "group", m: SharedMemory, group?: string) {
    const key = `${tier}:${group ?? ""}`;
    this.shared.set(key, [...(this.shared.get(key) ?? []), m]);
    this.bump();
  }

  /** Records an exchange as the kernel's book would, for tests. */
  addExchange(x: TeamExchange) {
    this.book.push(x);
    this.bump();
  }
}
