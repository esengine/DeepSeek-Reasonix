// Run: tsx src/__tests__/guardian-events.test.ts
//
// Coverage for formatGuardianAssessmentNotice (src/lib/guardianEvents.ts):
// joins "Guardian <outcome>" with the tool, subject, risk level, authorization
// and rationale using " · ", skipping any field that is missing or empty.

import { formatGuardianAssessmentNotice } from "../lib/guardianEvents";
import type { WireGuardian } from "../lib/types";

let passed = 0;
let failed = 0;

function eq(a: unknown, b: unknown, label: string) {
  if (JSON.stringify(a) === JSON.stringify(b)) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(b)}, got ${JSON.stringify(a)}\n`);
    failed += 1;
  }
}

function guardian(overrides: Partial<WireGuardian>): WireGuardian {
  return {
    id: "g1",
    tool: "bash",
    subject: "ls -la",
    outcome: "approved",
    ...overrides,
  };
}

console.log("\nguardian-events assessment notice");

// ── all fields present ──────────────────────────────────────────────
eq(
  formatGuardianAssessmentNotice(
    guardian({ risk_level: "high", user_authorization: "user", rationale: "explicit command" }),
  ),
  "Guardian approved · bash · ls -la · risk=high · authorization=user · explicit command",
  "all fields are joined with ' · ' in order",
);

// ── missing / empty optional fields are skipped ─────────────────────
eq(
  formatGuardianAssessmentNotice(guardian({})),
  "Guardian approved · bash · ls -la",
  "missing optional fields are omitted",
);
eq(
  formatGuardianAssessmentNotice(guardian({ tool: "", subject: "" })),
  "Guardian approved",
  "empty tool and subject are omitted",
);
eq(
  formatGuardianAssessmentNotice(guardian({ risk_level: "", user_authorization: "", rationale: "" })),
  "Guardian approved · bash · ls -la",
  "empty risk/authorization/rationale are omitted",
);
eq(
  formatGuardianAssessmentNotice(guardian({ subject: "" })),
  "Guardian approved · bash",
  "single populated field still joins correctly",
);

// ── outcome fallback ────────────────────────────────────────────────
eq(
  formatGuardianAssessmentNotice(guardian({ outcome: "" })),
  "Guardian unknown · bash · ls -la",
  "empty outcome falls back to 'unknown'",
);
eq(
  formatGuardianAssessmentNotice({ id: "g1" } as WireGuardian),
  "Guardian unknown",
  "missing outcome field falls back to 'unknown'",
);

console.log(`\n${passed} passed, ${failed} failed\n`);
process.exit(failed === 0 ? 0 : 1);
