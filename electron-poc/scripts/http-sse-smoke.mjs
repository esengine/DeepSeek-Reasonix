#!/usr/bin/env node
/**
 * Live smoke: supervise real reasonix serve (if available) and exercise HttpSseHost P0 calls.
 * Writes a machine-readable summary to SCRATCH when REASONIX_POC_SCRATCH is set.
 */
import fs from "node:fs";
import path from "node:path";
import os from "node:os";
import { fileURLToPath } from "node:url";
import { ServeSupervisor } from "../lib/serveSupervisor.mjs";
import { HttpSseHost } from "../lib/httpSseHost.mjs";
import { resolveReasonixBin } from "../lib/resolveReasonixBin.mjs";

const packageRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const repoRoot = path.resolve(packageRoot, "..");

const scratch =
  process.env.REASONIX_POC_SCRATCH ||
  process.env.SCRATCH ||
  "";

function writeLog(name, body) {
  if (!scratch) {
    process.stdout.write(body);
    return;
  }
  fs.mkdirSync(scratch, { recursive: true });
  const p = path.join(scratch, name);
  fs.writeFileSync(p, body);
  process.stdout.write(`wrote ${p}\n`);
}

async function main() {
  let bin;
  try {
    bin = resolveReasonixBin({
      env: process.env,
      repoRoot,
    });
  } catch (e) {
    const msg = `UNAVAILABLE: reasonix binary not found: ${e}\n`;
    writeLog("http-sse-smoke.log", msg);
    process.exitCode = 0; // honest unavailability is OK for criterion 5
    return;
  }

  const stateDir = fs.mkdtempSync(path.join(os.tmpdir(), "rx-smoke-"));
  const workspace = process.env.REASONIX_WORKSPACE || process.cwd();
  const sup = new ServeSupervisor({
    bin,
    stateDir,
    workspace,
    crashRestartOnce: false,
  });

  const lines = [];
  lines.push(`bin=${bin}`);
  lines.push(`workspace=${workspace}`);
  try {
    const info = await sup.start();
    lines.push(`baseUrl=${info.baseUrl}`);
    lines.push(`pid=${info.pid}`);
    lines.push(`uiUrl_has_token=${info.uiUrl.includes("token=")}`);
    lines.push(`argv_has_--token=${info.args.includes("--token")}`);
    lines.push(`argv_has_token_value=${info.args.some((a) => a.includes(info.token))}`);

    const host = new HttpSseHost({ baseUrl: info.baseUrl, token: info.token });
    try {
      const status = await host.status();
      lines.push(`status_ok=${JSON.stringify(status).slice(0, 200)}`);
      const hist = await host.history();
      lines.push(`history_type=${Array.isArray(hist) ? "array" : typeof hist}`);
      // cancel is always safe
      await host.cancel();
      lines.push("cancel=ok");
      // new session may succeed
      try {
        await host.newSession();
        lines.push("newSession=ok");
      } catch (e) {
        lines.push(`newSession=err:${e instanceof Error ? e.message : e}`);
      }
      // events: wait for connected stream (may be quiet)
      let sawEvent = false;
      await new Promise((resolve) => {
        const t = setTimeout(resolve, 1500);
        const unsub = host.onEvent((e) => {
          sawEvent = true;
          lines.push(`event_type=${e.type}`);
          clearTimeout(t);
          unsub();
          resolve();
        });
      });
      lines.push(`sse_event_seen=${sawEvent}`);
      // optional submit only if REASONIX_POC_SUBMIT=1 (avoids burning API)
      if (process.env.REASONIX_POC_SUBMIT === "1") {
        await host.submit("ping from electron-poc smoke");
        lines.push("submit=ok");
      } else {
        lines.push("submit=skipped (set REASONIX_POC_SUBMIT=1 to enable)");
      }
    } finally {
      host.dispose();
    }
    lines.push("RESULT=OK");
    writeLog("http-sse-smoke.log", lines.join("\n") + "\n");
  } catch (e) {
    lines.push(`RESULT=FAIL ${e instanceof Error ? e.stack || e.message : e}`);
    writeLog("http-sse-smoke.log", lines.join("\n") + "\n");
    process.exitCode = 1;
  } finally {
    await sup.stop();
    try {
      fs.rmSync(stateDir, { recursive: true, force: true });
    } catch {
      /* ignore */
    }
  }
}

main();
