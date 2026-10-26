import { createRoot } from "react-dom/client";
import { boot as bootLang } from "../src/i18n";
import { track as trackWidth } from "../src/ui/viewport";
import { pushToast } from "../src/state/toasts";
import { apply as applyPack } from "../src/ui/theme";
import { mount as mountEditor } from "./syntax-mount";
import "../src/styles/tokens.css";
import "../src/styles/app.css";
import "../src/styles/dock.css";
import "../src/styles/studio.css";
import "../src/styles/changedpill.css";
import "../src/styles/askskip.css";
import "../src/styles/pastekeep.css";
import "../src/styles/ctxitems.css";
import "../src/styles/sidepanel.css";
import "../src/styles/contentmatches.css";
import "../src/styles/scrolljump.css";
import "../src/styles/chart.css";
import "../src/styles/community.css";
import "../src/styles/providerpresets.css";
import "../src/styles/sessionnotes.css";
import "../src/styles/sessionquick.css";
import "../src/styles/sidebardrop.css";
import "../src/styles/sponsor.css";
import "../src/styles/versions.css";
import "../src/styles/phonecomposer.css";
import "../src/styles/stophint.css";
import "../src/styles/dialogcenter.css";
import "../src/styles/treerows.css";
import "../src/styles/workbench-changes.css";
import "../src/styles/agentrun.css";
import "../src/styles/gitgraph.css";
import "../src/styles/tips.css";
import "../src/styles/waitnote.css";
import "../src/styles/bring.css";
import "../src/styles/peerthreads.css";
import "../src/styles/line.css";
import "../src/styles/notice.css";
import "../src/styles/peers.css";
import "../src/styles/jsontext.css";
import "../src/styles/skillpin.css";
import "../src/styles/narrow.css";
import "../src/styles/workfold.css";
import "../src/styles/workbench-launcher.css";
import "../src/styles/feedback.css";
import "../src/styles/toasts.css";
import "../src/styles/modelmenu.css";
import "../src/styles/composer-menu.css";
import "../src/styles/runsignals.css";
import "../src/styles/overview.css";
import "../src/styles/themes.css";
import { App } from "../src/ui/App";
import { MockHub } from "../src/port/mock_hub";
import { MockPort } from "../src/port/mock";
import { fromHistory } from "../src/state/session";
import { HttpError } from "../src/port/port";
import type { AgentPort, Appearance, ChipCall, HistoryMessage, McpEntry } from "../src/port/port";
import type { HistoryPage } from "../src/port/session";
import type { UpdateProgress } from "../src/port/version";
import type { ThemePack } from "../src/port/look";
import type { RuntimeView, TreeWindow, TreeWorkspace } from "../src/port/hub";
import type { WireEvent } from "../src/port/wire";
import type { PreviewInfo } from "../src/port/workspace";
import type { SubagentRun } from "../src/port/subagent";
import { deliveryContents, deliveryHistory } from "./delivery-data";

// Saves are coalesced (src/ui/trailing.ts), and a fixture answers too fast for
// that to show: coalesced and not look identical at zero latency. Counting the
// writes needs a round trip with some length to it.
const saves: Appearance[] = [];
let saveLag = 0;
let inFlight = 0;
let peakInFlight = 0;

// A port whose event stream the driver owns outright: the fixture's scripted
// beats never reach the UI, so a measurement times exactly the frames it fed.
// ?pack=sky opens on a pack with the live sky; ?pack=photo on one with a
// picture. Both are the states where the shell goes translucent over a backdrop.
const PACK = new URLSearchParams(location.search).get("pack");
const GLOW = new URLSearchParams(location.search).get("glow");

function benchPack(): ThemePack | null {
  const tokens = {
    light: { bg: "#F2F6F8", bgSoft: "#F9FCFD", panel: "#FFFFFF", border: "#CBD9E0", fg: "#12191C", fgDim: "#4E5D65", accent: "#0E6E82" },
    dark: { bg: "#080D10", bgSoft: "#0C1316", panel: "#131C21", border: "#23333A", fg: "#E4EEF2", fgDim: "#8298A2", accent: "#4FB6CE" },
  };
  if (PACK === "sky") {
    return {
      id: "bench-sky", name: "Bench sky", active: true, tokens,
      sky: { ray: "rgba(255,216,142,.55)", cloud: "255,240,212", cloudLit: "242,206,140", rayAlpha: 0.85, cloudAlpha: 0.4 },
    };
  }
  if (PACK === "photo") {
    return {
      id: "bench-photo", name: "Bench photo", active: true, tokens,
      background: {
        image: true, focusX: 0.5, focusY: 0.5, homeOpacity: 0.9, taskOpacity: 0.5, overlayStrength: 0.4,
        ...(GLOW === "none" || GLOW === "default" ? { textGlow: GLOW } : {}),
      },
    };
  }
  return null;
}

