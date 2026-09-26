import { useEffect, useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort, BrowserToolsSettings } from "../port/port";
import { Switch } from "./Switch";

// One switch over whether the browser tools are bound at all. The row reads the
// user file; a project file that sets the same key outranks it, and then the
// value this workspace runs with is said beside it.
export function BrowserTools({ port, onChanged }: { port: AgentPort; onChanged: () => void }) {
  const [state, setState] = useState<BrowserToolsSettings | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    port
      .browserTools()
      .then(setState)
      .catch(() => setState(null));
  }, [port]);

  if (!state) return <div className="empty">{t("无法读取内置浏览器设置。")}</div>;

  const flip = async () => {
    setBusy(true);
    setError("");
    try {
      setState(await port.saveBrowserTools(!state.enabled));
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
          <span className="lb">{t("启用内置浏览器")}</span>
          <span className="ds">{t("agent 可以打开网页、读取内容并操作页面；浏览器在第一次使用时才启动。关闭后这些工具不会出现在工具列表中")}</span>
        </span>
        <Switch
          data-action="browser-tools.enabled"
          on={state.enabled}
          busy={busy}
          label={t("启用内置浏览器")}
          onClick={() => void flip()}
        />
      </div>
      {state.effective !== state.enabled && (
        <div className="kv">
          <span className="k">{t("实际生效")}</span>
          <span className="v">
            {t(state.effective ? "当前项目的配置文件开启了它，此工作区仍会提供内置浏览器。" : "当前项目的配置文件关闭了它，此工作区不会提供内置浏览器。")}
          </span>
        </div>
      )}
      {error && <div className="why">{error}</div>}
    </div>
  );
}
