/** `/api/skills` — edits files only; loop reloads on /new or restart. `builtin` scope is read-only. */

import {
  closeSync,
  existsSync,
  fstatSync,
  mkdirSync,
  openSync,
  readFileSync,
  readSync,
  readdirSync,
  rmSync,
  statSync,
  writeFileSync,
} from "node:fs";
import { homedir } from "node:os";
import { dirname, join } from "node:path";
import { parseFrontmatter } from "../../frontmatter.js";
import { SKILLS_DIRNAME, SKILL_FILE, validateSkillFrontmatter } from "../../skills.js";
import { readUsageLog } from "../../telemetry/usage.js";
import type { DashboardContext } from "../context.js";
import type { ApiResult } from "../router.js";

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

function globalSkillsDir(): string {
  return join(homedir(), ".reasonix", SKILLS_DIRNAME);
}

function projectSkillsDir(rootDir: string): string {
  return join(rootDir, ".reasonix", SKILLS_DIRNAME);
}

interface SkillListEntry {
  name: string;
  scope: "project" | "global" | "builtin";
  description?: string;
  path: string;
  size: number;
  mtime: number;
}

type SkillLayout = "folder" | "flat";

interface ResolvedSkillPath {
  path: string;
  layout: SkillLayout;
}

function parseFrontmatterDescription(raw: string): string | undefined {
  const desc = parseFrontmatter(raw).data.description?.trim();
  return desc ? desc : undefined;
}

function readSkillListEntry(
  skillPath: string,
  name: string,
  scope: "project" | "global",
): SkillListEntry | null {
  try {
    // Open once and reuse the fd so size/mtime/content all bind to
    // the same inode — closes the exists→stat→read TOCTOU races.
    const fd = openSync(skillPath, "r");
    let stat: ReturnType<typeof fstatSync>;
    let raw: string;
    try {
      stat = fstatSync(fd);
      if (!stat.isFile()) return null;
      const buf = Buffer.alloc(stat.size);
      let read = 0;
      while (read < stat.size) {
        const n = readSync(fd, buf, read, stat.size - read, read);
        if (n <= 0) break;
        read += n;
      }
      raw = buf.toString("utf8", 0, read);
    } finally {
      closeSync(fd);
    }
    const item: SkillListEntry = {
      name,
      scope,
      path: skillPath,
      size: stat.size,
      mtime: stat.mtime.getTime(),
    };
    const desc = parseFrontmatterDescription(raw);
    if (desc) item.description = desc;
    return item;
  } catch {
    return null;
  }
}

function resolveSkillPath(dir: string, name: string): ResolvedSkillPath | null {
  const folderPath = join(dir, name, SKILL_FILE);
  try {
    if (statSync(folderPath).isFile()) return { path: folderPath, layout: "folder" };
  } catch {
    /* try flat layout below */
  }
  const flatPath = join(dir, `${name}.md`);
  try {
    if (statSync(flatPath).isFile()) return { path: flatPath, layout: "flat" };
  } catch {
    /* not found */
  }
  return null;
}

function defaultSkillPath(dir: string, name: string): ResolvedSkillPath {
  return { path: join(dir, name, SKILL_FILE), layout: "folder" };
}

function listSkills(dir: string, scope: "project" | "global"): SkillListEntry[] {
  if (!existsSync(dir)) return [];
  const out: SkillListEntry[] = [];
  try {
    for (const entry of readdirSync(dir, { withFileTypes: true })) {
      let name: string;
      let skillPath: string;
      if (entry.isDirectory()) {
        name = entry.name;
        if (!SAFE_NAME.test(name)) continue;
        skillPath = join(dir, name, SKILL_FILE);
      } else if (entry.isFile() && entry.name.endsWith(".md")) {
        name = entry.name.slice(0, -3);
        if (!SAFE_NAME.test(name)) continue;
        skillPath = join(dir, entry.name);
      } else {
        continue;
      }
      const item = readSkillListEntry(skillPath, name, scope);
      if (item) out.push(item);
    }
  } catch {
    /* skip unreadable dir */
  }
  return out.sort((a, b) => a.name.localeCompare(b.name));
}