class BenchPort extends MockPort {
  opensFiles() { return query.has("deliveries") || super.opensFiles(); }
  revealsFiles() { return query.has("deliveries") || super.revealsFiles(); }
  async account() {
    const state = await super.account();
    const label = query.get("accountLabel");
    return label && state.signedIn && state.user ? { ...state, user: { ...state.user, label } } : state;
  }
  // ?mcpactions=1 adds a server whose tools are withheld and one that is followed.
  constructor() {
    super();
    // ?changes=N starts the workspace with N changed files spread over folders.
    const CHANGES = Number(query.get("changes") ?? 0);
    if (CHANGES > 0) {
      this.changeList = Array.from({ length: CHANGES }, (_, i) => ({ path: `pkg${i % 60}/dir${i % 9}/file${i}.go`, status: "??", unstaged: true }));
    }
    // ?peerset=1 starts with session messages on and two projects already allowed through.
    if (query.has("peerset")) {
      this.peerSettings = {
        enabled: true, dispatch: true, max_dispatch_depth: 1, max_dispatched_sessions: 8, applies: "next_turn", pending: query.has("pending"), available: true,
        path: "/Users/you/.reasonix/config.toml",
        release_projects: [{ path: "/Users/you/work/site", name: "site" }, { path: "/Users/you/work/api-gateway", name: "api-gateway" }],
      };
    }
    // ?git=multi is a folder of projects: three repositories, each with changes of its own.
    if (query.get("git") === "multi") {
      this.repoRows = [
        { dir: "api", name: "api", branch: "main", detached: false },
        { dir: "web", name: "web", branch: "feature/login", detached: false },
        { dir: "tools/release", name: "release", branch: "main", detached: false },
      ];
      this.repoChanges = {
        api: [
          { path: "internal/server/routes.go", status: "M", unstaged: true, insertions: 12, deletions: 3 },
          { path: "internal/server/routes_test.go", status: "M", staged: true, insertions: 20, deletions: 0 },
          { path: "README.md", status: "??", unstaged: true },
        ],
        web: [
          { path: "src/login/Form.tsx", status: "M", unstaged: true, insertions: 31, deletions: 9 },
          { path: "src/login/api.ts", status: "M", staged: true, unstaged: true, insertions: 4, deletions: 1 },
        ],
        "tools/release": [],
      };
    }
    if (!query.has("mcpactions")) return;
    (this as unknown as { servers: McpEntry[] }).servers.push(
      { name: "notes", state: "ready", enabled: true, transport: "stdio", tools: 2, description: "Notes the agent can search and add to.",
        held: [{ name: "mcp__notes__search", class: "changed" }, { name: "mcp__notes__add", class: "added" }], heldDigest: "digest-1" },
      { name: "wiki", state: "ready", enabled: true, transport: "http", tools: 4, followsChanges: true, description: "The team wiki." },
    );
  }

  // ?netshell=1 is a connection over HTTP: a `!` line is refused as the kernel refuses it.
  async submit(text: string, chips?: ChipCall) {
    (window.__sent ??= []).push(text);
    if (query.has("netshell") && text.trimStart().startsWith("!")) throw new HttpError(403, "shell commands are unavailable over HTTP", { code: "shell.unavailable_over_http" });
    return super.submit(text, chips);
  }

  // ?checkfail=<name> makes a plain test of that source answer 401.
  protected async answerCheck(name: string) {
    if (query.get("checkfail") !== name) return super.answerCheck(name);
    return { ok: false, code: "provider.probe.unauthorized", httpStatus: 401, detail: "Invalid Authentication" };
  }

  // ?twoports=1: two local servers on one host, told apart by port, sharing a key slot.
  // ?badprice=1: DeepSeek's flash price is a card the vendor's table does not have,
  // under a pinned display currency.
  async providers() {
    const all = await super.providers();
    if (query.has("twoports")) {
      const local = (name: string, port: number) => ({
        ...all[0], name, baseUrl: `http://127.0.0.1:${port}/v1`, keyEnv: "LOCAL_API_KEY", inUse: false, preset: false,
        models: [`${name}-chat`], default: `${name}-chat`,
      });
      return [...all, local("local", 8756), local("local-2", 8788)];
    }
    if (!query.has("badprice")) return all;
    return all.map((p) => p.name === "deepseek"
      ? { ...p, officialPricing: true, modelPrices: { "deepseek-flash": { input: 0.3, output: 1.2, cacheHit: 0, currency: "USD", official: false } } }
      : p);
  }

