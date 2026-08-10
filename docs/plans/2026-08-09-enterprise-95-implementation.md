# intelifar Enterprise 95+ Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Raise the current enterprise acceptance score from 78 to at least 95 by closing the live analysis-to-Wiki workflow and hardening UX, security, reliability, and test evidence.

**Architecture:** Add a replaceable durable publication registry behind the existing same-origin Node gateway. Enrich live analysis with stable evidence, publish reviewed jobs through explicit APIs, and render published assets/Wiki/evidence dynamically while retaining deterministic demo fixtures for offline acceptance.

**Tech Stack:** Node.js 22 built-ins, Astro 7, browser DOM APIs, Node test runner, Playwright/installed Chrome, MinerU v4, DeepSeek Chat Completions JSON Output.

---

### Task 1: Enterprise score baseline and contracts

**Files:**
- Create: `site/src/scripts/enterprise-contract.test.mjs`
- Modify: `site/package.json`

**Steps:**
1. Add failing contracts for truthful external-provider copy, working Wiki search hooks, publish controls, dynamic asset/Wiki containers, accessibility semantics, and no known dead primary action.
2. Add the contract suite to `npm test` and confirm it fails before implementation.
3. Keep the scorecard in the final acceptance report and require evidence for every awarded point.

### Task 2: Durable publication registry and evidence model

**Files:**
- Create: `site/server/publication-registry.mjs`
- Create: `site/server/publication-registry.test.mjs`
- Modify: `site/server/analysis-service.mjs`
- Modify: `site/server/analysis-service.test.mjs`

**Steps:**
1. Write tests for stable evidence IDs/hashes, atomic publication, idempotent republish, restart reload, search, and secret-safe serialized records.
2. Implement a configurable JSON registry directory with atomic temporary-file rename and an in-memory index.
3. Enrich completed analysis results with document hash, provider task, section precision, quote hash, and evidence coverage without inventing page/block locations.
4. Publish an immutable version snapshot only from a completed job.

### Task 3: Enterprise APIs and gateway hardening

**Files:**
- Modify: `site/server/real-analysis-server.mjs`
- Modify: `site/server/real-analysis-server.test.mjs`
- Modify: `site/server/http-security.mjs`

**Steps:**
1. Add failing API tests for publish, list assets, Wiki lookup, evidence lookup, search, duplicate publication, invalid IDs, and rate limiting.
2. Implement `POST /api/analysis/:id/publish`, `GET /api/assets`, `GET /api/assets/:id`, `GET /api/wiki/:id`, `GET /api/evidence/:id`, and `GET /api/search?q=`.
3. Return accurate provider/data-boundary information from health and add request correlation/rate-limit headers without leaking upstream details.
4. Re-run focused security and server tests.

### Task 4: Live review, publish, asset, Wiki, and evidence UX

**Files:**
- Modify: `site/src/pages/index.astro`
- Modify: `site/src/scripts/ip-platform.mjs`
- Modify: `site/src/scripts/ip-platform-state.mjs`
- Modify: `site/src/styles/ip-platform.css`

**Steps:**
1. Add a review-and-publish toolbar with draft/publishing/published/error states and provider-boundary disclosure.
2. Render published assets into the asset registry and populate the asset drawer from selected record data.
3. Render dynamic Wiki title, summary, metrics, mechanisms, relationships, source version, and citations.
4. Implement actual Wiki/global search with result counts, empty state, match highlighting, and keyboard navigation.
5. Populate the provenance drawer from real evidence and display honest section-level precision when page anchors are unavailable.
6. Wire or explicitly disable primary actions, preserve audit feedback, and make mobile Wiki navigation usable.

### Task 5: Accessibility and visual refinement

**Files:**
- Modify: `site/src/styles/ip-platform.css`
- Modify: `site/src/scripts/ip-platform.mjs`
- Modify: `site/src/pages/index.astro`

**Steps:**
1. Raise sub-10px production text to a readable type scale while preserving enterprise density.
2. Add focus containment/restoration for drawers and dialogs, Escape handling, accurate `aria-current`, `aria-expanded`, `aria-busy`, and live status messaging.
3. Add a collapsible Wiki focus mode, mobile section navigator, and clear loading/empty/error surfaces.
4. Verify contrast, 390px mobile layout, 1440px desktop layout, reduced motion, and keyboard-only critical flow.

### Task 6: Regression, live E2E, security, and score evidence

**Files:**
- Modify: `site/e2e/platform.e2e.mjs`
- Modify: `site/e2e/real-platform.e2e.mjs`
- Create at runtime: `artifacts/enterprise-95-scorecard.md`
- Create at runtime: `artifacts/screenshots/11..n-enterprise-*.png`
- Modify: `INTELIFAR-DELIVERY.md`

**Steps:**
1. Expand offline E2E to cover every module, Wiki search, focus behavior, empty states, truthful provider copy, and responsive layouts.
2. Expand live E2E to publish the real MinerU/DeepSeek result, find it in the asset registry, open its generated Wiki, and verify a real evidence hash.
3. Run `npm test`, build, offline E2E, live E2E, credential scan, `npm audit`, and `git diff --check`.
4. Visually inspect each final screenshot and iterate until no critical or high UX defect remains.
5. Award the final score only from passing evidence; continue iterating if it is below 95.
