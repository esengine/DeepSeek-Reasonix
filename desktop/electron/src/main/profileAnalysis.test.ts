import assert from "node:assert/strict";
import { test } from "node:test";
import { analyseProfile } from "./profileAnalysis.js";
import type { CpuProfile } from "./rendererDiagnostics.js";

test("profiles produce bounded app-only self-time summaries without source paths", () => {
  const profile: CpuProfile = {
    startTime: 0, endTime: 30_000,
    nodes: [
      { id: 1, callFrame: { functionName: "render", url: "reasonix://app/assets/main-abc.js", lineNumber: 12 } },
      { id: 2, callFrame: { functionName: "secret", url: "file:///private/user.js", lineNumber: 0 } },
      { id: 3, callFrame: { functionName: "remote", url: "https://example.com/app.js", lineNumber: 0 } },
    ], samples: [1, 2, 1, 3], timeDeltas: [5000, 10000, 5000, 10000],
  };
  assert.deepEqual(analyseProfile(profile), [{ label: "render (main-abc.js:13)", selfMs: 10, samples: 2 }]);
  assert.throws(() => analyseProfile({ ...profile, samples: Array(100_001).fill(1) }), /budget/);
});
