import type { PlanAction } from "./session";
import { HttpError } from "./port";
import type { AccountState, AgentPort, ChangeDiff, CompactionSettings, Completion, CompletionItem, DeviceGrant, VersionHub, ApprovalMode, ApprovalVerdict, Checkpoint, RewindPlan, RewindResult, RewindScope, HistoryMessage, ModelEntry, Preset, ProviderSetup, RoleAssignments, SessionEntry, SessionStatus, WalletReading, MemoryCatalog, MemoryEdit, UsageReport, MemoryEntry, WorkspaceInfo, WorkspaceChanges, Attachment, DroppedRef, Queue, QueueItem, Queued, TrayPrefs } from "./port";
import type { ExecutionGraphRead, WireEvent } from "./wire";
import { MockTheme } from "./mock_theme";
import { SCRIPT } from "./fixture";
import { MockExecutionHold, mockExecutionGraph } from "./mock_graph";
import { mockStorage, mockStoragePlan } from "./mock_storage";
import { MEMORIES, USAGE_PRICED, USAGE_TOKENS } from "./mock_memory";



export class MockPort extends MockTheme implements AgentPort {
  private listeners = new Set<(ev: WireEvent) => void>();
  private log: WireEvent[] = [];
  // What the user has sent, so checkpoints() can mirror one per turn.
  private prompts: string[] = [];
  private undone: string[] | null = null;
  private at = 0;
  // The kernel numbers every frame a client cannot afford to miss, and a
  // bootstrap cut is read against those numbers.
  private seq = 0;
  private hold = new MockExecutionHold();
  private timer: number | undefined;
  // The script pauses on approval_request/ask_request the same way the real
  // run blocks on Approve()/AnswerQuestion(); nothing advances until answered.
  private gated = false;
  // Queued mid-turn lines, by the id the kernel would have given them.
  private queued = new Map<string, number>();
  // The rows behind those timers. Kept apart because a paused queue still
  // holds its entries — stopping delivery is not dropping the line.
  private queueItems: QueueItem[] = [];
  private queueBodies = new Map<string, string>();
  private queuePaused = false;
  private queueRevision = 1;
  private state: SessionStatus = {
    label: "deepseek-v4-pro",
    running: false,
    plan: false,
    preset: "balanced",
    effort: "auto",
    modelRef: "deepseek/deepseek-v4-pro",
    toolApprovalMode: "ask",
    autoApproveTools: false,
    bypass: false,
    goal: "",
    goalStatus: "stopped",
    cwd: "~/projects/DeepSeek-Reasonix/.reasonix/sessions",
    workspaceRoot: "~/projects/DeepSeek-Reasonix",
    used: 0,
    window: 128000,
    cacheHit: 0,
    cacheMiss: 0,
  };

  private setupDone = false;
  private session: SessionEntry | null = null;

  async providerSetup(): Promise<ProviderSetup | null> {
    return this.setupDone ? null : { required: true, provider: "deepseek", model: "deepseek-v4-pro", keyEnv: "DEEPSEEK_API_KEY" };
  }

  async saveProviderKey(_apiKey: string) {
    this.setupDone = true;
  }

  // The subagent runs somewhere cheaper; everything else rides the main model.
  private assigned: RoleAssignments = {
    planner: "",
    subagent: "deepseek/deepseek-v4-flash",
    guardian: "",
    vision: "",
  };

  async roles(): Promise<RoleAssignments> {
    return this.assigned;
  }

  async setRole(role: string, ref: string) {
    this.assigned = { ...this.assigned, [role]: ref };
  }

