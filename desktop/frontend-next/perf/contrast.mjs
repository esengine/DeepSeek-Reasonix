// 正文与它落在的那块底之间的对比度。上一次清扫是人一处一处看出来的（浅色下
// 五处不达 AA），所以它只在有人去看的那一天成立 —— 而配色是令牌，一个值动了，
// 受影响的是所有引用它的地方，不是改它的人记得住的那几处。
//
// 三件事决定这个数，缺一处就会把不合格读成合格：
//   1. 颜色要按 CSS Color 4 解析。令牌写 oklch(...)、color-mix 算出 oklab(...)，
//      把分量当 RGB 读，色相角会变成蓝通道（见 pick.mjs 的同一个坑）。
//   2. 底要一层层合成上来。半透明的底不是底，它上面还压着别人的颜色。
//   3. 大字有豁免：24px 起，或 18.66px 且 700 —— AA 的原文就是这两档。
import { chromium } from "playwright";

const PAGE = process.env.PERF_URL ?? "http://localhost:4399/perf.html?pref=zh&ws=2&sess=6&turns=1";

// 已知不达标的那些：每条是 `配色/签名`。名单只能变短 —— 新出现的一条会红，
// 而名单里已经修好的那条会被报出来，好让它从这里消失，不是留着当免死金牌。
const KNOWN = new Set([]);

const fails = [];
const check = (name, ok, detail = "") => {
  console.log(`${ok ? "  ok" : "FAIL"}  ${name}${detail ? "  — " + detail : ""}`);
  if (!ok) fails.push(name);
};

const browser = await chromium.launch();

