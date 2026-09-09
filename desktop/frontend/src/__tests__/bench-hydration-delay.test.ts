import assert from "node:assert/strict";
import { benchHydrationDelay } from "../lib/bridgeBenchFixtures";

const soak = "?mock=bench&bench=1&app-lifecycle-probe=1&bench-hydration=soak";
assert.equal(benchHydrationDelay(soak), 0);
for (const parameter of ["mock", "bench", "app-lifecycle-probe", "bench-hydration"]) {
  const params = new URLSearchParams(soak);
  params.delete(parameter);
  assert.equal(benchHydrationDelay(params.toString()), 1_500, parameter);
}
assert.equal(benchHydrationDelay("?mock=bench&bench=1"), 1_500);
assert.equal(benchHydrationDelay("?mock=bench&bench=1&bench-hydration=unknown"), 1_500);
console.log("PASS only the explicit memory soak omits synthetic latency; browser/native fixtures keep delayed hydration");