  // Two protocols onto one host, plus a second vendor carrying the only model
  // that reads images: the two shapes the picker has to render correctly.
  async models(): Promise<ModelEntry[]> {
    const efforts = ["auto", "low", "high", "max"];
    return [
      {
        ref: "deepseek/deepseek-v4-pro", provider: "deepseek", model: "deepseek-v4-pro",
        kind: "openai", vendor: "api.deepseek.com", keyEnv: "DEEPSEEK_API_KEY", active: true, efforts, effort: "high",
        contextWindow: 131072, price: { input: 2, output: 8, currency: "CNY" },
      },
      {
        ref: "deepseek-anthropic/deepseek-v4-pro", provider: "deepseek-anthropic",
        model: "deepseek-v4-pro", kind: "anthropic", vendor: "api.deepseek.com", keyEnv: "DEEPSEEK_API_KEY",
        efforts, effort: "high", contextWindow: 131072,
      },
      {
        ref: "deepseek/deepseek-v4-flash", provider: "deepseek", model: "deepseek-v4-flash",
        kind: "openai", vendor: "api.deepseek.com", keyEnv: "DEEPSEEK_API_KEY", efforts, effort: "high",
        contextWindow: 131072, price: { input: 0.5, output: 2, currency: "CNY" },
      },
      {
        ref: "kimi/kimi-k2-vision", provider: "kimi", model: "kimi-k2-vision",
        kind: "openai", vendor: "api.moonshot.cn", keyEnv: "KIMI_API_KEY", vision: true, contextWindow: 262144,
      },
      {
        ref: "myrelay/gpt-4o", provider: "myrelay", model: "gpt-4o", kind: "openai",
        vendor: "relay.example.com", keyEnv: "MYRELAY_API_KEY", vision: true, contextWindow: 131072,
      },
      {
        ref: "myrelay/claude-sonnet-4", provider: "myrelay", model: "claude-sonnet-4", kind: "openai",
        vendor: "relay.example.com", keyEnv: "MYRELAY_API_KEY", contextWindow: 200000,
      },
      {
        ref: "myrelay-work/gpt-4o", provider: "myrelay-work", model: "gpt-4o", kind: "openai",
        vendor: "relay.example.com", keyEnv: "MYRELAY_WORK_API_KEY", contextWindow: 131072,
      },
    ];
  }

  private mem: MemoryEntry[] = MEMORIES.map((m) => ({ ...m }));

  // A fortnight with the shape a real one has: a couple of heavy days, a
  // quiet stretch, and an early span the recorder priced before cost was
  // persisted — those days carry tokens and no cost, which the panel must not
  // render as free.
  async usage(days: number): Promise<UsageReport> {
    const shape = USAGE_TOKENS;
    const priced = USAGE_PRICED;
    // Index against the full arrays, not the slice: taking 7 of 13 days shifted
    // both the dates and the cost column by six.
    const from = shape.length - Math.min(days, shape.length);
    const daily = shape.slice(from).map((total, i) => {
      const day = from + i;
      const at = new Date(Date.now() - (shape.length - 1 - day) * 864e5).toISOString().slice(0, 10);
      const amount = priced[day];
      return {
        day: at, total,
        byModel: (total ? { "deepseek/deepseek-v4-flash": total } : {}) as Record<string, number>,
        byProvider: (total ? { deepseek: total } : {}) as Record<string, number>,
        requests: Math.round(total / 20_000), turns: Math.round(total / 150_000),
        cacheHit: Math.round(total * 0.92), cacheMiss: Math.round(total * 0.08),
        cost: amount ? [{ amount, currency: "CNY" }] : undefined,
      };
    });
    const tokens = daily.reduce((a, d) => a + d.total, 0);
    return {
      from: daily[0]?.day ?? "", to: daily.at(-1)?.day ?? "",
      tokens, requests: daily.reduce((a, d) => a + d.requests, 0),
      turns: daily.reduce((a, d) => a + d.turns, 0),
      cache_hit: daily.reduce((a, d) => a + d.cacheHit, 0),
      cache_miss: daily.reduce((a, d) => a + d.cacheMiss, 0),
      cost: [{ amount: "10.4882", currency: "CNY" }],
      active_days: daily.filter((d) => d.total > 0).length,
      top_model: "deepseek/deepseek-v4-flash", top_provider: "deepseek",
      daily,
      models: [
        { model: "deepseek/deepseek-v4-flash", provider: "deepseek", tokens: Math.round(tokens * 0.597), percent: 59.7 },
        { model: "deepseek-flash/deepseek-v4-flash", provider: "deepseek-flash", tokens: Math.round(tokens * 0.401), percent: 40.1 },
        { model: "deepseek/deepseek-v4-pro", provider: "deepseek", tokens: Math.round(tokens * 0.002), percent: 0.2 },
      ],
      providers: [
        { provider: "deepseek", tokens: Math.round(tokens * 0.599), percent: 59.9 },
        { provider: "deepseek-flash", tokens: Math.round(tokens * 0.401), percent: 40.1 },
      ],
    };
  }

