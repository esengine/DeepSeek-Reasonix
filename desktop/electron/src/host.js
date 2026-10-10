"use strict";
const { spawn } = require("node:child_process");

const PROTOCOL_VERSION = 1;
// A child that never sends a handshake must not be able to grow this process
// while it waits, nor hold the launch open forever.
const HANDSHAKE_LIMIT = 4096;
const HANDSHAKE_TIMEOUT_MS = 60000;
const SLOW_AFTER_MS = 3000;

// HostStartError says why a launch has no kernel. code is one of "timeout",
// "exited", "spawn", "overflow" or "malformed"; callers branch on it, never on
// the sentence.
class HostStartError extends Error {
  constructor(code, message, detail = {}) {
    super(message);
    this.name = "HostStartError";
    this.code = code;
    Object.assign(this, detail);
  }
}

// start spawns the kernel and reads the handshake it writes to stdout. Every
// line after it is an act a handover asks of this process, because that pipe is
// the only channel pointing this way. stdin is the lease: the kernel drains
// when this end closes, which is what stops it outliving a parent that exited
// without being asked to.
function start(binary, args, { onStderr, onExit, onAct, onSlow, systemLanguage, timeoutMs = HANDSHAKE_TIMEOUT_MS } = {}) {
  const env = { ...process.env };
  if (systemLanguage) env.REASONIX_SYSTEM_LANG = systemLanguage;
  const child = spawn(binary, args, { stdio: ["pipe", "pipe", "pipe"], env });
  child.stderr.setEncoding("utf8");
  if (onStderr) child.stderr.on("data", onStderr);
  if (onExit) child.on("exit", onExit);
  const ready = firstLine(child, { onSlow, timeoutMs }).then(({ line, rest }) => {
    // Only once the handshake parses: a child whose first line was not one has
    // said nothing this process should act on.
    let handshake;
    try {
      handshake = parse(line);
    } catch (err) {
      throw new HostStartError("malformed", err.message);
    }
    if (onAct) readActs(child, rest, onAct);
    return handshake;
  });
  return { child, ready };
}

// readActs carries on where the handshake left off, starting from the bytes
// that arrived in the same chunk as it: dropping those would lose an act to
// nothing more than how the pipe happened to be flushed.
function readActs(child, rest, onAct) {
  let buffered = rest;
  const drain = () => {
    for (;;) {
      const end = buffered.indexOf("\n");
      if (end < 0) break;
      const line = buffered.slice(0, end).trim();
      buffered = buffered.slice(end + 1);
      if (line === "") continue;
      let body;
      try {
        body = JSON.parse(line);
      } catch {
        // A line this process cannot read is one it must not act on. The
        // kernel logs to stderr, so nothing here is a message worth relaying.
        continue;
      }
      if (typeof body.act === "string") onAct(body.act);
    }
    // A child writing without newlines must not be able to grow this process.
    if (buffered.length > HANDSHAKE_LIMIT) buffered = "";
  };
  child.stdout.setEncoding("utf8");
  child.stdout.on("data", (chunk) => {
    buffered += chunk;
    drain();
  });
  drain();
}

function firstLine(child, { onSlow, timeoutMs }) {
  return new Promise((resolve, reject) => {
    let buffered = "";
    const began = Date.now();
    const elapsed = () => Date.now() - began;
    const settle = (fn, value) => {
      clearTimeout(timer);
      clearTimeout(slow);
      child.stdout.off("data", onData);
      child.off("exit", onEarlyExit);
      child.off("error", onSpawnFailure);
      fn(value);
    };
    const slow = onSlow ? setTimeout(onSlow, Math.min(SLOW_AFTER_MS, timeoutMs)) : undefined;
    const timer = setTimeout(
      () =>
        settle(
          reject,
          new HostStartError(
            "timeout",
            `the kernel was still running but sent no handshake within ${Math.round(timeoutMs / 1000)} s`,
            { elapsedMs: elapsed() },
          ),
        ),
      timeoutMs,
    );
    const onData = (chunk) => {
      buffered += chunk;
      const end = buffered.indexOf("\n");
      if (end >= 0) {
        return settle(resolve, { line: buffered.slice(0, end), rest: buffered.slice(end + 1) });
      }
      if (buffered.length > HANDSHAKE_LIMIT) {
        settle(reject, new HostStartError("overflow", "the handshake ran past its limit"));
      }
    };
    const onEarlyExit = (code, signal) =>
      settle(
        reject,
        new HostStartError("exited", `the kernel exited with ${code ?? `signal ${signal}`} before saying anything`, {
          exitCode: code,
          signal,
          elapsedMs: elapsed(),
        }),
      );
    // A program that could not be started emits this and never exits, so without
    // it the launch waited out the handshake timeout and then reported the
    // kernel as silent — which sent the reader looking at the kernel.
    const onSpawnFailure = (err) => settle(reject, new HostStartError("spawn", `the kernel could not be started: ${err.message}`, { elapsedMs: elapsed() }));
    child.stdout.setEncoding("utf8");
    child.stdout.on("data", onData);
    child.on("exit", onEarlyExit);
    child.on("error", onSpawnFailure);
  });
}

// parse refuses anything it cannot account for. No message ever carries the
// line: the credential is in it.
function parse(line) {
  let body;
  try {
    body = JSON.parse(line);
  } catch {
    throw new Error("the handshake was not JSON");
  }
  if (body.version !== PROTOCOL_VERSION) {
    throw new Error(`unsupported handshake version ${String(body.version)}`);
  }
  if (typeof body.token !== "string" || body.token.length < 32) {
    throw new Error("the handshake carried no usable credential");
  }
  let url;
  try {
    url = new URL(String(body.origin));
  } catch {
    throw new Error("the handshake carried no usable origin");
  }
  if (url.protocol !== "http:") {
    throw new Error(`the handshake named ${url.protocol}, which this shell does not load`);
  }
  if (url.hostname !== "127.0.0.1") {
    throw new Error(`the handshake named ${url.hostname}, which is not this machine`);
  }
  if (!url.port) throw new Error("the handshake named no port");
  return { origin: url.origin, token: body.token };
}

const RETRY_WITHIN_MS = 10000;

// shouldRetry allows one more attempt only for a kernel that stopped, or could
// not be run, soon after it was started.
function shouldRetry(err, attempt, elapsedMs) {
  return attempt < 2 && (err?.code === "exited" || err?.code === "spawn") && elapsedMs <= RETRY_WITHIN_MS;
}

module.exports = { shouldRetry, start, parse, readActs, HostStartError, PROTOCOL_VERSION, HANDSHAKE_TIMEOUT_MS };
