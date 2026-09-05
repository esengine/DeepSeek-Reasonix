import { createRoot } from "react-dom/client";
import { boot as bootLang } from "../src/i18n";
import { track as trackWidth } from "../src/ui/viewport";
import "../src/styles/tokens.css";
import "../src/styles/app.css";
import { App } from "../src/ui/App";
import { MockHub } from "../src/port/mock_hub";
import { MockPort } from "../src/port/mock";
import { fromHistory } from "../src/state/session";
import type { AgentPort, Appearance, HistoryMessage } from "../src/port/port";
import type { RuntimeView } from "../src/port/hub";
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
class BenchPort extends MockPort {
  private readonly subs = new Set<(e: WireEvent) => void>();

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
    return null;
  }

  async appearance() {
    const look = await super.appearance();
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
      intent: "followup" as const,
      origin: "host" as const,
      state: "queued" as const,
      // 三种长度，各有各的对照：第一条没被截断（不该弹），第二条长到撞天花板
      // （浮层要自己滚），其余是寻常的被截断行。内核把 preview 截到 120 runes，
      // 所以第二条在队列里到不了 —— 但 .ovf 是通用的，天花板得有人证明它在。
      preview:
        i === 0
          ? "bash-1 — failed"
          : i === 1
            ? `bash-2 — failed: ${"error[E0308]: mismatched types in the trait implementation; ".repeat(14)}`
            : `bash-${i + 1} (cargo build --release --target x86_64-pc-windows-msvc 2>&1 | Select-Object -Last 30 | Select-String -Pattern "error\\[E[0-9]+\\]") — failed`,
      createdAt: new Date().toISOString(),
    }));
    return { ...q, items, capacity: { ...q.capacity, items: QUEUE, maxItems: 64 } };
  }

  // ?statusms= stands in for what the kernel's /status really costs when the
  // provider declares a wallet endpoint: that read goes out to the network.
  async status() {
    const st = await super.status();
    if (STATUS_MS > 0) await new Promise((r) => setTimeout(r, STATUS_MS));
    return st;
  }
}

function storedHistory(): HistoryMessage[] {
  const msgs: HistoryMessage[] = [];
  for (let i = 0; i < TURNS; i++) {
    msgs.push({ role: "user", content: `第 ${i} 个问题，麻烦看一下这个文件。` });
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
const SESSIONS = Number(query.get("sess") ?? 0);
// ?turns= is how long the conversation being opened already is.
const TURNS = Number(query.get("turns") ?? 0);
const STATUS_MS = Number(query.get("statusms") ?? 0);
// ?queue= is how many lines are already waiting when the window opens.
const QUEUE = Number(query.get("queue") ?? 0);

class BenchHub extends MockHub {
  readonly feeds = new Map<string, BenchPort>();

  portFor(rt: RuntimeView): AgentPort {
    let port = this.feeds.get(rt.id);
    if (!port) this.feeds.set(rt.id, (port = new BenchPort()));
    return port;
  }

  tree() {
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
  }
}
window.__feed = (ev) => hub.feeds.forEach((p) => p.feed(ev));
window.__panes = () => hub.feeds.size;
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
