// Run: tsx src/__tests__/heartbeat-next-run.test.ts

import { changeHeartbeatFrequency, cronToInterval, heartbeatBuildCycleInterval, heartbeatNextRunAt, intervalToCron, mergeEngineRunState, nextCycleRunAt, prepareTasksByNextRun } from "../custom/features/heartbeat/HeartbeatPanel";

let passed = 0;
let failed = 0;

function eq(a: unknown, b: unknown, label: string) {
  if (typeof a === "object" && a !== null && typeof b === "object" && b !== null && !Array.isArray(a) && !Array.isArray(b)) {
    const ak = JSON.stringify(a, Object.keys(a as object).sort());
    const bk = JSON.stringify(b, Object.keys(b as object).sort());
    if (ak === bk) {
      process.stdout.write(`  PASS  ${label}\n`);
      passed += 1;
    } else {
      process.stdout.write(`  FAIL  ${label}: expected ${bk}, got ${ak}\n`);
      failed += 1;
    }
    return;
  }
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

eq(
  heartbeatNextRunAt(
    { interval: "0 9 * * 1-5", lastRunAt: 0 },
    localMs(2026, 6, 15, 10, 0),
  ),
  localMs(2026, 6, 16, 9, 0),
  "cron weekday schedule computes next business-day run",
);

eq(
  heartbeatNextRunAt(
    { interval: "*/15 * * * *", lastRunAt: 0 },
    localMs(2026, 6, 18, 10, 7),
  ),
  localMs(2026, 6, 18, 10, 15),
  "cron every-15-minutes computes next slot",
);

eq(
  heartbeatNextRunAt(
    { interval: "0 9 * * 1-5", lastRunAt: localMs(2026, 6, 15, 9, 0) },
    localMs(2026, 6, 15, 9, 0),
  ),
  localMs(2026, 6, 16, 9, 0),
  "cron next run ignores lastRunAt and uses wall clock",
);

console.log("\nengine-run merge (SivanCola review: trigger must not clobber draft)");

// 立即运行返回后：磁盘旧快照只贡献 runHistory/topicId/lastRunAt，等待期间的
// 草稿编辑（title/prompt/interval）必须保留
eq(
  mergeEngineRunState(
    { id: "t1", title: "草稿新标题", prompt: "草稿新 prompt", interval: "30m", enabled: true, scope: "global", lastRunAt: 100 },
    { id: "t1", title: "旧标题", prompt: "旧 prompt", interval: "1h", enabled: true, scope: "global", runHistory: [{ at: 200, topicId: "topic-2" }], topicId: "topic-2", lastRunAt: 200 },
  ),
  { id: "t1", title: "草稿新标题", prompt: "草稿新 prompt", interval: "30m", enabled: true, scope: "global", runHistory: [{ at: 200, topicId: "topic-2" }], topicId: "topic-2", lastRunAt: 200 },
  "trigger merge keeps draft title while adopting engine run fields",
);
// 没有 runHistory 的旧磁盘快照不得清掉草稿中已有 runHistory
eq(
  mergeEngineRunState(
    { id: "t1", title: "草稿标题", prompt: "p", interval: "30m", enabled: true, runHistory: [{ at: 50, topicId: "t" }], lastRunAt: 100 },
    { id: "t1", title: "旧标题", prompt: "旧", interval: "1h", enabled: true, lastRunAt: 110 },
  ),
  { id: "t1", title: "草稿标题", prompt: "p", interval: "30m", enabled: true, runHistory: [{ at: 50, topicId: "t" }], lastRunAt: 110 },
  "trigger merge adopts fresh lastRunAt without dropping draft runHistory",
);

// ── 周期转 cron / 周期 next-run 语义（SivanCola review 回归） ──

console.log("cycle → cron conversion guards");

// biweekly 无法无损转 cron → null
eq(intervalToCron("336h|biweekly:mon@09:00"), null, "biweekly refuses cron conversion");

// 秒级任务无法转 cron → null
eq(intervalToCron("30s"), null, "seconds refuse cron conversion");

// 跨午夜窗口 22:00–06:00 无法表达 → null
eq(intervalToCron("1h", "22:00", "06:00"), null, "cross-midnight window refuses conversion");

// 时间窗口结束时刻 exclusive：09:00–17:00 → 小时 9-16（不含 17 点）
eq(intervalToCron("1h", "09:00", "17:00"), "0 9-16 * * *", "window end hour is exclusive");

// SivanCola review (2026-08-14): 2h + 窗口不能转成 0 9-16（那会把每2小时
// 变成每小时，频率放大）→ 拒绝转换
eq(intervalToCron("2h", "09:00", "17:00"), null, "2h window refuses conversion (frequency would grow)");

// 非整点窗口 09:30-17:30 会被截断成整点 → 拒绝转换
eq(intervalToCron("1h", "09:30", "17:30"), null, "non-top-of-hour window refuses conversion");
eq(intervalToCron("30m", "09:30", "17:30"), null, "non-top-of-hour window refuses m-interval conversion");

// 周期任务带残留窗口：周期任务有自己的时刻（@12:00），引擎忽略窗口，
// 转 cron 必须保持原时刻，不能折叠成窗口小时范围（否则每天变成每小时）
eq(intervalToCron("24h|daily@12:00", "09:00", "17:00"), "0 12 * * *", "daily with stale window keeps its own clock");

// daily 周期正常转换（无窗口）
eq(intervalToCron("24h|daily@09:00"), "0 9 * * *", "daily converts to cron");

// weekly 周期正常转换
eq(intervalToCron("168h|weekly:mon,wed@09:00"), "0 9 * * 1,3", "weekly converts to cron");

// monthly 周期正常转换
eq(intervalToCron("720h|monthly:15@09:00"), "0 9 15 * *", "monthly converts to cron");

console.log("cycle next-run (schedule semantics, not cron)");

// daily: 下个 22:00
eq(
  nextCycleRunAt("24h|daily@22:00", localMs(2026, 8, 11, 10, 0)),
  localMs(2026, 8, 11, 22, 0),
  "daily next run is today's 22:00",
);

// daily: 已过 22:00 → 明天 22:00
eq(
  nextCycleRunAt("24h|daily@22:00", localMs(2026, 8, 11, 23, 0)),
  localMs(2026, 8, 12, 22, 0),
  "daily next run rolls to tomorrow after 22:00",
);

// weekly: 下个周五 16:00（2026-08-14 是周五）
eq(
  nextCycleRunAt("168h|weekly:fri@16:00", localMs(2026, 8, 11, 10, 0)),
  localMs(2026, 8, 14, 16, 0),
  "weekly next run is next Friday 16:00",
);

// monthly: 下个 15 号 09:00
eq(
  nextCycleRunAt("720h|monthly:15@09:00", localMs(2026, 8, 11, 10, 0)),
  localMs(2026, 8, 15, 9, 0),
  "monthly next run is 15th 09:00",
);

// 月末 clamp：monthly:31 在 2026-04-01 之后的下一次是 2026-04-30（4 月只有
// 30 天，后端 monthlyCandidate 将 day=31 clamp 到当月末）——前端镜像后端
// 语义，不能跳到 5 月 31 号。
eq(
  nextCycleRunAt("720h|monthly:31@09:00", localMs(2026, 4, 1, 10, 0)),
  localMs(2026, 4, 30, 9, 0),
  "monthly day-31 clamps to April 30 (mirrors backend monthlyCandidate)",
);
// 闰年 2 月：day=29 在非闰年 2027 年 clamp 到 2 月 28 日
eq(
  nextCycleRunAt("720h|monthly:29@09:00", localMs(2027, 2, 1, 10, 0)),
  localMs(2027, 2, 28, 9, 0),
  "monthly day-29 clamps to February 28 in a non-leap year",
);
// 闰年 2028 年 2 月 29 日保持 29 日
eq(
  nextCycleRunAt("720h|monthly:29@09:00", localMs(2028, 2, 1, 10, 0)),
  localMs(2028, 2, 29, 9, 0),
  "monthly day-29 keeps February 29 in a leap year",
);

// yearly: 下个 1-1 09:00（2027-01-01）
eq(
  nextCycleRunAt("8760h|yearly:1-1@09:00", localMs(2026, 8, 11, 10, 0)),
  localMs(2027, 1, 1, 9, 0),
  "yearly next run is next Jan 1st 09:00",
);

// 离线期间已到期：daily 任务上次运行在 2026-08-16 09:00，schedule 基于
// lastRunAt 求下一次 = 2026-08-17 09:00，而当前已到 8/18 10:00 → next 落在
// 过去。taskNextRun 据此显示 dueSoon（当前应执行），而不是跳过到明天。
eq(
  heartbeatNextRunAt(
    { interval: "24h|daily@09:00", lastRunAt: localMs(2026, 8, 16, 9, 0) },
    localMs(2026, 8, 18, 10, 0),
  ),
  localMs(2026, 8, 17, 9, 0),
  "daily next run falls in the past when the task is overdue offline",
);

// ── review fixes 回归 ──

console.log("cron dow=7 Sunday alias");

eq(
  heartbeatNextRunAt(
    { interval: "0 9 * * 7", lastRunAt: 0 },
    localMs(2026, 8, 10, 10, 0), // Monday
  ),
  localMs(2026, 8, 16, 9, 0), // next Sunday 09:00
  "dow=7 (Sunday alias) computes the next Sunday run",
);

eq(
  heartbeatNextRunAt(
    { interval: "0 9 * * 0,7", lastRunAt: 0 },
    localMs(2026, 8, 10, 10, 0), // Monday
  ),
  localMs(2026, 8, 16, 9, 0),
  "dow=0,7 both spellings compute the next Sunday run",
);

console.log("biweekly parity mirrors the Go engine (Monday week anchor)");

// 2026-08-06 is a Thursday; engine parity is anchored on the Monday of the
// creation week (2026-08-03). Candidate Monday 2026-08-10 is week 1 away →
// skipped; next fire is Monday 2026-08-17 (week 2).
eq(
  nextCycleRunAt("336h|biweekly:mon@09:00", localMs(2026, 8, 10, 10, 0), localMs(2026, 8, 6, 9, 0)),
  localMs(2026, 8, 17, 9, 0),
  "biweekly with Thursday anchor skips the first Monday (Monday-anchored parity)",
);

// Anchor on a Monday itself: creation week = 2026-08-10; next fire is 2026-08-24.
eq(
  nextCycleRunAt("336h|biweekly:mon@09:00", localMs(2026, 8, 10, 10, 0), localMs(2026, 8, 10, 9, 0)),
  localMs(2026, 8, 24, 9, 0),
  "biweekly with Monday anchor fires every other Monday",
);

console.log("cronToInterval refuses lossy conversions");

eq(cronToInterval("0 9 * * 1"), null, "weekly cron refuses interval conversion");
eq(cronToInterval("0 9 * * *"), null, "fixed-time cron refuses interval conversion");
eq(cronToInterval("0 0 1 * *"), null, "dom-restricted cron refuses interval conversion");
eq(cronToInterval("*/15 * * * *"), "15m", "every-N-minutes cron converts");
eq(cronToInterval("0 */2 * * *"), "2h", "every-N-hours cron converts");
eq(cronToInterval("0 * * * *"), null, "top-of-every-hour is not a simple interval");

console.log("frontend isCronExpr field bounds (dom/month 1-based, step>0, lo<=hi)");

eq(
  heartbeatNextRunAt(
    { interval: "0 0 0 * *", lastRunAt: 0 }, // dom=0 — "midnight every day" typo
    localMs(2026, 8, 10, 10, 0),
  ),
  null,
  "dom=0 is rejected: no next run is computed for an invalid expression",
);

// SivanCola review: 永不执行的表达式必须被拒绝
eq(
  heartbeatNextRunAt(
    { interval: "*/0 * * * *", lastRunAt: 0 }, // step 0: value % 0 is never 0
    localMs(2026, 8, 10, 10, 0),
  ),
  null,
  "zero step */0 is rejected (never fires)",
);
eq(
  heartbeatNextRunAt(
    { interval: "0 0 5-1 * *", lastRunAt: 0 }, // descending range never matches
    localMs(2026, 8, 10, 10, 0),
  ),
  null,
  "descending range 5-1 is rejected (never matches)",
);
eq(
  heartbeatNextRunAt(
    { interval: "0 0 1 * 8", lastRunAt: 0 }, // dow 8 out of range
    localMs(2026, 8, 10, 10, 0),
  ),
  null,
  "dow=8 is rejected (out of range)",
);

eq(
  heartbeatNextRunAt(
    { interval: "0 0 1 0 *", lastRunAt: 0 }, // month=0
    localMs(2026, 8, 10, 10, 0),
  ),
  null,
  "month=0 is rejected",
);

eq(
  heartbeatNextRunAt(
    { interval: "0 0 1 * *", lastRunAt: 0 },
    localMs(2026, 8, 10, 10, 0),
  ),
  localMs(2026, 9, 1, 0, 0),
  "valid dom expression still computes a next run",
);

console.log("cron steps use field minima and retain long-horizon matches");

eq(
  heartbeatNextRunAt({ interval: "0 0 * */2 *", lastRunAt: 0 }, localMs(2025, 12, 31, 12, 0)),
  localMs(2026, 1, 1, 0, 0),
  "wildcard month step starts from January",
);
eq(
  heartbeatNextRunAt({ interval: "0 0 * */2 *", lastRunAt: 0 }, localMs(2026, 1, 31, 0, 0)),
  localMs(2026, 3, 1, 0, 0),
  "wildcard month step skips February",
);
eq(
  heartbeatNextRunAt({ interval: "1/2 * * * *", lastRunAt: 0 }, localMs(2026, 1, 1, 0, 1)),
  localMs(2026, 1, 1, 0, 3),
  "single-value step continues from its explicit start",
);
eq(
  heartbeatNextRunAt({ interval: "0 0 29 2 *", lastRunAt: 0 }, localMs(2028, 3, 1, 0, 0)),
  localMs(2032, 2, 29, 0, 0),
  "leap-day cron searches beyond one year",
);

console.log("task list computes each next run once");

const listNow = localMs(2026, 8, 10, 10, 0);
const listTasks = [
  { id: "late", title: "late", prompt: "", interval: "0 0 29 2 *", enabled: true, lastRunAt: 1 },
  { id: "new", title: "new", prompt: "", interval: "0 0 31 2 *", enabled: true },
  { id: "paused", title: "paused", prompt: "", interval: "0 0 31 2 *", enabled: false, lastRunAt: 1 },
];
let nextRunCalls = 0;
const preparedTasks = prepareTasksByNextRun(listTasks, listNow, (task) => {
  nextRunCalls += 1;
  return task.id === "late" ? listNow + 60000 : null;
});
eq(nextRunCalls, listTasks.length, "list preparation resolves each task exactly once");
eq(preparedTasks.map(({ task }) => task.id).join(","), "new,late,paused", "list preparation preserves next-run ordering");
eq(preparedTasks[1].nextRunAt, listNow + 60000, "list rows reuse the precomputed next-run value");

console.log("frequency conversion keeps the selected editor on lossy paths");

eq(changeHeartbeatFrequency({ id: "t", title: "t", prompt: "p", enabled: true, interval: "0 9 * * 1" }, "interval"), null, "weekly cron cannot switch to interval");
eq(changeHeartbeatFrequency({ id: "t", title: "t", prompt: "p", enabled: true, interval: "30s" }, "cron"), null, "seconds cannot switch to cron");
eq(changeHeartbeatFrequency({ id: "t", title: "t", prompt: "p", enabled: true, interval: "336h|biweekly:mon@09:00" }, "cron"), null, "biweekly cannot switch to cron");
eq(changeHeartbeatFrequency({ id: "t", title: "t", prompt: "p", enabled: true, interval: "1h", timeWindowStart: "22:00", timeWindowEnd: "06:00" }, "cron"), null, "cross-midnight interval cannot switch to cron");

console.log("biweekly parity uses civil dates across DST");

const originalTZ = process.env.TZ;
process.env.TZ = "America/New_York";
eq(
  nextCycleRunAt("336h|biweekly:mon@09:00", localMs(2026, 3, 9, 10, 0), localMs(2026, 3, 2, 9, 0)),
  localMs(2026, 3, 16, 9, 0),
  "spring-forward keeps the even-week Monday",
);
eq(
  nextCycleRunAt("336h|biweekly:mon@09:00", localMs(2026, 11, 2, 10, 0), localMs(2026, 10, 26, 9, 0)),
  localMs(2026, 11, 9, 9, 0),
  "fall-back keeps the even-week Monday",
);
if (originalTZ === undefined) delete process.env.TZ;
else process.env.TZ = originalTZ;

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
