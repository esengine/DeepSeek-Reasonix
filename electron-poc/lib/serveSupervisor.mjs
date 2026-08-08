import crypto from "node:crypto";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { spawn } from "node:child_process";
import { assertTrustedReasonixBin, resolveReasonixBin } from "./resolveReasonixBin.mjs";

/**
 * Build argv for supervised reasonix serve (token never on argv).
 * @param {{
 *   tokenFile: string,
 *   portFile: string,
 *   pidFile: string,
 *   addr?: string,
 * }} files
 * @returns {string[]}
 */
export function buildServeArgs(files) {
  const addr = files.addr ?? "127.0.0.1:0";
  if (!addr.startsWith("127.0.0.1:") && addr !== "127.0.0.1") {
    // PoC hard-constraint: loopback only.
    throw new Error(`serve addr must be loopback 127.0.0.1, got ${addr}`);
  }
  const args = [
    "serve",
    "--addr",
    addr,
    "--auth",
    "token",
    "--token-file",
    files.tokenFile,
    "--port-file",
    files.portFile,
    "--pid-file",
    files.pidFile,
  ];
  // Electron PoC prefers multi-Controller tabs when the binary supports it.
  if (files.multiTab !== false) {
    args.push("--multi-tab");
  }
  return args;
}

/**
 * @param {string} dir
 * @param {{ mode?: number, tokenBytes?: number }} [opts]
 */
export function prepareStateDir(dir, opts = {}) {
  fs.mkdirSync(dir, { recursive: true, mode: 0o700 });
  const tokenFile = path.join(dir, "token");
  const portFile = path.join(dir, "port");
  const pidFile = path.join(dir, "pid");
  const logFile = path.join(dir, "serve.log");
  const token = crypto.randomBytes(opts.tokenBytes ?? 32).toString("hex");
  fs.writeFileSync(tokenFile, token + "\n", { mode: opts.mode ?? 0o600 });
  // Clear stale discovery files
  for (const f of [portFile, pidFile]) {
    try {
      fs.unlinkSync(f);
    } catch {
      /* missing ok */
    }
  }
  return { dir, tokenFile, portFile, pidFile, logFile, token };
}

/**
 * Parse host:port from port-file contents (may include trailing newline).
 * Normalizes IPv6-style :port from Go's listener if needed.
 * @param {string} raw
 * @returns {{ host: string, port: number, display: string, baseUrl: string }}
 */
export function parsePortFileContents(raw) {
  const line = String(raw).trim().split(/\r?\n/)[0]?.trim() ?? "";
  if (!line) throw new Error("empty port file");
  // Go may write 127.0.0.1:12345
  const m = line.match(/^(127\.0\.0\.1|localhost|\[::1\]|::1):(\d+)$/i);
  if (!m) {
    // Also accept bare host:port if host is loopback after normalize
    const idx = line.lastIndexOf(":");
    if (idx <= 0) throw new Error(`invalid port file contents: ${line}`);
    const host = line.slice(0, idx).replace(/^\[|\]$/g, "");
    const port = Number(line.slice(idx + 1));
    if (!Number.isFinite(port) || port <= 0) throw new Error(`invalid port: ${line}`);
    if (host !== "127.0.0.1" && host !== "localhost" && host !== "::1") {
      throw new Error(`non-loopback address in port file: ${line}`);
    }
    const displayHost = host === "::1" ? "127.0.0.1" : host === "localhost" ? "127.0.0.1" : host;
    return {
      host: displayHost,
      port,
      display: `${displayHost}:${port}`,
      baseUrl: `http://${displayHost}:${port}`,
    };
  }
  const host = m[1] === "localhost" || m[1] === "[::1]" || m[1] === "::1" ? "127.0.0.1" : m[1];
  const port = Number(m[2]);
  return {
    host,
    port,
    display: `${host}:${port}`,
    baseUrl: `http://${host}:${port}`,
  };
}

/**
 * Poll until port file is non-empty and parseable.
 * @param {string} portFile
 * @param {{ timeoutMs?: number, intervalMs?: number, now?: () => number, sleep?: (ms: number) => Promise<void>, readFileSync?: typeof fs.readFileSync }} [opts]
 */
export async function waitForPortFile(portFile, opts = {}) {
  const timeoutMs = opts.timeoutMs ?? 30_000;
  const intervalMs = opts.intervalMs ?? 50;
  const now = opts.now ?? Date.now;
  const sleep =
    opts.sleep ??
    ((ms) => new Promise((r) => setTimeout(r, ms)));
  const readFileSync = opts.readFileSync ?? fs.readFileSync;
  const start = now();
  let lastErr = new Error("port file not ready");
  while (now() - start < timeoutMs) {
    try {
      if (fs.existsSync(portFile)) {
        const raw = readFileSync(portFile, "utf8");
        return parsePortFileContents(raw);
      }
    } catch (e) {
      lastErr = e instanceof Error ? e : new Error(String(e));
    }
    await sleep(intervalMs);
  }
  throw new Error(`timed out waiting for port file ${portFile}: ${lastErr.message}`);
}

/**
 * Build the UI URL with token query (serve sets cookie and redirects).
 * @param {string} baseUrl
 * @param {string} token
 * @param {string} [pathName]
 */
export function buildAuthenticatedUiUrl(baseUrl, token, pathName = "/") {
  const u = new URL(pathName, baseUrl.endsWith("/") ? baseUrl : baseUrl + "/");
  u.searchParams.set("token", token);
  return u.toString();
}

/**
 * Supervises one reasonix serve child.
 */
