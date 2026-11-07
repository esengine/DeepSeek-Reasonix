// An allowed guardian review at low risk is a Line, not a card: one row over a hairline with no box.
// A refusal, or an allow that carried real risk, keeps its note, and a remembered fact is a note that carries its take-back.
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
    f({ kind: "turn_started", authoredTurn: 1, msgIndex: 0, text: "go" });
    f({ kind: "tool_dispatch", tool: { id: "rm", name: "remember", args: "{}" } });
    f({ kind: "tool_result", tool: { id: "rm", name: "remember", output: "saved", args: JSON.stringify({ name: "n", title: "A fact", description: "why", scope: "project", activation: "always", body: "b" }) } });
    f({ kind: "guardian_assessment", guardian: { id: "g1", tool: "bash", subject: "go test", outcome: "allow", risk_level: "low", user_authorization: "high", rationale: "read only", duration_ms: 5 } });
    f({ kind: "guardian_assessment", guardian: { id: "g2", tool: "bash", subject: "git push -f", outcome: "allow", risk_level: "high", user_authorization: "low", rationale: "rewrites history", duration_ms: 5 } });
    f({ kind: "guardian_assessment", guardian: { id: "g3", tool: "bash", subject: "rm -rf /", outcome: "deny", risk_level: "critical", user_authorization: "unknown", rationale: "no", duration_ms: 5 } });
    f({ kind: "turn_done" });
  });
  await page.waitForTimeout(600);
  await page.mouse.move(995, 5);
  const m = await page.evaluate(() => {
    const rows = [...document.querySelectorAll(".flow .line-row")];
    const box = (el) => { const cs = getComputedStyle(el); return [cs.borderTopWidth, cs.borderLeftWidth, cs.borderRightWidth].join(","); };
    return {
      lines: rows.length,
      boxes: rows.map(box),
      radius: rows.map((r) => getComputedStyle(r).borderTopLeftRadius),
      cards: [...document.querySelectorAll(".flow .note")].filter((n) => n.textContent.includes("守卫复核")).length,
      forget: !!document.querySelector('.flow .note [data-action="memory.forget"]'),
      oldMem: document.querySelectorAll('.flow .call[data-k="mem"]').length,
    };
  });
  check(`${scheme}: the low-risk allow is a row`, m.lines === 1, `${m.lines} rows`);
  check(`${scheme}: rows carry no box`, m.lines > 0 && m.boxes.every((b) => b === "0px,0px,0px") && m.radius.every((r) => r === "0px"), JSON.stringify(m));
  check(`${scheme}: the high-risk allow and the refusal keep their notes`, m.cards === 2, `${m.cards} cards`);
  check(`${scheme}: the remembered fact keeps its take-back, and no memory card remains`, m.forget && m.oldMem === 0);
  await ctx.close();
}
await browser.close();
if (fails.length) {
  console.log(`\n${fails.length} failed`);
  process.exit(1);
}
console.log("\nall ok");
