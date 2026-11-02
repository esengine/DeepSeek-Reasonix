// The user's team of long-lived agents, as GET /team answers it. Each type
// here mirrors one the kernel sends; wire-parity holds the two together.

// Who a turn the host ran was for, when it was not the person: another agent's
// message, an agent's reply, or a member's turn in a group.
export interface MessageOrigin {
  kind: "message" | "reply" | "group_turn" | "schedule" | "secret" | "user_message";
  agent?: string;
  exchange?: string;
  group?: string;
}

export interface TeamAvatar {
  glyph?: string;
  color?: string;
  // Where a photo the person gave the agent is read; it is worn over the glyph.
  image?: string;
}

// What an agent is doing, read off its open sessions. `runtime` names the pane
// a prompt waiting on the person is in.
export interface TeamActivity {
  state: "idle" | "working" | "needs_you" | "waiting_on";
  on?: string[];
  runtime?: string;
}

// What an agent's conversations have spent, summed over its own chat, its
// earlier parts and its seats in groups.
export interface TeamUsage {
  promptTokens: number;
  completionTokens: number;
  cacheHitTokens: number;
  requests: number;
}

export interface TeamAgent {
  id: string;
  name: string;
  role?: string;
  instructions?: string;
  avatar: TeamAvatar;
  model?: string;
  folder?: string;
  permission?: string;
  main?: boolean;
  pinned?: boolean;
  hidden?: boolean;
  runtime?: string;
  lastActivityAt?: number;
  preview?: string;
  activity: TeamActivity;
  usage?: TeamUsage;
}

export interface TeamRound {
  speaking?: string;
  queue: string[];
  working: string[];
  parked?: number;
  relays?: number;
  limit: number;
}

export interface TeamGroup {
  id: string;
  name?: string;
  notice?: string;
  members: string[];
  admin?: string;
  mode: "admin" | "everyone";
  relayLimit?: number;
  folder?: string;
  pinned?: boolean;
  hidden?: boolean;
  archived?: boolean;
  lastSeq?: number;
  lastActivityAt?: number;
  preview?: string;
  // The group's latest line when it is an event, which has no text to preview.
  lastEvent?: GroupLine;
  round: TeamRound;
}

export interface TeamView {
  revision: number;
  readOnly?: boolean;
  agents: TeamAgent[];
  groups: TeamGroup[];
  // Absent from a kernel that predates the team's prefs.
  prefs?: TeamPrefs;
}

// What the person chose for the whole team.
export interface TeamPrefs {
  memoryReview: boolean;
}

// One entry of a group's log. An event names the member it is about in
// `from`, or "user" for the round itself; `length` is a cut post's full size.
export interface GroupLine {
  seq: number;
  at: string;
  from: string;
  kind: "post" | "event";
  text?: string;
  length?: number;
  mentions?: string[];
  event?: "passed" | "missed" | "working" | "finished" | "failed" | "paused" | "resumed" | "stopped" | "cut" | "memory";
  count?: number;
  code?: string;
  late?: boolean;
  // The scheduled task a user line came from.
  schedule?: string;
}

// One message between agents, as the kernel's book records it.
export interface TeamExchange {
  id: string;
  from: string;
  to: string;
  requestId?: string;
  digest: string;
  preview?: string;
  replyTo: "me" | "user";
  parent?: string;
  group?: string;
  ceiling?: string;
  state: "queued" | "delivered" | "waiting_children" | "replied" | "failed" | "stopped" | "interrupted";
  returned?: boolean;
  openedAt: string;
  at: string;
}

// A task the person set for an agent or a group. `clock` is a local "HH:MM",
// `days` weekdays with Sunday 0, `once` the one-off's moment and the anchor an
// every-N task counts from. `next` and `lastFired` are the kernel's.
export interface TeamSchedule {
  id?: string;
  name: string;
  agent?: string;
  group?: string;
  prompt: string;
  repeat: "once" | "daily" | "weekly" | "every";
  clock?: string;
  days?: number[];
  every?: number;
  unit?: "minutes" | "hours" | "days";
  once?: string;
  enabled: boolean;
  next?: string;
  lastFired?: string;
}

// A secret the person saved, as the window may know it: never its value.
// `agents` absent means every agent may use it.
export interface TeamSecret {
  name: string;
  purpose?: string;
  agents?: string[];
  requestedBy?: string;
  updatedAt?: number;
}

// An agent's request for a secret the person has not answered.
export interface TeamSecretRequest {
  name: string;
  purpose?: string;
  agent: string;
  at?: number;
}

