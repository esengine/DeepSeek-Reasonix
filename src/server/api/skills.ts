/** `/api/skills` — edits files only; loop reloads on /new or restart. `builtin` scope is read-only. */

import { mkdir, open, readFile, readdir, rm, stat, writeFile } from "node:fs/promises";
import { homedir } from "node:os";
import { dirname, join } from "node:path";
import { loadResolvedSkillPaths, loadSubagentModels } from "../../config.js";
import { parseFrontmatter } from "../../frontmatter.js";
import { SKILLS_DIRNAME, SKILL_FILE, SkillStore, validateSkillFrontmatter } from "../../skills.js";
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
  scope: "project" | "custom" | "global" | "builtin";
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

async function readSkillListEntry(
  skillPath: string,
  name: string,
  scope: "project" | "custom" | "global",
): Promise<SkillListEntry | null> {
  // Open once and reuse the fd so size/mtime/content all bind to
  // the same inode — closes the exists→stat→read TOCTOU races.
  const fd = await open(skillPath, "r");
  try {
    const fileStat = await fd.stat();
    if (!fileStat.isFile()) return null;
    const raw = await fd.readFile("utf8");
    const item: SkillListEntry = {
      name,
      scope,
      path: skillPath,
      size: fileStat.size,
      mtime: fileStat.mtime.getTime(),
    };
    const desc = parseFrontmatterDescription(raw);
    if (desc) item.description = desc;
    return item;
  } catch {
    return null;
  } finally {
    await fd.close();
  }
}

async function resolveSkillPath(dir: string, name: string): Promise<ResolvedSkillPath | null> {
  const folderPath = join(dir, name, SKILL_FILE);
  try {
    if ((await stat(folderPath)).isFile()) return { path: folderPath, layout: "folder" };
  } catch {
    /* try flat layout below */
  }
  const flatPath = join(dir, `${name}.md`);
  try {
    if ((await stat(flatPath)).isFile()) return { path: flatPath, layout: "flat" };
  } catch {
    /* not found */
  }
  return null;
}

function defaultSkillPath(dir: string, name: string): ResolvedSkillPath {
  return { path: join(dir, name, SKILL_FILE), layout: "folder" };
}

async function listSkills(
  dir: string,
  scope: "project" | "custom" | "global",
): Promise<SkillListEntry[]> {
  const out: SkillListEntry[] = [];
  try {
    const entries = await readdir(dir, { withFileTypes: true });
    const candidates: Array<{ skillPath: string; name: string }> = [];
    for (const entry of entries) {
      if (entry.isDirectory()) {
        if (!SAFE_NAME.test(entry.name)) continue;
        candidates.push({ skillPath: join(dir, entry.name, SKILL_FILE), name: entry.name });
      } else if (entry.isFile() && entry.name.endsWith(".md")) {
        const name = entry.name.slice(0, -3);
        if (!SAFE_NAME.test(name)) continue;
        candidates.push({ skillPath: join(dir, entry.name), name });
      }
    }
    const items = await Promise.all(
      candidates.map(({ skillPath, name }) => readSkillListEntry(skillPath, name, scope)),
    );
    for (const item of items) {
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
    const store = new SkillStore({
      projectRoot: cwd,
      customSkillPaths: loadResolvedSkillPaths(cwd ?? process.cwd(), ctx.configPath),
      subagentModels: loadSubagentModels(ctx.configPath),
    });
    const customRoots = store.customRoots();
    return {
      status: 200,
      body: {
        global: tag(await listSkills(globalSkillsDir(), "global")),
        custom: tag(
          (await Promise.all(customRoots.map((root) => listSkills(root.dir, "custom")))).flat(),
        ),
        project: cwd ? tag(await listSkills(projectSkillsDir(cwd), "project")) : [],
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
          custom: customRoots,
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
  const resolved = await resolveSkillPath(dir, name);

  if (method === "GET") {
    if (!resolved) return { status: 404, body: { error: "skill not found" } };
    return {
      status: 200,
      body: { path: resolved.path, body: await readFile(resolved.path, "utf8") },
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
    await mkdir(dirname(target.path), { recursive: true });
    await writeFile(target.path, contents, "utf8");
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
    await rm(resolved.layout === "folder" ? dirname(resolved.path) : resolved.path, {
      recursive: true,
      force: true,
    });
    ctx.audit?.({ ts: Date.now(), action: "delete-skill", payload: { scope, name } });
    return { status: 200, body: { deleted: true } };
  }

  return { status: 405, body: { error: `method ${method} not supported` } };
}
