import type { RuntimeView } from "./hub";
import { HttpError } from "./http_error";
import type { AgentEdit, GroupEdit, GroupLine, TeamAgent, TeamExchange, TeamGroup, TeamPort, TeamView } from "./team";

// An in-memory team for the mock hub: enough of the kernel's rules to draw
// every state the window has to, with no kernel behind it.
export class MockTeam implements TeamPort {
  private revision = 0;
  private seq = 0;
  private agents: TeamAgent[] = [];
  private groups: TeamGroup[] = [];
  private readonly logs = new Map<string, GroupLine[]>();
  calls: { method: string; args: unknown[] }[] = [];

  constructor(
    private readonly adopt: (view: RuntimeView) => void,
    private readonly changed: () => void,
  ) {}

  private snapshot(): TeamView {
    return { revision: this.revision, agents: structuredClone(this.agents), groups: structuredClone(this.groups) };
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
      avatar: edit.avatar ?? {}, permission: edit.permission, model: edit.model, main: false, activity: { state: "idle" },
    });
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
    this.bump();
  }

  /** Sets what an agent is doing, as the kernel would report it, for tests. */
  setActivity(id: string, activity: TeamAgent["activity"]) {
    this.agent(id).activity = activity;
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

  async exchanges(_agent: string): Promise<TeamExchange[]> {
    return [];
  }
}
