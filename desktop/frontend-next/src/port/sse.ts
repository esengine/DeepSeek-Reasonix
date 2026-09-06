import { PLAN_ACTIONS, type PlanAction } from "./session";
import type { AccountState, AgentPort, Appearance, CompactionSettings, Completion, DeviceGrant, ProviderProbe, UpdateProgress, VersionHub, ApprovalMode, ApprovalVerdict, Checkpoint, RewindPlan, RewindResult, RewindScope, HistoryMessage, ModelEntry, Preset, ProviderSetup, RoleAssignments, SessionEntry, SessionStatus, WalletReading, HookDryRun, HookEntry, MemoryCatalog, MemoryEdit, MemoryEntry, UsageReport, McpDraft, PluginExport, Queue, Queued, TrayPrefs, WorkspaceInfo } from "./port";
import { HttpError, type Attachment, type ChangeDiff, type DroppedRef, type WorkspaceChanges } from "./port";
import { SseTheme } from "./sse_theme";
import type { WailsBind } from "./wails";
import type { StoragePlan, StorageState } from "./storage";
import type { ExecutionGraphRead, WireEvent } from "./wire";
import { host } from "./host";

// The running project is the default, so its requests stay the bare path they
// have always been and only a cross-project read carries the folder.
// Must match wailsEventName / replayPath in desktop/next.
const WAILS_EVENT = "rx:event";
// Install progress rides its own channel: it is the shell reporting on itself,
// not something the kernel emitted into the conversation.
// Fast enough that a download bar moves, slow enough that an open panel is not
// a load. The read is one small JSON body and answers from memory.
const UPDATE_POLL_MS = 500;
const WAILS_REPLAY = "/rx-replay";

interface WailsBus {
  EventsOn(name: string, cb: (data: string) => void): () => void;
}


// Wails' own drop API: the only channel that reports where a dropped file
// lives. Absent in a browser tab, where a page never learns a path.
interface WailsFileDropBus {
  OnFileDrop(cb: (x: number, y: number, paths: string[]) => void, useDropTarget: boolean): void;
}

// Wails publishes bound methods at window.go.<package>.<Struct>.<Method>; the
// shell's package is main and the struct is App. Absent in a browser tab.

export class SsePort extends SseTheme implements AgentPort {
  private readonly dropSubs = new Set<(paths: string[]) => void>();
  private dropWired = false;

  status() {
    return this.get<SessionStatus>("/status");
  }

  async balance(): Promise<WalletReading | null> {
    const res = await fetch(this.base + "/balance", { credentials: "same-origin" });
    // 204 is a provider that has no wallet at all. Nothing to show, nothing
    // wrong — the failures come back coded instead.
    if (res.status === 204) return null;
    if (!res.ok) await SsePort.fail("/balance", res);
    return (await res.json()) as WalletReading;
  }

  async providerSetup(): Promise<ProviderSetup | null> {
    const res = await fetch(this.base + "/provider-setup", { credentials: "same-origin" });
    if (res.status === 404) return null;
    if (!res.ok) throw new Error(`/provider-setup: ${res.status}`);
    return (await res.json()) as ProviderSetup;
  }

  saveProviderKey(apiKey: string) {
    return this.post("/provider-setup", { apiKey });
  }

  async models() {
    const r = await this.get<{ models?: ModelEntry[] }>("/models");
    return r.models ?? [];
  }

  complete(line: string, cursor: number) {
    const q = new URLSearchParams({ line, cursor: String(cursor) });
    return this.get<Completion>("/complete?" + q);
  }

