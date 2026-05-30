import { randomUUID } from "node:crypto";
import { Usage } from "../client.js";
import { parseWorkflowScript } from "./parser.js";
import { runWorkflow } from "./runtime.js";
import { saveWorkflowScript } from "./saved.js";
import { WorkflowRunStore } from "./store.js";
import type {
  WorkflowAgentResult,
  WorkflowAgentRunner,
  WorkflowMode,
  WorkflowModelPolicy,
  WorkflowRunEvent,
  WorkflowRunSnapshot,
  WorkflowRunStatus,
  WorkflowSaveTarget,
  WorkflowToolMode,
} from "./types.js";

export interface WorkflowRunManagerOptions {
  runner?: WorkflowAgentRunner;
  rootDir: string;
  homeDir?: string;
  onEvent?: (event: WorkflowRunEvent) => void;
  persist?: boolean;
}

export interface WorkflowRunStartOptions {
  script: string;
  mode?: WorkflowMode;
  background?: boolean;
  args?: unknown;
  concurrency?: number;
  maxAgents?: number;
  modelPolicy?: WorkflowModelPolicy;
  tokenBudget?: number | null;
  toolMode?: WorkflowToolMode;
  runner?: WorkflowAgentRunner;
}

interface ManagedRun {
  snapshot: WorkflowRunSnapshot;
  controller: AbortController;
  promise: Promise<WorkflowRunSnapshot>;
}

export class WorkflowRunManager {
  private readonly runs = new Map<string, ManagedRun>();
  private readonly runner?: WorkflowAgentRunner;
  private readonly rootDir: string;
  private readonly homeDir: string;
  private readonly onEvent?: (event: WorkflowRunEvent) => void;
  private readonly store?: WorkflowRunStore;

  constructor(opts: WorkflowRunManagerOptions) {
    this.runner = opts.runner;
    this.rootDir = opts.rootDir;
    this.homeDir = opts.homeDir ?? process.env.HOME ?? this.rootDir;
    this.onEvent = opts.onEvent;
    this.store = opts.persist ? new WorkflowRunStore({ rootDir: this.rootDir }) : undefined;
    for (const snapshot of this.store?.loadAll() ?? []) {
      this.runs.set(snapshot.id, {
        snapshot,
        controller: new AbortController(),
        promise: Promise.resolve(this.clone(snapshot)),
      });
    }
  }

  startRun(opts: WorkflowRunStartOptions): WorkflowRunSnapshot {
    const parsed = parseWorkflowScript(opts.script);
    const id = `wf_${randomUUID().slice(0, 8)}`;
    const startedAt = Date.now();
    const controller = new AbortController();
    const snapshot: WorkflowRunSnapshot = {
      id,
      name: parsed.meta.name,
      description: parsed.meta.description,
      status: "running",
      mode: opts.mode ?? "run",
      background: opts.background,
      concurrency: opts.concurrency,
      maxAgents: opts.maxAgents,
      modelPolicy: opts.modelPolicy,
      tokenBudget: opts.tokenBudget,
      toolMode: opts.toolMode,
      startedAt,
      updatedAt: startedAt,
      phases: [],
      agents: [],
      logs: [],
      agentCount: 0,
      script: opts.script,
    };
    const run: ManagedRun = {
      snapshot,
      controller,
      promise: Promise.resolve(snapshot),
    };
    this.runs.set(id, run);
    this.persist(run);
    this.emit({
      type: "workflow.started",
      runId: id,
      name: parsed.meta.name,
      description: parsed.meta.description,
      startedAt,
    });
    run.promise = this.executeRun(id, run, opts);
    return this.clone(snapshot);
  }

  listRuns(): WorkflowRunSnapshot[] {
    return [...this.runs.values()].map((run) => this.clone(run.snapshot));
  }

  getRun(id: string): WorkflowRunSnapshot | null {
    const run = this.runs.get(id);
    return run ? this.clone(run.snapshot) : null;
  }

  stopRun(id: string): WorkflowRunSnapshot {
    const run = this.requireRun(id);
    if (run.snapshot.status === "running") {
      const now = Date.now();
      run.snapshot.status = "aborted";
      run.snapshot.error = "workflow aborted";
      run.snapshot.errorKind = "aborted";
      run.snapshot.updatedAt = now;
      run.snapshot.durationMs = now - run.snapshot.startedAt;
      for (const agent of run.snapshot.agents) {
        if (agent.status !== "running") continue;
        agent.status = "aborted";
        agent.completedAt = now;
        agent.durationMs = now - agent.startedAt;
      }
      run.controller.abort();
      this.persist(run);
    }
    return this.clone(run.snapshot);
  }