  async displayCurrency() {
    const got = await super.displayCurrency();
    return query.has("badprice") ? { ...got, mode: "CNY" as const, unconverted: [{ provider: "deepseek", model: "deepseek-flash", currency: "USD" }] } : got;
  }

  async browserTabs() {
    if (!query.has("agentpage")) return super.browserTabs();
    const url = query.get("agentpage") === "file" ? "file:///Users/me/work/site/index.html" : "https://example.com/docs/install";
    return [{ id: "t1", target: "view-a", url, title: "Install guide", active: true }];
  }

  // ?grant=1 grants the workspace a folder, /library, holding one document.
  async workspaceRoots() {
    const roots = await super.workspaceRoots();
    return query.has("grant") ? { ...roots, extra: ["/library"] } : roots;
  }
  async workspaceFiles(path = "", search = "", hidden = false) {
    if (!query.has("deliveries")) return super.workspaceFiles(path, search, hidden);
    const paths = Object.keys(deliveryContents);
    if (search) return { files: paths.filter((file) => file.toLowerCase().includes(search.toLowerCase())), directories: [] };
    const prefix = path ? `${path}/` : "";
    const entries = paths.filter((file) => file.startsWith(prefix));
    return {
      files: entries.filter((file) => !file.slice(prefix.length).includes("/")),
      directories: [...new Set(entries.filter((file) => file.slice(prefix.length).includes("/")).map((file) => prefix + file.slice(prefix.length).split("/")[0]))],
    };
  }


  async workspaceFile(path: string, root?: string) {
    if (query.has("deliveries") && path in deliveryContents) return { path, content: deliveryContents[path], revision: "delivery-fixture" };
    if (root) {
      const content = "# Release plan\n\nThe plan lives in the library folder you granted this project.\n\n- freeze on Friday\n- tag after the checks are green\n- announce the next morning\n";
      return { path, content, revision: "r1", root, readOnly: true };
    }
    if (!query.has("doc") || !path.endsWith(".md")) return super.workspaceFile(path);
    const content = "# Install guide\n\nDownload the package for your system, then run the installer. It places the program in your user folder and does not need administrator rights.\n\nAfter the first start, open Settings and add a model service before you begin a conversation.";
    return { path, content, revision: "r1" };
  }

  async updateMode() {
    return query.has("update") ? "notify" : super.updateMode();
  }

  async versions() {
    if (query.has("update")) return { current: "2.42.0", pinned: "", stalePin: false, latest: "2.43.0", newer: true, versions: [] };
    if (!query.has("versions")) return super.versions();
    const names = ["2.38.0", "2.37.0", "2.36.0", "2.35.0", "2.34.0", "2.33.0", "2.32.0", "2.31.0", "2.30.0", "2.29.0"];
    return {
      current: "2.37.0", pinned: "", stalePin: false, latest: "2.38.0", newer: true,
      versions: names.map((version) => ({ version, tag: `studio-v${version}`, publishedAt: new Date(Date.now() - (38 - Number(version.split(".")[1])) * 864e5).toISOString(), current: version === "2.37.0", older: Number(version.split(".")[1]) < 37, hasNotes: false })),
    };
  }

  private readonly subs = new Set<(e: WireEvent) => void>();

  async themes(): Promise<ThemePack[]> {
    const pack = benchPack();
    return pack ? [pack] : super.themes();
  }

  async saveAppearance(look: Appearance) {
    saves.push(look);
    inFlight++;
    peakInFlight = Math.max(peakInFlight, inFlight);
    try {
      if (saveLag) await new Promise((r) => setTimeout(r, saveLag));
      return await super.saveAppearance(look);
    } finally {
      inFlight--;
    }
  }

  // The opening sequence and the key prompt are scenes, not the session: a
  // measurement has to start on the workbench itself.
  async welcomeSeen() {
    return true;
  }

  async providerSetup() {
    return ONBOARDING ? { required: true, noProvider: true, provider: "", model: "" } : null;
  }

  async peers() {
    if (query.has("peerthreads")) {
      const states = ["waiting", "working", "replied", "declined"] as const;
      return {
        enabled: true,
        peers: [],
        held: [],
        threads: states.map((state, i) => ({ key: `s-${i}`, title: ["文档整理", "发布脚本", "拾音", "旧迁移"][i], project: "rx-archive", sessionPath: `/sessions/w0-s${i}.jsonl`, state, since: "2026-10-16T10:00:00Z", messageId: `m-${i}` })),
      };
    }
    if (!query.has("peers")) return super.peers();
    return {
      enabled: true,
      peers: [{ key: "s-1a2b3c4d", title: "拾音", project: "rx-archive" }, { key: "s-5e6f7a8b", title: "文档整理", project: "docs" }],
      held: [{ id: "m-1", fromSession: "s-1a2b3c4d", fromTitle: "拾音", fromProject: "rx-archive", sentAt: "2026-10-16T12:00:00Z", body: "迁移脚本跑完了，结果在 out/report.md，请你核对第三节再继续。" }],
    };
  }