  async exportPlugin(name: string): Promise<PluginExport> {
    const save = (window as WailsBind).go?.main?.App?.SavePluginExport;
    if (save) {
      const out = await save(name);
      return { required: out.required ?? [], savedTo: out.path || undefined };
    }
    const res = await fetch(this.base + "/plugins/" + encodeURIComponent(name) + "/export", {
      credentials: "same-origin",
    });
    if (!res.ok) throw new Error(`/plugins/${name}/export: ${res.status}`);
    const required = (res.headers.get("X-Reasonix-Required-Env") ?? "").split(",").filter(Boolean);
    const blob = await res.blob();
    // A shell with a save dialog puts the archive where it is asked to; the
    // anchor below is the browser's own way and the only one a tab has.
    const saved = await host().saveBytes(`${name}.zip`, new Uint8Array(await blob.arrayBuffer()));
    if (saved !== null) return { required, savedTo: saved || undefined };
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `${name}.zip`;
    a.click();
    URL.revokeObjectURL(url);
    return { required };
  }

  async dryRunHook(h: HookEntry): Promise<HookDryRun> {
    const res = await fetch(this.base + "/hooks/dry-run", {
      method: "POST",
      headers: { "content-type": "application/json" },
      credentials: "same-origin",
      body: JSON.stringify({ event: h.event, match: h.match, command: h.command, timeout: h.timeout, cwd: h.cwd }),
    });
    const body = (await res.json().catch(() => ({}))) as HookDryRun & { error?: string };
    if (!res.ok) throw new Error(body.error || `/hooks/dry-run: ${res.status}`);
    return body;
  }

  usage(days: number, source?: string) {
    const q = new URLSearchParams({ days: String(days) });
    if (source && source !== "all") q.set("source", source);
    return this.get<UsageReport>("/usage?" + q);
  }
  memories() {
    return this.get<MemoryCatalog>("/memory");
  }

  prepareFileRevert(path: string) {
    return this.post0<RewindPlan>("/rewind/file/prepare", { path });
  }
  commitFileRevert(planId: string, resolution?: string) {
    return this.post0<RewindResult>("/rewind/file/commit", { planId, resolution });
  }
  saveMemory(edit: MemoryEdit) {
    return this.post("/memory/save", edit);
  }
  forgetMemory(name: string) {
    return this.post("/memory/forget", { name });
  }
  async memoryRevisions(name: string) {
    const r = await this.get<{ revisions: MemoryEntry[] }>("/memory/revisions?name=" + encodeURIComponent(name));
    return r.revisions ?? [];
  }
  restoreMemory(name: string, revision: number) {
    return this.post("/memory/restore", { name, revision });
  }

  async reconnectMcp(name: string) {
    const res = await fetch(this.base + "/mcp/reconnect", {
      method: "POST",
      headers: { "content-type": "application/json" },
      credentials: "same-origin",
      body: JSON.stringify({ name }),
    });
    const body = (await res.json().catch(() => ({}))) as { state?: string; tools?: number; error?: string };
    if (!res.ok && !body.error) throw new Error(`/mcp/reconnect: ${res.status}`);
    return { state: body.state ?? (res.ok ? "ready" : "failed"), tools: body.tools, error: body.error };
  }

  async parseMcp(input: string): Promise<McpDraft> {
    const res = await fetch(this.base + "/mcp/parse", {
      method: "POST",
      headers: { "content-type": "application/json" },
      credentials: "same-origin",
      body: JSON.stringify({ input }),
    });
    const body = (await res.json().catch(() => ({}))) as McpDraft & { error?: string };
    if (!res.ok) throw new Error(body.error || `/mcp/parse: ${res.status}`);
    return { servers: body.servers ?? [], risks: body.risks ?? [] };
  }

  async probeProvider(baseUrl: string, apiKey: string): Promise<ProviderProbe> {
    const res = await fetch(this.base + "/providers/probe", {
      method: "POST",
      headers: { "content-type": "application/json" },
      credentials: "same-origin",
      body: JSON.stringify({ baseUrl, apiKey }),
    });
    const text = await res.text();
    if (!res.ok) throw new Error(text.trim() || `/providers/probe: ${res.status}`);
    return JSON.parse(text) as ProviderProbe;
  }

