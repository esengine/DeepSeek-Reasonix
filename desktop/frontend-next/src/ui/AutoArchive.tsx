import { useEffect, useState, type KeyboardEvent } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort } from "../port/port";
import type { AutoArchiveSettings } from "../port/boundary";
import { Group } from "./Group";
import { Switch } from "./Switch";

const LIMIT = 3650;

// Archiving hides a conversation from the everyday list and nothing else, so
// the switch ships off and the number only matters once it is on.
export function AutoArchive({ port }: { port: AgentPort }) {
  const [state, setState] = useState<AutoArchiveSettings | null>(null);
  const [days, setDays] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const adopt = (s: AutoArchiveSettings) => {
    setState(s);
    setDays(String(s.days));
  };

  useEffect(() => {
    port.autoArchive().then(adopt).catch(() => setState(null));
  }, [port]);

  const save = async (next: Pick<AutoArchiveSettings, "enabled" | "days">) => {
    setBusy(true);
    setError("");
    try {
      adopt(await port.saveAutoArchive(next));
    } catch (e) {
      setError(reason(e));
    } finally {
      setBusy(false);
    }
  };

  const commit = () => {
    if (!state) return;
    const n = Number(days);
    if (!Number.isInteger(n) || n < 1 || n > LIMIT) return setError(t("请输入 1 到 {n} 之间的整数。", { n: LIMIT }));
    if (n !== state.days) void save({ enabled: state.enabled, days: n });
  };

  const body = !state ? (
    <div className="empty">{t("无法读取自动归档设置。")}</div>
  ) : (
    <div className="box">
      <div className="lrow">
        <span className="tx">
          <span className="lb">{t("自动归档长时间未活动的对话")}</span>
          <span className="ds">{t("默认关闭。开启后，超过下面天数没有活动的对话会在后台被归档。")}</span>
        </span>
        <Switch
          data-action="auto-archive.enabled"
          on={state.enabled}
          busy={busy}
          label={t("自动归档长时间未活动的对话")}
          onClick={() => void save({ enabled: !state.enabled, days: state.days })}
        />
      </div>
      <div className="lrow threshold-row">
        <span className="tx">
          <span className="lb">{t("闲置天数")}</span>
          <span className="ds">{t("距上次活动超过这个天数才会归档。默认 {n} 天。", { n: state.defaultDays })}</span>
        </span>
        <div className="threshold-control">
          <span className="unit-field">
            <input type="text" inputMode="numeric" aria-label={t("闲置天数")} data-action-keydown="auto-archive.days" data-action-change="auto-archive.days"
              value={days} disabled={busy}
              onChange={(e) => setDays(e.target.value.replace(/\D/g, ""))}
              onBlur={commit}
              onKeyDown={blurOnEnter} />
            <i>{t("天")}</i>
          </span>
        </div>
      </div>
      {error && <div className="why">{error}</div>}
    </div>
  );

  return (
    <Group id="auto-archive" title={t("自动归档不活跃的对话")}
      hint={t("归档只是把对话移出常用列表，内容不动，随时可在“已归档”里恢复。置顶的、正在运行的、等待你回答或批准的对话不会被归档。只处理已打开会话窗格的文件夹；导入的旧会话按原有的最后活动时间计算，开启后可能一次归档较多。")}>
      {body}
    </Group>
  );
}

const blurOnEnter = (e: KeyboardEvent<HTMLInputElement>) => {
  if (e.key === "Enter") e.currentTarget.blur();
};
