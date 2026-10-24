import { useEffect, useState, type KeyboardEvent } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort, WriteLeaseSettings } from "../port/port";
import { Switch } from "./Switch";

// One switch over how sessions in one project wait on each other. On is the
// default and trades the whole-workspace hold for concurrency; off is strict.
// It says what that costs beside the switch, and says that only sessions
// opened afterwards pick it up: a running one keeps the lease it started with.
export function WriteLease({ port }: { port: AgentPort }) {
  const [state, setState] = useState<WriteLeaseSettings | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [wait, setWait] = useState("");

  useEffect(() => {
    port
      .writeLease()
      .then((s) => {
        setState(s);
        setWait(String(s.maxWaitMinutes));
      })
      .catch(() => setState(null));
  }, [port]);

  if (!state) return <div className="empty">{t("无法读取多会话写入设置。")}</div>;

  const flip = async () => {
    setBusy(true);
    setError("");
    try {
      setState(await port.saveWriteLease({ optimistic: !state.optimistic }));
    } catch (e) {
      setError(reason(e));
    } finally {
      setBusy(false);
    }
  };

  const commitWait = async () => {
    const minutes = Number(wait);
    if (minutes === state.maxWaitMinutes) return;
    setBusy(true);
    setError("");
    try {
      const next = await port.saveWriteLease({ maxWaitMinutes: minutes });
      setState(next);
      setWait(String(next.maxWaitMinutes));
    } catch (e) {
      setError(reason(e));
    } finally {
      setBusy(false);
    }
  };
  const enter = (e: KeyboardEvent<HTMLInputElement>) => { if (e.key === "Enter") e.currentTarget.blur(); };
  const waitLabel = t("等待其他会话释放写入权的最长时间");

  return (
    <div className="box">
      <div className="lrow">
        <span className="tx">
          <span className="lb">{t("乐观模式（多会话并行）")}</span>
          <span className="ds">{t("没声明写入范围的操作（bash、MCP、没写 write_paths 的子代理）不再占住整个工作区，所以同一个项目里的多个会话可以同时工作，不必互相等待。代价：声明了路径的写入（如编辑文件）仍按路径互斥（落在工作区之外的路径只和同一路径的写入互斥），但 bash 和 MCP 的写入没有事前的路径围栏，只在事后观察，两个会话用它们改同一个文件时可能互相覆盖。默认开启；关闭后为严格模式，没声明范围的操作会占住整个工作区。只对之后新开的会话生效。")}</span>
        </span>
        <Switch
          data-action="write-lease.optimistic"
          on={state.optimistic}
          busy={busy}
          label={t("乐观模式（多会话并行）")}
          onClick={() => void flip()}
        />
      </div>
      <div className="lrow threshold-row">
        <span className="tx">
          <span className="lb">{waitLabel}</span>
          <span className="ds">{t("另一个会话占着要写的路径时，这里的写入会一直等；设了分钟数，超过就失败，并给模型一个 workspace.claim_timeout 的明确原因。默认 0，一直等到本轮结束；想设上限，建议 120。之后新开的会话才采用新值。")}</span>
        </span>
        <div className="threshold-control">
          <span className="unit-field">
            <input type="text" inputMode="numeric" aria-label={waitLabel} data-action-keydown="write-lease.max-wait" data-action-change="write-lease.max-wait" value={wait} disabled={busy} onChange={(e) => setWait(e.target.value.replace(/\D/g, ""))} onBlur={() => void commitWait()} onKeyDown={enter} />
            <i>{t("分钟")}</i>
          </span>
        </div>
      </div>
      {error && <div className="why" role="alert">{error}</div>}
    </div>
  );
}
