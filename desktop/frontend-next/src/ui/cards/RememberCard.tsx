import { memo, useState } from "react";
import { t } from "../../i18n";
import type { RememberedFact } from "../../state/session";
import { LineRow } from "./LineRow";

// Saving a fact is the only tool call that changes what the agent will do in
// later sessions, and it happens without being asked. Left as an ordinary step
// it scrolls past between a grep and a read — and weeks later there is no way to
// connect the behaviour to the moment it was learned. So it says what it took
// away, when that will apply, and offers the one-click take-back that is only
// cheap right now, while the user still remembers the conversation.
const ACTIVATION: Record<string, string> = {
  pinned: "rememberCard.carriedOnEveryTurn",
  relevant: "rememberCard.retrievedWhenRelevant",
};

const SCOPE: Record<string, string> = { project: "common.thisProjectOnly", global: "rememberCard.inAllProjects" };

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
    <LineRow
      glyph="◇"
      gone={forgotten}
      title={t(forgotten ? "rememberCard.forgotten" : "rememberCard.remembered")}
      subject={m.title}
      subjectTitle={m.title}
      meta={`${t(SCOPE[m.scope] ?? m.scope)} · ${t(ACTIVATION[m.activation] ?? m.activation)}`}
      action={!forgotten && (
        <button className="act" data-action="memory.forget" data-target={m.name} disabled={busy} onClick={forget}>
          {t(busy ? "rememberCard.forgetting" : "common.forget")}
        </button>
      )}
      detail={m.description || undefined}
    />
  );
});
