/** Persists structured plan state alongside the JSONL log; markdown body lives in the log (it was a tool result) and replays on resume. */

import {
  existsSync,
  mkdirSync,
  readFileSync,
  readdirSync,
  renameSync,
  statSync,
  unlinkSync,
  writeFileSync,
} from "node:fs";
import { dirname, join } from "node:path";
import { sanitizeName, sessionsDir } from "../memory/session.js";
import type { PlanStep, StepCompletion, StepEvidence } from "../tools/plan.js";

export interface PlanStateOnDisk {
  /** File format version — bump when shape changes. */
  version: 1 | 2;
  steps: PlanStep[];
  completedStepIds: string[];
  stepCompletions?: Record<string, StepCompletion>;
  /** ISO8601 timestamp of the last write. */
  updatedAt: string;
  body?: string;
  summary?: string;
}

export function planStatePath(sessionName: string): string {
  return join(sessionsDir(), `${sanitizeName(sessionName)}.plan.json`);
}

export function loadPlanState(sessionName: string): PlanStateOnDisk | null {
  const path = planStatePath(sessionName);
  if (!existsSync(path)) return null;
  try {
    const raw = readFileSync(path, "utf8");
    const parsed = JSON.parse(raw) as Partial<PlanStateOnDisk>;
    if (!parsed || typeof parsed !== "object") return null;
    if (parsed.version !== 1 && parsed.version !== 2) return null;
    if (!Array.isArray(parsed.steps)) return null;
    if (!Array.isArray(parsed.completedStepIds)) return null;
    if (typeof parsed.updatedAt !== "string") return null;
    // Defensive: filter out any malformed step entries so a partially
    // corrupted file still yields a usable subset.
    const steps: PlanStep[] = [];
    for (const s of parsed.steps) {
      if (!s || typeof s !== "object") continue;
      const e = s as unknown as Record<string, unknown>;
      if (typeof e.id !== "string" || !e.id) continue;
      if (typeof e.title !== "string" || !e.title) continue;
      if (typeof e.action !== "string" || !e.action) continue;
      const step: PlanStep = { id: e.id, title: e.title, action: e.action };
      if (e.risk === "low" || e.risk === "med" || e.risk === "high") step.risk = e.risk;
      const targets = stringList(e.targets);
      if (targets) step.targets = targets;
      if (typeof e.acceptance === "string" && e.acceptance.trim()) {
        step.acceptance = e.acceptance.trim();
      }
      const verification = stringList(e.verification);
      if (verification) step.verification = verification;
      steps.push(step);
    }
    if (steps.length === 0) return null;
    const completedStepIds = parsed.completedStepIds.filter(
      (id): id is string => typeof id === "string" && id.length > 0,
    );
    const stepCompletions = sanitizeStepCompletions(parsed.stepCompletions);
    const out: PlanStateOnDisk = {
      version: parsed.version,
      steps,
      completedStepIds,
      updatedAt: parsed.updatedAt,
    };
    if (stepCompletions) out.stepCompletions = stepCompletions;
    if (typeof parsed.body === "string" && parsed.body) out.body = parsed.body;
    if (typeof parsed.summary === "string" && parsed.summary) out.summary = parsed.summary;
    return out;
  } catch {
    return null;
  }
}

/** Best-effort: write failure logs to stderr instead of crashing the TUI. */
export function savePlanState(
  sessionName: string,
  steps: PlanStep[],
  completedStepIds: Iterable<string>,
  extras?: {
    body?: string;
    summary?: string;
    stepCompletions?: ReadonlyMap<string, StepCompletion> | Record<string, StepCompletion>;
  },
): void {
  const path = planStatePath(sessionName);
  try {
    mkdirSync(dirname(path), { recursive: true });
    const state: PlanStateOnDisk = {
      version: 2,
      steps,
      completedStepIds: [...completedStepIds],
      updatedAt: new Date().toISOString(),
    };
    const stepCompletions = normalizeStepCompletionsForWrite(extras?.stepCompletions);
    if (stepCompletions) state.stepCompletions = stepCompletions;
    if (extras?.body) state.body = extras.body;
    if (extras?.summary) state.summary = extras.summary;
    writeFileSync(path, `${JSON.stringify(state, null, 2)}\n`, "utf8");
  } catch (err) {
    process.stderr.write(
      `▸ plan-store: failed to save plan for "${sessionName}": ${(err as Error).message}\n`,
    );
  }
}

/** Remove the persisted plan, if any. Used on cancel / clean reset. */
export function clearPlanState(sessionName: string): void {
  const path = planStatePath(sessionName);
  try {
    if (existsSync(path)) unlinkSync(path);
  } catch {
    /* nothing to do — leftover file is harmless, will be overwritten next save */
  }
}

