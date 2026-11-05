import { expect, it } from "vitest";
import { usageCsv } from "./usagecsv";
import type { UsageReport } from "../port/usage";

it("writes one row a day and one money column per currency, never adding two currencies", () => {
  const report = {
    daily: [
      { day: "2026-11-01", total: 10, cacheHit: 6, cacheMiss: 3, requests: 2, turns: 1, cost: [{ amount: "1.20", currency: "CNY" }] },
      { day: "2026-11-02", total: 5, cacheHit: 1, cacheMiss: 2, requests: 1, turns: 1, cost: [{ amount: "0.10", currency: "USD" }] },
    ],
  } as unknown as UsageReport;
  expect(usageCsv(report).trim().split("\n")).toEqual([
    "day,provider_total_tokens,input_cache_hit,input_cache_miss,requests,cost_CNY,cost_USD",
    "2026-11-01,10,6,3,2,1.20,",
    "2026-11-02,5,1,2,1,,0.10",
  ]);
});
