import { promises as fs } from "node:fs";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  type IndexEntry,
  STORE_VERSION,
  SemanticStore,
  compareIndexIdentity,
  normalize,
  openStore,
  readIndexMeta,
  wipeStoreFiles,
} from "../src/index/semantic/store.js";

function unitVector(values: number[]): Float32Array {
  return normalize(new Float32Array(values));
}

function entry(
  path: string,
  startLine: number,
  endLine: number,
  vec: number[],
  mtimeMs = 1700000000000,
): IndexEntry {
  return {
    path,
    startLine,
    endLine,
    text: `chunk for ${path}:${startLine}-${endLine}`,
    embedding: unitVector(vec),
    mtimeMs,
  };
}

describe("SemanticStore", () => {
  let dir: string;

  beforeEach(async () => {
    dir = await mkdtemp(join(tmpdir(), "reasonix-store-"));
  });

  afterEach(async () => {
    await rm(dir, { recursive: true, force: true });
  });

  describe("add + search", () => {
    it("returns top-K hits ordered by cosine similarity", async () => {
      const store = await openStore(dir, { provider: "ollama", model: "test-model" });
      await store.add([
        entry("a.ts", 1, 30, [1, 0, 0]),
        entry("b.ts", 1, 30, [0, 1, 0]),
        entry("c.ts", 1, 30, [0, 0, 1]),
        entry("d.ts", 1, 30, [0.7, 0.7, 0]),
      ]);
      const q = unitVector([1, 0, 0]);
      const hits = store.search(q, 2);
      expect(hits).toHaveLength(2);
      expect(hits[0]?.entry.path).toBe("a.ts");
      // d.ts (0.7,0.7,0) has cosine ~0.707 with (1,0,0) → second.
      expect(hits[1]?.entry.path).toBe("d.ts");
      expect(hits[0]!.score).toBeGreaterThan(hits[1]!.score);
    });

    it("respects minScore threshold", async () => {
      const store = await openStore(dir, { provider: "ollama", model: "test-model" });
      await store.add([entry("a.ts", 1, 30, [1, 0, 0]), entry("b.ts", 1, 30, [0, 1, 0])]);
      const q = unitVector([1, 0, 0]);
      // (1,0,0) vs (0,1,0) cosine = 0; threshold 0.5 should drop it.
      const hits = store.search(q, 5, 0.5);
      expect(hits).toHaveLength(1);
      expect(hits[0]?.entry.path).toBe("a.ts");
    });

    it("rejects mismatched dimensionality", async () => {
      const store = await openStore(dir, { provider: "ollama", model: "test-model" });
      await store.add([entry("a.ts", 1, 30, [1, 0, 0])]);
      await expect(store.add([entry("b.ts", 1, 30, [1, 0])])).rejects.toThrow(/dim mismatch/);
    });
  });

  describe("persistence", () => {
    it("round-trips entries through the JSONL store", async () => {
      const a = await openStore(dir, { provider: "ollama", model: "test-model" });
      await a.add([entry("a.ts", 1, 30, [1, 2, 3]), entry("b.ts", 1, 30, [4, 5, 6])]);
      const b = await openStore(dir, { provider: "ollama", model: "test-model" });
      expect(b.size).toBe(2);
      const q = unitVector([1, 2, 3]);
      const hits = b.search(q, 1);
      expect(hits[0]?.entry.path).toBe("a.ts");
    });

    it("rejects opening an index built with a different provider", async () => {
      const a = await openStore(dir, { provider: "ollama", model: "model-a" });
      await a.add([entry("a.ts", 1, 30, [1, 0, 0])]);
      await expect(openStore(dir, { provider: "openai-compat", model: "model-a" })).rejects.toThrow(
        /provider "ollama"/,
      );
    });

    it("treats legacy meta without provider as ollama", async () => {
      await fs.writeFile(
        join(dir, "index.meta.json"),
        JSON.stringify({
          version: STORE_VERSION,
          model: "legacy-model",
          dim: 3,
          updatedAt: new Date().toISOString(),
        }),
        "utf8",
      );
      await fs.writeFile(join(dir, "index.jsonl"), "", "utf8");
      const store = await openStore(dir, { provider: "ollama", model: "legacy-model" });
      expect(store.empty).toBe(true);
    });

    it("survives a mid-write crash via tmp-file rename on remove", async () => {
      const store = await openStore(dir, { provider: "ollama", model: "test-model" });
      await store.add([entry("a.ts", 1, 30, [1, 0, 0]), entry("b.ts", 1, 30, [0, 1, 0])]);
      const removed = await store.remove(["a.ts"]);
      expect(removed).toBe(1);
      const reloaded = await openStore(dir, { provider: "ollama", model: "test-model" });
      expect(reloaded.size).toBe(1);
      expect(reloaded.all[0]?.path).toBe("b.ts");
    });

    it("wipe clears disk + memory", async () => {
      const store = await openStore(dir, { provider: "ollama", model: "test-model" });
      await store.add([entry("a.ts", 1, 30, [1, 0, 0])]);
      await store.wipe();
      expect(store.empty).toBe(true);
      const reloaded = await openStore(dir, { provider: "ollama", model: "test-model" });
      expect(reloaded.empty).toBe(true);
    });

    it("readIndexMeta exposes persisted identity", async () => {
      const store = await openStore(dir, { provider: "openai-compat", model: "bge-m3" });
      await store.add([entry("a.ts", 1, 30, [1, 0, 0])]);
      const meta = await readIndexMeta(dir);
      expect(meta).not.toBeNull();
      expect(meta?.provider).toBe("openai-compat");
      expect(meta?.model).toBe("bge-m3");
    });

    it("wipeStoreFiles removes persisted index identity before reopen", async () => {
      const store = await openStore(dir, { provider: "ollama", model: "nomic-embed-text" });
      await store.add([entry("a.ts", 1, 30, [1, 0, 0])]);
      await wipeStoreFiles(dir);
      const meta = await readIndexMeta(dir);
      expect(meta).toBeNull();
      const reopened = await openStore(dir, { provider: "openai-compat", model: "bge-m3" });
      expect(reopened.empty).toBe(true);
    });
  });

  describe("fileMtimes", () => {
    it("returns the mtime per indexed file (incremental rebuild driver)", async () => {
      const store = await openStore(dir, { provider: "ollama", model: "test-model" });
      await store.add([
        entry("a.ts", 1, 30, [1, 0, 0], 1000),
        entry("a.ts", 31, 60, [0, 1, 0], 1000),
        entry("b.ts", 1, 30, [0, 0, 1], 2000),
      ]);
      const mtimes = store.fileMtimes();
      expect(mtimes.get("a.ts")).toBe(1000);
      expect(mtimes.get("b.ts")).toBe(2000);
      expect(mtimes.size).toBe(2);
    });
  });

  describe("identity helpers", () => {
    it("compareIndexIdentity reports provider mismatch first", () => {
      expect(
        compareIndexIdentity(
          { provider: "ollama", model: "nomic-embed-text" },
          { provider: "openai-compat", model: "nomic-embed-text" },
        ),
      ).toBe("provider");
    });

    it("compareIndexIdentity reports model mismatch when provider matches", () => {
      expect(
        compareIndexIdentity(
          { provider: "openai-compat", model: "text-embedding-3-small" },
          { provider: "openai-compat", model: "bge-m3" },
        ),
      ).toBe("model");
    });
  });

  describe("normalize helper", () => {
    it("produces unit vectors", () => {
      const v = normalize(new Float32Array([3, 0, 4]));
      const len = Math.sqrt(v[0]! * v[0]! + v[1]! * v[1]! + v[2]! * v[2]!);
      expect(len).toBeCloseTo(1, 5);
    });

    it("is a no-op on the zero vector", () => {
      const v = normalize(new Float32Array([0, 0, 0]));
      expect(v[0]).toBe(0);
      expect(v[1]).toBe(0);
      expect(v[2]).toBe(0);
    });
  });

  describe("STORE_VERSION", () => {
    it("is exported as a positive integer", () => {
      expect(STORE_VERSION).toBeGreaterThanOrEqual(1);
      expect(Number.isInteger(STORE_VERSION)).toBe(true);
    });
  });

  describe("constructor identity", () => {
    it("exposes indexDir + model on the instance", () => {
      const s = new SemanticStore("/tmp/x", { provider: "ollama", model: "m" });
      expect(s.indexDir).toBe("/tmp/x");
      expect(s.model).toBe("m");
      expect(s.empty).toBe(true);
    });
  });
});