  async memories(): Promise<MemoryCatalog> {
    return {
      memories: this.mem.map((m) => ({ ...m })),
      recallQuery: "缓存命中率为什么会掉",
      indexPath: "~/.reasonix/projects/reasonix/memory/MEMORY.md",
    };
  }

  // The fixture reports a clean revert on the first ask and a conflicted one
  // for a path that says so, since the conflict branch is the half a reader
  // cannot reach by accident.
  async prepareFileRevert(path: string): Promise<RewindPlan> {
    const clash = /conflict|冲突/.test(path);
    return {
      planId: "file-" + path, turn: 0, coverage: "full",
      canFiles: true, canConversation: false, fileCount: 1, files: [path],
      requiresConfirmation: clash, path,
      conflicts: clash ? [{ path, reason: "edited since the checkpoint", currentExisted: true }] : undefined,
    };
  }
  async commitFileRevert(): Promise<RewindResult> {
    return { ok: true, undoAvailable: true, written: [], deleted: [] };
  }

  async saveMemory(edit: MemoryEdit) {
    const at = this.mem.findIndex((m) => m.name === edit.name);
    if (at >= 0) {
      // Keep what it replaced, the way the store does: a history panel driven
      // by a fixture that overwrites would show one entry and prove nothing.
      this.memPast[edit.name] = [...(this.memPast[edit.name] ?? [this.mem[at]]), this.mem[at]];
      this.mem[at] = { ...this.mem[at], title: edit.title, description: edit.description,
        body: edit.body, activation: edit.activation, revision: (this.mem[at].revision ?? 1) + 1,
        updatedAt: new Date().toISOString().slice(0, 10) };
    }
  }

  private memPast: Record<string, MemoryEntry[]> = {};

  async memoryRevisions(name: string): Promise<MemoryEntry[]> {
    const current = this.mem.find((m) => m.name === name);
    const past = this.memPast[name] ?? [];
    const all = current ? [...past, current] : past;
    const seen = new Set<number>();
    return all
      .filter((m) => !seen.has(m.revision ?? 1) && seen.add(m.revision ?? 1) !== undefined)
      .sort((a, b) => (b.revision ?? 1) - (a.revision ?? 1))
      .map((m) => ({ ...m }));
  }

  // Restoring appends a revision rather than rewinding to one, so the entry a
  // reader was just looking at is still there afterwards.
  async restoreMemory(name: string, revision: number) {
    const at = this.mem.findIndex((m) => m.name === name);
    const old = (await this.memoryRevisions(name)).find((m) => (m.revision ?? 1) === revision);
    if (at < 0 || !old) return;
    this.memPast[name] = [...(this.memPast[name] ?? []), this.mem[at]];
    this.mem[at] = { ...this.mem[at], title: old.title, description: old.description,
      body: old.body, activation: old.activation, revision: (this.mem[at].revision ?? 1) + 1,
      updatedAt: new Date().toISOString().slice(0, 10) };
  }

  async forgetMemory(name: string) {
    this.mem = this.mem.filter((m) => m.name !== name);
  }

  async versions(): Promise<VersionHub> {
    return { current: "dev", pinned: "", stalePin: false, latest: "", newer: false, versions: [] };
  }

  private welcomed = true;
  async welcomeSeen(): Promise<boolean> { return this.welcomed; }
  async markWelcomed(): Promise<void> { this.welcomed = true; }

  async pinVersion(): Promise<void> {}

  async goToVersion(): Promise<void> {
    throw new Error("演示模式不会真的安装版本");
  }

  onUpdateProgress(): () => void {
    return () => {};
  }

  // Both of these are the shell reporting on itself; the fixture has no shell.
  onDroppedPaths(): () => void {
    return () => {};
  }