  async welcomeSeen(): Promise<boolean> {
    const res = await this.get<{ seen: boolean }>("/welcome");
    return !!res.seen;
  }

  markWelcomed() {
    return this.post("/welcome", {});
  }

  roles() {
    return this.get<RoleAssignments>("/roles");
  }

  setRole(role: string, ref: string) {
    return this.post("/roles", { role, ref });
  }

  storage() {
    return this.get<StorageState>("/storage");
  }

  // Both answer with a plan: a refused move is reported in the body rather
  // than as a failed request, because its refusals are what the panel shows.
  planStorageMove(root: string, dir: string) {
    return this.post0<StoragePlan>("/storage/plan", { root, dir });
  }

  moveStorage(root: string, dir: string) {
    return this.post0<StoragePlan>("/storage/move", { root, dir });
  }

  // Which build runs belongs to the application, not to a pane: a remote pane's
  // base names another machine, whose version is not the one this window would
  // install. So these two ignore this port's base, the way the tray does.
  async versions(): Promise<VersionHub> {
    const res = await fetch("/studio/versions", { credentials: "same-origin" });
    if (!res.ok) await SsePort.fail("/studio/versions", res);
    return (await res.json()) as VersionHub;
  }

  // The icon belongs to the window, not to a pane, so these two are the one
  // pair that ignores this port's base: a remote pane's base names another
  // machine, and the status icon there is not the one on this screen.
  async trayPrefs(): Promise<TrayPrefs | null> {
    return this.trayCall(await fetch("/tray/prefs", { credentials: "same-origin" }));
  }

  async setTrayPrefs(icon: boolean, closeToTray: boolean): Promise<TrayPrefs | null> {
    return this.trayCall(
      await fetch("/tray/prefs", {
        method: "PUT",
        headers: { "content-type": "application/json" },
        credentials: "same-origin",
        body: JSON.stringify({ icon, closeToTray }),
      }),
    );
  }

  async pinVersion(version: string): Promise<void> {
    const res = await fetch("/studio/pin", {
      method: "POST",
      headers: { "content-type": "application/json" },
      credentials: "same-origin",
      body: JSON.stringify({ version }),
    });
    if (!res.ok) await SsePort.fail("/studio/pin", res);
  }

  // Answered as soon as the move is under way. It cannot be answered when the
  // move is done: an install that worked ends by ending the kernel this asked.
  async goToVersion(version: string): Promise<void> {
    const res = await fetch("/update/install", {
      method: "POST",
      headers: { "content-type": "application/json" },
      credentials: "same-origin",
      body: JSON.stringify({ version }),
    });
    if (!res.ok) await SsePort.fail("/update/install", res);
  }

  // Pulled, not subscribed. Progress is a projection: a missed frame costs
  // nothing the next read does not restore, and the last thing an install does
  // is end the process that would have been streaming it. A subscription would
  // have to promise delivery across the restart it is itself causing.
  onUpdateProgress(cb: (p: UpdateProgress) => void): () => void {
    let stopped = false;
    const read = async () => {
      try {
        const res = await fetch("/update/install", { credentials: "same-origin" });
        if (!stopped && res.ok) cb((await res.json()) as UpdateProgress);
      } catch {
        // The kernel going away mid-install is the successful case, not an
        // error to render: the next launch is what reports the outcome.
      }
    };
    void read();
    const timer = setInterval(read, UPDATE_POLL_MS);
    return () => {
      stopped = true;
      clearInterval(timer);
    };
  }

