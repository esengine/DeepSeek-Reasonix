import test from "node:test";
import assert from "node:assert/strict";
import { memoryAffected } from "./app-memory-paths.mjs";
test("all frontend consumers, configuration and tests trigger the soak", () => {
  for (const path of ["desktop/frontend/src/App.tsx", "desktop/frontend/src/lib/types.ts", "desktop/frontend/pnpm-lock.yaml", "desktop/frontend/bench/fixture.json", ".github/workflows/app-memory.yml"])
    assert.equal(memoryAffected([path]), true, path);
});
test("known independent backend and documentation changes skip only this mock frontend soak", () => {
  assert.equal(memoryAffected(["internal/control/turn.go", "desktop/app.go", "sdk/types.ts", "docs/guide.md", "README.md"]), false);
});
test("unknown paths fail closed and cannot be hidden by a documentation change", () => {
  for (const path of [".npmrc", "shared/new-loader.js", ".github/workflows/ci.yml", "docs/build.js", "desktop/build/config.json"])
    assert.equal(memoryAffected(["README.md", path]), true, path);
});
