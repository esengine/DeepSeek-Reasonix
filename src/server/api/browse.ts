import { execSync } from "node:child_process";
import { existsSync, readdirSync, statSync } from "node:fs";
import { homedir } from "node:os";
import { dirname, isAbsolute, resolve } from "node:path";
import type { ApiResult } from "../router.js";

interface BrowseEntry {
  name: string;
  full: string;
}

interface BrowseResult {
  /** Directory currently being listed. Always absolute and resolved. */
  path: string;
  /** Parent of `path`, or null if `path` is a root (drive root / `/`). */
  parent: string | null;
  /** Sorted list of subdirectories. Files are filtered out — workdir picker only cares about directories. */
  entries: BrowseEntry[];
}

const SKIP_DIRS = new Set([
  "node_modules",
  ".git",
  ".svn",
  ".hg",
  ".next",
  ".cache",
  "__pycache__",
  ".venv",
  "venv",
  ".pytest_cache",
  ".mypy_cache",
  ".tox",
  "dist",
  "build",
]);

let cachedDriveList: string[] | null = null;

function listWindowsDrives(): string[] {
  if (cachedDriveList) return cachedDriveList;
  try {
    const raw = execSync("wmic logicaldisk get deviceid /value", {
      encoding: "utf8",
      timeout: 1500,
    });
    const drives = raw
      .split(/\r?\n/)
      .map((l) => l.trim())
      .filter((l) => l.startsWith("DeviceID="))
      .map((l) => `${l.slice("DeviceID=".length)}\\`)
      .filter((d) => existsSync(d));
    cachedDriveList = drives.length > 0 ? drives : ["C:\\"];
  } catch {
    // wmic absent (newer Windows builds drop it) — fall back to probing letters.
    const found: string[] = [];
    for (const letter of "CDEFGHIJKLMNOPQRSTUVWXYZ") {
      const p = `${letter}:\\`;
      try {
        if (existsSync(p)) found.push(p);
      } catch {
        /* skip unreachable drives without blocking */
      }
    }
    cachedDriveList = found.length > 0 ? found : ["C:\\"];
  }
  return cachedDriveList;
}

function isWindowsDriveRoot(p: string): boolean {
  return /^[A-Za-z]:[\\/]?$/.test(p);
}

function defaultRoot(): string {
  try {
    return homedir();
  } catch {
    return process.platform === "win32" ? "C:\\" : "/";
  }
}

function readSubdirs(path: string): BrowseEntry[] {
  let names: string[];
  try {
    names = readdirSync(path);
  } catch {
    return [];
  }
  const out: BrowseEntry[] = [];
  for (const name of names) {
    if (SKIP_DIRS.has(name)) continue;
    // Hide dotfile dirs but keep them reachable by typing the path manually.
    if (name.startsWith(".") && name.length > 1) continue;
    const full = resolve(path, name);
    try {
      if (!statSync(full).isDirectory()) continue;
    } catch {
      continue;
    }
    out.push({ name, full });
  }
  out.sort((a, b) => a.name.localeCompare(b.name, undefined, { sensitivity: "base" }));
  return out;
}

export async function handleBrowse(
  method: string,
  _rest: string[],
  _body: string,
  _ctx: unknown,
  query: URLSearchParams,
): Promise<ApiResult> {
  if (method !== "GET") return { status: 405, body: { error: "GET only" } };
  const rawPath = (query.get("path") ?? "").trim();
  const isWin = process.platform === "win32";

  // No path → seed with the user's home; on Windows we also surface every drive
  // so the user can navigate off the home drive without typing the letter.
  if (!rawPath) {
    const home = defaultRoot();
    const entries = readSubdirs(home);
    if (isWin) {
      const drives = listWindowsDrives()
        .filter((d) => resolve(d) !== resolve(home))
        .map((d) => ({ name: d, full: d }));
      entries.unshift(...drives);
    }
    const result: BrowseResult = { path: home, parent: null, entries };
    return { status: 200, body: result };
  }

  if (!isAbsolute(rawPath)) {
    return { status: 400, body: { error: "path must be absolute" } };
  }
  const absolute = resolve(rawPath);
  if (!existsSync(absolute)) {
    return { status: 404, body: { error: `no such directory: ${absolute}` } };
  }
  let isDir = false;
  try {
    isDir = statSync(absolute).isDirectory();
  } catch {
    /* falls through to 404-equivalent */
  }
  if (!isDir) {
    return { status: 400, body: { error: `not a directory: ${absolute}` } };
  }

  let parent: string | null = dirname(absolute);
  if (parent === absolute || (isWin && isWindowsDriveRoot(absolute))) parent = null;

  const result: BrowseResult = {
    path: absolute,
    parent,
    entries: readSubdirs(absolute),
  };
  return { status: 200, body: result };
}
