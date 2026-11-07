// Cards the reader reads or acts on share one shell: the same radius, border and ground, and a header with no fill of its
// own. A kind that draws its own (a different radius, a state-tinted border, a filled strip) reads as another family.
import { chromium } from "playwright";

const BASE = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const fails = [];
const check = (name, ok, detail = "") => {
  console.log(`${ok ? "  ok" : "FAIL"}  ${name}${detail ? "  — " + detail : ""}`);
  if (!ok) fails.push(name);
};

const browser = await chromium.launch();
for (const scheme of ["light", "dark"]) {
  const ctx = await browser.newContext({ locale: "zh-CN", viewport: { width: 1000, height: 900 }, colorScheme: scheme });
  const page = await ctx.newPage();
  page.on("pageerror", (e) => fails.push("page error: " + e.message));
  await page.goto(BASE, { waitUntil: "networkidle" });
  await page.waitForSelector(".app");
  await page.waitForTimeout(600);
  await page.evaluate(() => {

  const f = window.__feed;
  f({ kind: "turn_started", authoredTurn: 1, msgIndex: 0, text: "跑一遍" });
  f({ kind: "text", text: "我来处理。" });
  f({ kind: "tool_dispatch", tool: { id: "rm", name: "remember", args: '{}' } });
  f({ kind: "tool_result", tool: { id: "rm", name: "remember", output: "saved", args: JSON.stringify({ name: "n", title: "标题", description: "一句话", scope: "project", activation: "always", body: "正文" }) } });
  f({ kind: "guardian_assessment", guardian: { id: "gd", tool: "bash", subject: "rm -rf /", outcome: "deny", risk_level: "critical", user_authorization: "unknown", rationale: "越界", duration_ms: 900 } });
  f({ kind: "compaction_done", compaction: { trigger: "capacity", messages: 40, summary: "折叠了前 40 条。", sourceTokens: 90000, projectionTokens: 6000, coverageRequired: 6, coverageMissing: 2, boundary: "capacity", triggerTokens: 90000 } });
  f({ kind: "extension_surface", extension: { pluginId: "cov", surfaceId: "c", kind: "card", card: { title: "覆盖率", text: "78.4%", fields: [{ key: "语句", value: "78.4%" }], progress: 0.78, actions: [{ actionId: "o", label: "打开" }] } } });
  f({ kind: "turn_done", receipt: { verdict: "partial", changes: [{ path: "a.go", reviewed: true }], verifications: [{ command: "go test ./...", passed: false, stale: true }], gaps: [{ kind: "unverified_mutation", detail: "改了没跑" }], risks: [], unverified: [], saysSomething: true } });
  f({ kind: "turn_started" });
  f({ kind: "approval_request", approval: { id: "a1", tool: "bash", subject: "rm -rf build && make" } });
  f({ kind: "ask_request", ask: { id: "q1", questions: [{ id: "q", header: "选一个", prompt: "走哪条路", options: [{ label: "方案 A" }, { label: "方案 B" }] }] } });
  });
  await page.waitForTimeout(800);
  await page.mouse.move(995, 5);
  const rows = await page.evaluate(() => {
    const read = (kind, shell, hl) => {
      const cs = getComputedStyle(shell);
      return {
        kind,
        radius: cs.borderTopLeftRadius,
        border: cs.borderTopColor,
        bg: cs.backgroundColor,
        head: hl ? getComputedStyle(hl).backgroundColor : "rgba(0, 0, 0, 0)",
      };
    };
    const calls = [...document.querySelectorAll(".flow .call")]
      .filter((c) => ["mem", "host", "extension"].includes(c.getAttribute("data-k")) || c.hasAttribute("data-prompt"))
      .map((c) => read(c.getAttribute("data-k"), c.querySelector(":scope > .c"), c.querySelector(":scope > .c > .hl")));
    return calls;
  });
  check(`${scheme}: three kinds of card found`, rows.length === 3, rows.map((r) => r.kind).join(","));
  const same = (key) => new Set(rows.map((r) => r[key])).size === 1;
  check(`${scheme}: one radius`, rows.length > 0 && same("radius"), [...new Set(rows.map((r) => r.radius))].join(" / "));
  check(`${scheme}: one border colour, whatever the state`, rows.length > 0 && same("border"), [...new Set(rows.map((r) => r.border))].join(" / "));
  check(`${scheme}: one ground`, rows.length > 0 && same("bg"), [...new Set(rows.map((r) => r.bg))].join(" / "));
  check(`${scheme}: no header fill`, rows.length > 0 && rows.every((r) => r.head === "rgba(0, 0, 0, 0)"), [...new Set(rows.map((r) => r.head))].join(" / "));
  await ctx.close();
}
await browser.close();
if (fails.length) {
  console.log(`\n${fails.length} failed`);
  process.exit(1);
}
console.log("\nall ok");
