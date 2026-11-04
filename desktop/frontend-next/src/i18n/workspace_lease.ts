import type { WorkspaceLease } from "../port/wire";
import { t } from "./index";

export function workspaceLeaseDetail(claim: WorkspaceLease, showHolder = true): string {
  return [showHolder && (claim.holder || claim.holderSessionId) ? t("kernel.session_holder_session_holds_write", {
    holder: claim.holder ?? "", session: claim.holderSessionId ?? "", paths: JSON.stringify(claim.paths ?? []),
  }) : "", claim.requestedPaths?.length ? t("kernel.requested_write_claim_paths", { paths: JSON.stringify(claim.requestedPaths) }) : "",
  showHolder && claim.repository ? t("kernel.these_workspaces_are_inside_the", { repo: claim.repository }) : ""].filter(Boolean).join(" · ");
}
