import { createRoot } from "react-dom/client";
import { boot as bootLang } from "../src/i18n";
import { track as trackWidth } from "../src/ui/viewport";
import { pushToast } from "../src/state/toasts";
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
import type { AgentPort, Appearance, HistoryMessage, McpEntry } from "../src/port/port";
import type { UpdateProgress } from "../src/port/version";
import type { ThemePack } from "../src/port/look";
import type { RuntimeView, TreeWorkspace } from "../src/port/hub";
import type { WireEvent } from "../src/port/wire";

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
  // ?mcpactions=1 adds a server whose tools are withheld and one that is followed.
  constructor() {
    super();
    if (!query.has("mcpactions")) return;
    (this as unknown as { servers: McpEntry[] }).servers.push(
      { name: "notes", state: "ready", enabled: true, transport: "stdio", tools: 2, description: "Notes the agent can search and add to.",
        held: [{ name: "mcp__notes__search", class: "changed" }, { name: "mcp__notes__add", class: "added" }], heldDigest: "digest-1" },
      { name: "wiki", state: "ready", enabled: true, transport: "http", tools: 4, followsChanges: true, description: "The team wiki." },
    );
  }

  // ?checkfail=<name> makes a plain test of that source answer 401.
  protected async answerCheck(name: string) {
    if (query.get("checkfail") !== name) return super.answerCheck(name);
    return { ok: false, code: "provider.probe.unauthorized", httpStatus: 401, detail: "Invalid Authentication" };
  }

  // ?badprice=1: DeepSeek's flash price is a card the vendor's table does not have,
  // under a pinned display currency.
  async providers() {
    const all = await super.providers();
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
    return [{ id: "t1", target: "view-a", url: "https://example.com/docs/install", title: "Install guide", active: true }];
  }

  // ?grant=1 grants the workspace a folder, /library, holding one document.
  async workspaceRoots() {
    const roots = await super.workspaceRoots();
    return query.has("grant") ? { ...roots, extra: ["/library"] } : roots;
  }

  async workspaceFile(path: string, root?: string) {
    if (root) {
      const content = "# Release plan\n\nThe plan lives in the library folder you granted this project.\n\n- freeze on Friday\n- tag after the checks are green\n- announce the next morning\n";
      return { path, content, revision: "r1", root, readOnly: true };
    }
    if (!query.has("doc") || !path.endsWith(".md")) return super.workspaceFile(path);
    const content = "# Install guide\n\nDownload the package for your system, then run the installer. It places the program in your user folder and does not need administrator rights.\n\nAfter the first start, open Settings and add a model service before you begin a conversation.";
    return { path, content, revision: "r1" };
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
      deepseek_api_ready: query.get("webaccess") !== "noready", path: "/Users/you/.reasonix/config.toml",
    };
  }

  async probeSearxng(url: string) {
    const answer = query.get("probe");
    if (!answer || answer === "ok") return super.probeSearxng(url);
    return { ok: false, code: `search.searxng_${answer}`, status: answer === "status" ? 429 : answer === "json_disabled" ? 403 : undefined };
  }

  async peersSettings() {
    if (!query.has("peerset")) return super.peersSettings();
    return {
      enabled: true, dispatch: true, applies: "next_turn" as const, pending: query.has("pending"), available: true,
      path: "/Users/you/.reasonix/config.toml",
      release_projects: [{ path: "/Users/you/work/site", name: "site" }, { path: "/Users/you/work/api-gateway", name: "api-gateway" }],
    };
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

  subscribe(on: (e: WireEvent) => void) {
    this.subs.add(on);
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
    return storedHistory();
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
    const st = { ...(await super.status()), ...(query.has("running") ? { running: true } : {}), ...(query.has("goal") ? { goal: query.get("goal") ?? "" } : {}) };
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
    msgs.push({ role: "user", content: pinned ? SKILL_PIN : `第 ${i} 个问题，麻烦看一下这个文件。` });
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

  machinePort(): AgentPort {
    return this.settingsPort;
  }

  portFor(rt: RuntimeView): AgentPort {
    let port = this.feeds.get(rt.id);
    if (!port) this.feeds.set(rt.id, (port = new BenchPort()));
    return port;
  }

  runtimes(): Promise<RuntimeView[]> {
    if (NO_PANES) return Promise.resolve([]);
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

  tree(): Promise<TreeWorkspace[]> {
    const wait = TREE_AT - performance.now();
    if (wait > 0) return new Promise((resolve) => setTimeout(() => resolve(this.tree()), wait));
    if (!WORKSPACES) return super.tree();
    return Promise.resolve(
      Array.from({ length: WORKSPACES }, (_, w) => ({
        root: `~/projects/workspace-${w}`,
        name: `workspace-${w}`,
        open: w === 0,
        sessions: Array.from({ length: SESSIONS }, (_, s) => ({
          path: `/sessions/w${w}-s${s}.jsonl`,
          name: `w${w}-s${s}`,
          title: `第 ${s} 次会话，标题是内核起的一句话`,
          turns: (s % 40) + 1,
        })),
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
    // Feeds every open pane, so a split-view measurement costs what two live
    // conversations really cost.
    __feed: (ev: WireEvent) => void;
    __panes: () => number;
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
  }
}
window.__toast = pushToast;
window.__feed = (ev) => hub.feeds.forEach((p) => p.feed(ev));
window.__panes = () => hub.feeds.size;
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
