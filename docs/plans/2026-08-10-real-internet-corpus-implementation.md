# Real Internet Corpus Validation Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build and run a repeatable, secure multi-document internet corpus E2E that validates and improves long-document analysis, evidence accuracy, publication, and graph search.

**Architecture:** Keep source binaries in ignored runtime storage and commit only a fixed source manifest plus sanitized evidence. Add a deterministic section-balanced Markdown sampler before DeepSeek, validate every quoted source span against the sampled input, expose coverage metadata through the API/UI, then run three real provider jobs through one persistent gateway.

**Tech Stack:** Node.js ESM, built-in fetch/crypto/test, Astro UI, SQLite, MinerU API, DeepSeek JSON Output, Chromium E2E.

---

### Task 1: Add long-document sampling tests

**Files:**
- Create: `site/server/long-document-sampler.mjs`
- Create: `site/server/long-document-sampler.test.mjs`

**Steps:**
1. Write tests for short-document passthrough, deterministic output, front/middle/tail coverage, heading priority, hard budget and unchanged source fragments.
2. Run `node --test server/long-document-sampler.test.mjs` and verify the initial failure.
3. Implement heading-aware section parsing and balanced selection.
4. Rerun the focused test and expect PASS.

### Task 2: Validate DeepSeek source quotes

**Files:**
- Modify: `site/server/deepseek-client.mjs`
- Modify: `site/server/deepseek-client.test.mjs`
- Modify: `site/server/analysis-service.mjs`
- Modify: `site/server/publication-registry.mjs`

**Steps:**
1. Add tests proving hallucinated/translated quotes are removed and valid whitespace-normalized quotes remain.
2. Pass sampled Markdown to the model with explicit continuous-substring instructions.
3. Return sampling and quote-validation metadata from the adapter and persist it in the public job result.
4. Ensure publication evidence is marked verified only when quote validation did not fail.
5. Run the focused server tests and expect PASS.

### Task 3: Disclose long-document coverage in UI

**Files:**
- Modify: `site/src/pages/index.astro`
- Modify: `site/src/scripts/ip-platform.mjs`
- Modify: `site/src/scripts/ip-platform-contract.test.mjs`
- Modify: `site/e2e/real-platform.e2e.mjs`

**Steps:**
1. Add a contract test for analysis range disclosure.
2. Change the result card to show total parsed characters, model input characters and selected/total section counts.
3. Keep full-input copy for short documents and show a clear section-balanced label for long documents.
4. Build and run the contract tests.

### Task 4: Add secure internet corpus runner

**Files:**
- Create: `site/e2e/fixtures/internet-corpus.sources.json`
- Create: `site/e2e/internet-corpus.e2e.mjs`
- Modify: `site/package.json`

**Steps:**
1. Add tests/helpers for allowlisted HTTPS sources, size limits, MIME/magic checks and SHA-256 manifests.
2. Download only to `site/.runtime/internet-corpus/` and never commit source bodies.
3. Submit all sources through the real gateway with bounded concurrency, poll to completion, publish, validate evidence and query the graph.
4. Write sanitized JSON/Markdown reports and a browser screenshot under `artifacts/internet-corpus/`.

### Task 5: Run real corpus and optimize from evidence

**Files:**
- Modify as indicated by observed failures.
- Create: `artifacts/internet-corpus/report.md`
- Create: `artifacts/internet-corpus/results.json`
- Create: `artifacts/internet-corpus/source-manifest.json`
- Create: `artifacts/internet-corpus/01-real-corpus-results.png`

**Steps:**
1. Inspect PDF metadata and rendered representative pages before upload.
2. Run `npm run test:e2e:internet` with the real `apikey.txt` credentials.
3. Record provider queue time separately from processing failure; retain honest retry history.
4. Fix reproducible parser, extraction, evidence or UI issues and rerun only affected sources where safe.

### Task 6: Final regression and delivery

**Files:**
- Modify: `docs/INTELIFAR-USER-GUIDE.zh-CN.md`
- Modify: `INTELIFAR-DELIVERY.md`
- Modify: `artifacts/ip-asset-graph/acceptance-report.md`

**Steps:**
1. Run `npm test`, build, all offline E2E, graph performance, security scan and `npm audit --omit=dev`.
2. Use the real browser UI to inspect coverage disclosure, published assets and graph results; save final screenshots.
3. Verify no source binaries or credential values are staged.
4. Commit code and sanitized evidence in reviewable commits.