  async webAccessSettings() {
    if (!query.has("webaccess")) return super.webAccessSettings();
    return {
      search_backends: ["searxng", "deepseek_api"], searxng_url: "http://127.0.0.1:8080", search_paid_allowed: [],
      fetch_backends: ["local", "deepseek_api"], fetch_paid_allowed: [],
      known_search_backends: ["searxng", "deepseek_api"], known_fetch_backends: ["local", "deepseek_api"], paid_backends: ["deepseek_api"],
      deepseek_api_ready: query.get("webaccess") !== "noready", video_url_passthrough: query.has("videourl"), path: "/Users/you/.reasonix/config.toml",
    };
  }

  async probeSearxng() {
    const answer = query.get("probe");
    if (!answer || answer === "ok") return super.probeSearxng();
    return { ok: false, code: `search.searxng_${answer}`, status: answer === "status" ? 429 : answer === "json_disabled" ? 403 : undefined };
  }

  async foldWorkSettings() {
    return { ...(await super.foldWorkSettings()), fold: query.has("fold") };
  }

  async workspaceGit() {
    const git = await super.workspaceGit();
    return query.get("git") === "none" ? { ...git, repo: false } : git;
  }

  async appearance() {
    let look = await super.appearance();
    if (query.has("zoom")) look = { ...look, zoom: Number(query.get("zoom")) };
    return PREF === null ? look : { ...look, language: PREF };
  }

  private settingCardSent = false;
  private receiptSent = false;

  async receiptSettings() {
    const read = await super.receiptSettings();
    return query.has("receipt") ? { ...read, show: true } : read;
  }
  private meterSent = false;

  subscribe(on: (e: WireEvent) => void) {
    this.subs.add(on);
    if (query.has("meter") && !this.meterSent) {
      this.meterSent = true;
      const usage = (amount: number) => ({ cacheHitTokens: 90, cacheMissTokens: 10, completionTokens: 40, totalTokens: 140, cost: amount, currency: "CNY", currencyCode: "CNY", source: "executor" });
      setTimeout(() => {
        for (const [msgIndex, amount] of [[1, 1.2], [2, 0.35]] as const) {
          this.feed({ kind: "turn_started", msgIndex } as unknown as WireEvent);
          this.feed({ kind: "usage", usage: usage(amount) } as unknown as WireEvent);
          this.feed({ kind: "turn_done" } as unknown as WireEvent);
        }
      }, 500);
    }
    if (query.has("receipt") && !this.receiptSent) {
      this.receiptSent = true;
      const script = "cd \"C:/Users/me/work\" && cat > \"$TMPDIR/check/verify_006.py\" <<'PYEOF'\n" + "import os, re, sys, unittest\nclass T(unittest.TestCase):\n    def test_a(self):\n        self.assertEqual(1, 1)\n".repeat(18) + "PYEOF\npython -m unittest verify_006 2>&1 | tail -12";
      setTimeout(() => {
        this.feed({ kind: "turn_started", msgIndex: 9 } as unknown as WireEvent);
        this.feed({ kind: "turn_done", receipt: { saysSomething: true, severity: "warn", gaps: [{ kind: "failed_verification", detail: script }, { kind: "inconclusive_verification", detail: "go test ./internal/... 2>&1 | tail -5" }] } } as unknown as WireEvent);
      }, 500);
    }
    const card = query.get("settingcard");
    if (card && !this.settingCardSent) {
      this.settingCardSent = true;
      setTimeout(() => this.feed(settingCardFrame(card)), 400);
    }
    return () => this.subs.delete(on);
  }

  feed(ev: WireEvent) {
    // The driver's frames are the fixture's own frames: a fed approval has to
    // move /status the way a scripted one does, or a guard reads a card sitting
    // over a status that still says the turn is running.
    this.openGate(ev);
    this.subs.forEach((f) => f(ev));
  }

  // Opening a conversation reads its whole transcript back. The kernel answers
  // that read from a file already on disk, so the size of the answer — not the
  // stream — is what a switch costs.
  async history(): Promise<HistoryMessage[]> {
    return query.has("deliveries") ? deliveryHistory() : storedHistory();
  }

