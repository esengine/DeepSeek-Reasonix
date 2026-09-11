// Run: pnpm exec tsx src/__tests__/provider-failure-meta.test.ts
import assert from "node:assert/strict";

import { initialState, reducer } from "../lib/useController";

const started = reducer(initialState, { type: "event", e: { kind: "turn_started" } });
const failed = reducer(started, {
  type: "event",
  e: {
    kind: "turn_done",
    err: "Deepseek2 · Chat Completions: Request endpoint not found (HTTP 404).",
    detail: "Connection ID: deepseek-anthropic\nRequest path: /anthropic/v1/chat/completions",
    diagnostic: { kind: "request", status: 404, providerId: "deepseek-anthropic", providerDisplayName: "Deepseek2", protocol: "openai", requestPath: "/anthropic/v1/chat/completions" },
  },
});
const notice = failed.items.find((item) => item.kind === "notice" && item.level === "warn");

assert.equal(
  notice?.kind === "notice" ? notice.text : "",
  "Deepseek2 · Chat Completions: Request endpoint not found (HTTP 404).",
  "provider display identity stays in the primary live error",
);
assert.equal(
  notice?.kind === "notice" ? notice.detail : "",
  "Connection ID: deepseek-anthropic\nRequest path: /anthropic/v1/chat/completions",
  "stable provider id and sanitized path stay in live diagnostic details",
);
