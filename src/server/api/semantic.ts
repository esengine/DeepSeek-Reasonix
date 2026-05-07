/** Job state in a module-scoped Map keyed by project root so multi-root dashboards don't collide; CLI `reasonix index` runs independently. */

import { closeSync, fstatSync, openSync, readSync } from "node:fs";
import { join } from "node:path";
import {
  type EmbeddingProvider,
  type SemanticEmbeddingUserConfig,
  loadIndexConfig,
  loadSemanticEmbeddingUserConfig,
  readConfig,
  redactSemanticEmbeddingConfig,
  resolveSemanticEmbeddingConfig,
  saveSemanticEmbeddingConfig,
} from "../../config.js";
import {
  INDEX_DIR_NAME,
  buildIndex,
  indexCompatible,
  indexExists,
  querySemantic,
} from "../../index/semantic/builder.js";
import type { BuildProgress, BuildResult } from "../../index/semantic/builder.js";
import {
  checkOllamaStatus,
  pullOllamaModel,
  startOllamaDaemon,
} from "../../index/semantic/ollama-launcher.js";
import {
  compareIndexIdentity,
  readIndexMeta as readStoreIndexMeta,
} from "../../index/semantic/store.js";
import { registerSemanticSearchTool } from "../../index/semantic/tool.js";
import type { DashboardContext } from "../context.js";
import type { ApiResult } from "../router.js";

interface JobRecord {
  startedAt: number;
  finishedAt?: number;
  cancelledAt?: number;
  phase: BuildProgress["phase"] | "error" | "cancelled";
  lastPhase?: BuildProgress["phase"];
  filesScanned?: number;
  filesChanged?: number;
  filesSkipped?: number;
  chunksTotal?: number;
  chunksDone?: number;
  result?: BuildResult;
  error?: string;
  rebuild: boolean;
  aborted: boolean;
  controller: AbortController;
}

const JOBS = new Map<string, JobRecord>();

interface PullRecord {
  startedAt: number;
  status: "pulling" | "done" | "error";
  lastLine: string;
  exitCode: number | null;
}
const PULLS = new Map<string, PullRecord>();

function getRoot(ctx: DashboardContext): string | null {
  const cwd = ctx.getCurrentCwd?.();
  return cwd ?? null;
}

export async function handleSemantic(
  method: string,
  rest: string[],
  body: string,
  ctx: DashboardContext,
): Promise<ApiResult> {
  const sub = rest[0] ?? "";

  if (sub === "" && method === "GET") return await getStatus(ctx);
  if (sub === "config" && method === "GET") return getSemanticConfig(ctx);
  if (sub === "config" && method === "POST") return saveSemanticConfigApi(body, ctx);
  if (sub === "start" && method === "POST") return await startJob(body, ctx);
  if (sub === "stop" && method === "POST") return await stopJob(ctx);
  if (sub === "ollama" && method === "POST") {
    const action = rest[1] ?? "";
    if (action === "start") return await startDaemon(ctx);
    if (action === "pull") return await startPull(body, ctx);
  }
  if (sub === "search" && method === "POST") return await runSearch(body, ctx);
  return { status: 404, body: { error: "no such semantic endpoint" } };
}

async function runSearch(rawBody: string, ctx: DashboardContext): Promise<ApiResult> {
  const root = getRoot(ctx);
  if (!root) {
    return { status: 503, body: { error: "search requires an attached code-mode session" } };
  }
  let parsed: { query?: unknown; topK?: unknown; minScore?: unknown };
  try {
    parsed = JSON.parse(rawBody || "{}");
  } catch {
    return { status: 400, body: { error: "body must be JSON" } };
  }
  const query = typeof parsed.query === "string" ? parsed.query.trim() : "";
  if (!query) return { status: 400, body: { error: "query required" } };
  const topK =
    typeof parsed.topK === "number" && Number.isFinite(parsed.topK)
      ? Math.max(1, Math.min(16, Math.floor(parsed.topK)))
      : 8;
  const minScore =
    typeof parsed.minScore === "number" && Number.isFinite(parsed.minScore)
      ? Math.max(0, Math.min(1, parsed.minScore))
      : 0.3;
  const startedAt = Date.now();
  const embedding = resolveSemanticEmbeddingConfig(ctx.configPath);
  try {
    const hits = await querySemantic(root, query, {
      topK,
      minScore,
      configPath: ctx.configPath,
    });
    if (hits === null) {
      return { status: 404, body: { error: "no semantic index for this project" } };
    }
    return {
      status: 200,
      body: {
        hits: hits.map((h) => ({
          path: h.entry.path,
          startLine: h.entry.startLine,
          endLine: h.entry.endLine,
          score: h.score,
          snippet: h.entry.text,
        })),
        elapsedMs: Date.now() - startedAt,
        provider: embedding.provider,
        model: embedding.model,
      },
    };
  } catch (err) {
    return { status: 500, body: { error: (err as Error).message } };
  }
}

