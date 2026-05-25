import { existsSync, readFileSync } from "node:fs";
import { basename, resolve } from "node:path";
import { readProjectMemory } from "../memory/project.js";
import {
  type MemoryEntry,
  type MemoryScope,
  MemoryStore,
  readGlobalReasonixMemory,
} from "../memory/user.js";

export type MemoryEntryKind = "project_file" | "global_file" | "structured";

export interface MemoryEntryInfo {
  kind: MemoryEntryKind;
  scope: MemoryScope;
  name: string;
  path: string;
  description: string;
  type?: string;
}

export interface MemoryEntryDetail extends MemoryEntryInfo {
  body: string;
  createdAt?: string;
}

export interface MemoryBrowserOptions {
  /** Absolute ~/.reasonix directory. Tests override this; production uses homedir(). */
  reasonixHome?: string;
}

export function collectMemoryEntriesForWorkspace(
  projectRoot: string,
  opts: MemoryBrowserOptions = {},
): MemoryEntryInfo[] {
  const out: MemoryEntryInfo[] = [];
  const project = readProjectMemory(projectRoot);
  if (project) {
    out.push({
      kind: "project_file",
      scope: "project",
      name: basename(project.path),
      path: project.path,
      description: "Project memory file",
      type: "freeform",
    });
  }

  const global = readGlobalReasonixMemory(opts.reasonixHome);
  if (global) {
    out.push({
      kind: "global_file",
      scope: "global",
      name: basename(global.path),
      path: global.path,
      description: "Global memory file",
      type: "freeform",
    });
  }

  const store = new MemoryStore({ homeDir: opts.reasonixHome, projectRoot });
  for (const entry of store.list()) {
    out.push(structuredInfo(store, entry));
  }
  return out;
}

export function readMemoryEntryDetail(
  request: { path: string },
  projectRoot: string,
  opts: MemoryBrowserOptions = {},
): MemoryEntryDetail {
  const requested = resolve(request.path);
  const entry = collectMemoryEntriesForWorkspace(projectRoot, opts).find(
    (candidate) => resolve(candidate.path) === requested,
  );
  if (!entry) throw new Error(`memory path not available: ${request.path}`);

  if (entry.kind === "structured") {
    const store = new MemoryStore({ homeDir: opts.reasonixHome, projectRoot });
    const structured = store.read(entry.scope, entry.name);
    return {
      ...entry,
      description: structured.description,
      type: structured.type,
      body: structured.body,
      createdAt: structured.createdAt,
    };
  }

  if (!existsSync(entry.path)) throw new Error(`memory file missing: ${entry.path}`);
  return {
    ...entry,
    body: readFileSync(entry.path, "utf8").trim(),
  };
}

function structuredInfo(store: MemoryStore, entry: MemoryEntry): MemoryEntryInfo {
  return {
    kind: "structured",
    scope: entry.scope,
    name: entry.name,
    path: store.pathFor(entry.scope, entry.name),
    description: entry.description,
    type: entry.type,
  };
}