  async account(): Promise<AccountState> {
    return { signedIn: false };
  }

  async accountLogin(): Promise<DeviceGrant> {
    throw new Error("没有内核，无法登录");
  }

  async accountPoll(): Promise<{ status: "pending" | "complete" }> {
    return { status: "pending" };
  }

  async accountLogout(): Promise<void> {}

  async workspaces(): Promise<WorkspaceInfo> {
    return {
      current: this.state.workspaceRoot ?? "",
      canSwitch: true,
      canIsolate: true,
      recents: [
        { path: "~/projects/reasonix-site", name: "reasonix-site" },
        { path: "~/work/notes", name: "notes" },
      ],
    };
  }

  async setWorkspace(path: string) {
    this.state.workspaceRoot = path;
    this.state.cwd = path;
    await this.newSession();
  }

  async isolateWorkspace() {
    await this.setWorkspace((this.state.workspaceRoot ?? "") + " (隔离副本)");
  }

  async openExternal(url: string) {
    window.open(url, "_blank", "noopener,noreferrer");
  }

  // A fixture has no native picker at all, which is what null says.
  async pickFolder(): Promise<string | null> {
    return null;
  }

  // A demo shell has no workspace to read and no kernel to ask, so the fixture
  // answers from a tree of its own. Deliberately the short version of the
  // grammar: enough to drive the menu, never the place to look up what "@" or
  // "/" mean — that answer lives in control.Complete.
  private tree = [
    "REASONIX.md",
    "go.mod",
    "internal/control/complete.go",
    "internal/control/controller.go",
    "internal/serve/serve.go",
    "desktop/frontend-next/src/ui/Composer.tsx",
    "desktop/frontend-next/src/ui/Completion.tsx",
  ];

  private builtins: CompletionItem[] = [
    { label: "/compact", insert: "/compact ", hint: "压缩上下文，保留结论", kind: "builtin" },
    { label: "/context", insert: "/context", hint: "看这一会话的上下文占用", kind: "builtin" },
    { label: "/clear", insert: "/clear", hint: "清空上下文，留在同一会话", kind: "builtin" },
    { label: "/rewind", insert: "/rewind", hint: "回到某一轮之前", kind: "builtin" },
    { label: "/model", insert: "/model ", hint: "换模型", descend: true, kind: "builtin" },
    { label: "/memory", insert: "/memory ", hint: "看和管这个项目记住的事", descend: true, kind: "builtin" },
  ];

  private commands: CompletionItem[] = [
    { label: "/commit", insert: "/commit ", hint: "把当前改动整理成一条提交", kind: "command" },
    { label: "/review", insert: "/review ", hint: "复核这一轮改动，给出严重度分级", kind: "subagent" },
    { label: "/init", insert: "/init ", hint: "为这个仓库生成一份项目说明", kind: "skill" },
    { label: "/security-review", insert: "/security-review ", hint: "只读地过一遍安全面", kind: "subagent" },
  ];

  async complete(line: string, cursor: number): Promise<Completion> {
    const before = line.slice(0, cursor);
    const at = before.lastIndexOf("@");
    if (at >= 0 && !/\s/.test(before.slice(at + 1)) && (at === 0 || /\s/.test(line[at - 1]))) {
      const rest = line.slice(at + 1).search(/\s/);
      const to = rest < 0 ? line.length : at + 1 + rest;
      const frag = before.slice(at + 1);
      const dir = frag.includes("/") ? frag.slice(0, frag.lastIndexOf("/") + 1) : "";
      const names = new Set<string>();
      for (const path of this.tree) {
        if (!path.startsWith(dir)) continue;
        const rel = path.slice(dir.length);
        const cut = rel.indexOf("/");
        names.add(cut < 0 ? rel : rel.slice(0, cut + 1));
      }
      const items = [...names]
        .filter((n) => n.startsWith(frag.slice(dir.length)))
        .sort((a, b) => Number(b.endsWith("/")) - Number(a.endsWith("/")))
        .map((n) => ({
          label: n,
          insert: "@" + dir + n,
          descend: n.endsWith("/"),
          kind: n.endsWith("/") ? "dir" : "file",
        }));
      return { kind: "ref", from: at, to, query: frag.slice(dir.length), items };
    }
    if (line.startsWith("/") && !/\s/.test(line)) {
      const q = line.toLowerCase();
      const items = [...this.builtins, ...this.commands].filter((it) => it.label.toLowerCase().startsWith(q));
      return { kind: "slash", from: 0, to: line.length, query: line, items };
    }
    return { kind: "", from: 0, to: 0, items: [] };
  }

