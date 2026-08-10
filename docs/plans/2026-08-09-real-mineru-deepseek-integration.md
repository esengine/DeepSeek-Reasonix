# Real MinerU + DeepSeek Integration Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace the analysis fixture path with a secure, server-side MinerU parsing and DeepSeek structured-IP analysis workflow, then prove it through a real browser E2E run.

**Architecture:** A small Node.js gateway serves the Astro production build and exposes same-origin analysis endpoints. It reads credentials at runtime from environment variables or the workspace-level `apikey.txt`, uploads documents to MinerU, polls and extracts `full.md`, submits a bounded Markdown excerpt to DeepSeek JSON Output, and keeps only sanitized job state in memory. The browser never receives credentials or third-party upload URLs.

**Tech Stack:** Node.js 22 built-ins, Astro static output, MinerU v4 HTTP API, DeepSeek Chat Completions JSON Output, Playwright/installed Chrome, Node test runner.

---

### Task 1: Secure runtime configuration

**Files:**
- Create: `site/server/config.mjs`
- Create: `site/server/config.test.mjs`
- Modify: `.gitignore`

**Steps:**
1. Write tests for environment-variable precedence, the named-block `apikey.txt` format, missing keys, and error messages that never include secret values.
2. Run `node --test server/config.test.mjs` and verify failure before implementation.
3. Implement `loadRuntimeConfig()` with `MINERU_API_KEY`, `DEEPSEEK_API_KEY`, and `INTELIFAR_API_KEY_FILE` support plus safe workspace discovery.
4. Add secret and runtime-result patterns to `.gitignore` while keeping sanitized E2E artifacts trackable.
5. Re-run the focused tests and verify pass.

### Task 2: MinerU document parser client

**Files:**
- Create: `site/server/mineru-client.mjs`
- Create: `site/server/mineru-client.test.mjs`
- Create: `site/server/zip-reader.mjs`
- Create: `site/server/zip-reader.test.mjs`

**Steps:**
1. Write fetch-mock tests for upload-URL creation, binary PUT, polling transitions, failure sanitization, and timeout.
2. Write ZIP fixture tests that locate and inflate `full.md` without extracting arbitrary paths to disk.
3. Implement the documented MinerU v4 flow: `file-urls/batch` → signed PUT → `extract-results/batch/{id}` → download archive.
4. Enforce supported extensions, safe filenames, a 25 MB local limit, bounded result size, and abort timeouts.
5. Verify the focused tests pass without live network access.

### Task 3: DeepSeek structured analysis client

**Files:**
- Create: `site/server/deepseek-client.mjs`
- Create: `site/server/deepseek-client.test.mjs`

**Steps:**
1. Write tests for the Chat Completions payload, JSON Output, schema normalization, empty output, malformed JSON, and sanitized upstream errors.
2. Implement a bounded-input prompt that returns document metadata, IP assets, risks, Wiki sections, source quotations, confidence, and usage metadata.
3. Default to `deepseek-v4-flash`, allow `DEEPSEEK_MODEL` override, disable thinking for deterministic extraction, and cap generated tokens.
4. Verify the focused tests pass.

### Task 4: Real analysis orchestration and API gateway

**Files:**
- Create: `site/server/analysis-service.mjs`
- Create: `site/server/analysis-service.test.mjs`
- Create: `site/server/real-analysis-server.mjs`
- Create: `site/server/http-security.mjs`

**Steps:**
1. Test state transitions `queued → mineru-upload → mineru-running → deepseek → complete` plus sanitized failure states.
2. Implement in-memory jobs, random UUID identifiers, bounded retention, and API responses that expose provider IDs but no credentials or signed URLs.
3. Add `POST /api/analysis`, `GET /api/analysis/:id`, and `GET /api/health`; parse multipart uploads server-side and validate extension, size, and file signature where applicable.
4. Serve `site/dist` with CSP, `nosniff`, frame denial, referrer and permissions policies; reject unsupported methods and traversal.
5. Add unit/contract tests for the API surface.

### Task 5: Browser integration

**Files:**
- Modify: `site/src/pages/index.astro`
- Modify: `site/src/scripts/ip-platform.mjs`
- Modify: `site/src/styles/ip-platform.css`
- Modify: `site/src/scripts/ip-platform-contract.test.mjs`

**Steps:**
1. Add a real file input and selected-file state while retaining the offline demo path.
2. When a real file is selected, submit multipart data to the same-origin API, poll job status, and map provider stages to the existing five-stage UI.
3. Render provider badges, real task/model metadata, usage totals, extracted summary, assets, risks, Wiki content, and source quotations using DOM text nodes only.
4. Add retry/error UX with no upstream stack traces or credential-bearing details.
5. Run contract/unit tests and visually verify loading, error, and success states.

### Task 6: Real E2E and delivery evidence

**Files:**
- Create: `site/e2e/fixtures/intelifar-real-analysis.html`
- Create: `site/e2e/real-platform.e2e.mjs`
- Modify: `site/package.json`
- Modify: `INTELIFAR-DELIVERY.md`
- Create at runtime: `artifacts/real-e2e/report.md`
- Create at runtime: `artifacts/real-e2e/analysis.json`
- Create at runtime: `artifacts/screenshots/10-real-api-analysis.png`

**Steps:**
1. Build the Astro site and start the real gateway with the workspace credential file.
2. Use an installed Chrome instance to upload the deterministic HTML fixture through the visible intake dialog.
3. Wait for MinerU and DeepSeek completion; assert non-placeholder provider IDs, `deepseek-v4-*`, positive token usage, at least one IP asset, Wiki content, and a source quotation.
4. Save only sanitized JSON/report data and a completed UI screenshot; scan them for either API key before accepting.
5. Run all unit tests, the original offline E2E, the real E2E, production build, `npm audit`, and `git diff --check`.
