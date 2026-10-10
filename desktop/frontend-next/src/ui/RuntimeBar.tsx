import { t } from "../i18n";
import { NOTICE_TEXT } from "../i18n/notices";
import { EXTENSION_SKIPPED, extensionSkippedVars } from "../i18n/extension_skipped";
import { INBOX_RECOVERED, inboxRecoveredVars } from "../i18n/inbox_recovered";
import type { RuntimeNotice } from "../state/session";
import type { SessionState } from "../state/session_types";
import { stallShown } from "../state/stall";
import { StallBar, type StallAction } from "./StallBar";

// Something the runtime has to report about itself. It sits with the composer
// rather than in the transcript — nobody in the conversation said it — and it is
// dismissible, because reading it is the whole of the response it needs. The
// headline is the kernel's code said in this window's language, and the kernel's
// own English stands in for a code nothing here maps.
export function RuntimeBar({ notices, onSeen, watch, onStall, onSettings }: {
  notices: RuntimeNotice[];
  onSeen: (id: string) => void;
  watch?: Pick<SessionState, "stall" | "stallMuted">;
  onStall?: (a: StallAction) => void;
  onSettings?: (section: string) => void;
}) {
  const stall = watch && stallShown(watch);
  return (
    <>
      {stall && onStall && <StallBar stall={stall} onAct={onStall} />}
      {notices.map((n) => {
        const skipped = n.code === EXTENSION_SKIPPED ? extensionSkippedVars(n.detail) : undefined;
        const recovered = n.code === INBOX_RECOVERED ? inboxRecoveredVars(n.detail) : undefined;
        const said = n.code && (n.code !== EXTENSION_SKIPPED || skipped) && (n.code !== INBOX_RECOVERED || recovered) ? NOTICE_TEXT[n.code] : undefined;
        return (
          <div key={n.id} className="rtbar" data-lvl={n.level} role="status">
            <span className="t">{said ? t(said, skipped ?? recovered) : n.text}</span>
            {n.detail && !skipped && !recovered && n.code !== EXTENSION_SKIPPED && n.code !== INBOX_RECOVERED && <span className="why">{n.detail}</span>}
            {skipped && onSettings && (
              <button data-action="settings.section" data-value="ext" onClick={() => onSettings("ext")}>{t("打开「工具与集成」")}</button>
            )}
            <button onClick={() => onSeen(n.id)}>{t("知道了")}</button>
          </div>
        );
      })}
    </>
  );
}
