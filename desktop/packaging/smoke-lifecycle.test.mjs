import assert from "node:assert/strict";
import { fork } from "node:child_process";
import { once } from "node:events";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { closeAndVerify, processAlive, waitForProcessesToExit } from "./smoke-lifecycle.mjs";

async function ownedProcess(t) {
  const directory = mkdtempSync(join(tmpdir(), "reasonix-smoke-lifecycle-"));
  const script = join(directory, "child.cjs");
  writeFileSync(script, 'process.on("message", () => process.exit(0)); process.send("ready");\n');
  const child = fork(script, [], { stdio: ["ignore", "ignore", "ignore", "ipc"] });
  t.after(async () => {
    if (processAlive(child.pid)) {
      const exited = once(child, "exit");
      child.kill("SIGKILL");
      await exited;
    }
    rmSync(directory, { recursive: true, force: true });
  });
  await once(child, "message");
  return child;
}

test("normal app close must end both the actual shell and the Go service", async (t) => {
  const shell = await ownedProcess(t);
  const service = await ownedProcess(t);
  let closeCalls = 0;
  await closeAndVerify({ close: async () => { closeCalls++; shell.send("quit"); service.send("quit"); } }, { shellPid: shell.pid, servicePid: service.pid });
  assert.equal(closeCalls, 1);
  assert.equal(processAlive(shell.pid), false);
  assert.equal(processAlive(service.pid), false);
});

test("a live process is a verification failure and is never force-killed by verification", async (t) => {
  const service = await ownedProcess(t);
  await assert.rejects(waitForProcessesToExit([service.pid], 0), /processes outlived normal app quit/);
  assert.equal(processAlive(service.pid), true);
});

test("a hung normal app close fails without pretending forced cleanup is success", async (t) => {
  const shell = await ownedProcess(t);
  await assert.rejects(closeAndVerify({ close: () => new Promise(() => {}) }, { shellPid: shell.pid, servicePid: shell.pid }, 10), /normal app quit did not complete/);
  assert.equal(processAlive(shell.pid), true);
});