  // ?paged=N serves that transcript the way a paging kernel does: N messages a
  // page, counted back from the newest. Without the parameter the answer is 404
  // and the read falls back to full history, so scripts written before paging
  // keep measuring what they measured.
  async historyPage(cursor?: string): Promise<HistoryPage> {
    const size = Number(query.get("paged") ?? 0);
    if (!(size > 0)) throw new HttpError(404, "paged history is off");
    const all = query.has("deliveries") ? deliveryHistory() : storedHistory();
    const end = cursor ? Number(cursor) : all.length;
    const start = Math.max(0, end - size);
    return { messages: all.slice(start, end), toolContext: [], snapshot: "harness", start, end, total: all.length,
      hasMoreBefore: start > 0, nextCursor: start > 0 ? String(start) : undefined };
  }

  // ?queue= fills the pending queue to the count under measurement. Seeded
  // here rather than by calling steer sixty-four times: what the guard asks is
  // what a full queue does to the layout, not how it got full.
  async queue() {
    const q = await super.queue();
    if (!QUEUE) return q;
    const items = Array.from({ length: QUEUE }, (_, i) => ({
      id: `seed-${i}`,
      // Lines the person queued: the panel leaves out the host's own queued
      // continuations, so a seed of those would draw nothing.
      intent: (QUEUE_IMAGES && i === 0 ? "steer" : "followup") as "steer" | "followup",
      state: "queued" as const,
      ...(QUEUE_IMAGES && i === 0 ? { images: true } : {}),
      // 三种长度，各有各的对照：第一条没被截断（不该弹），第二条长到撞天花板
      // （浮层要自己滚），其余是寻常的被截断行。内核把 preview 截到 120 runes，
      // 所以第二条在队列里到不了 —— 但 .ovf 是通用的，天花板得有人证明它在。
      preview:
        QUEUE_IMAGES && i === 0
          ? "@.reasonix/attachments/clipboard-20261017-141249.png reasonix 的你改好了？我这边看到的还是旧界面"
          : i === 0
          ? "bash-1 — failed"
          : i === 1
            ? `bash-2 — failed: ${"error[E0308]: mismatched types in the trait implementation; ".repeat(14)}`
            : `bash-${i + 1} (cargo build --release --target x86_64-pc-windows-msvc 2>&1 | Select-Object -Last 30 | Select-String -Pattern "error\\[E[0-9]+\\]") — failed`,
      createdAt: new Date().toISOString(),
    }));
    return { ...q, items, capacity: { ...q.capacity, items: QUEUE, maxItems: 64 } };
  }

  // ?queuebody= makes a seeded line readable back, as the kernel's is.
  async readQueued(itemId: string): Promise<string> {
    if (!QUEUE_BODY) return super.readQueued(itemId);
    return "请把这个模块里所有的错误处理统一改成带类型的错误，并且保证调用方可以用 errors.Is 区分每一种失败，同时补上对应的单元测试，再检查文档里提到旧行为的地方是否需要更新。".repeat(3);
  }

  // ?statusms= stands in for what the kernel's /status really costs when the
  // provider declares a wallet endpoint: that read goes out to the network.
  // ?jobs= seeds that many background jobs; stopping one is kept here because
  // the mock's own status carries none.
  private readonly stopped = new Set<string>();
  private readonly jobsSince = Date.now() - 739_000;

  async status() {
    const usage = query.get("goalusage");
    const goalRuntime = usage
      ? { turnsUsed: 4, tokensUsed: 128_400, requestsUsed: 9, workDurationMs: 12 * 60_000, noProgressTurns: usage === "blocked" ? 2 : 0, ...(usage === "blocked" ? { stopCause: "goal_stuck", lastReason: "测试一直没有通过，改动没有让失败的数量减少" } : {}) }
      : undefined;
    const st = { ...(await super.status()), ...(query.has("running") ? { running: true } : {}), ...(query.has("goal") ? { goal: query.get("goal") ?? "" } : {}), ...(usage ? { goalStatus: usage, goalRuntime } : {}), ...(query.has("fullaccess") ? { toolApprovalMode: "yolo" as const } : {}), ...(query.has("effort") ? { effort: query.get("effort") ?? "" } : {}), ...(query.has("modelref") ? { modelRef: query.get("modelref") ?? "" } : {}), ...(query.has("trustpending") ? { toolApprovalMode: "ask" as const, approvalDefault: { defaulted: true, writesConfined: true, trust: "" as const, trustable: true } } : {}) };
    if (STATUS_MS > 0) await new Promise((r) => setTimeout(r, STATUS_MS));
    if (!JOBS) return st;
    const jobs = Array.from({ length: JOBS }, (_, i) => ({
      id: `job-${i}`,
      kind: "bash",
      label: i === 0 ? "npm run tauri:test 2>&1 | Select-Object -Last 40" : `go test ./internal/... -run Case${i}`,
      // ?jobnames=1 gives each job the name its caller wrote and the full command.
      ...(query.has("jobnames") ? {
        description: i === 0 ? "运行桌面端测试并只看最后 40 行输出" : `跑第 ${i} 组内核测试`,
        detail: i === 0 ? "npm run tauri:test 2>&1 | Select-Object -Last 40" : `go test ./internal/... -run Case${i} -count=1 -timeout 10m`,
      } : {}),
      status: this.stopped.has(`job-${i}`) ? "killed" : "running",
      startedAt: this.jobsSince,
    }));
    return { ...st, jobs };
  }

