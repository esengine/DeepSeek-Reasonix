import assert from "node:assert/strict";
import {
  moveProvider, normalizeProviderOrder, orderByProvider, PROVIDER_ORDER_STORAGE_KEY,
  readProviderOrder, writeProviderOrder,
} from "../lib/providerOrder";

const values = new Map<string, string>();
const storage = {
  getItem: (key: string) => values.get(key) ?? null,
  setItem: (key: string, value: string) => { values.set(key, value); },
};

assert.deepEqual(normalizeProviderOrder([" older ", "newer", "older", "", null, 4]), ["older", "newer"]);
values.set(PROVIDER_ORDER_STORAGE_KEY, "not json");
assert.deepEqual(readProviderOrder(storage), [], "bad storage must leave catalog order intact");
values.set(PROVIDER_ORDER_STORAGE_KEY, JSON.stringify({ order: ["newer"] }));
assert.deepEqual(readProviderOrder(storage), [], "unexpected storage shape must be ignored");

const original = [
  { provider: "older", model: "one" },
  { provider: "older", model: "two" },
  { provider: "newer", model: "three" },
  { provider: "later", model: "four" },
];
const next = moveProvider([], ["older", "newer"], "newer", -1);
assert.deepEqual(next, ["newer", "older"]);
assert.equal(writeProviderOrder(next!, storage), true);
assert.deepEqual(readProviderOrder(storage), ["newer", "older"], "order survives a new reader");
assert.deepEqual(orderByProvider(original, (item) => item.provider, readProviderOrder(storage)), [
  original[2], original[0], original[1], original[3],
], "models within one provider and newly added providers retain catalog order");
assert.equal(moveProvider(next!, ["older", "newer"], "newer", -1), null, "top item cannot move further up");

const withHidden = moveProvider(["older", "hidden", "newer"], ["older", "newer"], "newer", -1);
assert.deepEqual(withHidden, ["newer", "hidden", "older"], "another workspace's preference remains stored");
assert.deepEqual(orderByProvider(["older", "newer", "hidden"], (id) => id, withHidden!), ["newer", "hidden", "older"]);

assert.equal(writeProviderOrder(["newer"], { setItem: () => { throw new Error("storage denied"); } }), false);
console.log("provider order persistence and catalog ordering: PASS");