  // Wails registers its drop listeners once and ignores a second call, so the
  // one subscription is fanned out here. useDropTarget=false because the filter
  // it offers is the wrong one: it hit-tests the drop coordinates against the
  // CSS opt-in, and those coordinates are native pixels, which stop agreeing
  // with CSS pixels the moment the interface is zoomed. The page routes the
  // drop against the DOM instead, where the element under the pointer is a
  // fact rather than an arithmetic result.
  // A connect is blocked while one of these is on screen, so there is no
  // polling to fall back on: no bus means no window, and no window means the
  // kernel refused the question rather than asking it.
  onDroppedPaths(cb: (paths: string[]) => void): () => void {
    const rt = (window as unknown as { runtime?: WailsFileDropBus }).runtime;
    if (!rt?.OnFileDrop) return () => {};
    if (!this.dropWired) {
      this.dropWired = true;
      rt.OnFileDrop((_x, _y, paths) => this.dropSubs.forEach((f) => f(paths ?? [])), false);
    }
    this.dropSubs.add(cb);
    return () => {
      this.dropSubs.delete(cb);
    };
  }

  account() {
    return this.get<AccountState>("/account");
  }

  accountLogin() {
    return this.post0<DeviceGrant>("/account/login");
  }

  // A kernel with no window registers no tray routes at all, which is what a
  // browser tab reads as "there is no icon here" — the same null this panel
  // has always had to render.
  private async trayCall(res: Response): Promise<TrayPrefs | null> {
    if (res.status === 404) return null;
    if (!res.ok) await SsePort.fail("/tray/prefs", res);
    return (await res.json()) as TrayPrefs;
  }

  async accountPoll(deviceCode: string) {
    const res = await fetch(this.base + "/account/poll", {
      method: "POST",
      headers: { "content-type": "application/json" },
      credentials: "same-origin",
      body: JSON.stringify({ deviceCode }),
    });
    if (!res.ok) throw new Error(await res.text());
    return (await res.json()) as { status: "pending" | "complete"; slowDown?: boolean };
  }

  accountLogout() {
    return this.post("/account/logout");
  }

  workspaces() {
    return this.get<WorkspaceInfo>("/workspaces");
  }

  setWorkspace(path: string) {
    return this.post("/workspace", { path });
  }

  isolateWorkspace() {
    return this.post("/workspace", { isolate: true });
  }

  openExternal(url: string): Promise<void> {
    host().openExternal(url);
    return Promise.resolve();
  }

  async pickFolder(): Promise<string | null> {
    // Which workspace is running is the kernel's to answer, so it is read here
    // and handed to the shell rather than kept by either shell -- a picker that
    // opened on a folder the window remembered would be a second copy of it.
    const startIn = await this.workspaces()
      .then((w) => w.current)
      .catch(() => "");
    return host().pickFolder(startIn);
  }

  sessions() {
    return this.get<SessionEntry[]>("/sessions");
  }

  // /resume swaps the session file on the same controller — serve drives one
  // session at a time by design ("multiple browser tabs share it").
  resume(path: string) {
    return this.post("/resume", { path });
  }

  newSession() {
    return this.post("/new");
  }

  deleteSession(name: string) {
    return this.post("/delete-session", { name });
  }

  // JSON, not raw bytes: csrfGuard admits nothing else, and that guard is what
  // stops a cross-site form posting here at all.
  async attach(blob: Blob, name?: string) {
    const buf = new Uint8Array(await blob.arrayBuffer());
    let bin = "";
    for (let i = 0; i < buf.length; i += 0x8000) bin += String.fromCharCode(...buf.subarray(i, i + 0x8000));
    const res = await fetch(this.base + "/attachments", {
      method: "POST",
      headers: { "content-type": "application/json" },
      credentials: "same-origin",
      body: JSON.stringify({ mime: blob.type, name: name ?? "", data: btoa(bin) }),
    });
    if (!res.ok) throw new HttpError(res.status, `/attachments: ${res.status} ${await res.text()}`);
    return (await res.json()) as Attachment;
  }

  dropRefs(paths: string[]) {
    return this.post0<DroppedRef[]>("/drop", { paths });
  }

  changes() {
    return this.get<WorkspaceChanges>("/changes");
  }

