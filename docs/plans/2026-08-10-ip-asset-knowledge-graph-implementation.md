# IP Asset Knowledge Graph Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build a permission-safe, evidence-backed IP asset relationship store, graph-enhanced search, and polished list/graph dual-view with comprehensive automated and browser testing.

**Architecture:** Keep immutable publication snapshots and Wiki versions as audit truth. Add rebuildable SQLite node/edge/evidence projections, expose bounded workspace-scoped graph/search APIs, then render an accessible native-SVG network with an equivalent relationship list. AI edges remain proposed until an editor confirms them.

**Tech Stack:** Node.js ESM, better-sqlite3, Astro, native SVG/CSS/JavaScript, Node test runner, Chrome DevTools Protocol E2E.

---

### Task 1: Graph schema and publication projection

**Files:**
- Modify: `site/server/platform-store.mjs`
- Modify: `site/server/platform-store.test.mjs`

**Step 1: Write failing tests**

Add tests that publish three assets and assert:

- `asset_nodes`, `asset_relationships`, `relationship_evidence`, and `asset_aliases` exist;
- node projection is workspace-scoped and idempotent;
- model relations use canonical asset IDs and start as `proposed`;
- ambiguous or unknown targets do not create graph nodes or formal edges;
- publication snapshots remain unchanged.

**Step 2: Run the focused test and verify failure**

Run: `node --test server/platform-store.test.mjs`

Expected: FAIL because graph tables and methods do not exist.

**Step 3: Implement the schema and projector**

Add checked constants and parameterized statements for:

```js
const RELATION_TYPES = new Set(["depends_on", "implements", "derived_from", "replaces", "references", "part_of", "similar_to", "conflicts_with"]);
const RELATION_STATUSES = new Set(["proposed", "confirmed", "rejected", "superseded"]);
```

Project each saved publication into `asset_nodes`, normalize aliases, map supported relation labels, resolve exact canonical titles/IDs only, and attach existing evidence IDs. Execute publication insert, initial Wiki versions, and query projection in one database transaction.

**Step 4: Run tests**

Run: `node --test server/platform-store.test.mjs`

Expected: PASS.

### Task 2: Relationship lifecycle, bounded traversal, and graph search

**Files:**
- Modify: `site/server/platform-store.mjs`
- Modify: `site/server/platform-store.test.mjs`
- Modify: `site/server/publication-registry.mjs`
- Modify: `site/server/publication-registry.test.mjs`

**Step 1: Write failing tests**

Cover:

- manual relationship creation validates workspace endpoints and controlled type;
- confirmation/rejection transitions are audited by the caller and cannot cross workspaces;
- viewer graph excludes confidential nodes and every incident edge;
- traversal handles cycles, clamps depth to 2, caps nodes/edges, and reports truncation;
- search preserves exact text matches before one-hop and two-hop results;
- graph-expanded results contain a human-readable explanation and relationship path;
- default search excludes proposed edges unless explicitly requested.

**Step 2: Run focused tests and verify failure**

Run: `node --test server/platform-store.test.mjs server/publication-registry.test.mjs`

Expected: FAIL on missing relationship and graph-search methods.

**Step 3: Implement minimal graph services**

Add store methods:

```js
createRelationship(workspaceId, input)
updateRelationshipStatus(workspaceId, relationshipId, status, actorUserId)
getRelationship(workspaceId, relationshipId)
getAssetGraph(workspaceId, options)
searchAssetGraph(workspaceId, query, options)
rebuildAssetGraph(workspaceId)
```

Use only prepared statements. Filter visible nodes before selecting edges or traversing. Keep exact/title substring results first, then BFS expansion with visited-node tracking, depth decay, stable tie-breaking, and explicit paths.

**Step 4: Run tests**

Run the focused command and expect PASS.

### Task 3: Authenticated graph and relationship APIs

**Files:**
- Modify: `site/server/real-analysis-server.mjs`
- Modify: `site/server/smb-platform-server.test.mjs`
- Modify: `site/server/real-analysis-server.test.mjs`

**Step 1: Write failing API tests**

Test:

- `GET /api/assets/graph`, `GET /api/assets/:id/neighborhood`, and graph-enhanced `/api/search` require viewer;
- depth, limit, filters, and `includeProposed` are clamped server-side;
- viewer receives no confidential nodes, edges, counts, or explanation paths;
- editor can create/confirm/reject relationships;
- viewer mutations return 403;
- cross-origin mutations remain blocked;
- invalid IDs, types, states, JSON sizes, and missing endpoints return minimal safe errors;
- every mutation appends an audit event without source text.

**Step 2: Run tests and verify failure**

Run: `node --test server/smb-platform-server.test.mjs server/real-analysis-server.test.mjs`

Expected: FAIL with route not found or method not allowed.

**Step 3: Implement routes and validation**

Add strict path patterns and bounded query parsers. Pass `session.user.role` to store/registry queries. Use `requireRole("editor")` for mutations and append only relationship IDs, endpoint IDs, type, and state to the audit chain.

