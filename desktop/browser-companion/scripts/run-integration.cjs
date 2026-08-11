// Integration-test wrapper: Electron's exit paths (app.exit, app.quit,
// process.exit) all hang in some CI/headless environments while renderer
// helper processes linger, so this wrapper spawns Electron, collects its
// stderr, kills it after the authoritative RESULT marker (or a hard
// timeout), and maps that marker to a real exit code. Kill only after
// RESULT: PASS|FAIL|FATAL is fully captured — the human "N/N passed" line
// is written in a separate stderr write and must not trigger termination
// before the machine-readable marker arrives.
const { spawn } = require("node:child_process");
const path = require("node:path");

const electron = path.join(__dirname, "..", "node_modules", ".bin", "electron");
const script = path.join(__dirname, "integration-smoke.cjs");

const child = spawn(electron, [script], { stdio: ["ignore", "pipe", "pipe"] });
let out = "";
let resultSeen = false;
// Note: stderr is NOT streamed live — under piped CI stdio, writes can block
// and stall the wrapper. The result is emitted once, when the child exits.
child.stdout.on("data", (d) => (out += d));
child.stderr.on("data", (d) => {
  out += d;
  // Terminate only after the authoritative RESULT marker is fully in the
  // buffer. Killing on the earlier "N/N passed" line can drop a subsequent
  // RESULT write that lands in a different pipe chunk.
  if (!resultSeen && /RESULT: (PASS|FAIL|FATAL)/.test(out)) {
    resultSeen = true;
    child.kill("SIGKILL");
  }
});

const hardTimeout = setTimeout(() => {
  process.stderr.write("integration smoke: hard timeout, killing electron\n");
  child.kill("SIGKILL");
}, 90000);

let finished = false;
function finish() {
  if (finished) return;
  finished = true;
  clearTimeout(hardTimeout);
  clearInterval(killPoll);
  // The authoritative result is the machine-readable RESULT marker; the
  // human "N/N passed" line alone is rejected (an aborted run can still
  // print an equal count after a caught exception).
  const result = out.match(/RESULT: (PASS|FAIL|FATAL) ?(\d+)?\/?(\d+)?/);
  const marker = result ? result[1] : null;
  const total = result && result[2] !== undefined ? Number(result[3]) : 0;
  const allPass =
    marker === "PASS" &&
    total > 0 &&
    !/TEST ERROR/.test(out) &&
    !/UNHANDLED/.test(out) &&
    !/^FAIL  /m.test(out);
  process.stderr.write(out);
  process.stderr.write(allPass ? `integration smoke: RESULT PASS (${total} checks)\n` : `integration smoke: RESULT ${marker ?? "MISSING"} -> FAILED\n`);
  process.exit(allPass ? 0 : 1);
}

// 'exit' fires when the process ends; 'close' additionally waits for the
// stdio pipes, which is more reliable after a SIGKILL. A poll backs both up:
// in rare cases the exit event never arrives for a killed Electron.
child.on("close", finish);
child.on("exit", finish);
const killPoll = setInterval(() => {
  if (child.exitCode !== null || child.signalCode !== null) {
    finish();
  }
}, 300);

child.on("error", (err) => {
  clearTimeout(hardTimeout);
  process.stderr.write(`integration smoke: spawn failed: ${err.message}\n`);
  process.exit(1);
});
