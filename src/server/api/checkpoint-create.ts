import { createCheckpoint } from "../../code/checkpoints.js";
import type { DashboardContext } from "../context.js";
import type { ApiResult } from "../router.js";

export async function handleCheckpointCreate(
  method: string,
  _rest: string[],
  body: string,
  ctx: DashboardContext,
): Promise<ApiResult> {
  if (method !== "POST") return { status: 405, body: { error: "POST only" } };

  const rootDir = ctx.getCurrentCwd?.();
  if (!rootDir) return { status: 400, body: { error: "no active workspace" } };

  let parsed: { name?: string };
  try {
    parsed = JSON.parse(body);
  } catch {
    return { status: 400, body: { error: "invalid JSON" } };
  }
  if (!parsed.name) return { status: 400, body: { error: "missing name" } };

  // Snapshot all files in workspace by walking with git ls-files
  let paths: string[];
  try {
    const { execSync } = await import("node:child_process");
    // Strip GIT_* env vars: if this handler runs in a context where git set
    // GIT_DIR (e.g. inside a pre-push hook spawning the dashboard), ls-files
    // would resolve to the *outer* repo instead of rootDir.
    const env: NodeJS.ProcessEnv = { ...process.env };
    for (const k of Object.keys(env)) {
      if (k.startsWith("GIT_")) delete env[k];
    }
    const stdout = execSync("git ls-files --cached --others --exclude-standard", {
      cwd: rootDir,
      env,
      encoding: "utf8",
      maxBuffer: 10 * 1024 * 1024,
    });
    paths = stdout.split("\n").filter(Boolean);
  } catch (err: unknown) {
    const msg = err instanceof Error ? err.message : String(err);
    if (msg.includes("ENOENT") || msg.includes("not a git repository") || msg.includes("fatal")) {
      return {
        status: 400,
        body: {
          error: `Cannot snapshot — not a git repository or git is unavailable: ${msg}`,
        },
      };
    }
    return {
      status: 500,
      body: { error: `git ls-files failed: ${msg}` },
    };
  }

  const meta = createCheckpoint({
    rootDir,
    name: parsed.name,
    paths,
  });

  return {
    status: 200,
    body: {
      id: meta.id,
      name: meta.name,
      fileCount: meta.fileCount,
      bytes: meta.bytes,
    },
  };
}