  waitForRun(id: string): Promise<WorkflowRunSnapshot> {
    return this.requireRun(id).promise;
  }

  saveRun(id: string, target: WorkflowSaveTarget, name?: string): { path: string; name: string } {
    const run = this.requireRun(id).snapshot;
    return saveWorkflowScript({
      rootDir: this.rootDir,
      homeDir: this.homeDir,
      target,
      name: name ?? run.name,
      script: run.script,
    });
  }

  deleteRun(id: string): boolean {
    const run = this.runs.get(id);
    if (run?.snapshot.status === "running") {
      throw new Error(`cannot delete running workflow: ${id}`);
    }
    const deletedMemory = this.runs.delete(id);
    const deletedDisk = this.store?.delete(id) ?? false;
    return deletedMemory || deletedDisk;
  }

  retryRun(id: string, overrides: Partial<WorkflowRunStartOptions> = {}): WorkflowRunSnapshot {
    const prior = this.requireRun(id).snapshot;
    if (prior.status === "running") {
      throw new Error(`cannot retry running workflow: ${id}`);
    }
    return this.startRun({
      script: prior.script,
      mode: overrides.mode ?? prior.mode,
      background: overrides.background ?? prior.background,
      args: overrides.args,
      concurrency: overrides.concurrency ?? prior.concurrency,
      maxAgents: overrides.maxAgents ?? prior.maxAgents,
      modelPolicy: overrides.modelPolicy ?? prior.modelPolicy,
      tokenBudget: overrides.tokenBudget ?? prior.tokenBudget,
      toolMode: overrides.toolMode ?? prior.toolMode,
      runner: overrides.runner,
    });
  }

  private async executeRun(
    id: string,
    run: ManagedRun,
    opts: WorkflowRunStartOptions,
  ): Promise<WorkflowRunSnapshot> {
    try {
      const result = await runWorkflow(opts.script, {
        mode: opts.mode,
        args: opts.args,
        cwd: this.rootDir,
        concurrency: opts.concurrency,
        maxAgents: opts.maxAgents,
        modelPolicy: opts.modelPolicy,
        tokenBudget: opts.tokenBudget,
        signal: run.controller.signal,
        runner: opts.runner ?? this.runner,
        onPhase: (phase) => this.recordPhase(run, id, phase),
        onLog: (message) => this.recordLog(run, id, message),
        onAgentStart: (event) =>
          this.recordAgentStart(run, id, event.label, event.phase, event.prompt),
        onAgentEnd: (event) => this.recordAgentEnd(run, id, event.label, event.result),
      });
      const now = Date.now();
      run.snapshot.result = result.result;
      run.snapshot.error = result.error;
      run.snapshot.errorKind = result.errorKind;
      run.snapshot.agentCount = result.agentCount;
      run.snapshot.durationMs = now - run.snapshot.startedAt;
      run.snapshot.updatedAt = now;
      run.snapshot.status =
        run.snapshot.status === "aborted"
          ? "aborted"
          : statusFromResult(result.success, result.error);
      this.persist(run);
      this.emitTerminalEvent(id, run.snapshot, result.success);
    } catch (error) {
      const now = Date.now();
      run.snapshot.status = run.controller.signal.aborted ? "aborted" : "failed";
      run.snapshot.error = errorMessage(error);
      run.snapshot.errorKind = run.controller.signal.aborted ? "aborted" : "internal";
      run.snapshot.durationMs = now - run.snapshot.startedAt;
      run.snapshot.updatedAt = now;
      this.finishRunningAgents(run, now, run.snapshot.status, run.snapshot.error);
      this.persist(run);
      this.emitTerminalEvent(id, run.snapshot, false);
    }
    return this.clone(run.snapshot);
  }

  private recordPhase(run: ManagedRun, runId: string, phase: string): void {
    const now = Date.now();
    if (!run.snapshot.phases.some((item) => item.title === phase)) {
      run.snapshot.phases.push({ title: phase, startedAt: now, agentCount: 0 });
    }
    run.snapshot.updatedAt = now;
    this.persist(run);
    this.emit({ type: "workflow.phase.started", runId, phase, ts: now });
  }

  private recordLog(run: ManagedRun, runId: string, message: string): void {
    const now = Date.now();
    run.snapshot.logs.push(message);
    run.snapshot.updatedAt = now;
    this.persist(run);
    this.emit({ type: "workflow.log", runId, message, ts: now });
  }