  async switchBranch(name: string) {
    if (SWITCH_MS > 0) await new Promise((r) => setTimeout(r, SWITCH_MS));
    return super.switchBranch(name);
  }

  async goToVersion() {}

  onUpdateProgress(cb?: (p: UpdateProgress) => void) {
    if (cb && query.has("update")) queueMicrotask(() => cb({ version: "2.43.0", phase: "ready", received: 0, total: 0 }));
    return () => {};
  }

  async jobOutput(jobId: string) {
    if (!JOBS) return super.jobOutput(jobId);
    const lines = Array.from({ length: 28 }, (_, i) => `[${String(i).padStart(2, "0")}] compiling module ${i * 7 + 3} … ok`);
    return { text: lines.join("\n") + "\nwatching for changes\n", status: this.stopped.has(jobId) ? "killed" : "running", bytes: 1100, truncated: false };
  }

  async cancelJob(jobId: string) {
    if (!JOBS) return super.cancelJob(jobId);
    this.stopped.add(jobId);
  }
}

// ?skill=1 makes the last user turn a skill invocation, the way the host sends one.
const SKILL_PIN = `<skill-pin name="code-quality">\n# Skill: code-quality\n> 专业的代码质量审查 Skill：多维度 Code Review（正确性、安全性、性能、可维护性），带量化评分。\n(scope: user · ~/.reasonix/skills/code-quality/SKILL.md)\n\n${"审查要点：先读改动范围，再逐项检查。\n".repeat(30)}\n\nArguments: 看一下 internal/auth\n</skill-pin>`;

function storedHistory(): HistoryMessage[] {
  const msgs: HistoryMessage[] = [];
  for (let i = 0; i < TURNS; i++) {
    const pinned = query.get("skill") === "1" && i === TURNS - 1;
    msgs.push({ role: "user", content: pinned ? SKILL_PIN : `第 ${i} 个问题，麻烦看一下这个文件。`, createdAt: new Date(2026, 9, 17, 18, 33 + i).getTime() });
    msgs.push({
      role: "assistant",
      content: `第 ${i} 段回答，说明刚才那一步做了什么。\n\n- 要点一\n- 要点二\n`,
      reasoning: "先看文件再决定改哪里。",
      toolCalls: [{ id: `call_${i}`, name: "edit_file", arguments: JSON.stringify({ path: `internal/pkg/mod${i}/file${i}.go` }) }],
    });
    msgs.push({ role: "tool", toolCallId: `call_${i}`, toolName: "edit_file", content: `第 ${i} 次输出\n`.repeat(20) });
  }
  return msgs;
}

// ?ws=&sess= sizes the sidebar tree. It is read from the URL rather than set
// through a call so the first paint already has the tree under measurement —
// how a window opens onto a machine with hundreds of sessions is the question.
const query = new URLSearchParams(location.search);
const WORKSPACES = Number(query.get("ws") ?? 0);
// ?pref= 是「内核记着的语言」。真机上它来自 config；这里由地址给，好让
// 一次验证能把两侧摆成同一个值——否则 adopt 会认为本地缓存过期。
const PREF = query.get("pref");
const ONBOARDING = query.has("onboarding");
const SESSIONS = Number(query.get("sess") ?? 0);
const ROW_TITLES = ["rx home目录里有没有 obsidian 的笔记仓库，帮我整理一下", "obsidian 插件同步方案怎么选", "查一下 studio 最近一次发布改了什么", "阅读一下最近的提交记录并总结"];
// ?turns= is how long the conversation being opened already is.
const TURNS = Number(query.get("turns") ?? 0);
const STATUS_MS = Number(query.get("statusms") ?? 0);
const SWITCH_MS = Number(query.get("switchms") ?? 0);
// ?queue= is how many lines are already waiting when the window opens.
const QUEUE = Number(query.get("queue") ?? 0);
const QUEUE_BODY = query.has("queuebody");
// ?queueimages makes the first seeded line a steer that carries a picture.
const QUEUE_IMAGES = query.has("queueimages");
const JOBS = Number(query.get("jobs") ?? 0);
// ?treems= holds every tree read back until that long after load, so the
// window can be seen before the kernel has named a single folder.
const TREE_AT = performance.now() + Number(query.get("treems") ?? 0);
// ?panes=0 opens the window with no conversation open; ?panes=N opens N.
// ?tree=none answers every tree read with no folders.
const PANES = query.has("panes") ? Number(query.get("panes")) : 1;
const NO_PANES = PANES === 0;

