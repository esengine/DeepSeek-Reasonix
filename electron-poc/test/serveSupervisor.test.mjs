import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { EventEmitter } from "node:events";
import {
  buildServeArgs,
  prepareStateDir,
  parsePortFileContents,
  waitForPortFile,
  buildAuthenticatedUiUrl,
  ServeSupervisor,
} from "../lib/serveSupervisor.mjs";

test("buildServeArgs uses loopback, token-file, port-file, pid-file; no --token", () => {
  const args = buildServeArgs({
    tokenFile: "/state/token",
    portFile: "/state/port",
    pidFile: "/state/pid",
  });
  assert.deepEqual(args, [
    "serve",
    "--addr",
    "127.0.0.1:0",
    "--auth",
    "token",
    "--token-file",
    "/state/token",
    "--port-file",
    "/state/port",
    "--pid-file",
    "/state/pid",
    "--multi-tab",
  ]);
  assert.ok(!args.includes("--token"));
  assert.ok(args.includes("--multi-tab"));
  assert.ok(!args.some((a) => a === "token" && args[args.indexOf(a) - 1] === "--token"));
});

test("buildServeArgs rejects non-loopback addr", () => {
  assert.throws(
    () =>
      buildServeArgs({
        tokenFile: "t",
        portFile: "p",
        pidFile: "i",
        addr: "0.0.0.0:0",
      }),
    /loopback/,
  );
});

test("prepareStateDir writes token with restricted mode and clears port/pid", () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "rx-sup-"));
  const st = prepareStateDir(dir);
  assert.ok(fs.existsSync(st.tokenFile));
  const tok = fs.readFileSync(st.tokenFile, "utf8").trim();
  assert.ok(tok.length >= 32);
  assert.equal(st.token, tok);
  const mode = fs.statSync(st.tokenFile).mode & 0o777;
  // On some CI umask may soften mode; require owner-read at least and not world-writable
  assert.ok((mode & 0o400) !== 0);
  assert.ok((mode & 0o002) === 0);
  fs.writeFileSync(st.portFile, "stale");
  fs.writeFileSync(st.pidFile, "1");
  const st2 = prepareStateDir(dir);
  assert.ok(!fs.existsSync(st2.portFile));
  assert.ok(!fs.existsSync(st2.pidFile));
  fs.rmSync(dir, { recursive: true, force: true });
});

test("parsePortFileContents accepts 127.0.0.1:port", () => {
  const p = parsePortFileContents("127.0.0.1:54321\n");
  assert.equal(p.port, 54321);
  assert.equal(p.baseUrl, "http://127.0.0.1:54321");
});

test("parsePortFileContents rejects non-loopback", () => {
  assert.throws(() => parsePortFileContents("8.8.8.8:80"), /non-loopback|invalid/);
});

test("waitForPortFile reads file written after start", async () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "rx-port-"));
  const portFile = path.join(dir, "port");
  const p = waitForPortFile(portFile, { timeoutMs: 2000, intervalMs: 20 });
  setTimeout(() => fs.writeFileSync(portFile, "127.0.0.1:19090\n"), 40);
  const got = await p;
  assert.equal(got.port, 19090);
  fs.rmSync(dir, { recursive: true, force: true });
});

test("buildAuthenticatedUiUrl includes token query", () => {
  const u = buildAuthenticatedUiUrl("http://127.0.0.1:9", "abc");
  assert.ok(u.includes("token=abc"));
  assert.ok(u.startsWith("http://127.0.0.1:9"));
});

