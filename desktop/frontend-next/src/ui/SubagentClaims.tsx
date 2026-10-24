import { useEffect, useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort, SubagentClaimsSettings } from "../port/port";
import { Switch } from "./Switch";

// One switch over how a sub-agent that named no write_paths holds its claim.
// On is the default and trades the fence for concurrency; off is strict, the
// whole workspace. It says what that costs beside the switch.
export function SubagentClaims({ port, onChanged }: { port: AgentPort; onChanged: () => void }) {
  const [state, setState] = useState<SubagentClaimsSettings | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    port
      .subagentClaims()
      .then(setState)
      .catch(() => setState(null));
  }, [port]);

  if (!state) return <div className="empty">{t("无法读取子代理写入范围设置。")}</div>;

  const flip = async () => {
    setBusy(true);
    setError("");
    try {
      setState(await port.saveSubagentClaims(!state.optimistic));
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
          <span className="lb">{t("乐观模式（效率优先）")}</span>
          <span className="ds">{t("没有声明 write_paths 的子代理不再占住整个工作区，只在写某个文件的那一刻占用该路径，所以可以和主会话、其他子代理并发。代价：它的 bash 和 MCP 写入不受路径围栏约束，只在事后观察；两边同时改同一个文件时，后写的一方会被拒绝。默认开启；关闭后为严格模式，没声明范围的子代理会占住整个工作区。")}</span>
        </span>
        <Switch
          data-action="subagent-claims.optimistic"
          on={state.optimistic}
          busy={busy}
          label={t("乐观模式（效率优先）")}
          onClick={() => void flip()}
        />
      </div>
      {error && <div className="why">{error}</div>}
    </div>
  );
}
