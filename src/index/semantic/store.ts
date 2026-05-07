/** JSONL append-only (Ctrl+C-safe) + linear cosine scan over unboxed Float32Array — fast enough for ≤10k chunks. */

import { promises as fs } from "node:fs";
import path from "node:path";
import type { EmbeddingProvider } from "../../config.js";
import type { CodeChunk } from "./chunker.js";

export interface IndexEntry extends CodeChunk {
  embedding: Float32Array;
  mtimeMs: number;
}

export interface SearchHit {
  entry: IndexEntry;
  score: number;
}

export type IndexMismatch = "provider" | "model";

export interface IndexIdentity {
  provider: EmbeddingProvider;
  model: string;
}

export interface IndexMeta extends IndexIdentity {
  version: number;
  dim: number;
  updatedAt: string;
}

export const STORE_VERSION = 1;

const META_FILE = "index.meta.json";
const DATA_FILE = "index.jsonl";

export async function readIndexMeta(indexDir: string): Promise<IndexMeta | null> {
  try {
    const raw = await fs.readFile(path.join(indexDir, META_FILE), "utf8");
    return normalizeMeta(JSON.parse(raw) as Partial<IndexMeta>);
  } catch {
    return null;
  }
}

export function compareIndexIdentity(
  meta: IndexIdentity,
  identity: IndexIdentity,
): IndexMismatch | null {
  if (meta.provider !== identity.provider) return "provider";
  if (meta.model !== identity.model) return "model";
  return null;
}

export async function wipeStoreFiles(indexDir: string): Promise<void> {
  await fs.rm(path.join(indexDir, DATA_FILE), { force: true });
  await fs.rm(path.join(indexDir, META_FILE), { force: true });
}

export class SemanticStore {
  private entries: IndexEntry[] = [];
  private byPath = new Map<string, IndexEntry[]>();
  private dim = 0;

  constructor(
    public readonly indexDir: string,
    public readonly identity: IndexIdentity,
  ) {}

  get provider(): EmbeddingProvider {
    return this.identity.provider;
  }

  get model(): string {
    return this.identity.model;
  }

  get empty(): boolean {
    return this.entries.length === 0;
  }

  get size(): number {
    return this.entries.length;
  }

  get all(): readonly IndexEntry[] {
    return this.entries;
  }

  fileMtimes(): Map<string, number> {
    const out = new Map<string, number>();
    for (const [p, group] of this.byPath) {
      const first = group[0];
      if (first) out.set(p, first.mtimeMs);
    }
    return out;
  }

  async add(entries: readonly IndexEntry[]): Promise<void> {
    if (entries.length === 0) return;
    if (this.dim === 0) this.dim = entries[0]!.embedding.length;
    const lines: string[] = [];
    for (const e of entries) {
      if (e.embedding.length !== this.dim) {
        throw new Error(
          `embedding dim mismatch: expected ${this.dim}, got ${e.embedding.length} for ${e.path}:${e.startLine}`,
        );
      }
      this.entries.push(e);
      const list = this.byPath.get(e.path);
      if (list) list.push(e);
      else this.byPath.set(e.path, [e]);
      lines.push(serializeEntry(e));
    }
    await fs.mkdir(this.indexDir, { recursive: true });
    await fs.appendFile(path.join(this.indexDir, DATA_FILE), `${lines.join("\n")}\n`, "utf8");
    await this.writeMeta();
  }

  async remove(paths: readonly string[]): Promise<number> {
    if (paths.length === 0) return 0;
    const drop = new Set(paths);
    const before = this.entries.length;
    this.entries = this.entries.filter((e) => !drop.has(e.path));
    for (const p of paths) this.byPath.delete(p);
    const removed = before - this.entries.length;
    if (removed > 0) await this.flush();
    return removed;
  }

  search(query: Float32Array, topK = 8, minScore = 0): SearchHit[] {
    if (this.entries.length === 0) return [];
    if (query.length !== this.dim && this.dim !== 0) {
      throw new Error(`query dim ${query.length} ≠ index dim ${this.dim}`);
    }
    const heap: SearchHit[] = [];
    for (const entry of this.entries) {
      const score = dot(query, entry.embedding);
      if (score < minScore) continue;
      if (heap.length < topK) {
        heap.push({ entry, score });
        if (heap.length === topK) heap.sort((a, b) => a.score - b.score);
      } else if (score > heap[0]!.score) {
        heap[0] = { entry, score };
        for (let i = 0; i < heap.length - 1; i++) {
          if (heap[i]!.score > heap[i + 1]!.score) {
            const tmp = heap[i]!;
            heap[i] = heap[i + 1]!;
            heap[i + 1] = tmp;
          }
        }
      }
    }
    return heap.sort((a, b) => b.score - a.score);
  }