class BenchHub extends MockHub {
  readonly feeds = new Map<string, BenchPort>();

  // ?search=1 answers every query with two conversations that say it.
  async searchHistory(q: string) {
    if (!query.has("search")) return super.searchHistory(q);
    return [
      { path: "/sessions/mock.jsonl", id: "mock", snippet: `…上周讨论过要不要把 ${q} 的轮换放进发布流程，结论是每月一次…`, role: "assistant", messageIndex: 4, score: 2 },
      { path: "/sessions/older.jsonl", id: "older", snippet: `怎么把 ${q} 配到预发环境里？`, role: "user", messageIndex: 0, score: 1 },
    ];
  }

  private readonly settingsPort = new BenchPort();
  private opened = false;

  async open(req: Parameters<MockHub["open"]>[0]) {
    this.opened = true;
    return super.open(req);
  }

  machinePort(): AgentPort {
    return this.settingsPort;
  }

  portFor(rt: RuntimeView): AgentPort {
    let port = this.feeds.get(rt.id);
    if (!port) this.feeds.set(rt.id, (port = new BenchPort()));
    return port;
  }

  runtimes(): Promise<RuntimeView[]> {
    if (NO_PANES && !this.opened) return Promise.resolve([]);
    if (PANES <= 1) return super.runtimes();
    return Promise.resolve(
      Array.from({ length: PANES }, (_, i) => ({
        id: `r${i + 1}`,
        base: i === 0 ? "" : `/rt/r${i + 1}`,
        root: "~/projects/DeepSeek-Reasonix",
        name: "DeepSeek-Reasonix",
        sessionPath: `/sessions/pane-${i + 1}.jsonl`,
      })),
    );
  }

  async moveSessionTo(path: string, target: string) {
    (window.__moves ??= []).push({ path, target });
    return super.moveSessionTo(path, target);
  }

  // ?slowarchive=<ms>: one project with 27 active and 2127 archived conversations,
  // whose archived read answers after that many milliseconds.
  tree(window?: TreeWindow): Promise<TreeWorkspace[]> {
    const slow = Number(query.get("slowarchive") ?? "");
    if (slow > 0) {
      const archived = window?.scope === "archived";
      const rows = Array.from({ length: archived ? 8 : 6 }, (_, i) => ({
        path: `/sessions/${archived ? "old" : "new"}-${i}.jsonl`, name: `${archived ? "old" : "new"}-${i}`,
        title: `${archived ? "归档的" : "活跃的"}会话 ${i + 1}`, turns: i + 1, archived,
      }));
      const answer = [{ root: "~/projects/workspace-0", name: "workspace-0", remembered: true, open: true, total: 2154, archived: 2127, remaining: archived ? 2119 : 21, sessions: rows }];
      return new Promise((resolve) => setTimeout(() => resolve(answer), archived ? slow : 0));
    }
    const wait = TREE_AT - performance.now();
    if (wait > 0) return new Promise((resolve) => setTimeout(() => resolve(this.tree()), wait));
    if (query.get("tree") === "none") return Promise.resolve([]);
    if (!WORKSPACES && query.has("trustpending") && PANES > 1) {
      return super.tree().then((all) => all.map((ws, i) => i > 0 ? ws : { ...ws, sessions: Array.from({ length: PANES }, (_, k) => ({
        path: `/sessions/pane-${k + 1}.jsonl`, name: `pane-${k + 1}`, title: `并行会话 ${k + 1}`, turns: 3, runtimeId: `r${k + 1}`,
      })) }));
    }
    if (!WORKSPACES) return super.tree();
    return Promise.resolve(
      Array.from({ length: WORKSPACES }, (_, w) => ({
        root: `~/projects/workspace-${w}`,
        name: `workspace-${w}`,
        remembered: true,
        open: w === 0,
        sessions: this.placed(`~/projects/workspace-${w}`, Array.from({ length: SESSIONS }, (_, s) => ({
          path: `/sessions/w${w}-s${s}.jsonl`,
          name: `w${w}-s${s}`,
          title: query.has("rows") ? ROW_TITLES[s % ROW_TITLES.length] : `第 ${s} 次会话，标题是内核起的一句话`,
          turns: (s % 40) + 1,
          ...(query.has("rows") ? {
            lastActiveAt: Date.now() - (9 + s) * 86_400_000,
            versions: s % 2 === 1 ? [{ path: `/sessions/w${w}-s${s}-v1.jsonl`, name: `w${w}-s${s}-v1`, title: ROW_TITLES[s % ROW_TITLES.length], turns: 1 }] : undefined,
          } : {}),
        }))),
      })),
    );
  }
}

