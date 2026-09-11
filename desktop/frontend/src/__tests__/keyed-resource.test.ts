import {
  beginKeyedResourceRequest,
  emptyKeyedResource,
  rejectKeyedResourceRequest,
  resolveKeyedResourceRequest,
} from "../lib/keyedResource";

let passed = 0;
let failed = 0;
function check(value: boolean, label: string) {
  if (value) { passed += 1; process.stdout.write(`  PASS  ${label}\n`); }
  else { failed += 1; process.stdout.write(`  FAIL  ${label}\n`); }
}

console.log("\nkeyed stale-while-revalidate resource");
let state = beginKeyedResourceRequest(emptyKeyedResource<string>(), "a", 1, 1);
state = resolveKeyedResourceRequest(state, "a", 1, "first", 1);
state = beginKeyedResourceRequest(state, "a", 2, 2);
check(state.data === "first" && state.status === "refreshing", "same-key refresh keeps painted data");
const stale = resolveKeyedResourceRequest(state, "a", 1, "stale", 1);
check(stale === state, "stale completion is ignored by sequence");
state = rejectKeyedResourceRequest(state, "a", 2, "offline");
check(state.data === "first" && state.status === "error", "refresh error preserves painted data");
state = beginKeyedResourceRequest(state, "b", 3, 3);
check(state.data === null && state.status === "refreshing", "new key starts without unrelated data");
check(failed === 0, `${passed} resource assertions passed`);
if (failed > 0) process.exit(1);
