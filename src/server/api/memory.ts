/** Names sanitized via SAFE_NAME on every write — guards against path traversal. */

import { createHash } from "node:crypto";
import {
  existsSync,
  mkdirSync,
  readFileSync,
  readdirSync,
  statSync,
  unlinkSync,
  writeFileSync,
} from "node:fs";
import { homedir } from "node:os";
import { basename, dirname, join, resolve as resolvePath } from "node:path";
import {
  PROJECT_MEMORY_FILE,
  findProjectMemoryPath,
  resolveProjectMemoryWritePath,
} from "../../memory/project.js";
import type { DashboardContext } from "../context.js";
import type { ApiResult } from "../router.js";

function projectHash(rootDir: string): string {
  return createHash("sha1").update(resolvePath(rootDir)).digest("hex").slice(0, 16);
}

function globalMemoryDir(): string {
  return join(homedir(), ".reasonix", "memory", "global");
}

function projectMemoryDir(rootDir: string): string {
  return join(homedir(), ".reasonix", "memory", projectHash(rootDir));
}

interface WriteBody {
  body?: unknown;
}

function parseBody(raw: string): WriteBody {
  if (!raw) return {};
  try {
    const parsed = JSON.parse(raw);
    return typeof parsed === "object" && parsed !== null ? (parsed as WriteBody) : {};
  } catch {
    return {};
  }
}

const SAFE_NAME = /^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$/;

function listMemoryFiles(dir: string): Array<{ name: string; size: number; mtime: number }> {
  if (!existsSync(dir)) return [];
  try {
    return readdirSync(dir)
      .filter((f) => f.endsWith(".md"))
      .map((f) => {
        const stat = statSync(join(dir, f));
        return {
          name: f.replace(/\.md$/, ""),
          size: stat.size,
          mtime: stat.mtime.getTime(),
        };
      })
      .sort((a, b) => b.mtime - a.mtime);
  } catch {
    return [];
  }
}

export async function handleMemory(
  method: string,
  rest: string[],
  body: string,
  ctx: DashboardContext,
): Promise<ApiResult> {
  const cwd = ctx.getCurrentCwd?.();
  const globalDir = globalMemoryDir();
  const projectMemDir = cwd ? projectMemoryDir(cwd) : "";

  if (method === "GET" && rest.length === 0) {
    const existingProjectMemory = cwd ? findProjectMemoryPath(cwd) : null;
    const projectMemoryPath =
      existingProjectMemory ?? (cwd ? join(cwd, PROJECT_MEMORY_FILE) : null);
    const projectMemoryExists = existingProjectMemory !== null;
    return {
      status: 200,
      body: {
        project: {
          path: projectMemoryPath,
          exists: projectMemoryExists,
          file: projectMemoryPath ? basename(projectMemoryPath) : PROJECT_MEMORY_FILE,
        },
        global: {
          path: globalDir,
          files: listMemoryFiles(globalDir),
        },
        projectMem: {
          path: projectMemDir,
          files: projectMemDir ? listMemoryFiles(projectMemDir) : [],
        },
      },
    };
  }

  // /api/memory/<scope>/<name?>
  const [scope, ...nameParts] = rest;
  const name = nameParts.join("/"); // empty for `project` scope which is a single file

  if (method === "GET") {
    if (scope === "project") {
      if (!cwd) return { status: 503, body: { error: "no active project" } };
      const path = findProjectMemoryPath(cwd);
      if (!path) return { status: 404, body: { error: "project memory file not found" } };
      return { status: 200, body: { path, body: readFileSync(path, "utf8") } };
    }
    if ((scope === "global" || scope === "project-mem") && name && SAFE_NAME.test(name)) {
      const dir = scope === "global" ? globalDir : projectMemDir;
      if (!dir) return { status: 503, body: { error: "no project root for project-mem" } };
      const path = join(dir, `${name}.md`);
      if (!existsSync(path)) return { status: 404, body: { error: "not found" } };
      return { status: 200, body: { path, body: readFileSync(path, "utf8") } };
    }
    return { status: 400, body: { error: "bad scope or name" } };
  }

  if (method === "POST") {
    const { body: contents } = parseBody(body);
    if (typeof contents !== "string") {
      return { status: 400, body: { error: "body (string) required" } };
    }
    if (scope === "project") {
      if (!cwd) return { status: 503, body: { error: "no active project" } };
      const path = resolveProjectMemoryWritePath(cwd);
      mkdirSync(dirname(path), { recursive: true });
      writeFileSync(path, contents, "utf8");
      ctx.audit?.({ ts: Date.now(), action: "save-memory", payload: { scope, path } });
      return { status: 200, body: { saved: true, path } };
    }
    if ((scope === "global" || scope === "project-mem") && name && SAFE_NAME.test(name)) {
      const dir = scope === "global" ? globalDir : projectMemDir;
      if (!dir) return { status: 503, body: { error: "no project root for project-mem" } };
      mkdirSync(dir, { recursive: true });
      const path = join(dir, `${name}.md`);
      writeFileSync(path, contents, "utf8");
      ctx.audit?.({ ts: Date.now(), action: "save-memory", payload: { scope, name, path } });
      return { status: 200, body: { saved: true, path } };
    }
    return { status: 400, body: { error: "bad scope or name" } };
  }

  if (method === "DELETE") {
    if ((scope === "global" || scope === "project-mem") && name && SAFE_NAME.test(name)) {
      const dir = scope === "global" ? globalDir : projectMemDir;
      if (!dir) return { status: 503, body: { error: "no project root for project-mem" } };
      const path = join(dir, `${name}.md`);
      if (existsSync(path)) {
        unlinkSync(path);
        ctx.audit?.({ ts: Date.now(), action: "delete-memory", payload: { scope, name, path } });
        return { status: 200, body: { deleted: true } };
      }
      return { status: 404, body: { error: "not found" } };
    }
    if (scope === "project") {
      if (!cwd) return { status: 503, body: { error: "no active project" } };
      const path = findProjectMemoryPath(cwd);
      if (path) {
        unlinkSync(path);
        ctx.audit?.({ ts: Date.now(), action: "delete-memory", payload: { scope, path } });
        return { status: 200, body: { deleted: true } };
      }
      return { status: 404, body: { error: "not found" } };
    }
    return { status: 400, body: { error: "bad scope or name" } };
  }

  return { status: 405, body: { error: `method ${method} not supported` } };
}