  changeDiff(path: string) {
    return this.get<ChangeDiff>(`/changes/diff?path=${encodeURIComponent(path)}`);
  }

  trajectory() {
    return this.get<WireEvent[]>("/trajectory");
  }

  executionGraph() {
    return this.get<ExecutionGraphRead>("/execution-graph");
  }

  history() {
    return this.get<HistoryMessage[]>("/history");
  }

  checkpoints() {
    return this.get<Checkpoint[]>("/checkpoints");
  }

  prepareRewind(turn: number, scope: RewindScope) {
    return this.post0<RewindPlan>("/rewind/prepare", { turn, scope });
  }

  commitRewind(planId: string) {
    return this.post0<RewindResult>("/rewind/commit", { planId });
  }

  undoRewind(transactionId: string) {
    return this.post("/rewind/undo", { transactionId });
  }

  /** subscribe delivers the kernel's events, in order, with no holes it did not
   *  announce. Frames that matter are numbered, so a break in the numbers is
   *  the client noticing it missed something rather than a gap it renders as a
   *  quiet turn — and the two transports differ only in how they fetch the
   *  missing frames back, not in what a gap means.
   *
   *  onGap fires when the replay log can no longer close one. The hole is the
   *  transport's fact; which authority each read model returns to — /history,
   *  the run graph — is the caller's.
   *
   *  bootstrap is how a read model with a snapshot of its own joins without a
   *  seam: it reads one and answers with the frame that snapshot is at least as
   *  new as. Frames are held from before the transport is attached until the
   *  replay after that number has landed, so nothing between the two is
   *  skipped. The number is the whole stream's, never one model's — a frame of
   *  any kind carries the cursor past it, or the next number reads as a hole. */
  subscribe(onEvent: (ev: WireEvent) => void, onGap?: () => void, bootstrap?: () => Promise<number>) {
    // Frames arriving while a recovery request is in flight wait for it: the
    // whole point is that the reducer sees one ordered stream, and delivering
    // the new frame first would put a result ahead of the dispatch it answers.
    // null until the stream states a position: 0 cannot tell a subscriber that
    // just attached from one that has seen the stream start.
    let seen: number | null = null;
    // A bootstrapping subscription holds from before the transport is attached:
    // the shell's bus has no connection to open, so it is already delivering,
    // and a frame taken live here would be folded ahead of the replay's.
    let booting = !!bootstrap;
    let recovering = booting;
    let held: WireEvent[] = [];
    let live = true;

    const deliver = (ev: WireEvent) => {
      // The cursor is the record of what has been folded, and the replay and
      // the live stream reach the same frames — a snapshot's own watermark
      // included. Whichever arrives second is the same fact twice.
      if (ev.seq && seen !== null && ev.seq <= seen) return;
      if (ev.seq) seen = Math.max(seen ?? 0, ev.seq);
      onEvent(ev);
    };

    const flush = () => {
      recovering = false;
      const queued = held;
      held = [];
      for (const ev of queued) deliver(ev);
    };

    const recover = async (after: number) => {
      recovering = true;
      try {
        const res = await fetch(`${this.base}/events/replay?lastEventId=${after}`, { credentials: "same-origin" });
        if (!res.ok) throw new Error(String(res.status));
        const body = (await res.json()) as { frames?: WireEvent[]; complete?: boolean };
        if (!live) return;
        for (const ev of body.frames ?? []) deliver(ev);
        if (!body.complete) onGap?.();
      } catch {
        // The frames are gone and asking again would not bring them back. The
        // transcript is the one source that can still answer.
        if (live) onGap?.();
      } finally {
        flush();
      }
    };

    // What the snapshot and the replay overlap on folds twice onto the same
    // state; what falls between them is gone for good. So the read names where
    // it stands, and the stream resumes from there — one number, one stream.
    const bootstrapFrom = async (read: () => Promise<number>) => {
      let at: number | null = null;
      try {
        at = await read();
      } catch {
        // Nothing to resume onto. Holding the stream any longer would stall the
        // view rather than repair it, so it runs live and says it is behind.
        if (live) onGap?.();
      }
      if (!live) return;
      if (at === null) {
        booting = false;
        flush();
        return;
      }
      seen = at;
      booting = false;
      await recover(at);
    };

    const accept = (ev: WireEvent) => {
      // The stream describing itself: a watermark states the number the client
      // should have reached, which is the only way to notice a frame lost at
      // the end of a turn. Neither reaches the reducer.
      if (ev.kind === "stream_watermark") {
        // The first one states where this subscriber attached: a live-only
        // subscription replays nothing before it, so nothing before it was lost.
        // A bootstrap states that position from its snapshot instead, and taking
        // it from here would baseline the cursor over frames already held.
        if (seen === null) {
          if (!booting) seen = ev.seq ?? 0;
          return;
        }
        if (!recovering && ev.seq && ev.seq > seen) void recover(seen);
        return;
      }
      if (ev.kind === "stream_gap") {
        // The hole reaches the caller either way — it is the whole stream's,
        // not the bootstrapping model's — but the number it carries is not a
        // position this subscriber reached.
        if (!booting) seen = Math.max(seen ?? 0, ev.seq ?? 0);
        onGap?.();
        return;
      }
      if (recovering) {
        held.push(ev);
        return;
      }
      // Numbering that goes backwards is a different stream, not a gap: the
      // server restarted and is counting from one again. Inferred rather than
      // carried as a stream id — a resumed client is never sent a lower number
      // than it holds, so nothing else produces this. Without it the client
      // compares against a watermark that will never be reached again and
      // silently stops noticing losses.
      if (ev.seq && seen && ev.seq < seen) {
        seen = ev.seq;
        onGap?.();
        onEvent(ev);
        return;
      }
      if (ev.seq && seen && ev.seq > seen + 1) {
        held.push(ev);
        void recover(seen);
        return;
      }
      deliver(ev);
    };

    const feed = (raw: string) => {
      try {
        accept(JSON.parse(raw) as WireEvent);
      } catch {
        // A malformed frame must not tear down the stream.
      }
    };
    // Wails' asset server buffers a response until its handler returns, so the
    // SSE stream never reaches the page inside the shell. There it pushes the
    // same frames over its own bus; the payload is identical.
    const bus = (window as unknown as { runtime?: WailsBus }).runtime;
    let detach: () => void;
    if (bus?.EventsOn) {
      const off = bus.EventsOn(this.rt ? `${WAILS_EVENT}:${this.rt}` : WAILS_EVENT, feed);
      // Subscribing to the bus is not the handshake /events is: ask the shell
      // to replay whatever prompt is already waiting for an answer.
      void fetch(this.base + WAILS_REPLAY, { method: "POST" }).catch(() => {});
      detach = off;
    } else {
      // EventSource resumes on its own: it reconnects carrying the last id it
      // saw, and the server replays from there. Recovery here is for the frames
      // shed without the connection ever dropping.
      const es = new EventSource(this.base + "/events", { withCredentials: true });
      es.onmessage = (m) => feed(m.data);
      detach = () => es.close();
    }
    if (bootstrap) void bootstrapFrom(bootstrap);
    return () => {
      live = false;
      detach();
    };
  }

