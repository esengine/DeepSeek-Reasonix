import assert from "node:assert/strict";
import { sessionCatalogNotice } from "../lib/sessionCatalogPresentation";
import type { SessionCatalogStatus } from "../lib/sessionCatalogTypes";

const status = (overrides: Partial<SessionCatalogStatus> = {}): SessionCatalogStatus => ({
  state: "ready",
  revision: 1,
  indexed: 10,
  total: 10,
  repairPending: 0,
  canRebuild: false,
  ...overrides,
});

assert.deepEqual(
  sessionCatalogNotice(status({ state: "opening", indexed: 0, total: 0 })),
  "working",
  "opening without a known total uses the generic working label",
);
assert.deepEqual(
  sessionCatalogNotice(status({ state: "rebuilding", indexed: 7, total: 10, canRebuild: true })),
  "working",
  "rebuilding preserves known progress and never offers another rebuild",
);
assert.deepEqual(
  sessionCatalogNotice(status({ repairPending: 1, canRebuild: true })),
  "working",
  "automatic repair never exposes the manual rebuild action",
);
assert.equal(sessionCatalogNotice(status()), null, "healthy ready catalogs render no notice");
assert.deepEqual(
  sessionCatalogNotice(status({ state: "degraded", canRebuild: true })),
  "rebuild",
  "a degraded catalog can expose the explicit repair action",
);
assert.deepEqual(
  sessionCatalogNotice(status({ lastError: "private backend detail", canRebuild: true })),
  "rebuild",
  "backend errors select a generic failed presentation without exposing the detail",
);
assert.deepEqual(
  sessionCatalogNotice(status({ state: "degraded", canRebuild: undefined })),
  "failed",
  "older backends without canRebuild fail closed",
);

console.log("session catalog presentation tests passed");