export class ServeSupervisor {
  /**
   * @param {{
   *   bin?: string,
   *   stateDir?: string,
   *   workspace?: string,
   *   repoRoot?: string,
   *   env?: NodeJS.ProcessEnv,
   *   spawnImpl?: typeof spawn,
   *   crashRestartOnce?: boolean,
   *   onExit?: (code: number | null, signal: NodeJS.Signals | null) => void,
   *   onCrashRestarted?: (info: Awaited<ReturnType<ServeSupervisor['start']>>) => void | Promise<void>,
   *   onLog?: (chunk: string) => void,
   * }} [opts]
   */
  constructor(opts = {}) {
    this.opts = opts;
    this.child = null;
    this.state = null;
    this.endpoint = null;
    this._stopping = false;
    this._didCrashRestart = false;
    this._exitHandlers = [];
    this._lastInfo = null;
  }

  get token() {
    return this.state?.token ?? null;
  }

  get baseUrl() {
    return this.endpoint?.baseUrl ?? null;
  }

  get uiUrl() {
    if (!this.endpoint || !this.state) return null;
    return buildAuthenticatedUiUrl(this.endpoint.baseUrl, this.state.token);
  }

  get logFile() {
    return this.state?.logFile ?? null;
  }

  get pid() {
    return this.child?.pid ?? null;
  }

  /**
   * Start serve in workspace (cwd).
   * Reuses a fixed stateDir (and thus discovery paths) across crash restarts so
   * the host can reload the new uiUrl/token without leaking orphan dirs.
   * @param {{ workspace?: string, fromCrashRestart?: boolean }} [override]
   */
  async start(override = {}) {
    if (this.child) {
      throw new Error("serve already running");
    }
    this._stopping = false;
    const env = this.opts.env ?? process.env;
    const bin = this.opts.bin
      ? assertTrustedReasonixBin(this.opts.bin)
      : resolveReasonixBin({ env, repoRoot: this.opts.repoRoot });
    // Pin stateDir on first start so crash restart reuses the same directory.
    if (!this.opts.stateDir) {
      this.opts.stateDir = path.join(
        os.tmpdir(),
        `reasonix-electron-${process.pid}-${crypto.randomBytes(4).toString("hex")}`,
      );
    }
    const stateDir = this.opts.stateDir;
    this.state = prepareStateDir(stateDir);
    const args = buildServeArgs({
      tokenFile: this.state.tokenFile,
      portFile: this.state.portFile,
      pidFile: this.state.pidFile,
    });
    // Security: never put token on argv
    if (args.includes(this.state.token) || args.some((a) => a.includes(this.state.token))) {
      throw new Error("internal error: token leaked onto argv");
    }
    if (args.includes("--token")) {
      throw new Error("must use --token-file only, not --token");
    }

    const workspace = override.workspace ?? this.opts.workspace ?? process.cwd();
    const logFd = fs.openSync(this.state.logFile, "a");
    const spawnImpl = this.opts.spawnImpl ?? spawn;
    this.child = spawnImpl(bin, args, {
      cwd: workspace,
      env: { ...env },
      stdio: ["ignore", logFd, logFd],
      detached: false,
    });
    fs.closeSync(logFd);

    this.child.on("exit", (code, signal) => {
      const wasStopping = this._stopping;
      this.child = null;
      if (this.opts.onExit) this.opts.onExit(code, signal);
      for (const h of this._exitHandlers) h(code, signal);
      if (!wasStopping && this.opts.crashRestartOnce && !this._didCrashRestart) {
        this._didCrashRestart = true;
        // Restart once, then notify host so lastStart/UI can follow the new endpoint.
        this.start({ workspace, fromCrashRestart: true })
          .then((info) => {
            if (typeof this.opts.onCrashRestarted === "function") {
              return this.opts.onCrashRestarted(info);
            }
          })
          .catch((err) => {
            if (this.opts.onLog) this.opts.onLog(`crash restart failed: ${err}\n`);
          });
      }
    });

    try {
      this.endpoint = await waitForPortFile(this.state.portFile, {
        timeoutMs: Number(env.REASONIX_SERVE_READY_MS) || 45_000,
      });
    } catch (e) {
      await this.stop();
      throw e;
    }
    this.opts.workspace = workspace;
    const info = {
      baseUrl: this.endpoint.baseUrl,
      uiUrl: this.uiUrl,
      token: this.state.token,
      port: this.endpoint.port,
      pid: this.pid,
      logFile: this.state.logFile,
      stateDir: this.state.dir,
      args,
      bin,
      workspace,
      fromCrashRestart: !!override.fromCrashRestart,
    };
    this._lastInfo = info;
    return info;
  }

  /**
   * Restart serve with a new workspace directory.
   * @param {string} workspace
   */
  async restartInWorkspace(workspace) {
    await this.stop();
    this._didCrashRestart = false;
    this.opts.workspace = workspace;
    return this.start({ workspace });
  }

  /**
   * Graceful stop: SIGTERM then SIGKILL.
   * @param {{ termTimeoutMs?: number }} [opts]
   */
  async stop(opts = {}) {
    this._stopping = true;
    const child = this.child;
    if (!child || child.killed) {
      this.child = null;
      return;
    }
    const termTimeoutMs = opts.termTimeoutMs ?? 5_000;
    await new Promise((resolve) => {
      let done = false;
      const finish = () => {
        if (done) return;
        done = true;
        resolve();
      };
      child.once("exit", finish);
      try {
        child.kill("SIGTERM");
      } catch {
        finish();
        return;
      }
      const t = setTimeout(() => {
        try {
          child.kill("SIGKILL");
        } catch {
          /* ignore */
        }
        finish();
      }, termTimeoutMs);
      child.once("exit", () => clearTimeout(t));
    });
    this.child = null;
  }
}

/**
 * Create a default state directory under os.tmpdir or XDG.
 */
export function defaultStateDir() {
  return path.join(os.tmpdir(), `reasonix-electron-${process.pid}`);
}
