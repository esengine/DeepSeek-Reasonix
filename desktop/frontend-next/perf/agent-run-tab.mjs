// A sub-agent opened from the deck lands beside the conversation as a workbench tab that reads the kernel's record;
// a call with no run keeps the full-record dialog.
import { chromium } from "playwright";

const BASE = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const SHOTS = process.env.SHOTS;
const fails = [];
const check = (name, ok, detail = "") => {
  console.log(`${ok ? "  ok" : "FAIL"}  ${name}${detail ? "  — " + detail : ""}`);
  if (!ok) fails.push(name);
};

const calls = [
  { id: "c1", tool: "list_dir", args: '{"path":"internal/auth"}', out: "session.go\nstore.go\ntoken.go" },
  { id: "c2", tool: "read_file", args: '{"path":"internal/auth/session.go"}', out: "package auth\n\nfunc New() *Session { return &Session{} }" },
];
const messages = [
  { role: "user", content: "Audit the session code for token reuse.", msgIndex: 1 },
  { role: "assistant", content: "I will list the package, then read the session file.", msgIndex: 2, toolCalls: calls.map((c) => ({ id: c.id, name: c.tool, arguments: c.args })) },
  ...calls.map((c, i) => ({ role: "tool", content: c.out, toolCallId: c.id, toolName: c.tool, msgIndex: 3 + i })),
  { role: "assistant", content: "## Finding\n\nTokens are rotated on every refresh; I found no reuse path.", msgIndex: 5 },
];
const runs = [{ run: { ref: "sa_1", name: "security-review", kind: "task", model: "deepseek/deepseek-v4-pro", state: "completed", parentCall: "a1", startedAt: 1, updatedAt: 2 }, messages }];

const browser = await chromium.launch();
for (const scheme of ["light", "dark"]) {
  const page = await browser.newPage({ viewport: { width: 1440, height: 900 }, colorScheme: scheme, reducedMotion: "reduce" });
  page.setDefaultTimeout(8000);
  page.on("pageerror", (e) => fails.push("page error: " + e.message));
  await page.goto(`${BASE}?pref=en`, { waitUntil: "networkidle" });
  await page.waitForSelector(".app");
  await page.evaluate((runs) => {
    window.__subagents(runs);
    const args = '{"description":"review","subagent_type":"security-review"}';
    for (const [id, name] of [["a1", "security-review"], ["a2", "other"]]) {
      window.__feed({ kind: "tool_dispatch", tool: { id, name: "task", profile: { name }, args } });
      window.__feed({ kind: "tool_result", tool: { id, name: "task", profile: { name }, args, output: "done" } });
    }
  }, runs);
  await page.evaluate(() => {
    window.__feed({ kind: "turn_started", authoredTurn: 1, msgIndex: 0, text: "go" });
    window.__feed({ kind: "text", text: "Both reviews are done." });
  });
  await page.waitForSelector(".studio-deckchip");
  await page.locator('[data-action="deck.agents"]').hover();
  await page.locator('.studio-deck-pop .ag-open[data-target]').first().waitFor();
  const rows = await page.locator(".studio-deck-pop .ag").count();
  check(`${scheme}: the deck lists both delegations`, rows === 2, `rows=${rows}`);

  await page.locator('.studio-deck-pop .ag', { hasText: "security-review" }).locator('[data-action="agent.open"]').click();
  await page.locator('.workbench-tab-pick', { hasText: "security-review" }).waitFor();
  await page.locator(".agentrun .txt", { hasText: "rotated on every refresh" }).waitFor();
  await page.waitForFunction(() => !document.querySelector(".agentrun")?.textContent.includes("## Finding"));
  const text = await page.locator(".agentrun").evaluate((el) => el.textContent);
  check(`${scheme}: the run opens as a workbench tab, not a dialog`, (await page.locator(".agent-tx").count()) === 0);
  check(`${scheme}: it shows the model, the state and the final answer`, /deepseek-v4-pro/.test(text) && /Completed|Done/.test(text) && /rotated on every refresh/.test(text), text.slice(0, 160));
  check(`${scheme}: the user's task is in it and the tool steps are folded under one line`, /Audit the session code/.test(text) && (await page.locator(".agentrun .activity-group").count()) === 1 && (await page.locator(".agentrun .activity-group[open]").count()) === 0);
  await page.locator(".agentrun .activity-group > summary").click();
  await page.locator(".agentrun .activity-body").waitFor();
  check(`${scheme}: opening the fold shows the steps`, /list_dir|List|session\.go/.test(await page.locator(".agentrun .activity-body").evaluate((el) => el.textContent)));
  const box = await page.evaluate(() => {
    const hd = document.querySelector(".agentrun-hd").getBoundingClientRect();
    const wb = document.querySelector(".workbench").getBoundingClientRect();
    const flow = document.querySelector(".agentrun .flow").getBoundingClientRect();
    return { hdRight: hd.right, wbRight: wb.right, flowRight: flow.right, flowLeft: flow.left, wbLeft: wb.left };
  });
  if (SHOTS) await page.screenshot({ path: `${SHOTS}-tab-${scheme}.png` });
  check(`${scheme}: the record stays inside the workbench column`, box.flowRight <= box.wbRight + 1 && box.flowLeft >= box.wbLeft - 1, JSON.stringify(box));

  await page.locator('[data-action="deck.agents"]').hover();
  await page.locator('.studio-deck-pop .ag', { hasText: "other" }).locator('[data-action="agent.open"]').click();
  await page.locator(".agent-tx").waitFor();
  check(`${scheme}: a call with no run keeps the full-record dialog`, true);
  await page.close();
}
await browser.close();
if (fails.length) {
  console.log(`\n${fails.length} failed`);
  process.exit(1);
}
console.log("\nall ok");
