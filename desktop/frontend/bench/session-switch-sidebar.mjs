import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import path from "node:path";
import { createServer } from "vite";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
process.env.PLAYWRIGHT_BROWSERS_PATH ??= path.join(root, ".pw-browsers");
const { chromium } = await import("playwright");
const server = await createServer({ root, logLevel: "error", server: { host: "127.0.0.1", port: 0 } });
await server.listen();
let browser;
try {
  browser = await chromium.launch({ headless: true });
  const page = await browser.newPage({ viewport: { width: 1280, height: 800 } });
  const errors = [];
  page.on("pageerror", error => errors.push(error.message));
  page.on("console", event => { if (["warning", "error"].includes(event.type())) errors.push(event.text()); });
  await page.goto(`http://127.0.0.1:${server.httpServer.address().port}/bench/session-switch-sidebar.html`);
  await page.getByRole("button", { name: "Open session", exact: true }).waitFor();
  for (let cycle = 0; cycle < 40; cycle++) {
    await page.getByRole("button", { name: "Refresh list", exact: true }).click();
    assert.deepEqual(await page.locator("li").evaluateAll(rows => rows.map(row => row.dataset.session)),
      cycle % 2 ? ["other", "reviewed", "original"] : ["original", "reviewed"]);
  }
  await page.getByRole("button", { name: "Archive original", exact: true }).click();
  assert.equal(await page.locator('li[data-session="original"]').count(), 0);
  assert.equal(await page.locator('li[data-session="reviewed"]').count(), 1);
  await page.getByRole("button", { name: "Refresh sibling", exact: true }).click();
  assert.equal(await page.locator('li[data-session="original"]').count(), 0);
  await page.getByRole("button", { name: "Restore original", exact: true }).click();
  assert.equal(await page.locator('li[data-session="original"]').count(), 1);
  const samples = [];
  for (let run = 0; run < 5; run++) {
    await page.getByRole("button", { name: "Open session", exact: true }).click();
    await page.locator('output[data-status="opening"]').waitFor();
    // Keep publication pending across event turns, as real host IPC does.
    await page.getByRole("button", { name: "Refresh list", exact: true }).click();
    assert.equal(await page.locator("output").getAttribute("data-premature"), "0");
    await page.getByRole("button", { name: "Commit binding", exact: true }).click();
    await page.getByRole("button", { name: "Deliver baseline", exact: true }).click();
    await page.locator('output[data-status="ready"]').waitFor();
    await page.getByRole("button", { name: "Replay notifications", exact: true }).click();
    await page.getByRole("button", { name: "Refresh list", exact: true }).click();
    assert.equal(await page.locator("output").getAttribute("data-reads"), "1");
    assert.equal(await page.locator("output").getAttribute("data-premature"), "0");
    samples.push(JSON.parse(await page.locator("output").textContent()));
  }
  assert.deepEqual(errors, [], "browser console is clean");
  const evidence = process.env.REASONIX_SWITCH_EVIDENCE ?? "/tmp/reasonix-session-switch-sidebar";
  await mkdir(evidence, { recursive: true });
  await page.screenshot({ path: path.join(evidence, "verified.png") });
  await writeFile(path.join(evidence, "results.json"), JSON.stringify({ samples, errors, catalogRefreshes: 50 }, null, 2));
  console.log(`PASS 50 catalog refreshes, 5 canonical opens, one baseline/open, no premature reads; evidence: ${evidence}`);
  await page.goto(`http://127.0.0.1:${server.httpServer.address().port}/bench/session-switch-remote.html`);
  await page.locator('output[data-reads="1"]').waitFor();
  for (let i = 0; i < 5; i++) await page.getByRole("button", { name: "Replay ready", exact: true }).click();
  assert.equal(await page.locator("output").getAttribute("data-reads"), "1");
  await page.getByRole("button", { name: "Deliver latest", exact: true }).click();
  await page.locator('output[data-hydrated="true"][data-session="session-1"]').waitFor();
  await page.getByRole("button", { name: "Replay ready", exact: true }).click();
  assert.equal(await page.locator("output").getAttribute("data-reads"), "1");
  await page.getByRole("button", { name: "New generation", exact: true }).click();
  await page.locator('output[data-reads="2"]').waitFor();
  await page.getByRole("button", { name: "New generation", exact: true }).click();
  await page.locator('output[data-reads="3"]').waitFor();
  await page.getByRole("button", { name: "Deliver latest", exact: true }).click();
  await page.locator('output[data-hydrated="true"][data-session="session-3"]').waitFor();
  await page.getByRole("button", { name: "Deliver oldest", exact: true }).click();
  await page.getByRole("button", { name: "Replay ready", exact: true }).click();
  assert.equal(await page.locator("output").getAttribute("data-session"), "session-3");
  assert.equal(await page.locator("output").getAttribute("data-reads"), "3");
  assert.deepEqual(errors, [], "remote browser console is clean");
  await page.screenshot({ path: path.join(evidence, "remote-verified.png") });
  await writeFile(path.join(evidence, "remote-results.json"), JSON.stringify({ reads: 3, revisions: 3, duplicateReady: 7, staleCutIgnored: true, errors }, null, 2));
  console.log("PASS remote duplicate ready, genuine resets and stale baseline completion in Chromium");
} finally {
  await browser?.close();
  await server.close();
}
