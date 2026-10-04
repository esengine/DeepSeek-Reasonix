import { useEffect, useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort, OpaqueWriterSerializationSettings } from "../port/port";
import { Switch } from "./Switch";

// One switch over whether tools that cannot declare write paths serialize a whole
// workspace. The row reads the user file; a project file that sets the same key
// outranks it, and then the value this workspace runs with is said beside it.
export function OpaqueWriters({ port, onChanged }: { port: AgentPort; onChanged: () => void }) {
  const [state, setState] = useState<OpaqueWriterSerializationSettings | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    port
      .opaqueWriters()
      .then(setState)
      .catch(() => setState(null));
  }, [port]);

  if (!state) return <div className="empty">{t("无法读取写锁串行设置。")}</div>;

  const flip = async () => {
    setBusy(true);
    setError("");
    try {
      setState(await port.saveOpaqueWriters(!state.enabled));
      onChanged();
    } catch (e) {
      setError(reason(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="box">
      <div className="lrow">
        <span className="tx">
          <span className="lb">{t("写锁串行")}</span>
          <span className="ds">{t("无法声明写入范围的工具（bash、MCP）会占用整个工作区，同一项目的多个会话因此排队等待。关闭后这类工具可以并行，代价是并行写入不再被拦住")}</span>
        </span>
        <Switch
          data-action="opaque-writers.enabled"
          on={state.enabled}
          busy={busy}
          label={t("写锁串行")}
          onClick={() => void flip()}
        />
      </div>
      {state.effective !== state.enabled && (
        <div className="kv">
          <span className="k">{t("实际生效")}</span>
          <span className="v">
            {t(state.effective ? "当前项目的配置文件开启了它，此工作区仍会串行这类工具。" : "当前项目的配置文件关闭了它，此工作区不会串行这类工具。")}
          </span>
        </div>
      )}
      {error && <div className="why">{error}</div>}
    </div>
  );
}