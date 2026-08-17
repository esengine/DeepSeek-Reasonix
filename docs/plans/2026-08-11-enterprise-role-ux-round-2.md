# Enterprise Role UX Round 2 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Remove persistent-mode demo leakage, make built-in IP Agent templates resilient to invalid model plans, and close the audit/evidence usability gaps found in the enterprise-role UI review.

**Architecture:** Keep the existing Astro + vanilla module frontend and Node persistence/API boundary. Add a validated deterministic planner only after both model attempts fail for a registered template, derive all persistent UI summaries from existing APIs, and reuse the existing evidence drawer rather than introducing a second provenance flow.

**Tech Stack:** Astro, JavaScript ES modules, Node test runner, SQLite-backed Node API, Playwright.

---

### Task 1: Lock the new behavior with failing tests

**Files:**
- Modify: `site/server/agent-model-client.test.mjs`
- Modify: `site/src/scripts/agent-workbench.test.mjs`
- Modify: `site/e2e/real-ui-default-port.e2e.mjs`

**Steps:**

1. Add a model-client test where both model plans are invalid and a registered template must return a validated deterministic read-only plan.
2. Add the corresponding free-form test proving invalid plans still fail closed.
3. Add UI contracts for a retryable terminal failure and persistent-mode removal of static counts/governance names.
4. Run the focused tests and confirm the new assertions fail before implementation.

### Task 2: Implement deterministic Agent template fallback

**Files:**
- Modify: `site/server/agent-model-client.mjs`
- Modify: `site/server/agent-service.mjs`
- Modify: `site/src/scripts/agent-workbench.mjs`
- Modify: `site/src/pages/index.astro`
- Modify: `site/src/styles/ip-platform.css`

**Steps:**

1. Build minimal plans for each registered template using only allowed tools and explicit asset IDs when available.
2. Validate the fallback plan with the same service contract and aggregate both model attempts' usage.
3. Persist and expose the fallback marker in the plan-ready event without weakening execution authorization.
4. Add an “调整后重试” control that repopulates the task form but never submits automatically.
5. Run Agent unit, service, API and bounded E2E tests.

### Task 3: Replace persistent-mode sample content with workspace facts

**Files:**
- Modify: `site/src/pages/index.astro`
- Modify: `site/src/scripts/ip-platform.mjs`
- Modify: `site/src/styles/ip-platform.css`

**Steps:**

1. Replace navigation badge literals with named dynamic targets.
2. Rename analysis stages in business language and populate details from the latest real job.
3. Mark static lifecycle panels as demo-only and add a real governance summary, role capability card and event list.
4. Populate the new lifecycle panels from dashboard, shares, current role and audit responses.
5. Simplify high-frequency provider and implementation terminology while keeping operational diagnostics available.

### Task 4: Correct audit semantics and evidence navigation

**Files:**
- Modify: `site/src/scripts/ip-platform.mjs`
- Modify: `site/src/styles/ip-platform.css`
- Modify: `site/e2e/real-platform.e2e.mjs`

**Steps:**

1. Add user-facing labels and details for Agent, file scan and relationship events.
2. Map failed, blocked, cancelled, review and sensitive actions to distinct visible statuses.
3. Derive audit KPI values directly from the current ledger response.
4. Notify the platform when an Agent task reaches a terminal state and refresh affected summaries.
5. Match analysis quotations to published evidence and make each matched quote open the existing provenance drawer.

### Task 5: Verify both normal and real-corpus workspaces

**Files:**
- Modify: `artifacts/enterprise-role-ux-round-2/e2e-report.md`
- Create/update screenshots under: `artifacts/enterprise-role-ux-round-2/`

**Steps:**

1. Run the complete Node test suite and Astro production build.
2. Run the high-value Playwright suite without WSL; keep Go validation to the existing Windows-safe source test because local Go compilation is blocked by endpoint protection.
3. Exercise the main workspace at port 4388 and the isolated real-corpus workspace at port 63365.
4. Capture overview, analysis provenance, lifecycle, audit failure semantics and Agent completion screenshots.
5. Record commands, results, remaining production responsibilities and any non-product environmental limitation in the report.

