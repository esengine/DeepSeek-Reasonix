import assert from "node:assert/strict";
import { join } from "node:path";
import { test } from "node:test";
import { packagedSmokeEnv } from "./smoke-env.mjs";

test("packaged startup uses an isolated home without inherited development or service overrides", () => {
  const parent = {
    PATH: "system-path", DISPLAY: ":99", HOME: "system-home",
    REASONIX_DEV: "1", Reasonix_Dev: "1", REASONIX_CHANNEL: "dev", REASONIX_COMMIT: "old",
    REASONIX_DESKTOP_SERVICE: "old-service", REASONIX_ELECTRON_DEV_URL: "http://localhost:5173",
    REASONIX_HOME: "real-user-data", REASONIX_STATE_HOME: "real-state", REASONIX_CACHE_HOME: "real-cache",
    NODE_OPTIONS: "--require development-hook", ELECTRON_RUN_AS_NODE: "1",
  };
  assert.deepEqual(packagedSmokeEnv(parent, "fixture"), {
    PATH: "system-path", DISPLAY: ":99", HOME: "system-home",
    REASONIX_HOME: "fixture", REASONIX_STATE_HOME: "fixture", REASONIX_CACHE_HOME: join("fixture", "cache"),
  });
  assert.equal(parent.REASONIX_DEV, "1", "the caller environment is not mutated");
});
