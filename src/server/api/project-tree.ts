import { readdir, stat } from "node:fs/promises";
import { extname, join, relative, sep } from "node:path";
import type { DashboardContext } from "../context.js";
import type { ApiResult } from "../router.js";

const RESULT_CAP = 50;
const MAX_DEPTH = 6;
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

export interface TreeNode {
  name: string;
  path: string;
  isDir: boolean;
  children?: TreeNode[];
}

export async function handleProjectTree(
  method: string,
  _rest: string[],
  _body: string,
  ctx: DashboardContext,
): Promise<ApiResult> {
  if (method !== "GET") return { status: 405, body: { error: "GET only" } };
  const cwd = ctx.getCurrentCwd?.();
  if (!cwd) {
    return { status: 503, body: { error: "no project directory available" } };
  }
  try {
    if (!(await stat(cwd)).isDirectory()) {
      return { status: 503, body: { error: "no project directory available" } };
    }
  } catch {
    return { status: 503, body: { error: "no project directory available" } };
  }
  const tree = await buildTree(cwd, cwd, 0);
  return { status: 200, body: { tree } };
}

async function buildTree(root: string, dirPath: string, depth: number): Promise<TreeNode[]> {
  if (depth > MAX_DEPTH) return [];
  let names: string[];
  try {
    names = await readdir(dirPath);
  } catch {
    return [];
  }
  const nodes: TreeNode[] = [];
  const dirs: string[] = [];
  const files: string[] = [];
  for (const name of names) {
    if (SKIP_DIRS.has(name)) continue;
    const full = join(dirPath, name);
    let st: Awaited<ReturnType<typeof stat>>;
    try {
      st = await stat(full);
    } catch {
      continue;
    }
    if (st.isDirectory()) {
      dirs.push(name);
    } else if (st.isFile() && !SKIP_EXTS.has(extname(name).toLowerCase())) {
      files.push(name);
    }
  }
  dirs.sort();
  files.sort();
  for (const name of dirs) {
    const full = join(dirPath, name);
    const rel = relative(root, full).split(sep).join("/");
    const children = await buildTree(root, full, depth + 1);
    nodes.push({ name, path: rel, isDir: true, children });
  }
  for (const name of files) {
    const full = join(dirPath, name);
    const rel = relative(root, full).split(sep).join("/");
    nodes.push({ name, path: rel, isDir: false });
  }
  return nodes;
}

export async function handleFiles(
  method: string,
  _rest: string[],
  body: string,
  ctx: DashboardContext,
): Promise<ApiResult> {
  if (method === "GET") {
    return await handleProjectTree("GET", _rest, body, ctx);
  }
  if (method !== "POST") return { status: 405, body: { error: "GET or POST only" } };
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
      if (SKIP_DIRS.has(name)) continue;
      const full = join(path, name);
      let st: Awaited<ReturnType<typeof stat>>;
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