  submit(text: string) {
    return this.post("/submit", { input: text });
  }
  steer(text: string) {
    return this.post0<Queued>("/inbox/items", { input: text, intent: "steer" });
  }
  queueFollowup(text: string) {
    return this.post0<Queued>("/inbox/items", { input: text, intent: "followup" });
  }
  async cancelQueued(itemId: string) {
    await this.del("/inbox/items/" + encodeURIComponent(itemId));
  }
  async queue() {
    const q = await this.get<Queue>("/inbox");
    // An empty queue arrives as null, not []: that is what a Go nil slice
    // encodes to. Normalising here is what keeps every reader from having to
    // know it — and one that forgets only fails when the queue runs dry.
    return { ...q, items: q.items ?? [] };
  }
  async readQueued(itemId: string) {
    const r = await this.get<{ envelope?: { displayText?: string } }>("/inbox/items/" + encodeURIComponent(itemId));
    // Every stored entry has a body — the kernel refuses an empty one at
    // enqueue — so an absent field is an answer we do not understand, not an
    // empty instruction. Returning "" for it hands the editor a blank page.
    if (typeof r.envelope?.displayText !== "string") {
      throw new Error(`inbox item ${itemId}: answer carried no body`);
    }
    return r.envelope.displayText;
  }
  editQueued(itemId: string, text: string) {
    return this.patch("/inbox/items/" + encodeURIComponent(itemId), { input: text });
  }
  moveQueued(itemId: string, toIndex: number) {
    return this.post("/inbox/move", { id: itemId, toIndex });
  }
  retryQueued(itemId: string) {
    return this.post("/inbox/items/" + encodeURIComponent(itemId) + "/retry");
  }
  refreshQueued(itemId: string) {
    return this.post("/inbox/items/" + encodeURIComponent(itemId) + "/refresh");
  }
  setQueuePaused(paused: boolean) {
    return this.post(paused ? "/inbox/pause" : "/inbox/resume");
  }
  cancel() {
    return this.post("/cancel");
  }
  // Approve(id, allow, session, persist) — "always" is a session grant, not a
  // persisted config change.
  planDecision(id: string, action: PlanAction) {
    return this.post("/plan-decision", { id, action: PLAN_ACTIONS[action] });
  }

