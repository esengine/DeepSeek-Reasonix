import {
  existsSync,
  mkdirSync,
  readFileSync,
  readdirSync,
  renameSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { join } from "node:path";
import type { WorkflowRunSnapshot } from "./types.js";

export interface WorkflowRunStoreOptions {
  rootDir: string;
}

export class WorkflowRunStore {
  private readonly dir: string;

  constructor(opts: WorkflowRunStoreOptions) {
    this.dir = join(opts.rootDir, ".reasonix", "workflow-runs");
  }

  loadAll(): WorkflowRunSnapshot[] {
    if (!existsSync(this.dir)) return [];
    return readdirSync(this.dir)
      .filter((name) => name.endsWith(".json"))
      .map((name) => this.loadFile(join(this.dir, name)))
      .sort((a, b) => b.startedAt - a.startedAt);
  }

  save(snapshot: WorkflowRunSnapshot): void {
    mkdirSync(this.dir, { recursive: true });
    const path = this.pathFor(snapshot.id);
    const tmp = `${path}.tmp`;
    writeFileSync(tmp, `${JSON.stringify(snapshot, null, 2)}\n`, "utf8");
    renameSync(tmp, path);
  }

  delete(id: string): boolean {
    const path = this.pathFor(id);
    if (!existsSync(path)) return false;
    rmSync(path, { force: true });
    return true;
  }

  private loadFile(path: string): WorkflowRunSnapshot {
    const parsed = JSON.parse(readFileSync(path, "utf8")) as WorkflowRunSnapshot;
    if (parsed.status !== "running") return parsed;
    return {
      ...parsed,
      status: "failed",
      error: "workflow interrupted before completion",
      updatedAt: Date.now(),
      agents: parsed.agents.map((agent) =>
        agent.status === "running"
          ? { ...agent, status: "failed", error: "workflow interrupted before completion" }
          : agent,
      ),
    };
  }

  private pathFor(id: string): string {
    if (!/^wf_[a-zA-Z0-9_-]+$/.test(id)) throw new Error(`invalid workflow run id: ${id}`);
    return join(this.dir, `${id}.json`);
  }
}