  // Matches the kernel: the shell mints no session file at launch, so the list
  // is empty until a turn creates one. A static entry here is why a rail that
  // never refetched looked fine in dev and was blank in the real app.
  async sessions(): Promise<SessionEntry[]> {
    if (!this.session) return [];
    return [{ ...this.session, current: true }];
  }

  async newSession() {
    this.log = [];
    this.at = 0;
    this.state.goal = "";
    this.session = null;
    this.state.sessionPath = undefined;
  }

  async deleteSession(_name: string) {}

  async status() {
    return { ...this.state };
  }

  async balance(): Promise<WalletReading | null> {
    return {
      display: "¥110.00",
      available: true,
      stale: false,
      fetchedAt: new Date().toISOString(),
      lines: [{ currency: "CNY", total: "¥110.00", granted: "¥10.00" }],
    };
  }

  // No host behind the fixture, so a paste resolves to a token nothing reads.
  async attach(): Promise<Attachment> {
    return { path: ".reasonix/attachments/mock.png", ref: "@.reasonix/attachments/mock.png" };
  }

  async dropRefs(paths: string[]): Promise<DroppedRef[]> {
    return paths.map((path) => ({ ref: "@" + path, path }));
  }

  // The tree the scripted transcript is written against. It used to answer
  // repo:false, which made the panel fall back to the transcript — and left
  // every row unopenable, because only git can say what a path differs by.
  async changes(): Promise<WorkspaceChanges> {
    return {
      repo: true,
      changes: [
        { path: "internal/provider/retry.go", status: "M" },
        { path: "internal/config/credentials.go", status: "M" },
        { path: "internal/provider/openai/streaming/chunk_decoder.go", status: "A" },
      ],
    };
  }

  // Scripted, because there is no tree to read: enough of a diff for the
  // preview to be worked on without a kernel behind the window.
  async changeDiff(path: string): Promise<ChangeDiff> {
    return {
      path,
      diff: [
        `--- a/${path}`,
        `+++ b/${path}`,
        "@@ -1,6 +1,7 @@",
        " package permission",
        " ",
        "-func allows(cmd string) bool {",
        "-	return strings.Contains(cmd, \"rm -rf\")",
        "+func allows(cmd string) bool {",
        "+	ast, err := shellparse.Parse(cmd)",
        "+	if err != nil {",
        "+		return false",
        "+	}",
        " }",
      ].join("\n"),
      truncated: false,
    };
  }

  async trajectory(): Promise<WireEvent[]> {
    return [];
  }

  async executionGraph(): Promise<ExecutionGraphRead> {
    return mockExecutionGraph(this.log, this.session?.name ?? "", this.seq);
  }

  async history(): Promise<HistoryMessage[]> {
    return [];
  }

  // Mock mode has to be able to show the rewind entry, so every prompt it has
  // seen becomes a checkpoint the way the kernel opens one per user turn.
  async checkpoints(): Promise<Checkpoint[]> {
    return this.prompts.map((prompt, i) => ({ turn: i, prompt, files: i === 0 ? 0 : 3 }));
  }

  // The second prompt onwards is scripted to have run bash, so mock mode can
  // show the consent stage the real kernel demands on partial coverage.
  async prepareRewind(turn: number, scope: RewindScope): Promise<RewindPlan> {
    const partial = turn > 0;
    return {
      planId: `mock-plan-${turn}-${scope}`,
      turn,
      coverage: partial ? "partial" : "full",
      coverageGaps: partial
        ? [{ reason: "bash_side_effect", detail: "bash side effects are not path-tracked", tool: "bash" }]
        : undefined,
      canFiles: true,
      canConversation: true,
      files: ["note.txt"],
      fileCount: turn > 0 ? 3 : 0,
      requiresConfirmation: partial,
    };
  }

