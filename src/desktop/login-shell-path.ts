/** GUI launches inherit OS-level env, not the user's interactive-shell env (#1252).
 *  Probe `$SHELL -ilc` once at startup so nvm/asdf/fnm/volta/mise injected PATH entries survive. */

import { spawnSync } from "node:child_process";

let cached: { value: string | null } | undefined;

/** Returns the user's interactive-shell PATH on macOS/Linux, null on Windows or on error. Cached. */
export function resolveLoginShellPath(opts: { timeoutMs?: number } = {}): string | null {
  if (cached !== undefined) return cached.value;
  cached = { value: null };
  if (process.platform === "win32") return null;

  const shell = process.env.SHELL || "/bin/bash";
  // -i forces zsh/bash to source rc files; -l also sources profile. The literal
  // `printf '__REASONIX_PATH__=%s\\n'` framing protects us from rc files that
  // print banners / completion notices on every interactive shell.
  const marker = "__REASONIX_PATH__=";
  try {
    const result = spawnSync(shell, ["-ilc", `printf '${marker}%s\\n' "$PATH"`], {
      encoding: "utf8",
      timeout: opts.timeoutMs ?? 2000,
      stdio: ["ignore", "pipe", "ignore"],
    });
    if (result.status !== 0 && result.signal === null) return null;
    const stdout = result.stdout ?? "";
    const idx = stdout.lastIndexOf(marker);
    if (idx < 0) return null;
    const tail = stdout.slice(idx + marker.length);
    const newline = tail.indexOf("\n");
    const path = (newline >= 0 ? tail.slice(0, newline) : tail).trim();
    if (!path || !path.includes("/")) return null;
    cached.value = path;
    return path;
  } catch {
    return null;
  }
}

/** Prepend missing login-shell PATH entries onto `process.env.PATH`. Idempotent. */
export function augmentProcessPath(): { added: string[] } {
  const loginPath = resolveLoginShellPath();
  if (!loginPath) return { added: [] };
  const current = process.env.PATH ?? "";
  const seen = new Set(
    current
      .split(":")
      .map((s) => s.trim())
      .filter(Boolean),
  );
  const additions: string[] = [];
  for (const entry of loginPath.split(":")) {
    const t = entry.trim();
    if (!t || seen.has(t)) continue;
    seen.add(t);
    additions.push(t);
  }
  if (additions.length === 0) return { added: [] };
  process.env.PATH = additions.concat(current ? [current] : []).join(":");
  return { added: additions };
}

/** Test-only — clear the resolved-PATH cache so a fresh `resolveLoginShellPath()` re-probes. */
export function resetLoginShellPathCache(): void {
  cached = undefined;
}
