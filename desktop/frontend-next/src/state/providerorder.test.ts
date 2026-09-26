// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { moveAccount, normalizeProviderOrder, orderAccounts, PROVIDER_ORDER_KEY, readProviderOrder, writeProviderOrder } from "./providerorder";

beforeEach(() => {
  const values = new Map<string, string>();
  vi.stubGlobal("localStorage", {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => void values.set(key, value),
  });
});
afterEach(() => vi.unstubAllGlobals());

it("keeps catalog order until the user arranges accounts", () => {
  const accounts = [{ key: "alpha", model: "a" }, { key: "beta", model: "b" }, { key: "alpha", model: "a2" }];
  expect(orderAccounts(accounts, [])).toEqual(accounts);
  expect(orderAccounts(accounts, ["beta", "alpha"]).map((a) => a.model)).toEqual(["b", "a", "a2"]);
  expect(accounts.map((a) => a.model)).toEqual(["a", "b", "a2"]);
});

it("moves visible accounts without forgetting absent ones", () => {
  expect(moveAccount(["alpha", "hidden", "beta"], ["alpha", "beta", "gamma"], "beta", -1))
    .toEqual(["beta", "hidden", "alpha", "gamma"]);
  expect(moveAccount([], ["alpha", "beta"], "alpha", -1)).toBeNull();
  expect(moveAccount([], ["alpha", "beta"], "beta", -1)).toEqual(["beta", "alpha"]);
});

it("stores only account identities and recovers from malformed preferences", () => {
  expect(normalizeProviderOrder([" alpha ", "alpha", "", 2, "beta"])).toEqual(["alpha", "beta"]);
  expect(writeProviderOrder(["beta", "alpha", "beta"])).toBe(true);
  expect(readProviderOrder()).toEqual(["beta", "alpha"]);
  localStorage.setItem(PROVIDER_ORDER_KEY, "{broken");
  expect(readProviderOrder()).toEqual([]);
});
