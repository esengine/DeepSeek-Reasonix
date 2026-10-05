import { t } from "../i18n";
import { NOTICE_TEXT } from "../i18n/notices";
import type { RuntimeNotice } from "../state/session";
import type { SessionState } from "../state/session_types";
import { stallShown } from "../state/stall";
import { StallBar, type StallAction } from "./StallBar";

// Something the runtime has to report about itself. It sits with the composer
// rather than in the transcript — nobody in the conversation said it — and it is
// dismissible, because reading it is the whole of the response it needs. The
// headline is the kernel's code said in this window's language, and the kernel's
// own English stands in for a code nothing here maps.
export function RuntimeBar({ notices, onSeen, watch, onStall }: {
  notices: RuntimeNotice[];
  onSeen: (id: string) => void;
  watch?: Pick<SessionState, "stall" | "stallMuted">;
  onStall?: (a: StallAction) => void;
}) {
  const stall = watch && stallShown(watch);
  return (
    <>
      {stall && onStall && <StallBar stall={stall} onAct={onStall} />}
      {notices.map((n) => {
        const said = n.code ? NOTICE_TEXT[n.code] : undefined;
        return (
          <div key={n.id} className="rtbar" data-lvl={n.level} role="status">
            <span className="t">{said ? t(said) : n.text}</span>
            {n.detail && <span className="why">{n.detail}</span>}
            <button onClick={() => onSeen(n.id)}>{t("知道了")}</button>
          </div>
        );
      })}
    </>
  );
}