// 与 main.tsx 同一条路：语言在第一帧之前定下来。
bootLang();
// main.tsx 在挂 App 之前也调这一句。台架挂的是 App，却漏了 App 依赖的这段入口
// 接线，于是 data-fold 永远是空的：窄屏收拢从来没在这里发生过，而 panels 那两条
// 断言量的正是它 —— 守卫红了两年，红的是台架缺一行。
trackWidth();

const hub = new BenchHub();
// No StrictMode: its double render is the thing under measurement here.
createRoot(document.getElementById("root")!).render(<App hub={hub} />);

declare global {
  interface Window {
    __moves?: { path: string; target: string }[];
    __sent?: string[];
    // Feeds every open pane, so a split-view measurement costs what two live
    // conversations really cost.
    __feed: (ev: WireEvent) => void;
    __panes: () => number;
    // Answer every completion request with this one, as the kernel would for a command typed in full.
    __complete: (answer: import("../src/port/port").Completion) => void;
    __previewFiles: (files: Record<string, { kind: PreviewInfo["kind"]; text: string }>) => void;
    // Sub-agent runs the kernel would list, each with the conversation it would return.
    __subagents: (runs: { run: SubagentRun; messages: HistoryMessage[] }[]) => void;
    // What folding a stored transcript into cards costs on its own, with no
    // frame in it: the step between the read answering and the first paint.
    __foldCost: () => number;
    // Give each save a round trip and reset the tally. 0 turns it off.
    __saveLag: (ms: number) => void;
    __saves: () => { count: number; peak: number; last: Appearance | null };
    // Refuse the next calls to one hub method, so a guard can reach the
    // window's error bar through the path a real failure takes.
    __refuse: (method: string, message: string) => void;
    // Put a notice in the window's corner the way a failed setting does.
    __toast: (text: string) => void;
    __syntax: { apply: typeof applyPack; mount: typeof mountEditor };
  }
}
window.__toast = pushToast;
window.__syntax = { apply: applyPack, mount: mountEditor };
window.__feed = (ev) => hub.feeds.forEach((p) => p.feed(ev));
window.__panes = () => hub.feeds.size;
window.__previewFiles = (files) => hub.feeds.forEach((port) => {
  port.workspacePreviewInfo = async (ref) => ({
    ref, kind: files[ref.path].kind, size: new TextEncoder().encode(files[ref.path].text).length,
    version: "fixture", limit: 32 << 20, readOnly: false,
  });
  port.workspacePreviewContent = async (ref) => ({
    data: new TextEncoder().encode(files[ref.path].text).buffer, version: "fixture",
  });
});
window.__subagents = (runs) => hub.feeds.forEach((port) => {
  port.subagents = async () => runs.map((r) => r.run);
  port.subagentTranscript = async (ref) => {
    const found = runs.find((r) => r.run.ref === ref)!;
    return { run: found.run, messages: found.messages, after: 0, next: found.messages.length, total: found.messages.length, live: false };
  };
});
window.__complete = (answer) => hub.feeds.forEach((port) => { port.complete = async () => answer; });
window.__refuse = (method, message) => {
  (hub as unknown as Record<string, unknown>)[method] = () => Promise.reject(new Error(message));
};
window.__saveLag = (ms) => {
  saveLag = ms;
  saves.length = 0;
  peakInFlight = 0;
};
window.__saves = () => ({ count: saves.length, peak: peakInFlight, last: saves[saves.length - 1] ?? null });
window.__foldCost = () => {
  const msgs = storedHistory();
  fromHistory(msgs.slice(0, 60));
  const t0 = performance.now();
  fromHistory(msgs);
  return performance.now() - t0;
};

// ?settingcard= draws what the kernel sends when the model asks to change a
// setting: a preference card, an elevated one, or the notice that points at a
// setting only the user may change.
function settingCardFrame(kind: string): WireEvent {
  if (kind === "open") {
    return { kind: "notice", level: "info", code: "setting_open", text: "助手需要改动 permissions.mode，只有你本人能改。", detail: "permissions.mode" } as WireEvent;
  }
  const elevated = kind === "elevated";
  const id = elevated ? "peers.dispatch" : "ui.show_receipt";
  return {
    kind: "approval_request",
    approval: {
      id: "s1", tool: elevated ? "setting_change_elevated" : "setting_change", subject: id, kind: "tool", fresh: true,
      allowsSession: !elevated, allowsPersist: false,
      reason: `助手请求把设置 ${id} 从 false 改为 true。${elevated ? "从下一回合起生效。" : "立即生效。"}\n它给出的理由：要把任务交给另一个项目里的会话。`,
    },
  } as WireEvent;
}