/** Random suffix avoids same-millisecond collision; `:`/`.` swapped for Windows-safe filenames. */
export function archivePlanState(sessionName: string): string | null {
  const active = planStatePath(sessionName);
  if (!existsSync(active)) return null;
  const stamp = new Date().toISOString().replace(/[:.]/g, "-");
  const suffix = Math.random().toString(36).slice(2, 6);
  const archive = join(
    sessionsDir(),
    `${sanitizeName(sessionName)}.plan.${stamp}-${suffix}.done.json`,
  );
  try {
    renameSync(active, archive);
    return archive;
  } catch (err) {
    process.stderr.write(
      `▸ plan-store: failed to archive plan for "${sessionName}": ${(err as Error).message}\n`,
    );
    return null;
  }
}

export interface PlanArchiveSummary {
  path: string;
  completedAt: string;
  steps: PlanStep[];
  completedStepIds: string[];
  stepCompletions?: Record<string, StepCompletion>;
  /** Markdown body, when the archive carried it. */
  body?: string;
  /** One-line human-friendly title, when supplied. */
  summary?: string;
}

export function listPlanArchives(sessionName: string): PlanArchiveSummary[] {
  const dir = sessionsDir();
  if (!existsSync(dir)) return [];
  const prefix = `${sanitizeName(sessionName)}.plan.`;
  const suffix = ".done.json";
  let entries: string[];
  try {
    entries = readdirSync(dir);
  } catch {
    return [];
  }
  const summaries: PlanArchiveSummary[] = [];
  for (const name of entries) {
    if (!name.startsWith(prefix) || !name.endsWith(suffix)) continue;
    const full = join(dir, name);
    try {
      const raw = readFileSync(full, "utf8");
      const parsed = JSON.parse(raw) as Partial<PlanStateOnDisk>;
      if (parsed.version !== 1 && parsed.version !== 2) continue;
      if (!Array.isArray(parsed.steps) || parsed.steps.length === 0) continue;
      const steps = parsed.steps.filter(
        (s): s is PlanStep =>
          !!s &&
          typeof s === "object" &&
          typeof (s as PlanStep).id === "string" &&
          typeof (s as PlanStep).title === "string" &&
          typeof (s as PlanStep).action === "string",
      );
      if (steps.length === 0) continue;
      const completedStepIds = Array.isArray(parsed.completedStepIds)
        ? parsed.completedStepIds.filter((id): id is string => typeof id === "string" && !!id)
        : [];
      // Prefer the file's own updatedAt; fall back to mtime if missing
      // or unparseable so a hand-edited archive still sorts sensibly.
      let completedAt = typeof parsed.updatedAt === "string" ? parsed.updatedAt : "";
      if (!completedAt || Number.isNaN(Date.parse(completedAt))) {
        try {
          completedAt = statSync(full).mtime.toISOString();
        } catch {
          completedAt = new Date(0).toISOString();
        }
      }
      const entry: PlanArchiveSummary = { path: full, completedAt, steps, completedStepIds };
      const stepCompletions = sanitizeStepCompletions(parsed.stepCompletions);
      if (stepCompletions) entry.stepCompletions = stepCompletions;
      if (typeof parsed.body === "string" && parsed.body) entry.body = parsed.body;
      if (typeof parsed.summary === "string" && parsed.summary) entry.summary = parsed.summary;
      summaries.push(entry);
    } catch {
      // Skip the corrupt archive entirely.
    }
  }
  summaries.sort((a, b) => b.completedAt.localeCompare(a.completedAt));
  return summaries;
}

export interface PlanArchiveWithSession extends PlanArchiveSummary {
  sessionName: string;
}

/** Cross-session enumeration in a single dir scan — used by the dashboard plans panel where the per-session loop was O(N×M) and timed out for users with hundreds of sessions. */
export function listAllPlanArchives(): PlanArchiveWithSession[] {
  const dir = sessionsDir();
  if (!existsSync(dir)) return [];
  let entries: string[];
  try {
    entries = readdirSync(dir);
  } catch {
    return [];
  }
  const out: PlanArchiveWithSession[] = [];
  const suffix = ".done.json";
  const planMarker = ".plan.";
  for (const name of entries) {
    if (!name.endsWith(suffix)) continue;
    const planIdx = name.indexOf(planMarker);
    if (planIdx < 0) continue;
    const sessionName = name.slice(0, planIdx);
    if (!sessionName) continue;
    const full = join(dir, name);
    try {
      const raw = readFileSync(full, "utf8");
      const parsed = JSON.parse(raw) as Partial<PlanStateOnDisk>;
      if (parsed.version !== 1 && parsed.version !== 2) continue;
      if (!Array.isArray(parsed.steps) || parsed.steps.length === 0) continue;
      const steps = parsed.steps.filter(
        (s): s is PlanStep =>
          !!s &&
          typeof s === "object" &&
          typeof (s as PlanStep).id === "string" &&
          typeof (s as PlanStep).title === "string" &&
          typeof (s as PlanStep).action === "string",
      );
      if (steps.length === 0) continue;
      const completedStepIds = Array.isArray(parsed.completedStepIds)
        ? parsed.completedStepIds.filter((id): id is string => typeof id === "string" && !!id)
        : [];
      let completedAt = typeof parsed.updatedAt === "string" ? parsed.updatedAt : "";
      if (!completedAt || Number.isNaN(Date.parse(completedAt))) {
        try {
          completedAt = statSync(full).mtime.toISOString();
        } catch {
          completedAt = new Date(0).toISOString();
        }
      }
      const entry: PlanArchiveWithSession = {
        sessionName,
        path: full,
        completedAt,
        steps,
        completedStepIds,
      };
      const stepCompletions = sanitizeStepCompletions(parsed.stepCompletions);
      if (stepCompletions) entry.stepCompletions = stepCompletions;
      if (typeof parsed.body === "string" && parsed.body) entry.body = parsed.body;
      if (typeof parsed.summary === "string" && parsed.summary) entry.summary = parsed.summary;
      out.push(entry);
    } catch {
      // Skip the corrupt archive entirely.
    }
  }
  out.sort((a, b) => b.completedAt.localeCompare(a.completedAt));
  return out;
}

