import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { Cost } from "./Cost";
import { initialState } from "../../state/session";
import type { WalletReading } from "../../port/port";
import { ABSENT, type Wallet } from "../wallet";

const AT = "2026-08-27T00:00:00.000Z";

const card = (wallet: Wallet) =>
  renderToStaticMarkup(<Cost metrics={initialState.metrics} wallet={wallet} account="deepseek" onRefreshWallet={() => {}} />);

const read = (over: Partial<WalletReading> = {}): Wallet => ({
  kind: "read",
  reading: { display: "¥110.00", available: true, stale: false, fetchedAt: AT, lines: [{ currency: "CNY", total: "¥110.00" }], ...over },
});

describe("the wallet in the cost card", () => {
  // Most providers have no wallet endpoint. A dash there would read as a number
  // that failed to load, which is a different thing entirely.
  it("draws nothing at all when the provider has no wallet", () => {
    expect(card(ABSENT)).not.toContain("wallet");
  });

  it("names the account beside the balance", () => {
    const html = card(read());
    expect(html).toContain("钱包 · deepseek");
    expect(html).toContain("¥110.00");
  });

  // The provider saying this account can no longer serve calls matters more
  // than the number beside it.
  it("marks a wallet the provider has suspended", () => {
    expect(card(read({ available: false }))).toContain("已停用");
  });

  // A value standing in must not look current.
  it("dates a value that is standing in", () => {
    expect(card(read({ stale: true }))).toMatch(/分钟前|小时前|天前|刚刚/);
  });

  it("says nothing about age while the value is fresh", () => {
    expect(card(read())).not.toMatch(/分钟前|小时前|天前|刚刚/);
  });

  // Two currencies are two lines. Summing them would mean inventing a rate.
  it("stacks currencies instead of adding them", () => {
    const html = card(
      read({
        lines: [
          { currency: "CNY", total: "¥70.16" },
          { currency: "USD", total: "$12.00" },
        ],
      }),
    );
    expect(html).toContain("¥70.16");
    expect(html).toContain("$12.00");
    expect(html).toContain("两种币种不合计");
  });

  // Promotional credit expires, so it is worth naming; a zero is not, and the
  // kernel leaves it out rather than sending "0".
  it("names promotional credit when there is some", () => {
    expect(card(read({ lines: [{ currency: "CNY", total: "¥110.00", granted: "¥10.00" }] }))).toContain("含赠金 ¥10.00");
    expect(card(read())).not.toContain("含赠金");
  });

  // A wallet that could not be read says why, and never as a zero.
  it("gives the reason instead of a number", () => {
    const html = card({ kind: "unread", why: "该供应商拒绝了当前密钥，无法读取余额" });
    expect(html).toContain("该供应商拒绝了当前密钥");
    // The value column carries the mark for a number that is missing, never a
    // zero — "cannot be read" and "is empty" are opposite things.
    expect(html).toContain('<span class="v">—</span>');
  });
});
