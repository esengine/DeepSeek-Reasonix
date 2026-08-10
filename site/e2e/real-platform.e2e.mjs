import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { existsSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { loadRuntimeConfig } from "../server/config.mjs";
import { createRealAnalysisServer } from "../server/real-analysis-server.mjs";

const here = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(here, "../..");
const siteRoot = path.join(repoRoot, "site");
const keyFile = path.resolve(repoRoot, "..", "apikey.txt");
const fixture = path.join(here, "fixtures", "intelifar-real-analysis.html");
const artifacts = path.join(repoRoot, "artifacts", "real-e2e");
const screenshots = path.join(repoRoot, "artifacts", "screenshots");
await mkdir(artifacts, { recursive: true });
await mkdir(screenshots, { recursive: true });

const runtimeConfig = await loadRuntimeConfig({ keyFile, cwd: siteRoot });
const gateway = await createRealAnalysisServer({ config: runtimeConfig, distRoot: path.join(siteRoot, "dist"), mineruMaxWaitMs: 20 * 60_000 });
const baseURL = await gateway.start();
const workspaceRequire = createRequire(path.join(repoRoot, "..", "e2e-runner.cjs"));
const { chromium } = workspaceRequire("playwright");
const candidates = [
  "C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe",
  "C:\\Program Files (x86)\\Microsoft\\Edge\\Application\\msedge.exe",
];
const executablePath = process.platform === "win32" ? candidates.find(existsSync) : undefined;
const browser = await chromium.launch({ headless: true, executablePath, args: ["--no-proxy-server", "--proxy-bypass-list=<-loopback>"] });
const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: "zh-CN" });
const page = await context.newPage();
const startedAt = Date.now();
let finalJob;
let finalPublication;
let failure;

try {
  await page.goto(baseURL, { waitUntil: "networkidle" });
  await page.evaluate(() => localStorage.clear());
  await page.reload({ waitUntil: "networkidle" });
  const health = await (await fetch(`${baseURL}/api/health`)).json();
  assert.equal(health.mode, "real");
  assert.equal(health.providers.mineru, "configured");
  assert.match(health.model, /^deepseek-v4-/);

  await page.locator("[data-testid='nav-documents']").click();
  await page.locator("[data-testid='documents-upload']").click();
  await page.locator("[data-testid='real-file-input']").setInputFiles(fixture);
  await page.locator("[data-testid='intake-category']").selectOption({ label: "技术设计报告" });
  assert.match(await page.locator("#selected-file").textContent(), /MinerU \+ DeepSeek/);
  const submitResponse = page.waitForResponse((response) => response.url().endsWith("/api/analysis") && response.request().method() === "POST");
  await page.locator("[data-testid='start-analysis']").click();
  const submitted = await (await submitResponse).json();
  assert.match(submitted.job.id, /^JOB-REAL-/);

  let lastState = "";
  const deadline = Date.now() + 22 * 60_000;
  while (Date.now() < deadline) {
    finalJob = await fetch(`${baseURL}/api/analysis/${submitted.job.id}`).then((response) => response.json()).then((value) => value.job);
    if (finalJob.state !== lastState) {
      process.stdout.write(`[real-e2e] ${finalJob.state} ${finalJob.progress}%\n`);
      lastState = finalJob.state;
    }
    if (finalJob.state === "complete" || finalJob.state === "failed") break;
    await new Promise((resolve) => setTimeout(resolve, 3_000));
  }
  assert.equal(finalJob?.state, "complete", finalJob?.error || "Real analysis did not complete before timeout");
  await page.locator("[data-testid='real-analysis-results']:not([hidden])").waitFor({ timeout: 30_000 });

  const providerId = (await page.locator("[data-testid='real-provider-id']").textContent()).trim();
  const model = (await page.locator("[data-testid='real-model-name']").textContent()).trim();
  const tokenText = (await page.locator("[data-testid='real-token-usage']").textContent()).trim();
  assert.ok(providerId.length > 12 && !providerId.includes("batch-test"), "MinerU task ID is not real");
  assert.match(model, /^deepseek-v4-/);
  assert.ok(Number(tokenText.replace(/[^0-9]/g, "")) > 0, "DeepSeek token usage is missing");
  assert.ok(await page.locator("#real-asset-list article").count() > 0, "No real IP assets were rendered");
  assert.ok(await page.locator("#real-source-quotes blockquote").count() > 0, "No real source quotations were rendered");

  await page.screenshot({ path: path.join(screenshots, "10-real-api-analysis.png"), fullPage: true });
  const publishResponsePromise = page.waitForResponse((response) => response.url().endsWith(`/api/analysis/${submitted.job.id}/publish`) && response.request().method() === "POST");
  await page.locator("[data-testid='publish-analysis']").click();
  const publishResponse = await publishResponsePromise;
  assert.ok([200, 201].includes(publishResponse.status()), "Publication endpoint failed");
  finalPublication = (await publishResponse.json()).publication;
  await page.getByText("已发布", { exact: true }).waitFor();
  assert.ok(finalPublication.assets.length > 0, "No assets were published");
  assert.match(finalPublication.assets[0].id, /^IP-REAL-/);
  assert.match(finalPublication.assets[0].evidence[0].id, /^EV-/);
  assert.equal(finalPublication.assets[0].evidence[0].precision, "章节级");

  await page.locator("[data-testid='nav-assets']").click();
  await page.locator(`tr[data-asset-id='${finalPublication.assets[0].id}']`).click();
  await page.locator("#asset-drawer.is-open").waitFor();
  assert.match(await page.locator("#asset-drawer").textContent(), /真实引用/);
  await page.locator("#asset-open-wiki").click();
  await page.locator("#wiki-dynamic-title").waitFor();
  assert.equal((await page.locator("#wiki-dynamic-title").textContent()).trim(), finalPublication.assets[0].title);
  await page.locator("#view-wiki [data-evidence-id]").first().click();
  await page.locator("#provenance-drawer.is-open").waitFor();
  assert.match(await page.locator("#evidence-precision").textContent(), /章节级/);
  assert.match(await page.locator("#evidence-hash").textContent(), /^[a-f0-9]{16}…$/i);
  await page.screenshot({ path: path.join(screenshots, "14-real-published-wiki.png"), fullPage: true });
  const sanitized = JSON.stringify(finalJob, null, 2);
  const sanitizedPublication = JSON.stringify(finalPublication, null, 2);
  for (const secret of [runtimeConfig.mineruApiKey, runtimeConfig.deepseekApiKey]) {
    assert.ok(!sanitized.includes(secret), "A credential leaked into the E2E result");
    assert.ok(!sanitizedPublication.includes(secret), "A credential leaked into the publication result");
  }
  await writeFile(path.join(artifacts, "analysis.json"), sanitized, "utf8");
  await writeFile(path.join(artifacts, "publication.json"), sanitizedPublication, "utf8");
  await writeFile(path.join(artifacts, "mineru-preview.md"), finalJob.result.parser.markdownPreview, "utf8");
} catch (error) {
  failure = error;
  await page.screenshot({ path: path.join(screenshots, "10-real-api-analysis-failure.png"), fullPage: true }).catch(() => {});
} finally {
  await context.close();
  await browser.close();
  await gateway.stop();
}

