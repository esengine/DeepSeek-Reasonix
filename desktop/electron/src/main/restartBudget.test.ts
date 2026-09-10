import assert from "node:assert/strict";
import { test } from "node:test";
import { RestartBudget } from "./restartBudget.js";

test("three automatic restarts per five minutes, then the budget is exhausted", () => {
  const budget = new RestartBudget();
  const start = 1_000_000;
  assert.equal(budget.allow(start), true);
  assert.equal(budget.allow(start + 1_000), true);
  assert.equal(budget.allow(start + 2_000), true);
  assert.equal(budget.allow(start + 3_000), false);
  assert.equal(budget.allow(start + 4 * 60_000), false, "still inside the window");
  assert.equal(budget.allow(start + 5 * 60_000 + 1), true, "the oldest restart aged out");
  assert.equal(budget.allow(start + 5 * 60_000 + 2), false);
});