  async commitRewind(planId: string): Promise<RewindResult> {
    const turn = Number(planId.split("-")[2] ?? 0);
    this.undone = this.prompts.slice();
    this.prompts = this.prompts.slice(0, turn);
    return { ok: true, transactionId: `mock-tx-${turn}`, undoAvailable: true, deleted: ["note.txt"] };
  }

  async undoRewind(_transactionId: string): Promise<void> {
    if (this.undone) this.prompts = this.undone;
  }

  subscribe(onEvent: (ev: WireEvent) => void, _onGap?: () => void, bootstrap?: () => Promise<number>) {
    this.listeners.add(onEvent);
    if (bootstrap) {
      this.hold.begin();
      void bootstrap().finally(() => this.hold.release((ev) => this.listeners.forEach((l) => l(ev))));
    }
    return () => this.listeners.delete(onEvent);
  }

  private emit(ev: WireEvent) {
    const numbered = { ...ev, seq: ++this.seq };
    // Recorded either way: a snapshot read mid-flight answers with this frame
    // folded in, which is what makes the held copy a duplicate to drop.
    this.log.push(numbered);
    if (!this.hold.take(numbered)) this.listeners.forEach((l) => l(numbered));
  }

  private step = () => {
    if (this.gated || !this.state.running) return;
    const beat = SCRIPT[this.at];
    if (!beat) {
      this.state.running = false;
      return;
    }
    this.at += 1;
    this.emit(beat.ev);
    if (beat.ev.kind === "turn_done") {
      this.state.running = false;
      return;
    }
    if (this.openGate(beat.ev)) return;
    const next = SCRIPT[this.at];
    if (next) this.timer = window.setTimeout(this.step, next.wait);
  };

  /** openGate records that this frame stopped the run for a person, and answers
   *  whether it did. The event stream and /status are two views of one fact: a
   *  fixture that raises the card and leaves the decision list empty lets a
   *  guard measure the card while the state under it says the turn is moving.
   *  Driver-fed frames go through here for the same reason scripted ones do. */
  protected openGate(ev: WireEvent): boolean {
    if (ev.kind !== "approval_request" && ev.kind !== "ask_request") return false;
    this.gated = true;
    this.state.decisions = [
      ev.kind === "ask_request"
        ? { id: ev.ask?.id ?? "ask", kind: "ask" as const }
        : { id: ev.approval?.id ?? "apv", kind: approvalKind(ev) },
    ];
    return true;
  }

  private ungate() {
    if (!this.gated) return;
    this.gated = false;
    this.state.decisions = [];
    this.state.running = true;
    const next = SCRIPT[this.at];
    if (next) this.timer = window.setTimeout(this.step, next.wait);
  }

  // The real kernel holds a mid-turn line until the next tool boundary, and
  // that wait is the only window in which taking it back means anything. A
  // fixture that echoed it at once made the state undesignable.
  async steer(text: string): Promise<Queued> {
    const itemId = `inbox-${this.queued.size + 1}-${Date.now()}`;
    const at = window.setTimeout(() => {
      // Three states, not two. The kernel marks the line consumed when the turn
      // reads it and only sweeps it at AckDequeue, one turn boundary later —
      // a fixture that dropped it here left that whole window undesignable.
      this.markQueued(itemId, "steer_consumed");
      this.emit({ kind: "steer", text });
      this.queued.set(itemId, window.setTimeout(() => this.dropQueued(itemId), 4000));
    }, 4000);
    this.queued.set(itemId, at);
    this.addQueued(itemId, "steer", text);
    return { itemId, disposition: "steer_accepted" };
  }

  private markQueued(itemId: string, state: QueueItem["state"]) {
    this.queueItems = this.queueItems.map((i) => (i.id === itemId ? { ...i, state } : i));
    this.bumpQueue();
  }

