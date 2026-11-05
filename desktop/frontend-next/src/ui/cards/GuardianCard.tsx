import type { Guardian } from "../../port/wire";
import { t } from "../../i18n";
import { LineRow } from "./LineRow";
import { NoteCard } from "./NoteCard";

export function GuardianCard({ g }: { g: Guardian }) {
  const risk = g.risk_level ?? "unknown";
  const riskLabel = risk === "low" ? t("chat.low_risk") : risk === "medium" ? t("chat.medium_risk") : risk === "high" ? t("common.high_risk") : risk === "critical" ? t("chat.critical_risk") : t("chat.risk_unknown");
  if (g.outcome === "allow" && (risk === "low" || risk === "unknown")) {
    return <LineRow glyph="⊛" level="ok" title={`${t("chat.guardian_review")} · ${t("chat.allowed")}`} subject={g.subject} subjectTitle={g.subject} meta={riskLabel} detail={g.rationale || undefined} />;
  }
  const bars = risk === "low" ? 1 : risk === "medium" ? 2 : risk === "unknown" ? 0 : 3;
  const verdict = g.outcome === "deny" ? t("chat.blocked") : g.outcome === "allow" ? t("chat.allowed") : g.outcome;
  return (
    <NoteCard glyph="⊛" level={g.outcome === "deny" ? "err" : "warn"}
      lead={<>{t("chat.guardian_review")} · <span className="note-verdict">{verdict}</span></>}
      tail={<>
        <span className="note-risk">{riskLabel}</span>
        <span className="note-gauge" aria-hidden="true">{[1, 2, 3].map((n) => <i key={n} data-on={n <= bars ? "" : undefined} />)}</span>
      </>}>
      <div className="note-subject" title={g.subject}>{g.subject}</div>
      {g.rationale && <div className="note-text">{g.rationale}</div>}
    </NoteCard>
  );
}