function countSubagentRuns(usageLogPath: string): Map<string, number> {
  const cutoff = Date.now() - 7 * 86_400_000;
  const counts = new Map<string, number>();
  for (const r of readUsageLog(usageLogPath)) {
    if (r.kind !== "subagent" || r.ts < cutoff) continue;
    const skill = r.subagent?.skillName?.trim();
    if (!skill) continue;
    counts.set(skill, (counts.get(skill) ?? 0) + 1);
  }
  return counts;
}

export async function handleSkills(
  method: string,
  rest: string[],
  body: string,
  ctx: DashboardContext,
): Promise<ApiResult> {
  const cwd = ctx.getCurrentCwd?.();

  if (method === "GET" && rest.length === 0) {
    const runs7d = countSubagentRuns(ctx.usageLogPath);
    const tag = (rows: SkillListEntry[]) =>
      rows.map((r) => ({ ...r, runs7d: runs7d.get(r.name) ?? 0 }));
    return {
      status: 200,
      body: {
        global: tag(listSkills(globalSkillsDir(), "global")),
        project: cwd ? tag(listSkills(projectSkillsDir(cwd), "project")) : [],
        builtin: [
          {
            name: "explore",
            scope: "builtin",
            description: "subagent — broad codebase survey",
            runs7d: runs7d.get("explore") ?? 0,
          },
          {
            name: "research",
            scope: "builtin",
            description: "subagent — deep web + repo research",
            runs7d: runs7d.get("research") ?? 0,
          },
        ],
        paths: {
          global: globalSkillsDir(),
          project: cwd ? projectSkillsDir(cwd) : null,
        },
      },
    };
  }

  const [scope, ...nameParts] = rest;
  const name = nameParts.join("/");

  if (!scope || !name || !SAFE_NAME.test(name)) {
    return { status: 400, body: { error: "expected /api/skills/<scope>/<name>" } };
  }
  if (scope !== "project" && scope !== "global") {
    return {
      status: 400,
      body: { error: "scope must be project | global (builtin is read-only)" },
    };
  }
  let dir: string;
  if (scope === "project") {
    if (!cwd) {
      return {
        status: 503,
        body: { error: "no active project — open `/dashboard` from `reasonix code`" },
      };
    }
    dir = projectSkillsDir(cwd);
  } else {
    dir = globalSkillsDir();
  }
  const resolved = resolveSkillPath(dir, name);

  if (method === "GET") {
    if (!resolved) return { status: 404, body: { error: "skill not found" } };
    return {
      status: 200,
      body: { path: resolved.path, body: readFileSync(resolved.path, "utf8") },
    };
  }

  if (method === "POST") {
    const { body: contents } = parseBody(body);
    if (typeof contents !== "string") {
      return { status: 400, body: { error: "body (string) required" } };
    }
    const fm = validateSkillFrontmatter(contents);
    if ("error" in fm) {
      return { status: 400, body: { error: fm.error } };
    }
    const target = resolved ?? defaultSkillPath(dir, name);
    mkdirSync(dirname(target.path), { recursive: true });
    writeFileSync(target.path, contents, "utf8");
    ctx.audit?.({
      ts: Date.now(),
      action: "save-skill",
      payload: { scope, name, path: target.path },
    });
    return { status: 200, body: { saved: true, path: target.path } };
  }

  if (method === "DELETE") {
    if (!resolved) return { status: 404, body: { error: "skill not found" } };
    // Folder-layout skills may carry assets next to SKILL.md; flat skills are single-file entries.
    rmSync(resolved.layout === "folder" ? dirname(resolved.path) : resolved.path, {
      recursive: true,
      force: true,
    });
    ctx.audit?.({ ts: Date.now(), action: "delete-skill", payload: { scope, name } });
    return { status: 200, body: { deleted: true } };
  }

  return { status: 405, body: { error: `method ${method} not supported` } };
}
