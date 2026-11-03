import { expect, it } from "vitest";
import { modelHealthNote } from "./modelhealth";
import { t } from "../i18n";

it("never promotes a listing into a verified model or an unknown wallet into zero balance", () => {
  expect(modelHealthNote({ connection: "reachable", wallet: "available", checkedAt: 1 })).toBe(t("modelhealth.connectionReachableModelCallsUnverif"));
  for (const wallet of ["unknown", "unsupported"]) {
    expect(modelHealthNote({ connection: "unknown", wallet, checkedAt: 1 })).not.toContain(t("modelhealth.insufficientBalanceTopUpAnd"));
  }
  expect(modelHealthNote({ connection: "rate_limited", wallet: "unsupported", checkedAt: 1 })).toContain(t("modelhealth.temporarilyRateLimitedRetryLater"));
  expect(modelHealthNote({ connection: "future_state", wallet: "future_state", checkedAt: 1 })).toBe(t("modelhealth.connectionStatusUnconfirmed"));
});
