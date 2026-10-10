import { useEffect, useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import { Group } from "./Group";
import { arrowRadios } from "./tablist";
import type { AgentPort, WriteLeaseSettings } from "../port/port";

// One choice over how far the cross-session write lease reaches. The key is the
// user's alone — a project file cannot widen or remove the protection — so the
// row reads the user file and shows the mode that answer produces.
const MODES = ["strict", "optimistic", "off"] as const;

const MODE_NAME: Record<string, string> = {
  strict: "标准写锁",
  optimistic: "宽松写锁",
  off: "关闭写锁",
};

const MODE_WHY: Record<string, string> = {
  strict: "声明写入路径的写者按路径互斥；未声明路径的写者占用整个工作区，与其他写入一律排队。",
  optimistic: "声明写入路径的写者仍按路径互斥；未声明路径的写者不再占用整个工作区，可与其他写入并行。",
  off: "本会话不取写锁：写入不再受保护，也不再参与路径互斥，子代理之间同样如此。",
};

export function WriteLease({ port, onChanged }: { port: AgentPort; onChanged: () => void }) {
  const [state, setState] = useState<WriteLeaseSettings | null>(null);
  const [loaded, setLoaded] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    port
      .writeLease()
      .then((s) => {
        setState(s);
        setLoaded(true);
      })
      .catch(() => {
        setState(null);
        setLoaded(true);
      });
  }, [port]);

  // The first read is still in flight: render nothing rather than flash an
  // error that is not one.
  if (!loaded) return null;
  if (!state) return <div className="empty">{t("无法读取写锁档位。")}</div>;

  const pick = async (mode: string) => {
    if (mode === state.mode) return;
    setBusy(true);
    setError("");
    try {
      setState(await port.saveWriteLease(mode));
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
          <span className="lb">{t("写锁档位")}</span>
          <span className="ds">{t(MODE_WHY[state.mode] ?? MODE_WHY.strict)}</span>
        </span>
        <div className="seg" data-text role="radiogroup" aria-label={t("写锁档位")} data-action-keydown="write-lease.mode" onKeyDown={arrowRadios}>
          {MODES.map((mode) => (
            <button
              key={mode}
              role="radio"
              data-action="write-lease.mode"
              data-value={mode}
              aria-checked={state.mode === mode}
              tabIndex={state.mode === mode ? 0 : -1}
              disabled={busy}
              onClick={() => void pick(mode)}
            >
              {t(MODE_NAME[mode])}
            </button>
          ))}
        </div>
      </div>
      {state.effective !== state.mode && (
        <div className="kv">
          <span className="k">{t("实际生效")}</span>
          <span className="v">{t(MODE_WHY[state.effective] ?? MODE_WHY.strict)}</span>
        </div>
      )}
      {state.mode === "off" && (
        <p className="rmthint">{t("关闭后，其他会话的写入可能覆盖你的；确认能接受该风险再关闭。")}</p>
      )}
      {error && <div className="why">{error}</div>}
    </div>
  );
}

// The settings block ships whole so the settings page keeps one line for it
// instead of a frame plus its wording; that also keeps Settings.tsx from
// growing past its line ceiling for one more row.
export function WriteLeaseGroup({ port, onChanged }: { port: AgentPort; onChanged: () => void }) {
  return (
    <Group
      id="write-lease"
      title={t("写锁档位")}
      hint={t("三档决定本会话的写锁范围，对子代理与跨会话同样生效。修改会重建运行时，任务运行期间无法变更。")}
    >
      <WriteLease port={port} onChanged={onChanged} />
    </Group>
  );
}