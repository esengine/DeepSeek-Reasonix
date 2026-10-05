import { useEffect, useState, type KeyboardEvent } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort } from "../port/port";
import type { ProgressWatchSettings } from "../port/boundary";
import { Group } from "./Group";
import { Switch } from "./Switch";

type Draft = { rounds: string; tokenMultiple: string };
const LIMIT = 1000;

// When a run reads as no longer moving. The notice is always said; pausing is
// the one part that changes what a run does, so it is the only switch and it
// ships off. Both numbers feed both halves, which is why they sit under it
// rather than behind it.
export function ProgressWatch({ port }: { port: AgentPort }) {
  const [state, setState] = useState<ProgressWatchSettings | null>(null);
  const [draft, setDraft] = useState<Draft>({ rounds: "", tokenMultiple: "" });
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const adopt = (s: ProgressWatchSettings) => {
    setState(s);
    setDraft({ rounds: String(s.rounds), tokenMultiple: String(s.tokenMultiple) });
  };

  useEffect(() => {
    port.progressWatch().then(adopt).catch(() => setState(null));
  }, [port]);

  const commit = (text: string, apply: (n: number) => void) => {
    const n = Number(text);
    if (!Number.isInteger(n) || n < 1 || n > LIMIT) return setError(t("请输入 1 到 {n} 之间的整数。", { n: LIMIT }));
    apply(n);
  };

  const save = async (next: Pick<ProgressWatchSettings, "pause" | "rounds" | "tokenMultiple">) => {
    setBusy(true);
    setError("");
    try {
      adopt(await port.saveProgressWatch(next));
    } catch (e) {
      setError(reason(e));
    } finally {
      setBusy(false);
    }
  };

  const body = !state ? (
    <div className="empty">{t("无法读取无进展设置。")}</div>
  ) : (
    <div className="box">
      <div className="lrow">
        <span className="tx">
          <span className="lb">{t("长时间无进展时暂停任务")}</span>
          <span className="ds">{t("默认关闭：只在输入框上方提示，由你决定是否停止。开启后达到下面的条件会暂停任务，已完成的工作都会保留，发一条消息即可继续。")}</span>
        </span>
        <Switch
          data-action="progress-watch.pause"
          on={state.pause}
          busy={busy}
          label={t("长时间无进展时暂停任务")}
          onClick={() => void save({ ...pick(state), pause: !state.pause })}
        />
      </div>
      <div className="lrow threshold-row">
        <span className="tx">
          <span className="lb">{t("连续无进展轮数")}</span>
          <span className="ds">{t("没有改动文件、没有检查或任务步骤变化、没有读到新内容、也没有子代理返回的工具轮次。默认 {n}。", { n: state.defaultRounds })}</span>
        </span>
        <div className="threshold-control">
          <span className="unit-field">
            <input type="text" inputMode="numeric" aria-label={t("连续无进展轮数")} data-action-keydown="progress-watch.rounds" data-action-change="progress-watch.rounds"
              value={draft.rounds} disabled={busy}
              onChange={(e) => setDraft({ ...draft, rounds: digits(e.target.value) })}
              onBlur={() => commit(draft.rounds, (n) => n !== state.rounds && void save({ ...pick(state), rounds: n }))}
              onKeyDown={blurOnEnter} />
            <i>{t("轮")}</i>
          </span>
        </div>
      </div>
      <div className="lrow threshold-row">
        <span className="tx">
          <span className="lb">{t("输入 token 上限")}</span>
          <span className="ds">{t("与模型无关的后备：自上次有进展以来的累计输入达到上下文窗口的这个倍数时同样提示。默认 {n} 倍。", { n: state.defaultTokenMultiple })}</span>
        </span>
        <div className="threshold-control">
          <span className="unit-field">
            <input type="text" inputMode="numeric" aria-label={t("输入 token 上限")} data-action-keydown="progress-watch.token-multiple" data-action-change="progress-watch.token-multiple"
              value={draft.tokenMultiple} disabled={busy}
              onChange={(e) => setDraft({ ...draft, tokenMultiple: digits(e.target.value) })}
              onBlur={() => commit(draft.tokenMultiple, (n) => n !== state.tokenMultiple && void save({ ...pick(state), tokenMultiple: n }))}
              onKeyDown={blurOnEnter} />
            <i>{t("倍窗口")}</i>
          </span>
        </div>
      </div>
      {error && <div className="why">{error}</div>}
    </div>
  );

  return (
    <Group id="progress-watch" title={t("长时间无进展")}
      hint={t("判断只看宿主能观察到的效果，不看命令内容；提示和暂停都不会向模型发送任何内容，也不会拒绝任何调用。")}>
      {body}
    </Group>
  );
}

const digits = (v: string) => v.replace(/\D/g, "");
const pick = (s: ProgressWatchSettings) => ({ pause: s.pause, rounds: s.rounds, tokenMultiple: s.tokenMultiple });

// Enter commits by leaving the field, so one keystroke reaches the kernel once.
const blurOnEnter = (e: KeyboardEvent<HTMLInputElement>) => {
  if (e.key === "Enter") e.currentTarget.blur();
};
