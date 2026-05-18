import { promises as fs } from "node:fs";
import path from "node:path";
import { type ResolvedEmbeddingConfig, resolveSemanticEmbeddingConfig } from "../../config.js";
import { type ResolvedIndexConfig, defaultIndexConfig } from "../config.js";
import { walkChunks } from "./chunker.js";
import type { CodeChunk, SkipReason } from "./chunker.js";
import { embed, embedAll, probeOllama } from "./embedding.js";
import type { EmbedOptions } from "./embedding.js";
import {
  compareIndexIdentity,
  normalize,
  openStore,
  readIndexMeta,
  wipeStoreFiles,
} from "./store.js";
import type { IndexEntry, IndexIdentity, IndexMismatch, SearchHit } from "./store.js";

export const INDEX_DIR_NAME = path.join(".reasonix", "semantic");

type BuildOptions = {
  provider?: "ollama" | "openai-compat";
  baseUrl?: string;
  apiKey?: string;
  model?: string;
  extraBody?: Record<string, unknown>;
  timeoutMs?: number;
  signal?: AbortSignal;
  windowLines?: number;
  overlap?: number;
  rebuild?: boolean;
  indexConfig?: ResolvedIndexConfig;
  onProgress?: (info: BuildProgress) => void;
  configPath?: string;
};

export type SkipBuckets = Record<SkipReason, number>;

export interface BuildProgress {
  phase: "setup" | "scan" | "embed" | "write" | "done";
  filesScanned?: number;
  chunksTotal?: number;
  chunksDone?: number;
  filesSkipped?: number;
  filesChanged?: number;
  skipBuckets?: SkipBuckets;
}

export interface BuildResult {
  filesScanned: number;
  filesChanged: number;
  chunksAdded: number;
  chunksRemoved: number;
  chunksSkipped: number;
  skipBuckets: SkipBuckets;
  durationMs: number;
}

function emptyBuckets(): SkipBuckets {
  return {
    defaultDir: 0,
    defaultFile: 0,
    binaryExt: 0,
    binaryContent: 0,
    tooLarge: 0,
    gitignore: 0,
    pattern: 0,
    readError: 0,
  };
}

export async function buildIndex(root: string, opts: BuildOptions = {}): Promise<BuildResult> {
  const t0 = Date.now();
  const indexDir = path.join(root, INDEX_DIR_NAME);
  const resolved = resolveBuildEmbeddingConfig(opts);

  opts.onProgress?.({ phase: "setup" });
  throwIfAborted(opts.signal);
  await probeEmbeddingProvider(resolved, opts.signal);
  throwIfAborted(opts.signal);

  if (opts.rebuild) await wipeStoreFiles(indexDir);
  const store = await openStore(indexDir, {
    provider: resolved.provider,
    model: resolved.model,
  });

  const lastMtimes = store.fileMtimes();
  const seenPaths = new Set<string>();
  const fileChunks = new Map<string, { chunks: CodeChunk[]; mtimeMs: number }>();
  let filesScanned = 0;
  let filesSkipped = 0;
  const skipBuckets = emptyBuckets();
  for await (const chunk of walkChunks(root, {
    windowLines: opts.windowLines,
    overlap: opts.overlap,
    config: opts.indexConfig ?? defaultIndexConfig(),
    onSkip: (_p, reason) => {
      skipBuckets[reason]++;
    },
  })) {
    throwIfAborted(opts.signal);
    seenPaths.add(chunk.path);
    let bucket = fileChunks.get(chunk.path);
    if (!bucket) {
      filesScanned++;
      const abs = path.join(root, chunk.path);
      let mtimeMs = 0;
      try {
        const stat = await fs.stat(abs);
        mtimeMs = stat.mtimeMs;
      } catch {
        continue;
      }
      const last = lastMtimes.get(chunk.path);
      if (last !== undefined && last === mtimeMs && !opts.rebuild) {
        filesSkipped++;
        continue;
      }
      bucket = { chunks: [], mtimeMs };
      fileChunks.set(chunk.path, bucket);
    }
    bucket.chunks.push(chunk);
    opts.onProgress?.({ phase: "scan", filesScanned });
  }

  throwIfAborted(opts.signal);
  const deletedPaths: string[] = [];
  for (const oldPath of lastMtimes.keys()) {
    if (!seenPaths.has(oldPath)) deletedPaths.push(oldPath);
  }
  const replacePaths = [...fileChunks.keys()].filter((p) => lastMtimes.has(p));
  throwIfAborted(opts.signal);
  const removed = await store.remove([...deletedPaths, ...replacePaths]);

  let chunksAdded = 0;
  let chunksSkipped = 0;
  const filesChanged = fileChunks.size;
  let chunksTotal = 0;
  for (const { chunks } of fileChunks.values()) chunksTotal += chunks.length;
  let chunksDone = 0;
  for (const [, bucket] of fileChunks) {
    throwIfAborted(opts.signal);
    if (bucket.chunks.length === 0) continue;
    const texts = bucket.chunks.map((c) => c.text);
    const vectors = await embedAll(texts, {
      ...resolved,
      signal: opts.signal,
      onProgress: (done, total) => {
        opts.onProgress?.({
          phase: "embed",
          filesScanned,
          filesChanged,
          chunksTotal,
          chunksDone: chunksDone + done,
        });
        if (done === total) chunksDone += total;
      },
      onError: (idx, err) => {
        chunksSkipped++;
        const c = bucket.chunks[idx];
        const where = c ? `${c.path}:${c.startLine}-${c.endLine}` : `chunk #${idx}`;
        const msg = err instanceof Error ? err.message : String(err);
        process.stderr.write(`\n  ! skipped ${where}: ${msg}\n`);
      },
    });
    throwIfAborted(opts.signal);
    const entries: IndexEntry[] = [];
    for (let i = 0; i < bucket.chunks.length; i++) {
      const vec = vectors[i];
      if (!vec) continue;
      const c = bucket.chunks[i];
      if (!c) continue;
      normalize(vec);
      entries.push({
        path: c.path,
        startLine: c.startLine,
        endLine: c.endLine,
        text: c.text,
        embedding: vec,
        mtimeMs: bucket.mtimeMs,
      });
    }
    throwIfAborted(opts.signal);
    if (entries.length > 0) await store.add(entries);
    chunksAdded += entries.length;
  }

  throwIfAborted(opts.signal);
  opts.onProgress?.({
    phase: "done",
    filesScanned,
    filesSkipped,
    filesChanged,
    chunksTotal,
    chunksDone,
    skipBuckets,
  });

  return {
    filesScanned,
    filesChanged,
    chunksAdded,
    chunksRemoved: removed,
    chunksSkipped,
    skipBuckets,
    durationMs: Date.now() - t0,
  };
}