async function getStatus(ctx: DashboardContext): Promise<ApiResult> {
  const root = getRoot(ctx);
  if (!root) {
    return {
      status: 200,
      body: {
        attached: false,
        reason:
          "Semantic indexing requires a code-mode session — run `/dashboard` from inside `reasonix code` instead of standalone `reasonix dashboard`.",
      },
    };
  }
  const config = loadSemanticEmbeddingUserConfig(ctx.configPath);
  const configView = redactSemanticEmbeddingConfig(config);
  const resolved = resolveSemanticEmbeddingConfig(ctx.configPath);
  const [hasIndex, providerStatus, index] = await Promise.all([
    indexExists(root),
    getProviderStatusFromConfig(configView),
    readIndexMeta(root, { provider: resolved.provider, model: resolved.model }),
  ]);
  const job = JOBS.get(root) ?? null;
  const pull =
    providerStatus.kind === "ollama" ? (PULLS.get(providerStatus.modelName) ?? null) : null;
  return {
    status: 200,
    body: {
      attached: true,
      root,
      provider: configView.provider,
      providerConfig: configView,
      providerStatus,
      index: hasIndex ? index : { exists: false },
      ollama: providerStatus.kind === "ollama" ? providerStatus : undefined,
      job: job ? snapshotJob(job) : null,
      pull: pull ? snapshotPull(pull) : null,
    },
  };
}

interface IndexMetaResponse {
  exists: true;
  provider: EmbeddingProvider;
  chunks: number;
  files: number;
  dim: number;
  sizeBytes: number;
  lastBuiltMs: number;
  model: string;
  builtWith: { provider: EmbeddingProvider; model: string };
  current: { provider: EmbeddingProvider; model: string };
  compatible: boolean;
  mismatch: "provider" | "model" | null;
}

