import { memo, useState } from "react";
import { t } from "../../i18n";
import type { RememberedFact } from "../../state/session";
import { Sym } from "../Sym";

// Saving a fact is the only tool call that changes what the agent will do in
// later sessions, and it happens without being asked. Left as an ordinary step
// it scrolls past between a grep and a read — and weeks later there is no way to
// connect the behaviour to the moment it was learned. So it says what it took
// away, when that will apply, and offers the one-click take-back that is only
// cheap right now, while the user still remembers the conversation.
const ACTIVATION: Record<string, string> = {
  pinned: "后续每轮均会携带",
  relevant: "后续相关时会被检索",
};

const SCOPE: Record<string, string> = { project: "仅当前项目", global: "在所有项目里" };

export const RememberCard = memo(function RememberCard({
  m, forgotten, onForget,
}: {
  m: RememberedFact;
  forgotten?: boolean;
  onForget: (name: string) => void;
}) {
  const [busy, setBusy] = useState(false);
  const forget = () => {
    setBusy(true);
    onForget(m.name);
  };
  return (
    <div className="call" data-k="mem" data-gone={forgotten ? "" : undefined}>
      <div className="g">
        <Sym glyph="◇" />
        <span className="line" />
      </div>
      <div className="c">
        <div className="hl">
          <span className="nm">{t(forgotten ? "已经忘掉" : "记住了")}</span>
          <span className="tag">{t("记忆")}</span>
          <span className="aside">
            {SCOPE[m.scope] ?? m.scope} · {ACTIVATION[m.activation] ?? m.activation}
          </span>
          {!forgotten && (
            <button className="act" data-action="memory.forget" disabled={busy} onClick={forget}>
              {t(busy ? "忘掉中…" : "忘记")}
            </button>
          )}
        </div>
        <div className="out remember">
          <span className="ti">{m.title}</span>
          {m.description && <span className="ds">{m.description}</span>}
        </div>
      </div>
    </div>
  );
});
