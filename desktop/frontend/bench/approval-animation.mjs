#!/usr/bin/env node

import { spawn } from "node:child_process";
import http from "node:http";
import path from "node:path";
import { fileURLToPath } from "node:url";

const frontendDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
process.env.PLAYWRIGHT_BROWSERS_PATH = !process.env.PLAYWRIGHT_BROWSERS_PATH || process.env.PLAYWRIGHT_BROWSERS_PATH === ".pw-browsers"
  ? path.join(frontendDir, ".pw-browsers")
  : process.env.PLAYWRIGHT_BROWSERS_PATH;
const { chromium } = await import("playwright");
const port = Number(process.env.REASONIX_APPROVAL_BROWSER_PORT ?? 4620);
const url = `http://127.0.0.1:${port}/`;

function assert(condition, message) {
  if (!condition) throw new Error(message);
  process.stdout.write(`  PASS  ${message}\n`);
}

async function waitForServer() {
  const deadline = Date.now() + 30_000;
  while (Date.now() < deadline) {
    const ready = await new Promise((resolve) => {
      const request = http.get(url, (response) => {
        response.resume();
        resolve((response.statusCode ?? 500) < 500);
      });
      request.on("error", () => resolve(false));
    });
    if (ready) return;
    await new Promise((resolve) => setTimeout(resolve, 150));
  }
  throw new Error("approval browser preview did not become ready");
}

const preview = spawn("pnpm", ["exec", "vite", "preview", "--port", String(port), "--strictPort", "--host", "127.0.0.1"], {
  cwd: frontendDir,
  stdio: "ignore",
});

let browser;
try {
  await waitForServer();
  browser = await chromium.launch({ headless: true });
  const page = await browser.newPage({ viewport: { width: 1280, height: 800 } });
  const pageErrors = [];
  page.on("pageerror", (error) => pageErrors.push(String(error)));
  await page.addInitScript(() => {
    window.__reasonixApprovalAnimationCalls = [];
    const original = Element.prototype.animate;
    Element.prototype.animate = function (frames, options) {
      if (this instanceof HTMLElement && this.querySelector(".prompt-shelf")) {
        window.__reasonixApprovalAnimationCalls.push({
          easing: typeof options === "object" && options ? String(options.easing ?? "") : "",
        });
      }
      return original.call(this, frames, options);
    };
  });

  await page.goto(url, { waitUntil: "domcontentloaded" });
  await page.waitForFunction(() => !document.querySelector(".startup-splash"), undefined, { timeout: 30_000 });
  const composer = page.locator("#composer-input");
  await composer.waitFor({ state: "visible", timeout: 30_000 });
  await composer.fill("/mock-tool-approval");
  await page.locator(".composer__btn--send").click();

  const action = page.locator(".prompt-shelf__actions .prompt-action").first();
  await action.waitFor({ state: "visible", timeout: 30_000 });
  await action.click();
  await page.locator(".decision-confirm-bar__confirm").click();
  await page.waitForFunction(() => !document.querySelector(".prompt-shelf--tool-approval"), undefined, { timeout: 10_000 });

  const calls = await page.evaluate(() => window.__reasonixApprovalAnimationCalls ?? []);
  assert(calls.length === 1, `approval invokes one native Web Animation (${JSON.stringify(calls)})`);
  assert(calls[0]?.easing === "cubic-bezier(0.8, 0, 0.8, 0.28)", `approval uses the CSS easing contract (${calls[0]?.easing})`);
  assert(pageErrors.length === 0, `approval interaction completes without page errors (${JSON.stringify(pageErrors)})`);
  process.stdout.write("\napproval animation browser gate passed\n");
} finally {
  await browser?.close();
  preview.kill("SIGTERM");
}