const sweep = async (page, scheme, where) =>
  page.evaluate(
    ({ scheme, where }) => {
      const ink = document.createElement("canvas").getContext("2d", { willReadFrequently: true });
      const rgba = (css) => {
        ink.clearRect(0, 0, 1, 1);
        ink.fillStyle = "#000";
        ink.fillStyle = css;
        ink.fillRect(0, 0, 1, 1);
        const [r, g, b, a] = ink.getImageData(0, 0, 1, 1).data;
        return [r, g, b, a / 255];
      };
      // ImageData 永远是非预乘的，所以取回来的三个分量就是这个颜色本身，
      // 半透明只体现在 alpha 上 —— 再除一次 alpha 会把 13% 的红算成 1735。
      const over = (top, bottom) => {
        const a = top[3];
        if (a >= 1) return [top[0], top[1], top[2], 1];
        return [0, 1, 2].map((i) => top[i] * a + bottom[i] * (1 - a)).concat(1);
      };
      const lum = ([r, g, b]) => {
        const c = [r, g, b].map((v) => {
          const s = v / 255;
          return s <= 0.04045 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
        });
        return 0.2126 * c[0] + 0.7152 * c[1] + 0.0722 * c[2];
      };
      const ratio = (fg, bg) => {
        const [a, b] = [lum(fg), lum(bg)].sort((x, y) => y - x);
        return (a + 0.05) / (b + 0.05);
      };

      // 一直往上合成，直到落在一块不透明的底上。页面自己的底是最后一层。
      const groundOf = (el) => {
        let acc = [0, 0, 0, 0];
        for (let n = el; n; n = n.parentElement) {
          const c = (rgba(getComputedStyle(n).backgroundColor));
          if (c[3] === 0) continue;
          acc = acc[3] === 0 ? c : over(acc, c);
          if (acc[3] >= 1) return acc;
        }
        return over(acc, (rgba(getComputedStyle(document.body).backgroundColor)));
      };

      const sig = (el) => {
        const cls = [...el.classList].slice(0, 3).join(".");
        return el.tagName.toLowerCase() + (cls ? "." + cls : "");
      };

      const hex = (c) => "#" + c.slice(0, 3).map((v) => Math.round(v).toString(16).padStart(2, "0")).join("").toUpperCase();

      // A signature is the dedup key, not an address: one class lands on four
      // different grounds, and reporting the key alone says nothing about which.
      const inside = (el) => {
        const out = [];
        for (let n = el.parentElement; n && n !== document.body && out.length < 4; n = n.parentElement) {
          const op = getComputedStyle(n).opacity;
          const bg = rgba(getComputedStyle(n).backgroundColor);
          out.push(sig(n) + (op !== "1" ? `@${op}` : "") + (bg[3] > 0 ? "#bg" : ""));
        }
        return out.join(" < ");
      };

      // 记号也要看得见：一个语义色被画在屏幕上，就是在说一件事，而 WCAG 1.4.11
      // 给非文本的界面部件定的是 3:1。范围不按"细不细"划 —— 那会把分隔线也圈
      // 进来，而发丝线本来就该若隐若现 —— 按**画的是不是语义色**划：accent /
      // net / ok / err / warn / deleg 出现在哪里，哪里就在表达状态。
      const HUES = ["--accent", "--net", "--ok", "--err", "--warn", "--deleg"];
      const swatch = HUES.map((n) => ({ n, c: rgba(getComputedStyle(document.documentElement).getPropertyValue(n)) }));
      const near = (c) => swatch.find((s) => Math.abs(s.c[0] - c[0]) + Math.abs(s.c[1] - c[1]) + Math.abs(s.c[2] - c[2]) <= 6);

      const out = [];
      const seen = new Set();
      for (const el of document.querySelectorAll("body *")) {
        const own = [...el.childNodes].some((n) => n.nodeType === 3 && n.textContent.trim());
        if (el.closest("[hidden], [disabled], [aria-disabled='true'], :disabled")) continue;
        const st = getComputedStyle(el);
        if (st.visibility !== "visible" || st.display === "none") continue;
        const box = el.getBoundingClientRect();
        if (box.width < 2 || box.height < 2) continue;
        if (!own) {
          // 画了语义色的那些：比的是它自己和它压着的那层。
          const fill = rgba(st.backgroundColor);
          const hue = fill[3] >= 0.9 && near(fill);
          if (!hue || el.textContent.trim()) continue;
          const under = groundOf(el.parentElement ?? el);
          const got = ratio(over(fill, under), under);
          if (got >= 3) continue;
          const key = `${scheme}/记号 ${sig(el)} (${hue.n})`;
          if (seen.has(key)) continue;
          seen.add(key);
          out.push({ key, where, got: Math.round(got * 100) / 100, need: 3, size: Math.round(Math.min(box.width, box.height)), text: hue.n });
          continue;
        }
        // 透明度是整棵子树一起淡出，正文和它自己的底一起被稀释到祖先的底上。
        let alpha = 1;
        for (let n = el; n; n = n.parentElement) alpha *= Number(getComputedStyle(n).opacity);
        if (alpha < 0.15) continue;

        const ground = groundOf(el.parentElement ?? el);
        const bg = alpha >= 1 ? groundOf(el) : over([...groundOf(el).slice(0, 3), alpha], ground);
        const fgRaw = (rgba(st.color));
        // Fully transparent text is not painted, so it has no contrast. Whether
        // a hover-only control is reachable is a different judgement.
        if (fgRaw[3] === 0) continue;
        const fg = over([fgRaw[0], fgRaw[1], fgRaw[2], fgRaw[3] * alpha], bg);

        const size = parseFloat(st.fontSize);
        const weight = Number(st.fontWeight) || 400;
        const large = size >= 24 || (size >= 18.66 && weight >= 700);
        const need = large ? 3 : 4.5;
        const got = ratio(fg, bg);
        if (got >= need) continue;
        const s = `${scheme}/${sig(el)}`;
        if (seen.has(s)) continue;
        seen.add(s);
        out.push({
          key: s,
          where,
          got: Math.round(got * 100) / 100,
          need,
          size: Math.round(size * 10) / 10,
          text: (el.textContent || "").trim().slice(0, 18),
          at: inside(el),
          ink: hex(fg),
          ground: hex(bg),
        });
      }
      return out;
    },
    { scheme, where },
  );

// Entrance animations are staggered and fill both ways, so an un-settled frame
// measures a half-transparent one. Turning them off is what this asks: the
// judgement is the steady-state colour, and every keyframe ends at opacity 1.
const settled = async (page) => {
  await page.addStyleTag({ content: "*, *::before, *::after { animation: none !important; transition: none !important }" });
  await page.waitForTimeout(300);
};

