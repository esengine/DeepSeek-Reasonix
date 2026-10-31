import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
import { chromium } from "playwright";

const base = process.env.PERF_URL ?? "http://localhost:4399/perf.html";
const shots = process.env.TOOL_OUTPUT_SHOTS;
if (shots) await mkdir(shots, { recursive: true });
const browser = await chromium.launch();
const failures = [];
const errors = [];
const check = (name, ok) => {
  console.log(`${ok ? "ok" : "FAIL"} ${name}`);
  if (!ok) failures.push(name);
};
// The page's grain adds small channel differences between neighbouring pixels.
const sameGround = (a, b) => a.every((channel, i) => Math.abs(channel - b[i]) <= 2);
const search = Array.from({ length: 8 }, (_, i) =>
  `- **Search result ${i + 1}: release notes and model information**\n  <https://example.com/news/${"long-source-path-".repeat(12)}${i + 1}>`,
).join("\n");
const terminal = Array.from({ length: 20 }, (_, i) => `line ${i + 1}: ${"output ".repeat(40)}`).join("\n");
const calls = [
  { id: "clip-search", name: "web_search", args: '{"query":"release notes"}', output: search },
  { id: "clip-terminal", name: "bash", args: '{"command":"print results"}', output: terminal },
  { id: "clip-short", name: "bash", args: '{"command":"echo ok"}', output: "ok" },
];
try {
  for (const scheme of ["light", "dark"]) for (const width of [1440, 390]) {
    const context = await browser.newContext({ viewport: { width, height: 1000 }, colorScheme: scheme, reducedMotion: "reduce", permissions: ["clipboard-read", "clipboard-write"] });
    await context.addInitScript((theme) => localStorage.setItem("rx-theme", theme), scheme);
    const page = await context.newPage();
    page.on("pageerror", (error) => errors.push(error.message));
    await page.goto(`${base}?pref=en&turns=0`, { waitUntil: "networkidle" });
    await page.waitForSelector(".compose");
    await page.evaluate((calls) => {
      window.__feed({ kind: "turn_started", authoredTurn: 1, msgIndex: 0, text: "Inspect search and terminal results" });
      for (const call of calls) {
        const { output, ...tool } = call;
        window.__feed({ kind: "tool_dispatch", tool });
        window.__feed({ kind: "tool_result", tool: { ...tool, output, durationMs: 499 } });
      }
      window.__feed({ kind: "turn_done" });
    }, calls);
    const group = page.locator("details.activity-group");
    await group.locator(":scope > summary").click();
    const card = (id) => page.locator(`[data-call="${id}"]`);
    for (const call of calls) await card(call.id).locator("summary.hl").click();
    await page.evaluate(() => document.fonts.ready);
    const searchCard = card("clip-search");
    await searchCard.locator(".out-more").waitFor();
    await searchCard.scrollIntoViewIfNeeded();
    if (shots) await page.screenshot({ path: `${shots}/${scheme}-${width}.png`, animations: "disabled" });
    const rect = await searchCard.locator(".out-clip").boundingBox();
    const pixels = await page.evaluate(async ({ png, rect }) => {
      const image = await createImageBitmap(new Blob([Uint8Array.from(atob(png), (c) => c.charCodeAt(0))], { type: "image/png" }));
      const canvas = new OffscreenCanvas(image.width, image.height);
      const ctx = canvas.getContext("2d");
      ctx.drawImage(image, 0, 0);
      const pixel = (x, y) => [...ctx.getImageData(Math.floor(x), Math.floor(y), 1, 1).data];
      return [
        pixel(rect.x + rect.width - 3, rect.y + rect.height - 3),
        pixel(rect.x + rect.width + 3, rect.y + rect.height - 3),
        pixel(rect.x + rect.width - 3, rect.y + 2),
        pixel(rect.x + rect.width + 3, rect.y + 2),
      ];
    }, { png: (await page.screenshot({ animations: "disabled" })).toString("base64"), rect });
    check(`${scheme} ${width}: no painted shadow beside Show all`, sameGround(pixels[0], pixels[1]));
    check(`${scheme} ${width}: no right-edge shadow on the result ground`, sameGround(pixels[2], pixels[3]));

    for (const call of calls.slice(0, 2)) {
      const current = card(call.id);
      const body = current.locator(".out-body");
      const dimensions = () => body.evaluate((el) => ({ height: el.clientHeight, full: el.scrollHeight, width: el.clientWidth, fullWidth: el.scrollWidth, left: el.scrollLeft }));
      const clipped = await dimensions();
      assert.ok(clipped.full > clipped.height, `${call.id} starts clipped`);
      await current.getByRole("button", { name: "Show all", exact: true }).click();
      await page.waitForFunction((id) => {
        const body = document.querySelector(`[data-call="${id}"] .out-body`);
        return body.clientHeight >= body.scrollHeight - 1;
      }, call.id);
      if (call.name === "web_search") assert.equal(await body.locator(".hit-row").count(), 8);
      if (call.name === "bash") {
        assert.equal(await body.textContent(), terminal, "expansion preserves every terminal line");
        assert.ok(clipped.fullWidth > clipped.width, "long lines overflow horizontally");
        await body.evaluate((el) => { el.scrollLeft = el.scrollWidth; });
        assert.ok((await dimensions()).left > 0, "long lines remain horizontally scrollable");
        await body.evaluate((el) => { el.scrollLeft = 0; });
      }
      await current.locator("summary.hl").click();
      await current.locator("summary.hl").click();
      assert.equal(await current.locator(".out-clip").getAttribute("data-open"), "", "Show all survives closing and reopening the card");
      await current.getByRole("button", { name: "Collapse", exact: true }).click();
      await current.getByRole("button", { name: "Show all", exact: true }).waitFor();
      await page.waitForFunction((id) => {
        const body = document.querySelector(`[data-call="${id}"] .out-body`);
        return body.scrollHeight > body.clientHeight + 1;
      }, call.id);
      console.log(`ok ${scheme} ${width}: ${call.name} expands, retains content and disclosure state, and collapses`);
    }
    await searchCard.getByRole("button", { name: "Copy", exact: true }).click();
    assert.equal(await page.evaluate(() => navigator.clipboard.readText()), search, "copy includes every result, not just the visible preview");
    assert.equal(await card("clip-short").locator(".out-more").count(), 0, "short output does not acquire a Show all control");
    assert.equal(await card("clip-short").locator(".term").textContent(), "ok");
    console.log(`ok ${scheme} ${width}: full-result copy and short output`);
    await context.close();
  }
  assert.deepEqual(errors, [], "no browser errors");
  assert.deepEqual(failures, [], "clipping must not paint an unrelated surface over results or controls");
} finally {
  await browser.close();
}