**Step 4: Run focused and complete server tests**

Run: `node --test server/*.test.mjs`

Expected: PASS.

### Task 4: Pure graph layout and rendering contracts

**Files:**
- Create: `site/src/scripts/asset-graph.mjs`
- Create: `site/src/scripts/asset-graph.test.mjs`
- Modify: `site/package.json`

**Step 1: Write failing pure-function tests**

Test deterministic layout, empty/single-node/cycle graphs, maximum node bounds, safe label truncation, accessible edge descriptions, role-safe payload assumptions, type filters, neighbor focus, and reduced-motion mode.

**Step 2: Run test and verify failure**

Run: `node --test src/scripts/asset-graph.test.mjs`

Expected: FAIL because module is missing.

**Step 3: Implement native layout utilities**

Provide exported pure functions for graph normalization, deterministic concentric/cluster positions, filtered subgraphs, SVG path generation, and relationship labels. Do not use `innerHTML`; callers build DOM with `createElementNS` and `textContent`.

**Step 4: Run unit tests**

Expected: PASS and package `npm test` includes the new test.

### Task 5: Asset panorama interface

**Files:**
- Modify: `site/src/pages/index.astro`
- Modify: `site/src/styles/ip-platform.css`
- Modify: `site/src/scripts/ip-platform.mjs`
- Modify: `site/src/scripts/ip-platform-contract.test.mjs`
- Modify: `site/src/scripts/enterprise-contract.test.mjs`

**Step 1: Write failing UI contract tests**

Assert the asset view contains list/graph tabs, graph status, filters, SVG viewport, relationship list, relationship drawer, loading/empty/error/truncated states, keyboard instructions, and no inline unsafe HTML.

**Step 2: Run contract tests and verify failure**

Run: `node --test src/scripts/ip-platform-contract.test.mjs src/scripts/enterprise-contract.test.mjs`

Expected: FAIL on missing panorama controls.

**Step 3: Implement the refined industrial knowledge-map UI**

Keep the list as default. Add a full-height “资产全景” workspace with a dark blueprint-like graph field inside the existing intelifar light shell, restrained violet/cyan accents, compact filters, confidence/evidence legend, selected-node inspector, and equivalent table.

Implement:

- lazy graph API loading when the tab opens;
- loading, retry, empty, and truncated states;
- node/edge selection, focus neighborhood, search focus, zoom buttons, and reset;
- Tab navigation for nodes, Enter selection, Escape reset, reduced motion;
- confirmed solid edges and proposed dashed edges;
- mobile fallback to the relationship list;
- asset drawer, Wiki, and evidence actions from graph selections.

Use native SVG and text-safe DOM construction; do not add a graph dependency.

**Step 4: Run UI and full unit tests**

Run: `npm test`

Expected: all tests pass.

### Task 6: Browser E2E, security, performance, screenshots, and documentation

**Files:**
- Create: `site/e2e/ip-asset-graph.e2e.mjs`
- Modify: `site/package.json`
- Modify: `docs/INTELIFAR-USER-GUIDE.zh-CN.md`
- Create: `artifacts/ip-asset-graph-report.md`
- Create: `artifacts/ip-asset-graph-review/*.png`

**Step 1: Add end-to-end fixtures and tests**

The E2E must create or seed at least five assets with confirmed, proposed, cyclic, conflict, and confidential relationships. Verify owner and viewer sessions separately.

Test:

- list/graph switching and API data rendering;
- filters, search focus, node and edge selection;
- evidence and Wiki navigation;
- editor confirmation/rejection and audit appearance;
- viewer confidential-node non-disclosure;
- keyboard traversal, reduced motion, mobile relationship fallback;
- empty, loading, error, and truncated states.

Capture desktop panorama, relationship evidence, viewer boundary, and mobile path screenshots.

**Step 2: Run all verification**

Run:

```powershell
npm test
npm run build
npm run test:e2e
npm run test:e2e:smb
npm run test:e2e:operations
npm run test:e2e:collaboration
npm run test:e2e:modules
npm run test:e2e:graph
npm run security:scan
npm audit --audit-level=high
git diff --check
```

Expected: all pass, build succeeds, audit reports zero high/critical findings, no credentials found.

**Step 3: Performance and security probes**

Generate 10,000 nodes and 100,000 relationships in a disposable database. Assert common text + one-hop search p95 under 400 ms and bounded two-hop graph p95 under 500 ms on the current machine. Verify no query can cross workspace or reveal confidential nodes to viewer.

**Step 4: Update user documentation and report**

Add task-oriented Chinese instructions with real screenshots and no internal implementation jargon. Record exact test counts, performance measurements, known limits, and screenshot paths in the artifact report.

**Step 5: Final commit and handoff**

Stage only intended files, run `git diff --cached --check`, commit the completed feature, and report the branch/commit without pushing unless requested.