  private addQueued(itemId: string, intent: QueueItem["intent"], text: string) {
    this.queueBodies.set(itemId, text);
    this.queueItems.push({
      id: itemId,
      intent,
      state: this.queuePaused ? "queued" : "steer_accepted",
      preview: text.slice(0, 120),
      createdAt: new Date().toISOString(),
    });
    this.bumpQueue();
  }

  private dropQueued(itemId: string) {
    this.queueItems = this.queueItems.filter((i) => i.id !== itemId);
    this.queueBodies.delete(itemId);
    this.bumpQueue();
  }

  // Every queue mutation announces itself the way the kernel does, so the panel
  // is exercised by the same one-way path in the fixture and against a kernel.
  private bumpQueue() {
    this.queueRevision++;
    this.emit({ kind: "inbox_changed" });
  }

  // The fixture has a window with an icon, because the state worth designing
  // is the one where both switches mean something.
  private tray: TrayPrefs = { icon: true, live: true, closeToTray: false };

  async trayPrefs(): Promise<TrayPrefs | null> {
    return { ...this.tray };
  }

  // live never follows icon down: the icon this launch got stays until the
  // launch ends, which is the state the panel has to be able to describe.
  async setTrayPrefs(icon: boolean, closeToTray: boolean): Promise<TrayPrefs | null> {
    this.tray = { icon, live: this.tray.live, closeToTray: closeToTray && icon && this.tray.live };
    return { ...this.tray };
  }

  // The fixture never refuses /submit, so this is only reachable from a test —
  // but the port is a contract and half of one is not a contract.
  async queueFollowup(text: string): Promise<Queued> {
    const itemId = `inbox-followup-${Date.now()}`;
    this.addQueued(itemId, "followup", text);
    return { itemId, disposition: "queued_followup" };
  }

  async queue(): Promise<Queue> {
    return {
      revision: this.queueRevision,
      paused: this.queuePaused,
      items: this.queueItems.map((i) => ({ ...i })),
      capacity: { items: this.queueItems.length, maxItems: 64, bytes: 0, maxBytes: 64 << 20 },
    };
  }

  // The fixture keeps the whole line, so what comes back is what went in —
  // which is the point being modelled: a preview is not the text. An entry the
  // fixture no longer holds refuses the way the kernel does, so the failure a
  // reader can actually hit is reachable here too.
  async readQueued(itemId: string) {
    const body = this.queueBodies.get(itemId);
    if (body === undefined) throw new HttpError(404, "no such entry", { code: "inbox.not_found" });
    return body;
  }

  async editQueued(itemId: string, text: string) {
    const item = this.queueItems.find((i) => i.id === itemId);
    if (!item) throw new HttpError(404, "no such entry", { code: "inbox.not_found" });
    item.preview = text.slice(0, 120);
    this.queueBodies.set(itemId, text);
    this.bumpQueue();
  }

  async moveQueued(itemId: string, toIndex: number) {
    const from = this.queueItems.findIndex((i) => i.id === itemId);
    if (from < 0) throw new HttpError(404, "no such entry", { code: "inbox.not_found" });
    const [item] = this.queueItems.splice(from, 1);
    this.queueItems.splice(Math.max(0, Math.min(toIndex, this.queueItems.length)), 0, item);
    this.bumpQueue();
  }

  // Nothing in the fixture blocks, so nothing can be retried out of it. The
  // port is still a contract, and half of one is not a contract.
  async retryQueued(itemId: string) {
    void itemId;
  }

  async refreshQueued(itemId: string) {
    void itemId;
  }

  // Pausing holds the timers rather than cancelling them: what was said still
  // waits, which is the state worth designing against.
  async setQueuePaused(paused: boolean) {
    this.queuePaused = paused;
    for (const at of this.queued.values()) window.clearTimeout(at);
    if (paused) this.queued.clear();
    this.bumpQueue();
  }

  async cancelQueued(itemId: string) {
    const at = this.queued.get(itemId);
    // Refused the way the kernel refuses it: by then the turn has read it, and
    // there is nothing left to take back.
    if (at === undefined) throw new HttpError(409, "already applied", { code: "steer.already_applied" });
    window.clearTimeout(at);
    this.queued.delete(itemId);
  }

