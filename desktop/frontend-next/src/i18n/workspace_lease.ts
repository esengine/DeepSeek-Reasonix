import type { WorkspaceLease } from "../port/wire";
import { t } from "./index";

export function workspaceLeaseDetail(claim: WorkspaceLease, showHolder = true): string {
  return [showHolder && (claim.holder || claim.holderSessionId) ? t("会话 {holder}（{session}）持有写入范围：{paths}", {
    holder: claim.holder ?? "", session: claim.holderSessionId ?? "", paths: JSON.stringify(claim.paths ?? []),
  }) : "", claim.requestedPaths?.length ? t("所需写入范围：{paths}", { paths: JSON.stringify(claim.requestedPaths) }) : ""].filter(Boolean).join(" · ");
}
