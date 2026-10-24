import { useEffect, useMemo, useRef, useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import { fromHistory } from "../state/session";
import type { AgentPort, HistoryMessage } from "../port/port";
import type { SubagentRun } from "../port/subagent";
import { delegateStateLabel } from "./delegation";
import { Transcript } from "./Transcript";

const POLL_MS = 1500;
const noop = async () => undefined as never;
const none = { entering: [] as string[], onEntered: () => {}, waiting: {}, onExtInvoke: () => {}, checkpoints: new Map(), onOpenProject: () => {}, onKeepHere: () => {}, takeovers: {}, needsProject: false };

/** A sub-agent run read from the kernel's own record, so what it shows does not
 *  depend on what the progress stream happened to carry. A running run is polled
 *  from where the last page ended; a finished one is read once. */
export function AgentRunView({ port, run, shown }: { port: AgentPort; run: Pick<SubagentRun, "ref" | "name">; shown: boolean }) {
  const [messages, setMessages] = useState<HistoryMessage[]>([]);
  const [info, setInfo] = useState<SubagentRun>();
  const [failed, setFailed] = useState("");
  const scroll = useRef<HTMLDivElement>(null);

  useEffect(() => {
    let current = true;
    let timer: ReturnType<typeof setTimeout> | undefined;
    setMessages([]);
    setInfo(undefined);
    setFailed("");
    const read = async (after: number) => {
      try {
        const page = await port.subagentTranscript(run.ref, after);
        if (!current) return;
        setInfo(page.run);
        setFailed("");
        if (page.messages.length) setMessages((all) => (after === 0 ? page.messages : [...all, ...page.messages]));
        if (page.next < page.total) void read(page.next);
        else if (page.live) timer = setTimeout(() => void read(page.next), POLL_MS);
      } catch (e) {
        if (current) setFailed(reason(e));
      }
    };
    if (shown) void read(0);
    return () => {
      current = false;
      clearTimeout(timer);
    };
  }, [port, run.ref, shown]);

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
      {!failed && !info && <div className="workbench-empty">{t("正在读取…")}</div>}
      {info && (
        <Transcript items={items} {...none} scroll={scroll} revision={messages.length} hidden={false} focus={null} onApprove={noop} onFullAccess={noop} onPlan={noop} onAnswer={noop} onForget={noop}
          onExtSubmit={noop} onPrepareRewind={noop} onCommitRewind={noop} onPrepareFileRevert={noop} onCommitFileRevert={noop} port={port} turnRunning={info.state === "running"} />
      )}
    </div>
  );
}