  async submit(text: string) {
    if (this.state.running) {
      this.emit({ kind: "steer", text });
      return;
    }
    this.prompts.push(text);
    // The first turn is what puts the session on disk. serve answers with a
    // truncated first message straight away and swaps in the generated title
    // once the background job lands, so the rail is never left blank.
    if (!this.session) {
      const name = "20260813-004512.881204300-deepseek-v4-pro";
      this.session = { name, path: `/sessions/${name}.jsonl`, title: text.slice(0, 47), turns: 0 };
      this.state.sessionPath = this.session.path;
      setTimeout(() => this.session && (this.session.title = "定位仓库里失败的测试"), 2500);
    }
    this.session.turns = (this.session.turns ?? 0) + 1;
    // Not the goal: the kernel only sets that from /goal or plan mode. Echoing
    // the prompt into it made the header read the same text twice.
    this.state.running = true;
    this.at = 0;
    this.emit({ kind: "message", text, itemId: "user" });
    this.timer = window.setTimeout(this.step, SCRIPT[0].wait);
  }

  async cancel() {
    window.clearTimeout(this.timer);
    this.state.running = false;
  }

  async resume(_path: string) {
    this.log = [];
    this.at = 0;
  }

  // 三条出路是三个不同的内核状态转移，不是「批准/拒绝」。只有开始执行把计划
  // 带进执行、因而离开计划模式；改计划留在里面。真机上这个标志由 /status 说，
  // 台架不建模它，靠它判断的守卫就只能永远红 —— 那正是它红了的原因。
  async planDecision(_id: string, action: PlanAction) {
    if (action !== "revise") this.state.plan = false;
    this.ungate();
  }

  async approve(_id: string, verdict: ApprovalVerdict) {
    if (verdict === "deny") {
      this.state.running = false;
      this.gated = false;
      return;
    }
    if (verdict === "always") this.state.toolApprovalMode = "dontAsk";
    this.ungate();
  }

  async answer(_id: string, _answers: { questionId: string; selected: string[] }[]) {
    this.ungate();
  }

  async invokeExtensionAction(name: string) {
    return `${name} 在 mock 里没有真实扩展可执行`;
  }
  async submitExtensionForm(_pluginId: string, _surfaceId: string, _values: Record<string, unknown>) {}

  async setPlanMode(on: boolean) {
    this.state.plan = on;
  }
  async setApprovalMode(mode: ApprovalMode) {
    this.state.toolApprovalMode = mode;
  }
  async setPreset(preset: Preset) {
    this.state.preset = preset;
  }
  async storage() {
    return mockStorage();
  }

  async planStorageMove(root: string, dir: string) {
    return mockStoragePlan(root, dir);
  }

  async moveStorage(root: string, dir: string) {
    return mockStoragePlan(root, dir);
  }

  async setModel(ref: string) {
    this.state.modelRef = ref;
    this.state.label = ref.split("/").pop() ?? ref;
  }
  async compaction(): Promise<CompactionSettings> {
    return { soft_limit_tokens: 0, default_soft_limit: 160000, ratio: 0.85, context_window: 128000, trigger: 108800, path: "~/.reasonix/config.toml" };
  }
  async saveCompaction(softLimitTokens: number): Promise<CompactionSettings> {
    const s = await this.compaction();
    return { ...s, soft_limit_tokens: softLimitTokens };
  }
  async setEffort(effort: string) {
    this.state.effort = effort;
  }
  async setGoal(text: string) {
    this.state.goal = text;
  }
}

// Which owner answers a pending approval. The kernel decides it from the
// approval itself — a recovery gate, the plan tool, otherwise the tool gate —
// and the fixture reads the same fields the card does rather than inventing a
// third rule.
function approvalKind(ev: WireEvent): "plan_approval" | "recovery_approval" | "tool_approval" {
  const a = ev.approval;
  if (a?.kind === "plan" || a?.tool === "exit_plan_mode") return "plan_approval";
  if (a?.kind === "recovery") return "recovery_approval";
  return "tool_approval";
}
