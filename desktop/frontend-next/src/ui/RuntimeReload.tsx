import { useCallback, useEffect, useRef, useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort } from "../port/port";

// Reloading the runtime is the one action on this page with a duration: sidecars
// stop and start, and the skill/command/hook directories are walked again. The
// states below are what the control has to say for itself — offering, working,
// landed, refused — because without them the only signal a reload happened at
// all is that the app did not visibly change.
type State = "" | "run" | "ok" | "bad";

// Long enough to read, short enough that the row goes back to offering the
// action rather than sitting on a stale success.
const SETTLED_MS = 4000;

// A hook rather than a component: the button belongs in the group's header slot
// and the note in its body, and one component cannot fill two slots.
export function useRuntimeReload(port: AgentPort, onDone: () => void) {
  const [connection, setConnection] = useState({ port });
  const currentConnection = useRef(connection);
  currentConnection.current = connection;
  const attempts = useRef(0);
  const [feedback, setFeedback] = useState<{ state: State; note: string }>({ state: "", note: "" });
  const { state, note } = feedback;
  if (connection.port !== port) {
    setConnection({ port });
    setFeedback({ state: "", note: "" });
  }

  const go = useCallback(async () => {
    const attempt = ++attempts.current;
    setFeedback({ state: "run", note: t("正在重启常驻进程，重新扫描技能、命令和钩子…") });
    try {
      await port.reloadExtensions();
      if (currentConnection.current !== connection || attempts.current !== attempt) return;
      setFeedback({ state: "ok", note: t("已生效，下一轮开始用新的扩展") });
      onDone();
    } catch (e) {
      if (currentConnection.current !== connection || attempts.current !== attempt) return;
      // A refusal is the kernel's, and it knows why: a turn in flight, a
      // background job, a session that moved. Say its reason, not a generic one.
      setFeedback({ state: "bad", note: reason(e) });
    }
  }, [port, onDone, connection]);

  useEffect(() => {
    if (feedback.state !== "ok") return;
    const id = setTimeout(() => {
      setFeedback({ state: "", note: "" });
    }, SETTLED_MS);
    return () => clearTimeout(id);
  }, [feedback]);

  return {
    applied: () => {
      attempts.current++;
      setFeedback({ state: "ok", note: t("已生效，下一轮开始用新的扩展") });
    },
    report: (message: string) => {
      attempts.current++;
      setFeedback({ state: "bad", note: t("更改已保存，运行时未重载：{reason}。请用「重载运行时」重试。", { reason: message }) });
    },
    action: (
      <button className="act reload" data-action="extensions.reload" data-s={state || undefined} disabled={state === "run"} onClick={go}>
        <i className="rdot" />
        {state === "run" ? t("重载中") : state === "ok" ? t("已生效") : t("重载运行时")}
      </button>
    ),
    note: note ? (
      <div className="rnote" data-s={state} role="status">
        {note}
      </div>
    ) : null,
  };
}
