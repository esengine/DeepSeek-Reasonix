import assert from "node:assert/strict";
import test from "node:test";
import { mkdtemp, rm } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { prepareCorpusSource, validateCorpusSource } from "./internet-corpus-source.mjs";

test("rejects non-HTTPS, non-allowlisted and unsafe corpus sources", () => {
  assert.throws(() => validateCorpusSource({ url: "http://arxiv.org/a.pdf", fileName: "a.pdf", format: "pdf" }), /allowlisted/);
  assert.throws(() => validateCorpusSource({ url: "https://example.com/a.pdf", fileName: "a.pdf", format: "pdf" }), /allowlisted/);
  assert.throws(() => validateCorpusSource({ url: "https://arxiv.org/a.pdf", fileName: "../a.pdf", format: "pdf" }), /filename/);
});

test("downloads a valid source and records an auditable hash", async () => {
  const directory = await mkdtemp(path.join(os.tmpdir(), "intelifar-corpus-"));
  try {
    const prepared = await prepareCorpusSource({ id: "paper", url: "https://arxiv.org/paper.pdf", fileName: "paper.pdf", format: "pdf" }, {
      cacheDir: directory,
      fetchImpl: async () => new Response("%PDF-1.7\nreal", { status: 200, headers: { "content-type": "application/pdf" } }),
    });
    assert.equal(prepared.cached, false);
    assert.equal(prepared.size, 13);
    assert.match(prepared.sha256, /^[a-f0-9]{64}$/u);
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});

test("rejects redirects outside the allowlist and invalid document signatures", async () => {
  const directory = await mkdtemp(path.join(os.tmpdir(), "intelifar-corpus-"));
  try {
    await assert.rejects(
      prepareCorpusSource({ id: "paper", url: "https://arxiv.org/paper.pdf", fileName: "paper.pdf", format: "pdf" }, {
        cacheDir: directory,
        fetchImpl: async () => new Response(null, { status: 302, headers: { location: "https://evil.example/paper.pdf" } }),
      }),
      /redirect host/,
    );
    await assert.rejects(
      prepareCorpusSource({ id: "paper", url: "https://arxiv.org/paper.pdf", fileName: "paper.pdf", format: "pdf" }, {
        cacheDir: directory,
        fetchImpl: async () => new Response("<html>wrong</html>", { status: 200, headers: { "content-type": "application/pdf" } }),
      }),
      /signature/,
    );
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});