async function readIndexMeta(
  root: string,
  current: { provider: EmbeddingProvider; model: string },
): Promise<IndexMetaResponse | { exists: false }> {
  const dir = join(root, INDEX_DIR_NAME);
  const dataPath = join(dir, "index.jsonl");
  const diskMeta = await readStoreIndexMeta(dir);
  if (!diskMeta) return { exists: false };
  let chunks = 0;
  const files = new Set<string>();
  let sizeBytes = 0;
  try {
    const fd = openSync(dataPath, "r");
    let raw: string;
    try {
      const stat = fstatSync(fd);
      sizeBytes = stat.size;
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
    for (const line of raw.split(/\r?\n/)) {
      if (!line.trim()) continue;
      chunks++;
      try {
        const rec = JSON.parse(line) as { p?: string };
        if (typeof rec.p === "string") files.add(rec.p);
      } catch {
        /* skip malformed */
      }
    }
  } catch {
    /* partial counts allowed */
  }
  const mismatch = compareIndexIdentity(diskMeta, current);
  return {
    exists: true,
    provider: diskMeta.provider,
    chunks,
    files: files.size,
    dim: diskMeta.dim ?? 0,
    sizeBytes,
    lastBuiltMs: diskMeta.updatedAt ? Date.parse(diskMeta.updatedAt) || 0 : 0,
    model: diskMeta.model ?? "",
    builtWith: { provider: diskMeta.provider, model: diskMeta.model },
    current,
    compatible: mismatch === null,
    mismatch,
  };
}

function snapshotPull(p: PullRecord): unknown {
  return {
    startedAt: p.startedAt,
    status: p.status,
    lastLine: p.lastLine,
    exitCode: p.exitCode,
  };
}

async function startDaemon(ctx: DashboardContext): Promise<ApiResult> {
  const resolved = resolveSemanticEmbeddingConfig(ctx.configPath);
  if (resolved.provider !== "ollama") {
    return { status: 409, body: { error: "ollama actions require provider=ollama" } };
  }
  const r = await startOllamaDaemon({ baseUrl: resolved.baseUrl, timeoutMs: 15_000 }).catch(
    (err: Error) => ({
      ready: false,
      pid: null,
      error: err.message,
    }),
  );
  if ("error" in r) return { status: 500, body: { ready: false, error: r.error } };
  return { status: r.ready ? 200 : 504, body: r };
}

interface PullBody {
  model?: unknown;
}

async function startPull(body: string, ctx: DashboardContext): Promise<ApiResult> {
  const resolved = resolveSemanticEmbeddingConfig(ctx.configPath);
  if (resolved.provider !== "ollama") {
    return { status: 409, body: { error: "ollama actions require provider=ollama" } };
  }
  let parsed: PullBody = {};
  if (body) {
    try {
      parsed = JSON.parse(body) as PullBody;
    } catch {
      return { status: 400, body: { error: "invalid JSON body" } };
    }
  }
  const model = typeof parsed.model === "string" && parsed.model ? parsed.model : resolved.model;
  const existing = PULLS.get(model);
  if (existing && existing.status === "pulling") {
    return {
      status: 409,
      body: { error: `${model} is already pulling`, pull: snapshotPull(existing) },
    };
  }
  const rec: PullRecord = {
    startedAt: Date.now(),
    status: "pulling",
    lastLine: `pulling ${model}…`,
    exitCode: null,
  };
  PULLS.set(model, rec);
  void pullOllamaModel(model, {
    onLine: (line) => {
      if (line.trim().length > 0) rec.lastLine = line.trim();
    },
  })
    .then((code) => {
      rec.exitCode = code;
      rec.status = code === 0 ? "done" : "error";
      if (code !== 0 && (!rec.lastLine || !rec.lastLine.toLowerCase().includes("error"))) {
        rec.lastLine = `ollama pull exited with code ${code}`;
      }
    })
    .catch((err: Error) => {
      rec.status = "error";
      rec.lastLine = err.message;
    });
  return { status: 202, body: { started: true, pull: snapshotPull(rec) } };
}

function snapshotJob(j: JobRecord): unknown {
  return {
    startedAt: j.startedAt,
    finishedAt: j.finishedAt ?? null,
    cancelledAt: j.cancelledAt ?? null,
    phase: j.phase,
    lastPhase: j.lastPhase ?? null,
    rebuild: j.rebuild,
    filesScanned: j.filesScanned ?? null,
    filesChanged: j.filesChanged ?? null,
    filesSkipped: j.filesSkipped ?? null,
    chunksTotal: j.chunksTotal ?? null,
    chunksDone: j.chunksDone ?? null,
    aborted: j.aborted,
    result: j.result ?? null,
    error: j.error ?? null,
  };
}

interface StartBody {
  rebuild?: unknown;
}

async function startJob(body: string, ctx: DashboardContext): Promise<ApiResult> {
  const root = getRoot(ctx);
  if (!root) {
    return {
      status: 400,
      body: { error: "no project root — only available in attached (code-mode) dashboards" },
    };
  }
  const existing = JOBS.get(root);
  if (
    existing &&
    (existing.phase === "setup" ||
      existing.phase === "scan" ||
      existing.phase === "embed" ||
      existing.phase === "write")
  ) {
    return {
      status: 409,
      body: { error: "an indexing job is already running", job: snapshotJob(existing) },
    };
  }

  let parsed: StartBody = {};
  if (body) {
    try {
      parsed = JSON.parse(body) as StartBody;
    } catch {
      return { status: 400, body: { error: "invalid JSON body" } };
    }
  }
  const rebuild = parsed.rebuild === true;

  const job: JobRecord = {
    startedAt: Date.now(),
    phase: "setup",
    lastPhase: "setup",
    rebuild,
    aborted: false,
    controller: new AbortController(),
  };
  JOBS.set(root, job);

  void runIndex(root, job, ctx).catch((err) => {
    job.phase = "error";
    job.finishedAt = Date.now();
    job.error = err instanceof Error ? err.message : String(err);
  });

  const resolved = resolveSemanticEmbeddingConfig(ctx.configPath);
  return {
    status: 202,
    body: {
      started: true,
      provider: resolved.provider,
      model: resolved.model,
      job: snapshotJob(job),
    },
  };
}

async function runIndex(root: string, job: JobRecord, ctx: DashboardContext): Promise<void> {
  try {
    const resolved = resolveSemanticEmbeddingConfig(ctx.configPath);
    const result = await buildIndex(root, {
      rebuild: job.rebuild,
      configPath: ctx.configPath,
      signal: job.controller.signal,
      indexConfig: loadIndexConfig(ctx.configPath),
      onProgress: (p) => {
        job.phase = p.phase;
        if (p.phase !== "done") job.lastPhase = p.phase;
        if (p.filesScanned !== undefined) job.filesScanned = p.filesScanned;
        if (p.filesChanged !== undefined) job.filesChanged = p.filesChanged;
        if (p.filesSkipped !== undefined) job.filesSkipped = p.filesSkipped;
        if (p.chunksTotal !== undefined) job.chunksTotal = p.chunksTotal;
        if (p.chunksDone !== undefined) job.chunksDone = p.chunksDone;
      },
    });
    job.phase = "done";
    job.finishedAt = Date.now();
    job.result = result;
    if (ctx.tools && ctx.addToolToPrefix) {
      try {
        const added = await registerSemanticSearchTool(ctx.tools, { root, ...resolved });
        if (added) {
          const spec = ctx.tools.specs().find((s) => s.function.name === "semantic_search");
          if (spec) ctx.addToolToPrefix(spec);
        }
      } catch {
        /* non-fatal */
      }
    }
  } catch (err) {
    if (isAbortError(err)) {
      job.phase = "cancelled";
      job.cancelledAt = Date.now();
      job.finishedAt = job.cancelledAt;
      job.error = undefined;
      return;
    }
    job.phase = "error";
    job.finishedAt = Date.now();
    job.error = err instanceof Error ? err.message : String(err);
  }
}

async function stopJob(ctx: DashboardContext): Promise<ApiResult> {
  const root = getRoot(ctx);
  if (!root) return { status: 400, body: { error: "no project root" } };
  const job = JOBS.get(root);
  if (!job || job.phase === "done" || job.phase === "error" || job.phase === "cancelled") {
    return { status: 404, body: { error: "no running job" } };
  }
  job.aborted = true;
  job.controller.abort(new Error("semantic indexing aborted"));
  return { status: 202, body: { stopping: true, job: snapshotJob(job) } };
}

function getSemanticConfig(ctx: DashboardContext): ApiResult {
  return {
    status: 200,
    body: redactSemanticEmbeddingConfig(loadSemanticEmbeddingUserConfig(ctx.configPath)),
  };
}

function saveSemanticConfigApi(rawBody: string, ctx: DashboardContext): ApiResult {
  let parsed: {
    provider?: unknown;
    ollama?: { baseUrl?: unknown; model?: unknown };
    openaiCompat?: {
      baseUrl?: unknown;
      apiKey?: unknown;
      model?: unknown;
      extraBody?: unknown;
    };
  };
  try {
    parsed = JSON.parse(rawBody || "{}");
  } catch {
    return { status: 400, body: { error: "body must be JSON" } };
  }
  const existing = loadSemanticEmbeddingUserConfig(ctx.configPath);
  const next: SemanticEmbeddingUserConfig = {
    provider: parsed.provider === "openai-compat" ? "openai-compat" : "ollama",
    ollama: {
      baseUrl:
        typeof parsed.ollama?.baseUrl === "string"
          ? parsed.ollama.baseUrl
          : existing.ollama?.baseUrl,
      model:
        typeof parsed.ollama?.model === "string" ? parsed.ollama.model : existing.ollama?.model,
    },
    openaiCompat: {
      baseUrl:
        typeof parsed.openaiCompat?.baseUrl === "string"
          ? parsed.openaiCompat.baseUrl
          : existing.openaiCompat?.baseUrl,
      apiKey:
        typeof parsed.openaiCompat?.apiKey === "string"
          ? parsed.openaiCompat.apiKey.trim() || existing.openaiCompat?.apiKey
          : existing.openaiCompat?.apiKey,
      model:
        typeof parsed.openaiCompat?.model === "string"
          ? parsed.openaiCompat.model
          : existing.openaiCompat?.model,
      extraBody:
        parsed.openaiCompat?.extraBody === undefined
          ? existing.openaiCompat?.extraBody
          : (parsed.openaiCompat.extraBody as Record<string, unknown>),
    },
  };
  try {
    saveSemanticEmbeddingConfig(next, ctx.configPath);
  } catch (err) {
    return { status: 400, body: { error: (err as Error).message } };
  }
  ctx.audit?.({
    ts: Date.now(),
    action: "set-semantic-config",
    payload: { provider: next.provider },
  });
  return {
    status: 200,
    body: {
      changed: collectSemanticConfigChanges(existing, next),
      config: redactSemanticEmbeddingConfig(loadSemanticEmbeddingUserConfig(ctx.configPath)),
    },
  };
}

function collectSemanticConfigChanges(
  before: SemanticEmbeddingUserConfig,
  after: SemanticEmbeddingUserConfig,
): string[] {
  const left = JSON.stringify(before);
  const right = JSON.stringify(after);
  if (left === right) return [];
  return ["semantic"];
}

async function getProviderStatusFromConfig(
  config: ReturnType<typeof redactSemanticEmbeddingConfig>,
): Promise<
  | {
      kind: "ollama";
      ready: boolean;
      baseUrl: string;
      binaryFound: boolean;
      daemonRunning: boolean;
      modelPulled: boolean;
      modelName: string;
      installedModels: string[];
      error?: string;
    }
  | {
      kind: "openai-compat";
      ready: boolean;
      baseUrl: string;
      apiKeySet: boolean;
      model: string;
      extraBodyKeys: string[];
    }
> {
  if (config.provider === "openai-compat") {
    return {
      kind: "openai-compat",
      ready: Boolean(
        config.openaiCompat.baseUrl && config.openaiCompat.apiKeySet && config.openaiCompat.model,
      ),
      baseUrl: config.openaiCompat.baseUrl,
      apiKeySet: config.openaiCompat.apiKeySet,
      model: config.openaiCompat.model,
      extraBodyKeys: Object.keys(config.openaiCompat.extraBody),
    };
  }
  const ollama = await checkOllamaStatus(config.ollama.model, config.ollama.baseUrl).catch(
    (err) => ({
      binaryFound: false,
      daemonRunning: false,
      modelPulled: false,
      modelName: config.ollama.model,
      installedModels: [] as string[],
      error: err instanceof Error ? err.message : String(err),
    }),
  );
  return {
    kind: "ollama",
    ready: ollama.binaryFound && ollama.daemonRunning && ollama.modelPulled,
    baseUrl: config.ollama.baseUrl,
    ...ollama,
  };
}

async function getProviderStatus(
  resolved: ReturnType<typeof resolveSemanticEmbeddingConfig>,
): Promise<
  | {
      kind: "ollama";
      ready: boolean;
      baseUrl: string;
      binaryFound: boolean;
      daemonRunning: boolean;
      modelPulled: boolean;
      modelName: string;
      installedModels: string[];
      error?: string;
    }
  | {
      kind: "openai-compat";
      ready: boolean;
      baseUrl: string;
      apiKeySet: boolean;
      model: string;
      extraBodyKeys: string[];
    }
> {
  if (resolved.provider === "openai-compat") {
    return {
      kind: "openai-compat",
      ready: Boolean(resolved.baseUrl && resolved.apiKey && resolved.model),
      baseUrl: resolved.baseUrl,
      apiKeySet: Boolean(resolved.apiKey),
      model: resolved.model,
      extraBodyKeys: Object.keys(resolved.extraBody),
    };
  }
  const ollama = await checkOllamaStatus(resolved.model, resolved.baseUrl).catch((err) => ({
    binaryFound: false,
    daemonRunning: false,
    modelPulled: false,
    modelName: resolved.model,
    installedModels: [] as string[],
    error: err instanceof Error ? err.message : String(err),
  }));
  return {
    kind: "ollama",
    ready: ollama.binaryFound && ollama.daemonRunning && ollama.modelPulled,
    baseUrl: resolved.baseUrl,
    ...ollama,
  };
}

void readConfig;

function isAbortError(err: unknown): boolean {
  if (err instanceof Error) {
    if (err.name === "AbortError") return true;
    if (/aborted/i.test(err.message)) return true;
  }
  return false;
}