// A value the person deleted or replaced: it reaches no command, and is kept
// only so the copies already in conversations stay masked.
export interface TeamRetiredSecret {
  name: string;
  at?: number;
}

export interface TeamSecrets {
  secrets: TeamSecret[];
  requests: TeamSecretRequest[];
  retired: TeamRetiredSecret[];
  // Whether agents' commands on this host are denied the file the values live in.
  guarded: boolean;
}

// What the window sends to save a secret.
export interface SecretSave {
  name: string;
  value: string;
  purpose?: string;
  agents?: string[];
  requester?: string;
}

// One memory agents share, as the kernel lists it.
export interface SharedMemory {
  name: string;
  title?: string;
  description: string;
  type?: string;
  updatedAt?: number;
}

// What the window sends to create or change an agent; an absent field is left
// as it is. Permission, main and the relay limit are only ever the person's.
export interface AgentEdit {
  name?: string;
  role?: string;
  instructions?: string;
  avatar?: TeamAvatar;
  model?: string;
  folder?: string;
  permission?: string;
  main?: boolean;
  pinned?: boolean;
  hidden?: boolean;
}

export interface GroupEdit {
  name?: string;
  notice?: string;
  members?: string[];
  admin?: string;
  mode?: "admin" | "everyone";
  relayLimit?: number;
  folder?: string;
  pinned?: boolean;
  hidden?: boolean;
}

// The kernel's /team surface. A kernel with the team off answers 404 to every
// route, which view() reports as null so the window leaves the team out.
export interface TeamPort {
  view(): Promise<TeamView | null>;
  bootstrap(name: string, role?: string): Promise<TeamView>;
  createAgent(edit: AgentEdit): Promise<TeamView>;
  // A picture already cropped by the window, as a data URL.
  setPhoto(id: string, image: string): Promise<TeamView>;
  clearPhoto(id: string): Promise<TeamView>;
  updateAgent(id: string, edit: AgentEdit): Promise<TeamView>;
  deleteAgent(id: string): Promise<TeamView>;
  openAgent(id: string): Promise<import("./hub").RuntimeView>;
  stopAgent(id: string): Promise<void>;
  createGroup(edit: GroupEdit): Promise<TeamView>;
  updateGroup(id: string, edit: GroupEdit): Promise<TeamView>;
  deleteGroup(id: string): Promise<TeamView>;
  groupLog(id: string, page: { after?: number; before?: number; limit?: number }): Promise<GroupLine[]>;
  postGroup(id: string, text: string, mentions: string[], everyone?: boolean): Promise<GroupLine>;
  continueGroup(id: string): Promise<void>;
  stopMember(group: string, agent: string): Promise<void>;
  openMember(group: string, agent: string): Promise<import("./hub").RuntimeView>;
  exchanges(agent: string): Promise<TeamExchange[]>;
  // What the team, or one group, remembers; the person can make it forget one.
  sharedMemory(tier: "team" | "group", group?: string): Promise<SharedMemory[]>;
  forgetShared(tier: "team" | "group", name: string, group?: string): Promise<void>;
  setPrefs(prefs: Partial<TeamPrefs>): Promise<TeamView>;
  secrets(): Promise<TeamSecrets>;
  saveSecret(s: SecretSave): Promise<void>;
  cancelSecret(name: string, agent: string): Promise<void>;
  deleteSecret(name: string): Promise<void>;
  forgetRetiredSecrets(): Promise<void>;
  schedules(): Promise<TeamSchedule[]>;
  saveSchedule(s: TeamSchedule): Promise<TeamSchedule>;
  deleteSchedule(id: string): Promise<void>;
}

type Refusal = { code?: string; error?: string; params?: Record<string, string | number> };