const found = new Map();
for (const scheme of ["light", "dark"]) {
  const page = await browser.newPage({ viewport: { width: 1512, height: 950 }, locale: "zh-CN", colorScheme: scheme });
  page.on("pageerror", (e) => fails.push("页面异常: " + e.message));
  // The receipt card is off unless a machine asked for it, and an unrendered
  // card is a palette nothing measures.
  await page.addInitScript(() => {
    try {
      localStorage.setItem("rx-turn-receipt", "on");
    } catch {
    }
  });
  await page.goto(PAGE, { waitUntil: "networkidle" });
  await page.waitForSelector(".compose");
  await page.waitForTimeout(400);

  // 一屏里画不出所有状态：审批、提问、失败的调用、设置页各自带着自己的配色。
  await page.evaluate(() => {
    const f = window.__feed;
    f({ kind: "turn_started" });
    f({ kind: "text", text: "## 一段回答\n\n带 `行内代码`、**加重**，和一条列表：\n\n- 要点一\n\n```go\n// transient at the gateway\nfunc retry(n int) error {\n\tif n >= max {\n\t\treturn fmt.Errorf(\"gave up after %d\", n)\n\t}\n\treturn nil\n}\n```\n\n```\nplain\n```\n" });
    f({ kind: "message" });
    f({ kind: "tool_dispatch", tool: { id: "w1", name: "edit_file", args: '{"path":"a.css"}' } });
    // contextTokens 带上那个第二读数：.cost 的第二个 span 有自己的颜色规则，
    // 没有它这条路径上一个元素都没有，扫过去就是空的。
    f({ kind: "tool_result", tool: { id: "w1", name: "edit_file", args: '{"path":"a.css"}', output: "写入完成", added: 3, removed: 1, durationMs: 120, contextTokens: 82 } });
    f({ kind: "tool_dispatch", tool: { id: "b1", name: "bash", args: '{"command":"go test ./..."}' } });
    f({ kind: "tool_result", tool: { id: "b1", name: "bash", args: '{"command":"go test ./..."}', err: "exit status 1", durationMs: 900 } });
    f({ kind: "approval_request", approval: { id: "a1", tool: "bash", subject: "rm -rf build && make" } });
    f({ kind: "ask_request", ask: { id: "q1", questions: [{ id: "q", header: "选一个", prompt: "走哪条路", options: [{ label: "方案 A" }, { label: "方案 B" }] }] } });
    // Four cards is not the card set. Every kind carries its own ground and its
    // own semantic colours, and ten of them had never been swept.
    f({ kind: "tool_result", tool: { id: "d1", name: "edit_file", args: '{"path":"retry.go"}', diff: "@@ -1,2 +1,3 @@\n-\told()\n+\tif n < max {\n+\t\tnew()", durationMs: 90 } });
    f({ kind: "tool_result", tool: { id: "p1", name: "todo_write", readOnly: true, output: "Todos updated", args: JSON.stringify({ todos: [
      { content: "读历史", status: "completed" },
      { content: "复现", status: "in_progress", activeForm: "正在复现" },
      { content: "给修法", status: "pending", level: 1 },
    ] }) } });
    f({ kind: "tool_result", tool: { id: "r1", name: "read_file", readOnly: true, args: '{"path":"a.go"}', output: "  1→package a" } });
    f({ kind: "tool_result", tool: { id: "r2", name: "read_file", readOnly: true, args: '{"path":"b.go"}', output: "  1→package b" } });
    f({ kind: "tool_result", tool: { id: "g1", name: "read_file", readOnly: true, args: '{"path":"c.go"}', err: "no such file" } });
    f({ kind: "tool_dispatch", tool: { id: "tk", name: "use_capability", resolvedName: "task", profile: { name: "security-review", count: 1 } } });
    f({ kind: "tool_result", tool: { id: "tk-kid", parentId: "tk", name: "grep", readOnly: true, args: '{"pattern":"x"}', output: "a.go:1:x", contextTokens: 40 } });
    f({ kind: "tool_result", tool: { id: "tk", name: "use_capability", resolvedName: "task", profile: { name: "security-review", count: 1 }, output: "没问题", durationMs: 8000 } });
    f({ kind: "tool_result", tool: { id: "gl", name: "update_goal", args: JSON.stringify({ status: "blocked", reason: "缺少抓包" }), output: "ok" } });
    f({ kind: "tool_result", tool: { id: "rm", name: "remember", output: "saved", args: JSON.stringify({ name: "n", title: "标题", description: "一句话", scope: "project", activation: "always", body: "正文" }) } });
    f({ kind: "guardian_assessment", guardian: { id: "gd", tool: "bash", subject: "rm -rf /", outcome: "deny", risk_level: "critical", user_authorization: "unknown", rationale: "越界", duration_ms: 900 } });
    for (const level of ["info", "warn", "error"]) f({ kind: "notice", level, text: `一条 ${level} 提示`, detail: "细节" });
    f({ kind: "compaction_done", compaction: { trigger: "capacity", messages: 40, summary: "折叠了前 40 条。", sourceTokens: 90000, projectionTokens: 6000, coverageRequired: 6, coverageMissing: 2, boundary: "capacity", triggerTokens: 90000 } });
    f({ kind: "extension_surface", extension: { pluginId: "cov", surfaceId: "c", kind: "card", card: { title: "覆盖率", text: "78.4%", fields: [{ key: "语句", value: "78.4%" }], progress: 0.78, actions: [{ actionId: "o", label: "打开" }] } } });
    f({ kind: "extension_surface", extension: { pluginId: "dep", surfaceId: "f", kind: "form", form: { title: "部署", message: "选环境", fields: [{ key: "env", label: "环境", kind: "select", options: ["staging", "prod"], required: true }, { key: "note", label: "备注", kind: "input" }] } } });
    f({ kind: "extension_surface", extension: { pluginId: "ci", surfaceId: "n", kind: "notification", notification: { title: "CI 失败", body: "一个用例红了", severity: "error" } } });
    f({ kind: "extension_surface", extension: { pluginId: "bn", surfaceId: "v", kind: "view", view: { slot: "transcript", body: [
      { kind: "text", value: "基准", tone: "strong" }, { kind: "kv", key: "P50", value: "12ms" },
      { kind: "meter", label: "风险", progress: 0.3, tone: "ok" }, { kind: "pip", tone: "warn" },
      { kind: "divider" }, { kind: "button", label: "明细", actionId: "d" },
    ] } } });
    f({ kind: "turn_done", receipt: { verdict: "partial", changes: [{ path: "a.go", reviewed: true }], verifications: [{ command: "go test ./...", passed: false, stale: true }], gaps: [{ kind: "unverified_mutation", detail: "改了没跑" }], risks: ["只在 darwin 验过"], unverified: ["Windows"], saysSomething: true } });
  });
  await settled(page);
  for (const row of await sweep(page, scheme, "工作台")) found.set(row.key, row);

  await page.locator('[data-action="chrome.settings"]').first().click();
  await settled(page);
  for (const row of await sweep(page, scheme, "设置")) if (!found.has(row.key)) found.set(row.key, row);
  await page.close();
}

const rows = [...found.values()];
const fresh = rows.filter((r) => !KNOWN.has(r.key));
const stale = [...KNOWN].filter((k) => !found.has(k));

for (const r of rows.sort((a, b) => a.got - b.got)) {
  console.log(`  ${r.got.toFixed(2)} < ${r.need}  ${r.key}  ${r.size}px  ${r.where}  「${r.text}」${KNOWN.has(r.key) ? "  (在名单里)" : ""}`);
  if (r.at) console.log(`        ${r.ink} on ${r.ground}   ${r.at}`);
}
check(`没有新的不达标处（名单 ${KNOWN.size} 条）`, fresh.length === 0, `新增 ${fresh.length} 处`);
if (stale.length) console.log(`\n名单里已经修好的 ${stale.length} 条，删掉它们：\n  ` + stale.join("\n  "));

await browser.close();
if (fails.length) {
  console.error(`\n${fails.length} 项不合格：\n  ` + fails.join("\n  "));
  process.exit(1);
}
console.log("\n正文都落在 AA 以上。");
