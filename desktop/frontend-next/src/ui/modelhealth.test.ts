import { expect, it } from "vitest";
import { modelHealthNote } from "./modelhealth";
import { t } from "../i18n";

it("never promotes a listing into a verified model or an unknown wallet into zero balance", () => {
  expect(modelHealthNote({ connection: "reachable", wallet: "available", checkedAt: 1 })).toBe(t("连接正常，模型调用未验证"));
  for (const wallet of ["unknown", "unsupported"]) {
    expect(modelHealthNote({ connection: "unknown", wallet, checkedAt: 1 })).not.toContain(t("余额不足，请充值后重试"));
  }
  expect(modelHealthNote({ connection: "rate_limited", wallet: "unsupported", checkedAt: 1 })).toContain(t("请求暂时被限流，请稍后重试"));
  expect(modelHealthNote({ connection: "future_state", wallet: "future_state", checkedAt: 1 })).toBe(t("连接状态尚未确认"));
});
