import { describe, expect, it } from "vitest";
import { type BalanceInfo, pickPrimaryBalance } from "../src/client.js";

describe("pickPrimaryBalance — issue #724", () => {
  it("returns null for an empty list", () => {
    expect(pickPrimaryBalance([])).toBeNull();
  });

  it("picks the only entry when there is just one wallet", () => {
    const only: BalanceInfo = { currency: "CNY", total_balance: "0.47" };
    expect(pickPrimaryBalance([only])).toBe(only);
  });

  it("picks the largest top-up wallet when USD + CNY both exist (issue #724 case)", () => {
    const usd: BalanceInfo = { currency: "USD", total_balance: "4.99", topped_up_balance: "4.99" };
    const cny: BalanceInfo = { currency: "CNY", total_balance: "0.47", topped_up_balance: "0.47" };
    expect(pickPrimaryBalance([cny, usd])).toBe(usd);
    expect(pickPrimaryBalance([usd, cny])).toBe(usd);
  });

  it("keeps CNY when USD has zero balance", () => {
    const usd: BalanceInfo = { currency: "USD", total_balance: "0" };
    const cny: BalanceInfo = { currency: "CNY", total_balance: "50" };
    expect(pickPrimaryBalance([usd, cny])).toBe(cny);
  });

  it("breaks ties by keeping the first entry (stable)", () => {
    const a: BalanceInfo = { currency: "USD", total_balance: "5" };
    const b: BalanceInfo = { currency: "CNY", total_balance: "5" };
    expect(pickPrimaryBalance([a, b])).toBe(a);
  });
});
