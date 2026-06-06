import type { Stats } from "node:fs";
import { readdir, stat } from "node:fs/promises";
import { extname, join, relative, sep } from "node:path";
import type { DashboardContext } from "../context.js";
import type { ApiResult } from "../router.js";

const RESULT_CAP = 50;
const MAX_DEPTH = 4;
const SKIP_DIRS = new Set([
  "node_modules",
  ".git",
  ".reasonix",
  "dist",
  "build",
  "out",
  ".next",
  "coverage",
  ".cache",
  "__pycache__",
  ".venv",
  ".pytest_cache",
]);
const SKIP_EXTS = new Set([
  ".png",
  ".jpg",
  ".jpeg",
  ".gif",
  ".webp",
  ".ico",
  ".pdf",
  ".zip",
  ".tar",
  ".gz",
  ".lock",
  ".woff",
  ".woff2",
  ".ttf",
]);

export async function handleFiles(
  method: string,
  _rest: string[],
  body: string,
  ctx: DashboardContext,
  query: URLSearchParams = new URLSearchParams(),
): Promise<ApiResult> {
  if (method === "GET" && _rest[0] === "search") {
    const cwd = ctx.getCurrentCwd?.();
    if (!cwd) {
      return { status: 503, body: { error: "@-mention picker requires a code-mode session" } };
    }
    try {
      if (!(await stat(cwd)).isDirectory()) {
        return { status: 503, body: { error: "@-mention picker requires a code-mode session" } };
      }
    } catch {
      return { status: 503, body: { error: "@-mention picker requires a code-mode session" } };
    }
    const prefix = (query.get("q") ?? "").trim().toLowerCase();
    const matches = await walk(cwd, prefix);
    return {
      status: 200,
      body: { nonce: query.get("nonce"), query: query.get("q") ?? "", results: matches },
    };
  }
  if (method !== "POST") return { status: 405, body: { error: "POST only" } };
  const cwd = ctx.getCurrentCwd?.();
  if (!cwd) {
    return { status: 503, body: { error: "@-mention picker requires a code-mode session" } };
  }
  try {
    await stat(cwd);
  } catch {
    return { status: 503, body: { error: "@-mention picker requires a code-mode session" } };
  }
  let parsed: { prefix?: unknown };
  try {
    parsed = JSON.parse(body || "{}");
  } catch {
    return { status: 400, body: { error: "body must be JSON" } };
  }
  const prefix = typeof parsed.prefix === "string" ? parsed.prefix.trim().toLowerCase() : "";
  const matches = await walk(cwd, prefix);
  return { status: 200, body: { files: matches } };
}

async function walk(root: string, prefix: string): Promise<string[]> {
  const out: string[] = [];
  const stack: Array<{ path: string; depth: number }> = [{ path: root, depth: 0 }];
  while (stack.length > 0 && out.length < RESULT_CAP) {
    const { path, depth } = stack.pop()!;
    if (depth > MAX_DEPTH) continue;
    let names: string[];
    try {
      names = await readdir(path);
    } catch {
      continue;
    }
    for (const name of names) {
      if (out.length >= RESULT_CAP) break;
      if (name.startsWith(".") && depth === 0) continue;
      if (SKIP_DIRS.has(name)) continue;
      const full = join(path, name);
      let st: Stats;
      try {
        st = await stat(full);
      } catch {
        continue;
      }
      if (st.isDirectory()) {
        stack.push({ path: full, depth: depth + 1 });
        continue;
      }
      if (!st.isFile()) continue;
      if (SKIP_EXTS.has(extname(name).toLowerCase())) continue;
      const rel = relative(root, full).split(sep).join("/");
      if (prefix && !rel.toLowerCase().includes(prefix)) continue;
      out.push(rel);
    }
  }
  return out.sort((a, b) => a.localeCompare(b));
}
