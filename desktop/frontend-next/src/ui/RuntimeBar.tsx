import { t } from "../i18n";
import { EXTENSION_SKIPPED } from "../i18n/extension_skipped";
import { leftoverDetail, noticeWording } from "../i18n/notice_wording";
import type { RuntimeNotice } from "../state/session";
import type { SessionState } from "../state/session_types";
import { stallShown } from "../state/stall";
import { StudioIcon } from "./StudioIcon";
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
        const wording = noticeWording(n.code, n.detail);
        const why = leftoverDetail(n.code, n.detail, wording?.vars);
        return (
          <div key={n.id} className="rtbar" data-lvl={n.level} role="status">
            <StudioIcon name={n.level === "warn" ? "warning" : "info"} />
            <span className="t">{wording ? t(wording.text, wording.vars) : n.text}</span>
            {why && <span className="why">{why}</span>}
            {n.code === EXTENSION_SKIPPED && wording?.vars && onSettings && (
              <button data-action="settings.section" data-value="ext" onClick={() => onSettings("extensions")}>{t("app.open_tools_and_integrations")}</button>
            )}
            <button onClick={() => onSeen(n.id)}>{t("common.got_it")}</button>
          </div>
        );
      })}
    </>
  );
}