type QueryOptions = {
  provider?: "ollama" | "openai-compat";
  baseUrl?: string;
  apiKey?: string;
  model?: string;
  extraBody?: Record<string, unknown>;
  timeoutMs?: number;
  signal?: AbortSignal;
  topK?: number;
  minScore?: number;
  configPath?: string;
};

export async function querySemantic(
  root: string,
  query: string,
  opts: QueryOptions = {},
): Promise<SearchHit[] | null> {
  const indexDir = path.join(root, INDEX_DIR_NAME);
  const resolved = resolveQueryEmbeddingConfig(opts);
  const store = await openStore(indexDir, {
    provider: resolved.provider,
    model: resolved.model,
  });
  if (store.empty) return null;
  const qvec = await embed(query, { ...resolved, signal: opts.signal });
  normalize(qvec);
  return store.search(qvec, opts.topK ?? 8, opts.minScore ?? 0.3);
}

export async function indexExists(root: string): Promise<boolean> {
  const meta = path.join(root, INDEX_DIR_NAME, "index.meta.json");
  try {
    await fs.access(meta);
    return true;
  } catch {
    return false;
  }
}

export async function indexCompatible(
  root: string,
  opts: { provider?: "ollama" | "openai-compat"; model?: string; configPath?: string } = {},
): Promise<boolean> {
  const meta = await readIndexMeta(path.join(root, INDEX_DIR_NAME));
  if (!meta) return false;
  return compareIndexIdentity(meta, resolveIndexIdentity(opts)) === null;
}

function resolveBuildEmbeddingConfig(opts: BuildOptions): ResolvedEmbeddingConfig {
  if (opts.provider === "openai-compat") {
    if (!opts.baseUrl || !opts.apiKey || !opts.model) {
      throw new Error(
        "OpenAI-compatible embeddings require baseUrl, apiKey, and model when passed directly.",
      );
    }
    return {
      provider: "openai-compat",
      baseUrl: opts.baseUrl,
      apiKey: opts.apiKey,
      model: opts.model,
      extraBody: opts.extraBody ?? {},
      timeoutMs: opts.timeoutMs ?? 30_000,
    };
  }
  if (opts.baseUrl || opts.model) {
    return {
      provider: "ollama",
      baseUrl: opts.baseUrl ?? process.env.OLLAMA_URL ?? "http://localhost:11434",
      model: opts.model ?? process.env.REASONIX_EMBED_MODEL ?? "nomic-embed-text",
      timeoutMs: opts.timeoutMs ?? 30_000,
    };
  }
  return resolveSemanticEmbeddingConfig(opts.configPath);
}

function resolveIndexIdentity(opts: {
  provider?: "ollama" | "openai-compat";
  model?: string;
  configPath?: string;
}): IndexIdentity {
  if (opts.provider && opts.model) {
    return { provider: opts.provider, model: opts.model };
  }
  const resolved = resolveSemanticEmbeddingConfig(opts.configPath);
  return { provider: resolved.provider, model: resolved.model };
}

function resolveQueryEmbeddingConfig(opts: QueryOptions): ResolvedEmbeddingConfig {
  return resolveBuildEmbeddingConfig(opts);
}

async function probeEmbeddingProvider(
  config: ResolvedEmbeddingConfig,
  signal: AbortSignal | undefined,
): Promise<void> {
  if (config.provider === "openai-compat") return;
  const probe = await probeOllama({ baseUrl: config.baseUrl, signal });
  if (!probe.ok) {
    throw new Error(
      `Ollama is not reachable: ${probe.error}. Install from https://ollama.com, then \`ollama serve\` and \`ollama pull ${config.model}\`.`,
    );
  }
}

function throwIfAborted(signal: AbortSignal | undefined): void {
  if (signal?.aborted) {
    throw new Error("semantic indexing aborted");
  }
}
