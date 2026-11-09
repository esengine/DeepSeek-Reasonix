import { useState } from "react";
import { host } from "../../port/host";
import { t } from "../../i18n";
import { Sym } from "../Sym";
import { answerSource } from "../source";
import { useViewer } from "../../state/viewer";
import type { ElicitProps } from "./ElicitCard";

export function ElicitURLCard({ item, onAnswer }: ElicitProps) {
  const origin = item.ask.origin!;
  const url = externalURL(origin.url!);
  const sealed = item.answered !== undefined;
  const answeredBy = answerSource(item.by, useViewer(), item.said);
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState(false);

  const open = async () => {
    if (sealed || busy || !url) return;
    setFailure(false);
    setBusy(true);
    try {
      await host().openExternal(origin.url!);
    } catch {
      setFailure(true);
    } finally {
      setBusy(false);
    }
  };
  const answer = async (action: string) => {
    if (sealed || busy) return;
    setBusy(true);
    try {
      await onAnswer(item.id, item.ask.id, [{ questionId: item.ask.questions[0].id, selected: [action] }]);
    } finally {
      setBusy(false);
    }
  };
  const outcome = item.answered?.[0]?.[0];

  return (
    <div className="call" data-k="ask" data-prompt={sealed ? "settled" : "pending"}>
      <div className="g"><Sym icon="external" /></div>
      <div className="c">
        <div className="hl"><span className="nm">{t("chat.mcp_server_name_is_asking_2", { name: origin.source })}</span></div>
        <div className="out">
          <div className="ask" data-sealed={sealed ? "" : undefined} aria-busy={busy}>
            <div className="ask-pane" data-on="">
              <div className="ask-hint">{t("chat.complete_the_interaction_in_your")}</div>
              {origin.message && <div className="ask-q">{origin.message}</div>}
              {url && <div className="ask-q"><b>{origin.urlHost || url.hostname}</b></div>}
              <div className="ask-hint" style={{ overflowWrap: "anywhere" }}>{origin.url}</div>
              {url && (url.protocol !== "https:" || url.hostname.includes("xn--")) && <div className="ask-hint" role="note">{t("chat.check_the_full_address_this")}</div>}
              {origin.urlLocal && <div className="ask-hint" role="note">{t("chat.this_link_targets_this_device")}</div>}
              {(failure || !url) && <div className="ask-hint" role="alert">{t("chat.could_not_open_the_browser")}</div>}
            </div>
            {sealed ? (
              <div className="ask-done"><b>{item.answeredElsewhere ? answeredBy || t("chat.handled_in_another_window_refer") : outcome === "accept" ? t("chat.completion_confirmed") : outcome === "cancel" ? t("common.cancelled") : t("chat.declined")}</b></div>
            ) : (
              <div className="ask-foot">
                <button className="dismiss" data-action="ask.answer" data-value="cancel" disabled={busy} onClick={() => void answer("cancel")}>{t("common.cancel")}</button>
                <button className="dismiss" data-action="ask.answer" data-value="decline" disabled={busy} onClick={() => void answer("decline")}>{t("chat.decline")}</button>
                <button className="btn" data-action="external.open" disabled={busy || !url} onClick={() => void open()}>{t("chat.open_browser")}</button>
                <button className="btn" data-primary data-action="ask.answer" data-value="accept" disabled={busy} onClick={() => void answer("accept")}>{t("chat.completed_continue")}</button>
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

function externalURL(raw: string): URL | null {
  try {
    const url = new URL(raw);
    return (url.protocol === "http:" || url.protocol === "https:") && !url.username && !url.password ? url : null;
  } catch {
    return null;
  }
}