  private async flush(): Promise<void> {
    await fs.mkdir(this.indexDir, { recursive: true });
    const tmp = path.join(this.indexDir, `${DATA_FILE}.tmp`);
    const final = path.join(this.indexDir, DATA_FILE);
    const lines = this.entries.map(serializeEntry).join("\n");
    await fs.writeFile(tmp, lines.length > 0 ? `${lines}\n` : "", "utf8");
    await fs.rename(tmp, final);
    await this.writeMeta();
  }

  private async writeMeta(): Promise<void> {
    const meta: IndexMeta = {
      version: STORE_VERSION,
      provider: this.provider,
      model: this.model,
      dim: this.dim,
      updatedAt: new Date().toISOString(),
    };
    await fs.writeFile(
      path.join(this.indexDir, META_FILE),
      `${JSON.stringify(meta, null, 2)}\n`,
      "utf8",
    );
  }

  async wipe(): Promise<void> {
    this.entries = [];
    this.byPath.clear();
    this.dim = 0;
    await wipeStoreFiles(this.indexDir);
  }
}

export async function openStore(indexDir: string, identity: IndexIdentity): Promise<SemanticStore> {
  const store = new SemanticStore(indexDir, identity);
  const dataPath = path.join(indexDir, DATA_FILE);

  const meta = await readIndexMeta(indexDir);

  if (meta) {
    if (meta.version !== STORE_VERSION) {
      throw new Error(
        `Index format version ${meta.version} does not match current ${STORE_VERSION}. Run \`reasonix index --rebuild\`.`,
      );
    }
    const mismatch = compareIndexIdentity(meta, identity);
    if (mismatch !== null) {
      throw new Error(
        `Index was built with provider "${meta.provider}" model "${meta.model}" but current config is provider "${identity.provider}" model "${identity.model}". Run \`reasonix index --rebuild\`.`,
      );
    }
  }

  let raw: string;
  try {
    raw = await fs.readFile(dataPath, "utf8");
  } catch {
    return store;
  }
  for (const line of raw.split("\n")) {
    if (line.length === 0) continue;
    try {
      const entry = deserializeEntry(line);
      (store as unknown as { dim: number }).dim = entry.embedding.length;
      (store as unknown as { entries: IndexEntry[] }).entries.push(entry);
      const map = (store as unknown as { byPath: Map<string, IndexEntry[]> }).byPath;
      const list = map.get(entry.path);
      if (list) list.push(entry);
      else map.set(entry.path, [entry]);
    } catch {
      /* tolerate malformed line */
    }
  }
  return store;
}

export function normalize(v: Float32Array): Float32Array {
  let sum = 0;
  for (let i = 0; i < v.length; i++) sum += v[i]! * v[i]!;
  const inv = sum > 0 ? 1 / Math.sqrt(sum) : 0;
  for (let i = 0; i < v.length; i++) v[i] = v[i]! * inv;
  return v;
}

function dot(a: Float32Array, b: Float32Array): number {
  let s = 0;
  for (let i = 0; i < a.length; i++) s += a[i]! * b[i]!;
  return s;
}

function serializeEntry(e: IndexEntry): string {
  const buf = Buffer.from(e.embedding.buffer, e.embedding.byteOffset, e.embedding.byteLength);
  return JSON.stringify({
    p: e.path,
    s: e.startLine,
    e: e.endLine,
    m: e.mtimeMs,
    t: e.text,
    v: buf.toString("base64"),
  });
}

function deserializeEntry(line: string): IndexEntry {
  const parsed = JSON.parse(line) as {
    p: string;
    s: number;
    e: number;
    m: number;
    t: string;
    v: string;
  };
  const buf = Buffer.from(parsed.v, "base64");
  const embedding = new Float32Array(
    buf.buffer.slice(buf.byteOffset, buf.byteOffset + buf.byteLength),
  );
  return {
    path: parsed.p,
    startLine: parsed.s,
    endLine: parsed.e,
    mtimeMs: parsed.m,
    text: parsed.t,
    embedding: new Float32Array(embedding),
  };
}

function normalizeMeta(meta: Partial<IndexMeta>): IndexMeta {
  return {
    version: typeof meta.version === "number" ? meta.version : STORE_VERSION,
    provider: meta.provider === "openai-compat" ? "openai-compat" : "ollama",
    model: typeof meta.model === "string" ? meta.model : "",
    dim: typeof meta.dim === "number" ? meta.dim : 0,
    updatedAt: typeof meta.updatedAt === "string" ? meta.updatedAt : new Date(0).toISOString(),
  };
}
