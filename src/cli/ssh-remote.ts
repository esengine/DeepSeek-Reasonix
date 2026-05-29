// SSH remote workspace RFC / dry-run — #2140. Full remote execution is a future deliverable.

import { execFileSync, execSync } from "node:child_process";
import { platform } from "node:os";
import { VERSION } from "../version.js";

export interface SshUri {
  user: string;
  host: string;
  port: number;
  path: string;
}

/** Parse ssh://[user@]host[:port]/path. Trailing `:port` is optional; defaults to 22. */
export function parseSshUri(raw: string): SshUri | null {
  const m = /^ssh:\/\/(?:([^@:]+)@)?([^/:]+)(?::(\d+))?(\/.*)?$/.exec(raw);
  if (!m) return null;
  const user = m[1] ?? inferSshUser();
  const host = m[2] ?? "";
  if (!host) return null;
  const port = m[3] ? Number.parseInt(m[3], 10) : 22;
  const path = m[4] ?? "/";
  return { user, host, port, path };
}

function inferSshUser(): string {
  try {
    const out = execFileSync("id", ["-un"], { encoding: "utf8", timeout: 1000 }).trim();
    if (out) return out;
  } catch {
    /* fall through */
  }
  // Best-effort fallback on Windows.
  if (platform() === "win32") {
    try {
      const out = execFileSync("whoami", [], { encoding: "utf8", timeout: 1000 }).trim();
      if (out) return out;
    } catch {
      /* fall through */
    }
  }
  return "root";
}

export interface SshProbe {
  sshBin: string;
  version: string;
}

export function probeSsh(): SshProbe | null {
  try {
    // ssh -V writes to stderr on all platforms; merge into stdout.
    const version = execSync("ssh -V 2>&1", { encoding: "utf8", timeout: 3000 });
    return { sshBin: "ssh", version: version.trim() };
  } catch {
    return null;
  }
}

/** Minimal POSIX shell quoting — wraps the value in single quotes and escapes
 *  embedded single quotes. Sufficient for dry-run copy-paste safety. */
function shellQuote(value: string): string {
  if (!/[^a-zA-Z0-9,._+:@%/-]/.test(value)) return value;
  return `'${value.replace(/'/g, "'\\''")}'`;
}

export function generateSshDryRunReport(uri: SshUri, ssh: SshProbe | null): string {
  const sq = shellQuote;
  const sections = [
    `reasonix ${VERSION}  ·  SSH remote workspace RFC dry-run`,
    "issue: https://github.com/esengine/DeepSeek-Reasonix/issues/2140",
    "",
    `target:  ${sq(`ssh://${uri.user}@${uri.host}:${uri.port}${uri.path}`)}`,
    "",
  ];

  if (!ssh) {
    sections.push(
      "WARNING: `ssh` binary not found on PATH. Install an SSH client before running the real command.",
      "",
    );
  } else {
    sections.push(`ssh:     ${ssh.version}`);
  }

  sections.push(
    "--- parsed ---",
    `  user:   ${uri.user}`,
    `  host:   ${uri.host}`,
    `  port:   ${uri.port}`,
    `  path:   ${uri.path}`,
    "",
    "--- planned steps (dry run — no remote commands execute) ---",
    "",
  );

  const remote = `${sq(uri.user)}@${sq(uri.host)}`;
  const cdCmd = `cd ${sq(uri.path)} && reasonix code --no-dashboard`;

  if (ssh) {
    sections.push(
      "1. verify connectivity",
      `   $ ssh -p ${uri.port} ${remote} -- 'echo ok'`,
      "",
      "2. probe remote environment",
      `   $ ssh -p ${uri.port} ${remote} -- 'node --version && npm --version && uname -s'`,
      "",
      "3. install or update Reasonix on remote",
      `   $ ssh -p ${uri.port} ${remote} -- 'npm i -g reasonix'`,
      "",
      "4. launch Reasonix in the target workspace on the remote host",
      `   $ ssh -p ${uri.port} ${remote} -- '${cdCmd}'`,
      "",
      "5. (local) open an SSH tunnel to the remote dashboard",
      `   $ ssh -N -L 8420:127.0.0.1:8420 -p ${uri.port} ${remote}`,
      "   Then open http://127.0.0.1:8420 in your local browser.",
      "",
    );
  } else {
    sections.push(
      "1. install an SSH client",
      "   macOS:   built-in (OpenSSH)",
      "   Windows: built-in (OpenSSH Client optional feature) or `winget install Microsoft.OpenSSH.Beta`",
      "   Linux:   `apt install openssh-client` / `dnf install openssh-clients`",
      "",
      "2. verify connectivity",
      `   $ ssh -p ${uri.port} ${remote} -- 'echo ok'`,
      "",
      "3. install Reasonix on remote",
      `   $ ssh -p ${uri.port} ${remote} -- 'npm i -g reasonix'`,
      "",
      "4. launch Reasonix remotely",
      `   $ ssh -p ${uri.port} ${remote} -- '${cdCmd}'`,
      "",
    );
  }

  sections.push(
    "--- short-term recommendation ---",
    "",
    "Until native remote execution lands, the simplest working setup is:",
    "  1. Run Reasonix directly on the remote host (`ssh user@host`, then `reasonix code`).",
    "  2. Forward the dashboard port to your local machine:",
    `     $ ssh -N -L 8420:127.0.0.1:8420 -p ${uri.port} ${remote}`,
    "  3. Open http://127.0.0.1:8420 locally. The dashboard token gates access.",
    "",
    "--- RFC scope ---",
    "",
    "This dry-run is a reviewable design bootstrap for #2140.",
    "It parses the URI, checks local tooling, and prints the steps Reasonix",
    "would take. No remote commands execute and no network connections are made.",
    "",
    "#2141 (GPU passthrough) is tracked separately and is not part of this RFC.",
  );

  return sections.join("\n");
}