  private recordAgentStart(
    run: ManagedRun,
    runId: string,
    label: string,
    phase: string | undefined,
    prompt: string,
  ): void {
    const now = Date.now();
    const agentId = `${runId}_a${run.snapshot.agents.length + 1}`;
    const promptPreview = preview(prompt, 80);
    run.snapshot.agents.push({
      id: agentId,
      label,
      phase,
      status: "running",
      promptPreview,
      startedAt: now,
    });
    run.snapshot.agentCount = run.snapshot.agents.length;
    const phaseSnapshot = phase
      ? run.snapshot.phases.find((item) => item.title === phase)
      : undefined;
    if (phaseSnapshot) phaseSnapshot.agentCount += 1;
    run.snapshot.updatedAt = now;
    this.persist(run);
    this.emit({
      type: "workflow.agent.started",
      runId,
      agentId,
      label,
      phase,
      promptPreview,
      ts: now,
    });
  }

  private recordAgentEnd(
    run: ManagedRun,
    runId: string,
    label: string,
    result: WorkflowAgentResult | null,
  ): void {
    const agent = [...run.snapshot.agents]
      .reverse()
      .find((item) => item.label === label && item.status === "running");
    if (!agent) {
      if (run.snapshot.status !== "running") return;
      throw new Error(`no running workflow agent for label "${label}" in ${runId}`);
    }
    const now = Date.now();
    agent.status = result?.ok ? "completed" : "failed";
    agent.completedAt = now;
    agent.durationMs = now - agent.startedAt;
    agent.outputPreview = result?.output ? preview(result.output, 120) : undefined;
    agent.error = result?.error;
    if (result?.raw) {
      run.snapshot.usage = addUsage(run.snapshot.usage, result.raw.usage);
      run.snapshot.costUsd = (run.snapshot.costUsd ?? 0) + result.raw.costUsd;
    }
    run.snapshot.updatedAt = now;
    this.persist(run);
    this.emit({
      type: "workflow.agent.completed",
      runId,
      agentId: agent.id,
      label,
      ok: Boolean(result?.ok),
      outputPreview: agent.outputPreview,
      error: agent.error,
      durationMs: agent.durationMs,
      ts: now,
    });
  }

  private emitTerminalEvent(id: string, snapshot: WorkflowRunSnapshot, success: boolean): void {
    const durationMs = snapshot.durationMs ?? Date.now() - snapshot.startedAt;
    if (snapshot.status === "aborted") {
      this.emit({
        type: "workflow.aborted",
        runId: id,
        reason: snapshot.error ?? "workflow aborted",
        durationMs,
        ts: Date.now(),
      });
      return;
    }
    this.emit({
      type: "workflow.completed",
      runId: id,
      success,
      durationMs,
      agentCount: snapshot.agentCount,
      ts: Date.now(),
    });
  }

  private requireRun(id: string): ManagedRun {
    const run = this.runs.get(id);
    if (!run) throw new Error(`workflow run not found: ${id}`);
    return run;
  }

  private clone<T>(snapshot: WorkflowRunSnapshot<T>): WorkflowRunSnapshot<T> {
    return {
      ...snapshot,
      phases: snapshot.phases.map((phase) => ({ ...phase })),
      agents: snapshot.agents.map((agent) => ({ ...agent })),
      logs: [...snapshot.logs],
    };
  }

  private emit(event: WorkflowRunEvent): void {
    this.onEvent?.(event);
  }

  private persist(run: ManagedRun): void {
    this.store?.save(run.snapshot);
  }

  private finishRunningAgents(
    run: ManagedRun,
    now: number,
    status: Extract<WorkflowRunStatus, "failed" | "aborted">,
    error: string | undefined,
  ): void {
    for (const agent of run.snapshot.agents) {
      if (agent.status !== "running") continue;
      agent.status = status;
      agent.completedAt = now;
      agent.durationMs = now - agent.startedAt;
      agent.error = error;
    }
  }
}

function statusFromResult(success: boolean, error: string | undefined): WorkflowRunStatus {
  if (success) return "completed";
  return error?.toLowerCase().includes("aborted") ? "aborted" : "failed";
}

function preview(value: string, max: number): string {
  return value.length > max ? `${value.slice(0, max - 1)}…` : value;
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

function addUsage(base: Usage | undefined, next: Usage): Usage {
  return new Usage(
    (base?.promptTokens ?? 0) + next.promptTokens,
    (base?.completionTokens ?? 0) + next.completionTokens,
    (base?.totalTokens ?? 0) + next.totalTokens,
    (base?.promptCacheHitTokens ?? 0) + next.promptCacheHitTokens,
    (base?.promptCacheMissTokens ?? 0) + next.promptCacheMissTokens,
  );
}
