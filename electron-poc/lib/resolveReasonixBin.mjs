import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { execFileSync } from "node:child_process";

/**
 * Resolve a trusted reasonix binary path.
 * Order: REASONIX_BIN env → candidates relative to repo → PATH `reasonix`.
 * Rejects empty / non-file paths. Does not accept arbitrary UI-supplied paths
 * unless they pass through this resolver with allowlist checks.
 *
 * @param {{
 *   env?: NodeJS.ProcessEnv,
 *   repoRoot?: string,
 *   which?: (name: string) => string | null,
 *   existsSync?: (p: string) => boolean,
 *   isFile?: (p: string) => boolean,
 * }} [opts]
 * @returns {string}
 */
export function resolveReasonixBin(opts = {}) {
  const env = opts.env ?? process.env;
  const existsSync = opts.existsSync ?? fs.existsSync;
  const isFile =
    opts.isFile ??
    ((p) => {
      try {
        return fs.statSync(p).isFile();
      } catch {
        return false;
      }
    });
  const which =
    opts.which ??
    ((name) => {
      try {
        const out = execFileSync("which", [name], { encoding: "utf8" }).trim();
        return out || null;
      } catch {
        return null;
      }
    });

  const candidates = [];
  if (env.REASONIX_BIN && String(env.REASONIX_BIN).trim()) {
    candidates.push(path.resolve(String(env.REASONIX_BIN).trim()));
  }
  if (opts.repoRoot) {
    const root = path.resolve(opts.repoRoot);
    candidates.push(path.join(root, "bin", "reasonix"));
    candidates.push(path.join(root, "bin", "reasonix.exe"));
  }
  // Default: walk up from this package to find repo bin/
  const packageRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
  const repoGuess = path.resolve(packageRoot, "..");
  candidates.push(path.join(repoGuess, "bin", "reasonix"));
  candidates.push(path.join(repoGuess, "bin", "reasonix.exe"));

  const fromPath = which("reasonix");
  if (fromPath) candidates.push(fromPath);

  const seen = new Set();
  for (const c of candidates) {
    if (!c || seen.has(c)) continue;
    seen.add(c);
    if (!existsSync(c) || !isFile(c)) continue;
    // Basic trust: basename must be reasonix or reasonix.exe
    const base = path.basename(c).toLowerCase();
    if (base !== "reasonix" && base !== "reasonix.exe") {
      throw new Error(`refusing untrusted binary name: ${base}`);
    }
    return c;
  }
  throw new Error(
    "reasonix binary not found; set REASONIX_BIN or build with `make build` / install reasonix on PATH",
  );
}

/**
 * Ensure a candidate path is acceptable if supplied explicitly (e.g. tests).
 * @param {string} binPath
 */
export function assertTrustedReasonixBin(binPath) {
  const base = path.basename(binPath).toLowerCase();
  if (base !== "reasonix" && base !== "reasonix.exe") {
    throw new Error(`refusing untrusted binary name: ${base}`);
  }
  if (!fs.existsSync(binPath) || !fs.statSync(binPath).isFile()) {
    throw new Error(`reasonix binary not found at ${binPath}`);
  }
  return path.resolve(binPath);
}
