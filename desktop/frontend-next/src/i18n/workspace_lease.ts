import type { WorkspaceLease } from "../port/wire";
import { t } from "./index";

const REASONS: Record<string, string> = {
  cancelled: "等待已取消",
  timeout: "等待超时，可重新调度",
  conflict: "输入版本已改变，需要重新读取",
  rejected: "事务申请被拒绝，未执行变更",
};

export function workspaceLeaseDetail(claim: WorkspaceLease, showHolder = true): string {
  const capacity = claim.resourceKind === "capacity";
  return [claim.transactionId ? t(capacity ? "执行额度申请 {id}" : "写入事务 {id}", { id: claim.transactionId }) : "",
  claim.executionId ? t("执行 {id}", { id: claim.executionId }) : "",
  claim.callId ? t("工具调用 {id}", { id: claim.callId }) : "",
  claim.reason ? t(REASONS[claim.reason] ?? claim.reason) : "",
  claim.waitedMs ? t("已等待 {ms} 毫秒", { ms: claim.waitedMs }) : "",
  showHolder && (claim.holder || claim.holderSessionId) ? t(capacity ? "会话 {holder}（{session}）正在使用执行额度" : "会话 {holder}（{session}）持有写入范围：{paths}", {
    holder: claim.holder ?? "", session: claim.holderSessionId ?? "", paths: JSON.stringify(claim.paths ?? []),
  }) : "", claim.requestedPaths?.length ? t("所需写入范围：{paths}", { paths: JSON.stringify(claim.requestedPaths) }) : "",
  showHolder && claim.repository ? t("这些工作区在同一个 Git 仓库 {repo} 内，同一仓库共用一个写入域；把项目移出该仓库，或在用户配置里设 write_lease = \"optimistic\"，才能并行写入", { repo: claim.repository }) : ""].filter(Boolean).join(" · ");
}

export function workspaceLeaseTitle(claim?: WorkspaceLease): string | undefined {
  if (claim?.resourceKind !== "capacity") return undefined;
  const titles: Record<string, string> = {
    waiting: "正在等待执行额度，可用后会自动继续",
    acquired: "已取得执行额度，请求已继续",
    abandoned: "请求开始前，执行额度等待已结束",
  };
  const title = claim.state ? titles[claim.state] : undefined;
  return title ? t(title) : undefined;
}