function stringList(raw: unknown): string[] | undefined {
  if (!Array.isArray(raw)) return undefined;
  const out = raw
    .map((entry) => (typeof entry === "string" ? entry.trim() : ""))
    .filter((entry) => entry.length > 0);
  return out.length > 0 ? out : undefined;
}

function normalizeStepCompletionsForWrite(
  raw: ReadonlyMap<string, StepCompletion> | Record<string, StepCompletion> | undefined,
): Record<string, StepCompletion> | undefined {
  if (!raw) return undefined;
  const entries =
    raw instanceof Map
      ? [...raw.entries()]
      : (Object.entries(raw) as Array<[string, StepCompletion]>);
  const out: Record<string, StepCompletion> = {};
  for (const [key, value] of entries) {
    const completion = sanitizeStepCompletion(value, key);
    if (completion) out[completion.stepId] = completion;
  }
  return Object.keys(out).length > 0 ? out : undefined;
}

function sanitizeStepCompletions(raw: unknown): Record<string, StepCompletion> | undefined {
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) return undefined;
  const out: Record<string, StepCompletion> = {};
  for (const [key, value] of Object.entries(raw as Record<string, unknown>)) {
    const completion = sanitizeStepCompletion(value, key);
    if (completion) out[completion.stepId] = completion;
  }
  return Object.keys(out).length > 0 ? out : undefined;
}

function sanitizeStepCompletion(raw: unknown, fallbackStepId?: string): StepCompletion | undefined {
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) return undefined;
  const entry = raw as Record<string, unknown>;
  const stepId =
    typeof entry.stepId === "string" && entry.stepId.trim()
      ? entry.stepId.trim()
      : fallbackStepId?.trim();
  const result = typeof entry.result === "string" ? entry.result.trim() : "";
  if (!stepId || !result) return undefined;
  const completion: StepCompletion = { kind: "step_completed", stepId, result };
  if (typeof entry.title === "string" && entry.title.trim()) completion.title = entry.title.trim();
  if (typeof entry.notes === "string" && entry.notes.trim()) completion.notes = entry.notes.trim();
  const evidence = sanitizeEvidenceList(entry.evidence);
  if (evidence) completion.evidence = evidence;
  return completion;
}

function sanitizeEvidenceList(raw: unknown): StepEvidence[] | undefined {
  if (!Array.isArray(raw)) return undefined;
  const out: StepEvidence[] = [];
  for (const item of raw) {
    if (!item || typeof item !== "object" || Array.isArray(item)) continue;
    const entry = item as Record<string, unknown>;
    const kind = entry.kind;
    if (kind !== "verification" && kind !== "diff" && kind !== "checkpoint" && kind !== "manual") {
      continue;
    }
    const summary = typeof entry.summary === "string" ? entry.summary.trim() : "";
    if (!summary) continue;
    const evidence: StepEvidence = { kind, summary };
    if (typeof entry.command === "string" && entry.command.trim()) {
      evidence.command = entry.command.trim();
    }
    const paths = stringList(entry.paths);
    if (paths) evidence.paths = paths;
    out.push(evidence);
  }
  return out.length > 0 ? out : undefined;
}

/** Falls back to raw ISO string past a week — "47 days ago" misleads more than it helps. */
export function relativeTime(updatedAt: string, now: number = Date.now()): string {
  const t = Date.parse(updatedAt);
  if (Number.isNaN(t)) return updatedAt;
  const diffMs = Math.max(0, now - t);
  const sec = Math.floor(diffMs / 1000);
  if (sec < 60) return `${sec}s ago`;
  const min = Math.floor(sec / 60);
  if (min < 60) return `${min}m ago`;
  const hr = Math.floor(min / 60);
  if (hr < 24) return `${hr}h ago`;
  const day = Math.floor(hr / 24);
  if (day < 7) return `${day}d ago`;
  return updatedAt.slice(0, 10);
}
