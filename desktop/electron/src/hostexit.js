"use strict";
const fs = require("node:fs");
const path = require("node:path");

const CRASH_DIR = "crash";
const MARKER = "unexpected-host-exit.json";
const CRASH_FILE = /^host-.*\.log$/;
const TAIL_BYTES = 8192;
const TAIL_LINES = 30;

const crashDir = (logsDir) => path.join(logsDir, CRASH_DIR);
const markerFile = (userData) => path.join(userData, MARKER);

// Only a file written during this launch can belong to this death.
function newestCrashFile(dir, startedAt) {
  let best = null;
  let names = [];
  try {
    names = fs.readdirSync(dir);
  } catch {}
  for (const name of names) {
    if (!CRASH_FILE.test(name)) continue;
    try {
      const at = fs.statSync(path.join(dir, name)).mtimeMs;
      if (at >= startedAt && (!best || at > best.at)) best = { name, at };
    } catch {}
  }
  return best?.name ?? "";
}

function tailOf(file) {
  try {
    const fd = fs.openSync(file, "r");
    try {
      const size = fs.fstatSync(fd).size;
      const len = Math.min(size, TAIL_BYTES);
      const buf = Buffer.alloc(len);
      fs.readSync(fd, buf, 0, len, size - len);
      return buf.toString("utf8").split(/\r?\n/).filter((l) => l.trim() !== "").slice(-TAIL_LINES).join("\n");
    } finally {
      fs.closeSync(fd);
    }
  } catch {
    return "";
  }
}

// recordHostExit is for a kernel that died after its handshake without the
// shell asking it to: the cause goes to shell.log, and a marker waits for the
// next launch to tell the person.
function recordHostExit({ logs, userData, code, signal, startedAt, now = () => new Date() }) {
  const dir = crashDir(logs.dir);
  const crashFile = newestCrashFile(dir, startedAt);
  logs.shell.line(`host: died code=${code} signal=${signal} crash=${crashFile || "(none)"}`);
  const tail = crashFile ? tailOf(path.join(dir, crashFile)) : "";
  if (tail) logs.shell.line(`host: last output of ${crashFile}:\n${tail}`);
  try {
    fs.writeFileSync(markerFile(userData), JSON.stringify({ at: now().toISOString(), code, signal, crashFile }), { mode: 0o600 });
  } catch {}
}

function pendingHostExit(userData) {
  try {
    const seen = JSON.parse(fs.readFileSync(markerFile(userData), "utf8"));
    return seen && typeof seen === "object" ? seen : null;
  } catch {
    return null;
  }
}

function clearHostExit(userData) {
  try {
    fs.rmSync(markerFile(userData), { force: true });
  } catch {}
}

function hostExitNotice(locale, exit, logsDir) {
  const zh = String(locale).toLowerCase().startsWith("zh");
  const how = exit?.signal ? `signal ${exit.signal}` : `code ${exit?.code}`;
  if (zh) {
    return {
      message: "上次运行时内核异常退出了",
      ok: "知道了",
      detail: `内核进程在上次使用中途停止（${exit?.signal ? `信号 ${exit.signal}` : `退出码 ${exit?.code}`}）。会话记录不受影响。反馈问题时请附上这个目录里的 shell.log、host.log 和 crash 文件夹：\n\n${logsDir}`,
    };
  }
  return {
    message: "The kernel exited unexpectedly last time",
    ok: "OK",
    detail: `The kernel process stopped partway through the last session (${how}). Your sessions are not affected. When reporting a problem, attach shell.log, host.log and the crash folder from:\n\n${logsDir}`,
  };
}

module.exports = { crashDir, recordHostExit, pendingHostExit, clearHostExit, hostExitNotice };
