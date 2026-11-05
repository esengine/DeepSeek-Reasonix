import { memo, useState } from "react";
import { t } from "../../i18n";
import type { RememberedFact } from "../../state/session";
import { NoteCard } from "./NoteCard";

const ACTIVATION: Record<string, string> = {
  pinned: "后续每轮均会携带",
  relevant: "后续相关时会被检索",
};

const SCOPE: Record<string, string> = { project: "ext.this_project_only", global: "在所有项目里" };

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
    <NoteCard
      glyph="◇"
      gone={forgotten}
      lead={t(forgotten ? "chat.forgotten" : "chat.remembered")}
      strong={m.title}
      meta={`· ${t(SCOPE[m.scope] ?? m.scope)} · ${t(ACTIVATION[m.activation] ?? m.activation)}`}
      action={!forgotten && (
        <button className="note-act" data-action="memory.forget" data-target={m.name} disabled={busy} onClick={forget}>
          {t(busy ? "chat.forgetting" : "chat.forget")}
        </button>
      )}
    >
      {m.description && <div className="note-text">{m.description}</div>}
    </NoteCard>
  );
});
