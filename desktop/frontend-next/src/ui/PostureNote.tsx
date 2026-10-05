import { useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort, SessionStatus } from "../port/port";

export type PostureNoteKind = "trust" | "unconfined";

const UNCONFINED_SEEN = "rx-posture-unconfined-seen";

// Which note a session on its default posture owes. The trust question is the
// terminal's: asked only where the sandbox would confine what trusting allows.
// Without a sandbox trusting changes nothing, so what is said is why it asks.
export function postureNote(status: SessionStatus | null): PostureNoteKind | null {
  const d = status?.approvalDefault;
  if (!d?.defaulted || status?.toolApprovalMode !== "ask") return null;
  if (!d.writesConfined) return "unconfined";
  return d.trust === "" && d.trustable ? "trust" : null;
}

function seenUnconfined(): boolean {
  try {
    return localStorage.getItem(UNCONFINED_SEEN) === "1";
  } catch {
    return false;
  }
}

function markUnconfinedSeen() {
  try {
    localStorage.setItem(UNCONFINED_SEEN, "1");
  } catch {
    // A viewer convenience: without storage the note returns next session.
  }
}

export function PostureNote({ port, status, onChanged }: {
  port: AgentPort;
  status: SessionStatus | null;
  onChanged: () => void;
}) {
  const [busy, setBusy] = useState(false);
  const [refused, setRefused] = useState("");
  const [hidden, setHidden] = useState(seenUnconfined);
  const kind = postureNote(status);
  if (!kind || (kind === "unconfined" && hidden)) return null;

  const act = (run: () => Promise<void>) => {
    if (busy) return;
    setBusy(true);
    setRefused("");
    run().then(onChanged).catch((e: unknown) => setRefused(reason(e))).finally(() => setBusy(false));
  };

  if (kind === "trust") {
    return (
      <div className="rtbar posturenote" data-kind="trust" role="group" aria-label={t("是否信任此文件夹")}>
        <span className="t">{t("是否信任此文件夹？")}</span>
        <span className="why">
          {refused || t("信任后，在此文件夹里的编辑和命令不再逐条询问（桌面端和终端都是）。系统沙盒把命令的写入限制在此文件夹、你额外允许写入的目录、临时目录和工具链缓存内；网络与拒绝规则照常生效。")}
        </span>
        <button data-action="workspace.trust" data-value="trusted" disabled={busy} onClick={() => act(() => port.decideWorkspaceTrust("trusted"))}>{t("信任此文件夹")}</button>
        <button data-action="workspace.trust" data-value="declined" disabled={busy} onClick={() => act(() => port.decideWorkspaceTrust("declined"))}>{t("暂不信任")}</button>
      </div>
    );
  }
  return (
    <div className="rtbar posturenote" data-kind="unconfined" data-lvl="warn" role="status">
      <span className="t">{t("新会话默认每次询问")}</span>
      <span className="why">
        {refused || t("当前没有生效的系统沙盒（Windows 暂不支持，或已在设置中关闭），命令的写入不受限制，所以编辑和命令执行前都会先问你。切换到自动批准后，之后所有文件夹的新会话都会使用它。")}
      </span>
      <button data-action="tool-approval.mode" data-value="auto" disabled={busy} onClick={() => act(() => port.setApprovalMode("auto"))}>{t("切换到自动批准")}</button>
      <button data-action="posture-note.dismiss" onClick={() => { markUnconfinedSeen(); setHidden(true); }}>{t("知道了")}</button>
    </div>
  );
}