const duration = Date.now() - startedAt;
const report = [
  "# intelifar MinerU + DeepSeek Real E2E Report",
  "",
  `- Run: ${new Date().toISOString()}`,
  `- Result: ${failure ? "FAIL" : "PASS"}`,
  `- Duration: ${duration} ms`,
  `- Input: ${path.basename(fixture)}`,
  `- MinerU state: ${finalJob?.state ?? "not-started"}`,
  `- MinerU model: ${finalJob?.result?.parser?.model ?? "n/a"}`,
  `- MinerU task: ${finalJob?.result?.parser?.batchId ?? finalJob?.providerTaskId ?? "n/a"}`,
  `- Parsed Markdown: ${finalJob?.result?.parser?.markdownCharacters ?? 0} characters`,
  `- DeepSeek model: ${finalJob?.result?.llm?.model ?? "n/a"}`,
  `- DeepSeek response: ${finalJob?.result?.llm?.responseId ?? "n/a"}`,
  `- DeepSeek tokens: ${finalJob?.result?.llm?.usage?.totalTokens ?? 0}`,
  `- IP assets: ${finalJob?.result?.analysis?.assets?.length ?? 0}`,
  `- Source quotations: ${finalJob?.result?.analysis?.assets?.flatMap((asset) => asset.source_quotes).length ?? 0}`,
  `- Published assets: ${finalPublication?.assets?.length ?? 0}`,
  `- Published evidence precision: ${finalPublication?.assets?.[0]?.evidence?.[0]?.precision ?? "n/a"}`,
  "- Credential leakage scan: PASS (runtime values checked in memory; values not written)",
  failure ? `- Failure: ${String(failure.message).replace(/https?:\/\/\S+/g, "[redacted-url]").slice(0, 240)}` : "",
  "",
].filter(Boolean).join("\n");
const fixtureSource = await readFile(fixture, "utf8");
assert.ok(!fixtureSource.includes(runtimeConfig.mineruApiKey) && !fixtureSource.includes(runtimeConfig.deepseekApiKey));
await writeFile(path.join(artifacts, "report.md"), `\uFEFF${report}`, "utf8");
if (failure) throw failure;
process.stdout.write(`Real E2E passed in ${duration} ms: MinerU + ${finalJob.result.llm.model}, ${finalJob.result.llm.usage.totalTokens} tokens.\n`);
