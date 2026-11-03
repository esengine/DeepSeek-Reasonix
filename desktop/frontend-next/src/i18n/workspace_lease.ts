import type { WorkspaceLease } from "../port/wire";
import { t } from "./index";

export function workspaceLeaseDetail(claim: WorkspaceLease, showHolder = true): string {
  return [showHolder && (claim.holder || claim.holderSessionId) ? t("workspaceLease.sessionHoldsWriteClaim", {
    holder: claim.holder ?? "", session: claim.holderSessionId ?? "", paths: JSON.stringify(claim.paths ?? []),
  }) : "", claim.requestedPaths?.length ? t("workspaceLease.requestedWriteClaim", { paths: JSON.stringify(claim.requestedPaths) }) : "",
  showHolder && claim.repository ? t("workspaceLease.theseWorkspacesAreInsideThe", { repo: claim.repository }) : ""].filter(Boolean).join(" · ");
}
