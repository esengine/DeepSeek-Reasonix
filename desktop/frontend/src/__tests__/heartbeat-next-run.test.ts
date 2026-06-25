// Run: tsx src/__tests__/heartbeat-next-run.test.ts

import { heartbeatBuildCycleInterval, heartbeatNextRunAt } from "../custom/features/heartbeat/HeartbeatPanel";

let passed = 0;
let failed = 0;

function eq(a: unknown, b: unknown, label: string) {
  if (a === b) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(b)}, got ${JSON.stringify(a)}\n`);
    failed += 1;
  }
}

function localMs(year: number, month: number, day: number, hour: number, minute: number): number {
  return new Date(year, month - 1, day, hour, minute, 0, 0).getTime();
}

console.log("\nheartbeat next run");

eq(
  heartbeatNextRunAt(
    { interval: "30m", lastRunAt: localMs(2026, 6, 18, 16, 30) },
    localMs(2026, 6, 18, 17, 20),
  ),
  localMs(2026, 6, 18, 17, 0),
  "plain interval stays due after elapsed",
);

eq(
  heartbeatNextRunAt(
    { interval: "30m", lastRunAt: localMs(2026, 6, 18, 16, 30), timeWindowStart: "09:00", timeWindowEnd: "17:00" },
    localMs(2026, 6, 18, 17, 20),
  ),
  localMs(2026, 6, 19, 9, 0),
  "time window defers elapsed interval to next opening",
);

eq(
  heartbeatNextRunAt(
    { interval: "30m", lastRunAt: localMs(2026, 6, 18, 16, 0), timeWindowStart: "09:00", timeWindowEnd: "17:00" },
    localMs(2026, 6, 18, 16, 10),
  ),
  localMs(2026, 6, 18, 16, 30),
  "time window keeps next run inside the open window",
);

eq(
  heartbeatNextRunAt(
    { interval: "30m", lastRunAt: localMs(2026, 6, 18, 21, 50), timeWindowStart: "22:00", timeWindowEnd: "06:00" },
    localMs(2026, 6, 18, 22, 10),
  ),
  localMs(2026, 6, 18, 22, 20),
  "cross-midnight window keeps due time in the open window",
);

eq(
  heartbeatNextRunAt(
    { interval: "30m", lastRunAt: localMs(2026, 6, 18, 11, 30), timeWindowStart: "22:00", timeWindowEnd: "06:00" },
    localMs(2026, 6, 18, 12, 10),
  ),
  localMs(2026, 6, 18, 22, 0),
  "cross-midnight window waits for today's opening from midday",
);

eq(
  heartbeatNextRunAt(
    { interval: "24h|daily@20:00", lastRunAt: localMs(2026, 6, 18, 20, 0), timeWindowStart: "09:00", timeWindowEnd: "17:00" },
    localMs(2026, 6, 19, 19, 0),
  ),
  localMs(2026, 6, 19, 20, 0),
  "cycle next run ignores stale interval time windows",
);

eq(
  heartbeatBuildCycleInterval("daily", [], "09:00"),
  "24h|weekly:mon@09:00",
  "empty daily day selection does not save as every day",
);

eq(
  heartbeatBuildCycleInterval("weekly", [], "09:00"),
  "168h|weekly:mon@09:00",
  "weekly default uses one weekday",
);

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
