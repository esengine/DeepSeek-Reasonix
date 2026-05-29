import { existsSync, mkdirSync, readFileSync, readdirSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { parseWorkflowScript } from "./parser.js";
import type { WorkflowSaveTarget } from "./types.js";

export interface SavedWorkflow {
  name: string;
  description: string;
  path: string;
  scope: WorkflowSaveTarget;
  script: string;
}

export interface WorkflowStorageOptions {
  rootDir: string;
  homeDir: string;
}

export function savedWorkflowPath(
  opts: WorkflowStorageOptions & { target: WorkflowSaveTarget; name: string },
): string {
  const name = normalizeWorkflowFileName(opts.name);
  const base =
    opts.target === "project"
      ? join(opts.rootDir, ".reasonix", "workflows")
      : join(opts.homeDir, ".reasonix", "workflows");
  return join(base, `${name}.js`);
}

export function saveWorkflowScript(
  opts: WorkflowStorageOptions & { target: WorkflowSaveTarget; name: string; script: string },
): { path: string; name: string } {
  parseWorkflowScript(opts.script);
  const path = savedWorkflowPath(opts);
  mkdirSync(dirname(path), { recursive: true });
  writeFileSync(path, opts.script, "utf8");
  return { path, name: normalizeWorkflowFileName(opts.name) };
}

export function loadSavedWorkflows(opts: WorkflowStorageOptions): SavedWorkflow[] {
  const byName = new Map<string, SavedWorkflow>();
  for (const scope of ["user", "project"] as const) {
    const dir =
      scope === "project"
        ? join(opts.rootDir, ".reasonix", "workflows")
        : join(opts.homeDir, ".reasonix", "workflows");
    if (!existsSync(dir)) continue;
    for (const entry of readdirSync(dir, { withFileTypes: true })) {
      if (!entry.isFile() || !entry.name.endsWith(".js")) continue;
      const path = join(dir, entry.name);
      const script = readFileSync(path, "utf8");
      const parsed = parseWorkflowScript(script);
      byName.set(parsed.meta.name, {
        name: parsed.meta.name,
        description: parsed.meta.description,
        path,
        scope,
        script,
      });
    }
  }
  return [...byName.values()].sort((a, b) => a.name.localeCompare(b.name));
}

function normalizeWorkflowFileName(name: string): string {
  const value = name.trim();
  if (!/^[a-zA-Z0-9_-]+$/.test(value)) {
    throw new Error("workflow name must contain only letters, numbers, underscore, or dash");
  }
  return value;
}
