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
    expect(box.querySelector(".rc-ok .rc-t")?.textContent).toBe(t("chat.nothing_left_unverified"));
  });

  it("is one of the host's cards once it has a gap, and names its speaker", () => {
    const box = draw({ gaps: [{ kind: "unverified_change", detail: "src/a.ts" }] });
    expect(box.querySelector(".call")?.getAttribute("data-k")).toBe("host");
    expect(box.querySelector(".hl .nm")?.textContent).toBe(t("chat.delivery_receipt"));
    expect(box.querySelector(".hl .tag")?.textContent).toBe(t("chat.host"));
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
    expect(finds[0].querySelector(".t")?.textContent).toBe(t("chat.the_verification_predates_the_last"));
    expect(finds[0].querySelector("code.gapd")?.textContent).toBe("go test ./...");
    expect(finds[1].querySelector(".t")?.textContent).toBe("没跑 Windows");
  });

  it("shows only the start of a long or multi-line command and keeps the whole of it for hover", () => {
    const script = "cd \"$TMPDIR/wxcheck\" && python - <<'PYEOF'\n" + "p = open(\"verify.py\").read()\n".repeat(40) + "PYEOF\npython -m unittest verify -v 2>&1 | tail -12";
    const box = draw({ gaps: [{ kind: "inconclusive_verification", detail: script }] });
    const code = box.querySelector("code.gapd")!;
    expect(code.textContent!.length).toBeLessThanOrEqual(121);
    expect(code.textContent).toContain("cd \"$TMPDIR/wxcheck\" && python - <<'PYEOF'");
    expect(code.textContent!.endsWith("…")).toBe(true);
    expect(code.getAttribute("title")).toBe(script);
  });

  it("leaves a short one-line command exactly as it was", () => {
    const box = draw({ gaps: [{ kind: "failed_verification", detail: "go test ./internal/..." }] });
    const code = box.querySelector("code.gapd")!;
    expect(code.textContent).toBe("go test ./internal/...");
    expect(code.getAttribute("title")).toBe("go test ./internal/...");
  });

  it("keeps the total honest when it stops listing", () => {
    const gaps = Array.from({ length: 8 }, (_, i) => ({ kind: "missing_check", detail: `c${i}` }));
    const box = draw({ gaps });
    expect(box.querySelectorAll(".find").length).toBe(5);
    expect(box.querySelector(".rc-more")?.textContent).toBe(t("chat.and_n_more", { n: 3 }));
  });
});