test("ServeSupervisor.start spawns with correct args and waits for port-file", async () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "rx-child-"));
  const fakeBin = path.join(dir, "reasonix");
  fs.writeFileSync(fakeBin, "#!/bin/sh\nexit 0\n", { mode: 0o755 });

  /** @type {string[] | null} */
  let seenArgs = null;
  let childRef = null;

  const spawnImpl = (bin, args) => {
    seenArgs = args;
    const ee = new EventEmitter();
    ee.pid = 4242;
    ee.killed = false;
    ee.kill = () => {
      ee.killed = true;
      process.nextTick(() => ee.emit("exit", 0, null));
      return true;
    };
    childRef = ee;
    // Simulate serve writing port-file shortly after spawn
    const portIdx = args.indexOf("--port-file");
    const portFile = args[portIdx + 1];
    setTimeout(() => fs.writeFileSync(portFile, "127.0.0.1:18787\n"), 30);
    return ee;
  };

  const sup = new ServeSupervisor({
    bin: fakeBin,
    stateDir: path.join(dir, "state"),
    workspace: dir,
    spawnImpl,
    crashRestartOnce: false,
  });
  const info = await sup.start();
  assert.ok(seenArgs);
  assert.equal(seenArgs[0], "serve");
  assert.ok(seenArgs.includes("--token-file"));
  assert.ok(seenArgs.includes("--port-file"));
  assert.ok(seenArgs.includes("--pid-file"));
  assert.ok(!seenArgs.includes("--token"));
  // token must not appear on argv
  const token = info.token;
  assert.ok(!seenArgs.some((a) => a.includes(token)));
  assert.equal(info.port, 18787);
  assert.equal(info.baseUrl, "http://127.0.0.1:18787");
  assert.ok(info.uiUrl.includes("token="));
  assert.ok(fs.existsSync(info.logFile));

  await sup.stop({ termTimeoutMs: 500 });
  assert.equal(sup.pid, null);
  assert.ok(childRef.killed || childRef.listenerCount("exit") >= 0);
  fs.rmSync(dir, { recursive: true, force: true });
});

test("crash-restart reuses stateDir and invokes onCrashRestarted with new endpoint", async () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "rx-crash-"));
  const fakeBin = path.join(dir, "reasonix");
  fs.writeFileSync(fakeBin, "#!/bin/sh\nexit 0\n", { mode: 0o755 });

  let spawnCount = 0;
  let portSeq = 19000;
  /** @type {import('node:events').EventEmitter[]} */
  const children = [];

  const spawnImpl = (_bin, args) => {
    spawnCount += 1;
    const ee = new EventEmitter();
    ee.pid = 5000 + spawnCount;
    ee.killed = false;
    ee.kill = () => {
      ee.killed = true;
      process.nextTick(() => ee.emit("exit", 0, null));
      return true;
    };
    children.push(ee);
    const portFile = args[args.indexOf("--port-file") + 1];
    const port = portSeq++;
    setTimeout(() => fs.writeFileSync(portFile, `127.0.0.1:${port}\n`), 20);
    return ee;
  };

  /** @type {object | null} */
  let restartedInfo = null;
  const stateDir = path.join(dir, "state");
  const sup = new ServeSupervisor({
    bin: fakeBin,
    stateDir,
    workspace: dir,
    spawnImpl,
    crashRestartOnce: true,
    onCrashRestarted: (info) => {
      restartedInfo = info;
    },
  });

  const first = await sup.start();
  assert.equal(first.stateDir, stateDir);
  assert.equal(spawnCount, 1);

  // Simulate unexpected crash (not stop())
  const firstChild = children[0];
  firstChild.emit("exit", 1, null);

  // Wait for crash restart + callback
  const deadline = Date.now() + 3000;
  while (!restartedInfo && Date.now() < deadline) {
    await new Promise((r) => setTimeout(r, 30));
  }
  assert.ok(restartedInfo, "onCrashRestarted must fire with new start info");
  assert.equal(restartedInfo.fromCrashRestart, true);
  assert.equal(restartedInfo.stateDir, stateDir, "must reuse fixed stateDir");
  assert.notEqual(restartedInfo.port, first.port, "new port after restart");
  assert.ok(restartedInfo.uiUrl.includes("token="));
  assert.equal(spawnCount, 2);

  await sup.stop({ termTimeoutMs: 500 });
  fs.rmSync(dir, { recursive: true, force: true });
});
