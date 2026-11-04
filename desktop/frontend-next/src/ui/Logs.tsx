import { useEffect, useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort } from "../port/port";
import type { StudioLogs } from "../port/version";
import { CopyButton } from "./CopyButton";

// Where to look when something goes wrong: the folder the shell and the kernel
// write to, and any crash file the kernel left after an abnormal exit.
export function Logs({ port }: { port: Pick<AgentPort, "studioLogs"> }) {
  const [logs, setLogs] = useState<StudioLogs | null>(null);
  const [failed, setFailed] = useState("");

  useEffect(() => {
    port.studioLogs().then(setLogs, (e) => setFailed(reason(e)));
  }, [port]);

  if (failed) return <p className="acct-note">{failed}</p>;
  if (!logs) return <p className="acct-note">{t("common.loading")}</p>;
  return (
    <div className="vers">
      <p className="acct-note">
        <code>{logs.dir}</code>{" "}
        <CopyButton text={logs.dir} iconOnly showFeedback label={t("data.copy_the_log_folder")} />
      </p>
      {logs.crashes.length === 0 ? (
        <p className="acct-note">{t("data.no_abnormal_exit_record_has")}</p>
      ) : (
        <>
          <p className="acct-note">{t("data.records_left_when_the_kernel")}</p>
          {logs.crashes.map((c) => (
            <p className="acct-note" key={c.name}>
              <code>{c.name}</code>{" · "}{new Date(c.at).toLocaleString()}{" · "}{Math.max(1, Math.round(c.bytes / 1024))} KB
            </p>
          ))}
          {logs.truncated && <p className="acct-note">{t("data.older_records_are_not_listed")}</p>}
        </>
      )}
    </div>
  );
}
