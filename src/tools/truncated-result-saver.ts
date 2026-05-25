import { randomUUID } from "node:crypto";
import {
  chmodSync,
  existsSync,
  mkdirSync,
  readdirSync,
  rmSync,
  statSync,
  writeFileSync,
} from "node:fs";
import { homedir } from "node:os";
import { join, relative, resolve } from "node:path";

const TRUNCATED_DIR = "truncated-results";
const DEFAULT_MAX_AGE_MS = 30 * 24 * 60 * 60 * 1000; // 30 days

/** Sanitize a tool name for safe use in a filename. */
function sanitizeToolName(name: string): string {
  return name.replace(/[^\w\-]/g, "_").slice(0, 48) || "unknown";
}

/** Resolve the absolute storage directory for truncated results. */
export function storageDir(rootDir: string): string {
  const base = rootDir ? join(resolve(rootDir), ".reasonix") : join(homedir(), ".reasonix");
  return join(base, TRUNCATED_DIR);
}

/** Generate a unique filename for a truncated result. */
function resultFilename(toolName: string): string {
  const ts = Date.now().toString();
  const suffix = randomUUID().slice(0, 8);
  const safeName = sanitizeToolName(toolName);
  return `${ts}-${suffix}-${safeName}.txt`;
}

/** Save truncated result to .reasonix/truncated-results/; returns relative path. */
export function saveTruncatedResult(content: string, toolName: string, rootDir: string): string {
  // Tidy old files before writing a new one so the directory doesn't grow unbounded.
  cleanupOldResults(rootDir);

  const dir = storageDir(rootDir);
  if (!existsSync(dir)) {
    mkdirSync(dir, { recursive: true });
  }

  const filename = resultFilename(toolName);
  const absPath = join(dir, filename);

  writeFileSync(absPath, content, "utf-8");
  // Match session file permission model.
  try {
    chmodSync(absPath, 0o600);
  } catch {
    // Permission change is best-effort (Windows doesn't support posix chmod).
  }

  // Return a relative path the model can pass to read_file.
  // Normalize to forward slashes for cross-platform consistency.
  if (rootDir) {
    const absRoot = resolve(rootDir);
    return relative(absRoot, absPath).replaceAll("\\", "/");
  }
  // Fallback: return absolute path.
  return absPath.replaceAll("\\", "/");
}

/** Remove truncated result files older than maxAgeMs. No-op on missing dir. */
export function cleanupOldResults(rootDir: string, maxAgeMs: number = DEFAULT_MAX_AGE_MS): void {
  const dir = storageDir(rootDir);
  if (!existsSync(dir)) return;

  const cutoff = Date.now() - maxAgeMs;
  let entries: string[];
  try {
    entries = readdirSync(dir);
  } catch {
    return;
  }

  for (const entry of entries) {
    if (!entry.endsWith(".txt")) continue;
    const abs = join(dir, entry);
    try {
      const st = statSync(abs);
      if (st.isFile() && st.mtimeMs < cutoff) {
        rmSync(abs);
      }
    } catch {
      // Stale symlink / race — skip.
    }
  }
}

/** Check whether the tool name is in the skip list for truncation saving. */
export function shouldSkipSave(toolName: string, skipTruncationSave?: boolean): boolean {
  if (skipTruncationSave) return true;
  // Built-in tools that are always skipped regardless of the flag.
  const alwaysSkip = new Set(["get_env", "everything_get-env"]);
  return alwaysSkip.has(toolName);
}