  approve(id: string, verdict: ApprovalVerdict) {
    return this.post("/approve", {
      id,
      allow: verdict !== "deny",
      session: verdict === "always",
      persist: false,
    });
  }
  answer(id: string, answers: { questionId: string; selected: string[] }[]) {
    return this.post("/answer", {
      id,
      answers: answers.map((a) => ({ QuestionID: a.questionId, Selected: a.selected })),
    });
  }
  async uploadWallpaper(blob: Blob) {
    const buf = new Uint8Array(await blob.arrayBuffer());
    let bin = "";
    for (let i = 0; i < buf.length; i += 0x8000) bin += String.fromCharCode(...buf.subarray(i, i + 0x8000));
    const res = await fetch(this.base + "/appearance/wallpaper", {
      method: "POST",
      headers: { "content-type": "application/json" },
      credentials: "same-origin",
      body: JSON.stringify({ mime: blob.type, data: btoa(bin) }),
    });
    const body = (await res.json().catch(() => ({}))) as Appearance & { error?: string };
    if (!res.ok) throw new Error(body.error || `/appearance/wallpaper: ${res.status}`);
    return body;
  }

  async invokeExtensionAction(name: string) {
    const out = await this.post0<{ message?: string }>("/extensions/action", { name });
    return out.message ?? "";
  }
  submitExtensionForm(pluginId: string, surfaceId: string, values: Record<string, unknown>) {
    return this.post("/extensions/submit", { pluginId, surfaceId, values });
  }
  setPlanMode(on: boolean) {
    return this.post("/plan", { on });
  }
  setApprovalMode(mode: ApprovalMode) {
    return this.post("/tool-approval-mode", { mode });
  }
  setPreset(preset: Preset) {
    return this.post("/preset", { preset });
  }
  setModel(ref: string) {
    return this.post("/model", { ref });
  }
  setEffort(effort: string) {
    return this.post("/effort", { effort });
  }
  compaction() {
    return this.get<CompactionSettings>("/compaction");
  }
  saveCompaction(softLimitTokens: number) {
    return this.post0<CompactionSettings>("/compaction", { soft_limit_tokens: softLimitTokens });
  }
  setGoal(text: string) {
    return this.post("/goal", { goal: text });
  }
}
