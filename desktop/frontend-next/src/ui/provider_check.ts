import { t } from "../i18n";
import { say } from "../i18n/kernel";
import type { ProviderCheck } from "../port/port";

export function checkFailure(check: ProviderCheck): string {
  const said = say({ code: check.code, params: check.params }) || t("检查失败，没有具体原因");
  const status = check.httpStatus ? ` · HTTP ${check.httpStatus}` : "";
  const words = check.detail ? ` · ${check.detail}` : "";
  return said + status + words;
}
