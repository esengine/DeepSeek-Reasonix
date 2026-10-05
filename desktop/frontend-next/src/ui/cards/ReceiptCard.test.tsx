// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render } from "@testing-library/react";
import "../testkit";
import { ReceiptCard } from "./ReceiptCard";
import { t } from "../../i18n";
import type { Receipt } from "../../port/wire";

afterEach(cleanup);

const receipt = (over: Partial<Receipt> = {}): Receipt => ({ ...over }) as Receipt;
const draw = (over: Partial<Receipt> = {}) => render(<ReceiptCard r={receipt(over)} />).container;

// A receipt and a notice are the same runtime speaking, so anything the host
// has to say here is one of its cards. Only the clean turn stays a quiet line.
describe("a delivery receipt", () => {
  it("settles into one line when there is nothing to report", () => {
    const box = draw({ changes: [{ path: "a.ts", reviewed: true }] });
    expect(box.querySelector(".call")).toBeNull();
    expect(box.querySelector(".rc-ok .rc-t")?.textContent).toBe(t("没有未经验证的部分"));
  });

  it("is one of the host's cards once it has a gap, and names its speaker", () => {
    const box = draw({ gaps: [{ kind: "unverified_change", detail: "src/a.ts" }] });
    expect(box.querySelector(".call")?.getAttribute("data-k")).toBe("host");
    expect(box.querySelector(".hl .nm")?.textContent).toBe(t("交付验收"));
    expect(box.querySelector(".hl .tag")?.textContent).toBe(t("主机"));
  });

  it("wears its severity where the gutter can follow it", () => {
    const withGap = draw({ gaps: [{ kind: "failed_verification" }] });
    expect(withGap.querySelector(".call")?.getAttribute("data-lvl")).toBe("warn");
    // Volunteered, not found: a turn's own note is not a finding against it.
    const declaredOnly = draw({ unverified: ["网络路径没走到"] });
    expect(declaredOnly.querySelector(".call")?.getAttribute("data-lvl")).toBeNull();
  });

  it("draws each item as the .find every other host card uses", () => {
    const box = draw({ gaps: [{ kind: "stale_verification", detail: "go test ./..." }], unverified: ["没跑 Windows"] });
    const finds = [...box.querySelectorAll(".find")];
    expect(finds.map((f) => f.getAttribute("data-lvl"))).toEqual(["warn", null]);
    expect(finds[0].querySelector(".t")?.textContent).toBe(t("验证早于最后一次改动"));
    expect(finds[0].querySelector("code.gapd")?.textContent).toBe("go test ./...");
    expect(finds[1].querySelector(".t")?.textContent).toBe("没跑 Windows");
  });

  it("keeps the total honest when it stops listing", () => {
    const gaps = Array.from({ length: 8 }, (_, i) => ({ kind: "missing_check", detail: `c${i}` }));
    const box = draw({ gaps });
    expect(box.querySelectorAll(".find").length).toBe(5);
    expect(box.querySelector(".rc-more")?.textContent).toBe(t("另有 {n} 项", { n: 3 }));
  });
});
