import { useMemo, useRef } from "react";
import { t } from "../i18n";
import { fromHistory } from "../state/session";
import type { AgentPort } from "../port/port";
import type { SubagentRun } from "../port/subagent";
import { delegateStateLabel } from "./delegation";
import { Transcript } from "./Transcript";
import { AgentGuidance } from "./AgentGuidance";
import { AgentReports } from "./AgentReports";
import { AgentRecovery } from "./AgentRecovery";
import { useAgentHistory } from "./useAgentHistory";

const noop = async () => undefined as never;
const none = { entering: [] as string[], onEntered: () => {}, waiting: {}, onExtInvoke: () => {}, checkpoints: new Map(), onOpenProject: () => {}, onKeepHere: () => {}, takeovers: {}, needsProject: false };

/** A sub-agent run read from the kernel's own record, so what it shows does not
 *  depend on what the progress stream happened to carry. A running run is polled
 *  from where the last page ended; a finished one is read once. */
export function AgentRunView({ port, run, shown }: { port: AgentPort; run: Pick<SubagentRun, "ref" | "name">; shown: boolean }) {
  const scroll = useRef<HTMLDivElement>(null);
  const { messages, info, failed, before, loading, earlier } = useAgentHistory(port, run.ref, shown, scroll);

  const items = useMemo(() => fromHistory(messages).items, [messages]);
  const status = info ? delegateStateLabel(info.state) : "";
  return (
    <div className="agentrun" data-state={info?.state}>
      <div className="agentrun-hd">
        <span className="who">{run.name}</span>
        {info?.model && <span className="model">{info.model}</span>}
        {status && <span className="rt">{status}</span>}
      </div>
      {failed && <div className="workbench-error" role="alert">{failed}</div>}
      {shown && <AgentRecovery port={port} runRef={run.ref} />}
      {!failed && !info && <div className="workbench-empty">{t("正在读取…")}</div>}
      {before > 0 && <button className="btn btn--small" data-action="agent.history-earlier" type="button" disabled={loading} onClick={() => void earlier()}>{t("加载更早的消息")}</button>}
      {info?.ref === run.ref && <AgentReports messages={messages} />}
      {info && (
        <Transcript items={items} {...none} scroll={scroll} revision={messages.length} hidden={false} focus={null} onApprove={noop} onFullAccess={noop} onPlan={noop} onAnswer={noop} onForget={noop}
          onExtSubmit={noop} onPrepareRewind={noop} onCommitRewind={noop} onPrepareFileRevert={noop} onCommitFileRevert={noop} port={port} turnRunning={info.state === "running"} />
      )}
      {info?.ref === run.ref && <AgentGuidance key={run.ref} port={port} runRef={run.ref} running={info.state === "running"} />}
    </div>
  );
}
