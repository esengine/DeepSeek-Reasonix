import { useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import { HttpError, type AgentPort } from "../port/port";
import { useUpdateReady } from "../state/updatewatch";
import { ToastRow } from "./ToastRow";

const RESTART_BUSY = "update.restart_busy";

/** A release the window downloaded in the background and verified, waiting for
 *  the person. It appears only while nothing is running, restarts only when
 *  asked, and never forces a restart over work the kernel says is running. */
export function UpdateReady({ port, idle }: { port: Pick<AgentPort, "restartToVersion">; idle: boolean }) {
  const version = useUpdateReady();
  const [putOff, setPutOff] = useState("");
  const [says, setSays] = useState("");
  if (!version || !idle || putOff === version) return null;
  const restart = async () => {
    setSays("");
    try {
      await port.restartToVersion(version, false);
    } catch (e) {
      setSays(e instanceof HttpError && e.reason?.code === RESTART_BUSY
        ? t("有 {n} 个会话还在运行，等它们结束后再重启。", { n: Number(e.reason.params?.n) || 1 })
        : reason(e));
    }
  };
  return (
    <ToastRow kind="update" role="status" className="update-ready" actions={<>
      <button className="act ghost" data-action="update.later" onClick={() => setPutOff(version)}>{t("稍后")}</button>
      <button className="act" data-primary data-action="update.restart" onClick={() => void restart()}>{t("重启并更新")}</button>
    </>}>{says || t("新版本 {v} 已下载。重启后使用，不会自动重启。", { v: version })}</ToastRow>
  );
}
