import { t } from "../i18n";
import type { HistoryMessage } from "../port/port";
import type { Tool } from "../port/wire";

export function reportText(tool: Tool): string | null {
  if ((tool.resolvedName || tool.name) !== "reply_to_parent") return null;
  try {
    const args = JSON.parse(tool.args ?? "{}");
    const input = tool.name === "use_capability" ? args.arguments : args;
    return typeof input?.message === "string" ? input.message : null;
  } catch { return null; }
}

export function AgentReport({ tool }: { tool: Tool }) {
  const text = reportText(tool);
  if (text === null) return null;
  return <div className="agent-report" data-state={tool.err ? "failed" : tool.output !== undefined ? "queued" : "pending"}>
    <div className="agent-report-status">{t(tool.err ? "报告发送失败" : tool.output !== undefined ? "已排队，不代表父代理已读取" : "等待发送回执")}</div>
    <div className="agent-report-text">{text}</div>
    {tool.err && <div className="agent-report-error">{tool.err}</div>}
  </div>;
}

function reportsFromHistory(messages: HistoryMessage[]): Tool[] {
  const reports: Tool[] = [];
  const pending = new Map<string, number>();
  messages.forEach((message, index) => {
    if (message.role === "assistant") message.toolCalls?.forEach((call, ordinal) => {
      if ((call.resolvedName || call.name) !== "reply_to_parent") return;
      pending.set(call.id, reports.length);
      reports.push({ id: `${message.msgIndex ?? index}:${ordinal}:${call.id}`, name: call.name,
        resolvedName: call.resolvedName, args: call.arguments, readOnly: true });
    });
    if (message.role === "tool" && message.toolCallId) {
      const report = pending.get(message.toolCallId);
      if (report !== undefined) {
        reports[report] = { ...reports[report], output: message.content, err: message.toolFailed || message.toolRefusalCode ? message.content || t("报告发送失败") : undefined };
        pending.delete(message.toolCallId);
      }
    }
  });
  return reports.filter((tool) => reportText(tool) !== null);
}

export function AgentReports({ messages }: { messages: HistoryMessage[] }) {
  const reports = reportsFromHistory(messages);
  return <section className="agentrun-reports" aria-label={t("子代理报告")}>
    <details open={reports.length > 0 || undefined}>
      <summary>{t("子代理报告")} · {reports.length}</summary>
      {reports.length === 0 ? <p>{t("暂无报告；子代理可在工作中向直接父代理汇报。")}</p> :
        <div className="agentrun-report-list">{reports.map((tool) => <AgentReport key={tool.id} tool={tool} />)}</div>}
    </details>
  </section>;
}