// SseTeam reaches the hub's /team routes. A refusal keeps its code, which is
// what has words in the reader's language.
export class SseTeam implements TeamPort {
  private async call<T>(method: string, path: string, body?: unknown): Promise<T> {
    if (body === undefined && method === "POST") body = {};
    const res = await fetch(path, {
      method,
      credentials: "same-origin",
      headers: body === undefined ? undefined : { "content-type": "application/json" },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    if (!res.ok) {
      const raw = (await res.text().catch(() => "")).trim();
      let reason: Refusal | undefined;
      try {
        reason = raw ? (JSON.parse(raw) as Refusal) : undefined;
      } catch {
        reason = undefined;
      }
      const { HttpError } = await import("./http_error");
      throw new HttpError(res.status, reason?.error || raw.slice(0, 400) || `${path}: ${res.status}`, reason, raw !== "");
    }
    return res.status === 204 ? (undefined as T) : ((await res.json()) as T);
  }

  async view() {
    const res = await fetch("/team", { credentials: "same-origin" });
    if (res.status === 404) return null;
    if (!res.ok) return this.call<TeamView>("GET", "/team");
    return (await res.json()) as TeamView;
  }
  bootstrap(name: string, role?: string) { return this.call<TeamView>("POST", "/team/bootstrap", { name, role }); }
  createAgent(edit: AgentEdit) { return this.call<TeamView>("POST", "/team/agents", edit); }
  setPhoto(id: string, image: string) { return this.call<TeamView>("POST", `/team/agents/${encodeURIComponent(id)}/photo`, { image }); }
  clearPhoto(id: string) { return this.call<TeamView>("DELETE", `/team/agents/${encodeURIComponent(id)}/photo`); }
  updateAgent(id: string, edit: AgentEdit) { return this.call<TeamView>("PATCH", `/team/agents/${encodeURIComponent(id)}`, edit); }
  deleteAgent(id: string) { return this.call<TeamView>("DELETE", `/team/agents/${encodeURIComponent(id)}`); }
  openAgent(id: string) { return this.call<import("./hub").RuntimeView>("POST", `/team/agents/${encodeURIComponent(id)}/open`); }
  stopAgent(id: string) { return this.call<void>("POST", `/team/agents/${encodeURIComponent(id)}/stop`); }
  createGroup(edit: GroupEdit) { return this.call<TeamView>("POST", "/team/groups", edit); }
  updateGroup(id: string, edit: GroupEdit) { return this.call<TeamView>("PATCH", `/team/groups/${encodeURIComponent(id)}`, edit); }
  deleteGroup(id: string) { return this.call<TeamView>("DELETE", `/team/groups/${encodeURIComponent(id)}`); }
  groupLog(id: string, page: { after?: number; before?: number; limit?: number }) {
    const q = new URLSearchParams();
    if (page.after) q.set("after", String(page.after));
    if (page.before) q.set("before", String(page.before));
    if (page.limit) q.set("limit", String(page.limit));
    return this.call<GroupLine[]>("GET", `/team/groups/${encodeURIComponent(id)}/log?${q}`);
  }
  postGroup(id: string, text: string, mentions: string[], everyone = false) {
    return this.call<GroupLine>("POST", `/team/groups/${encodeURIComponent(id)}/messages`, { text, mentions, everyone });
  }
  continueGroup(id: string) { return this.call<void>("POST", `/team/groups/${encodeURIComponent(id)}/continue`); }
  stopMember(group: string, agent: string) {
    return this.call<void>("POST", `/team/groups/${encodeURIComponent(group)}/members/${encodeURIComponent(agent)}/stop`);
  }
  openMember(group: string, agent: string) {
    return this.call<import("./hub").RuntimeView>("POST", `/team/groups/${encodeURIComponent(group)}/members/${encodeURIComponent(agent)}/open`);
  }
  exchanges(agent: string) { return this.call<TeamExchange[]>("GET", `/team/exchanges?agent=${encodeURIComponent(agent)}`); }
  sharedMemory(tier: "team" | "group", group?: string) {
    return this.call<SharedMemory[]>("GET", `/team/memory?tier=${tier}${group ? `&group=${encodeURIComponent(group)}` : ""}`);
  }
  setPrefs(prefs: Partial<TeamPrefs>) { return this.call<TeamView>("PATCH", "/team/prefs", prefs); }
  secrets() { return this.call<TeamSecrets>("GET", "/team/secrets"); }
  saveSecret(s: SecretSave) { return this.call<void>("POST", "/team/secrets", s); }
  cancelSecret(name: string, agent: string) { return this.call<void>("POST", `/team/secrets/${encodeURIComponent(name)}/cancel`, { agent }); }
  deleteSecret(name: string) { return this.call<void>("DELETE", `/team/secrets/${encodeURIComponent(name)}`); }
  forgetRetiredSecrets() { return this.call<void>("DELETE", "/team/secrets-retired"); }
  schedules() { return this.call<TeamSchedule[]>("GET", "/team/schedules"); }
  saveSchedule(s: TeamSchedule) {
    return s.id ? this.call<TeamSchedule>("PATCH", `/team/schedules/${encodeURIComponent(s.id)}`, s) : this.call<TeamSchedule>("POST", "/team/schedules", s);
  }
  deleteSchedule(id: string) { return this.call<void>("DELETE", `/team/schedules/${encodeURIComponent(id)}`); }
  forgetShared(tier: "team" | "group", name: string, group?: string) {
    return this.call<void>("DELETE", `/team/memory/${tier}/${encodeURIComponent(name)}${group ? `?group=${encodeURIComponent(group)}` : ""}`);
  }
}
