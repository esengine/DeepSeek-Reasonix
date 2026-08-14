# intelifar IP Wiki Platform Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Deliver a branded, browser-verifiable enterprise long-document IP analysis and Wiki workspace on the DeepSeek-Reasonix web stack.

**Architecture:** Preserve the upstream Go kernel and transform the existing Astro site into a deterministic client-side application. Keep business-state transitions in a testable module and render the product as progressive-enhancement HTML with local persistence.

**Tech Stack:** Astro 7, semantic HTML, modular CSS, browser ES modules, Node test runner, Playwright-compatible E2E.

---

### Task 1: Brand and application shell

**Files:**
- Create: `site/src/styles/ip-platform.css`
- Create: `site/src/components/intelifarLogo.astro`
- Modify: `site/src/pages/index.astro`
- Create: `site/public/brand/intelifar-logo.png`
- Create: `site/public/brand/intelifar-logo-dark.png`

**Steps:** add official brand assets; implement responsive rail/header/content shell; verify semantic landmarks, keyboard focus, compact layout, and production build.

### Task 2: Deterministic domain state

**Files:**
- Create: `site/src/scripts/ip-platform-state.mjs`
- Create: `site/src/scripts/ip-platform-state.test.mjs`
- Create: `site/src/data/ip-platform.ts`

**Steps:** write failing tests for intake validation, pipeline progression, filtering, share validation, and audit append; implement minimal pure functions; run `npm test` and confirm all pass.

### Task 3: Six business acceptance surfaces

**Files:**
- Create: `site/src/components/platform/*.astro`
- Create: `site/src/scripts/ip-platform.mjs`
- Modify: `site/src/pages/index.astro`

**Steps:** build command center, documents, analysis, assets, Wiki/provenance, redaction, lifecycle, audit, and system health views; wire navigation, drawers, dialogs, search, filters, progress, state persistence, feedback, and downloads; test each interaction.

### Task 4: Delivery documentation and product identity

**Files:**
- Modify: `site/package.json`
- Create: `INTELIFAR-DELIVERY.md`
- Create: `docs/architecture/intelifar-ip-wiki.md`

**Steps:** document upstream attribution, run/build commands, implemented versus integration-ready capabilities, and the mapping from report acceptance criteria to UI evidence.

### Task 5: E2E and screenshots

**Files:**
- Create: `site/e2e/platform.e2e.mjs`
- Create: `artifacts/e2e-report.md`
- Create: `artifacts/screenshots/*.png`
- Create: `artifacts/delivery-tree.txt`

**Steps:** run unit contracts; build the production site; start the preview server; execute intake-to-audit E2E at desktop and mobile widths; visually inspect screenshots; fix defects and rerun until clean; generate the final delivery tree.
