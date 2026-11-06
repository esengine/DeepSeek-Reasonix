import { memo, useState } from "react";
import { t } from "../../i18n";
import type { RememberedFact } from "../../state/session";
import { NoteCard } from "./NoteCard";

const ACTIVATION: Record<string, string> = {
  pinned: "chat.remember_pinned",
  relevant: "chat.remember_relevant",
};

const SCOPE: Record<string, string> = { project: "chat.remember_scope_project", global: "chat.remember_scope_global" };

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
      meta={forgotten ? `· ${t(SCOPE[m.scope] ?? m.scope)}` : `· ${t(SCOPE[m.scope] ?? m.scope)} · ${t(ACTIVATION[m.activation] ?? m.activation)}`}
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
