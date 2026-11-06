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
        ? t("about.n_sessions_are_still_running", { n: Number(e.reason.params?.n) || 1 })
        : reason(e));
    }
  };
  return (
    <ToastRow kind="update" role="status" className="update-ready" actions={<>
      <button className="act ghost" data-action="update.later" onClick={() => setPutOff(version)}>{t("about.later")}</button>
      <button className="act" data-primary data-action="update.restart" onClick={() => void restart()}>{t("about.restart_and_update")}</button>
    </>}><b>{t("about.version_v_is_downloaded", { v: version })}</b>
      <div className="toast-sub">{says || t("about.restart_to_use_it_never_automatic")}</div>
    </ToastRow>
  );
}
