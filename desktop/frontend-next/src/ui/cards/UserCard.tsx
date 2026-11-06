import { useEffect, useMemo, useRef, useState } from "react";
import type { AgentPort, Checkpoint, RewindPlan, RewindResult, RewindScope } from "../../port/port";
import type { Item } from "../../state/session";
import { RewindControl } from "./RewindControl";
import { SkillPinView } from "./SkillPinView";
import { parseSkillPin } from "./skillpin";
import { CopyButton } from "../CopyButton";
import { reason } from "../../i18n/kernel";
import { t } from "../../i18n";
import { clock } from "../../i18n/format";
import { StudioIcon } from "../StudioIcon";
import { messageSource } from "../source";
import { useSendShortcut } from "../../state/prefs";
import { sendAria, sendHint, sendsOnKey } from "../sendkey";
import { useIme } from "../ime";
import { touchKeyboard } from "../touchKeyboard";
import { useFitHeight } from "../fitHeight";
import { useViewer } from "../../state/viewer";
import { ImagePreview } from "../ImagePreview";

function SavedImage({ path, src }: { path: string; src: string }) {
  const [failed, setFailed] = useState(false);
  const name = path.split("/").at(-1) ?? path;
  return <div className="user-image">
    {failed ? <span>{t("chat.image_unavailable")}</span> : <ImagePreview src={src} name={name} className="user-image-trigger">
      <img src={src} alt={name} loading="lazy" width={240} height={160} onError={() => setFailed(true)} />
    </ImagePreview>}
  </div>;
}

export function UserCard({
  item,
  port,
  cp,
  onResend,
  onWithdraw,
  onPrepareRewind,
  onCommitRewind,
}: {
  item: Extract<Item, { t: "user" }>;
  port?: Pick<AgentPort, "workspaceImageURL">;
  cp?: Checkpoint;
  onResend?: (turn: number, text: string) => Promise<void>;
  onWithdraw?: (itemId: string) => void | Promise<void>;
  onPrepareRewind?: (turn: number, scope: RewindScope) => Promise<RewindPlan>;
  onCommitRewind?: (planId: string, text?: string) => Promise<RewindResult>;
}) {
  // A rewind needs a turn the kernel claimed, and a queued line has not
  // happened yet — there is nothing behind it to take back.
  const shortcut = useSendShortcut();
  const ime = useIme();
  const editable = !!onResend && !!cp && !item.pending;
  const [draft, setDraft] = useState<string | null>(null);
  const [sending, setSending] = useState(false);
  const [failed, setFailed] = useState("");
  const box = useRef<HTMLTextAreaElement>(null);
  const source = messageSource(item.via, useViewer());
  const reopen = editable && draft === null;
  const rewind = !!(cp && onPrepareRewind && onCommitRewind);
  const pin = useMemo(() => parseSkillPin(item.text), [item.text]);
  const images = useMemo(() => [...new Set((item.attachments ?? []).map((a) => a.path))], [item.attachments]);

  useEffect(() => {
    const el = box.current;
    if (draft === null || !el) return;
    el.focus();
    el.setSelectionRange(el.value.length, el.value.length);
  }, [draft === null]);

  useFitHeight(box, draft ?? "");

  const resend = () => {
    const text = (draft ?? "").trim();
    if (!editable || !text || sending) return;
    setSending(true);
    setFailed("");
    onResend!(cp!.turn, text)
      .then(() => setDraft(null))
      .catch((e) => setFailed(reason(e)))
      .finally(() => setSending(false));
  };

  if (item.pending && item.queued === "steer" && item.itemId && onWithdraw) {
    const itemId = item.itemId;
    return (
      <div className="steerwait" data-pending="">
        <div className="sw-hd">
          {t("composer.steer_waiting")}
          <button type="button" className="sw-undo" data-action="steer.withdraw" data-target={itemId} onClick={() => void onWithdraw(itemId)}>
            {t("composer.withdraw")}
          </button>
        </div>
        <div className="sw-txt" dir="auto">{item.text}</div>
      </div>
    );
  }

  return (
    <div className="call" data-k="me" data-pending={item.pending ? "" : undefined}>
      <div className="g">
        <span className="sym">{t("chat.you")}</span>
      </div>
      <div className="c">
        {(item.steer || source) && <div className="hl user-hl">
          {/* It reached the model inside a turn already running, which is why
              there is no checkpoint on this row to rewind to. */}
          {item.steer && <span className="steermark">{t("common.steer")}</span>}
          {source && <span className="viamark">{source}</span>}
        </div>}
        <div className="out">
          {draft === null ? (
            <>
              {port && images.length > 0 && <div className="user-images">{images.map((path) => {
                const src = port.workspaceImageURL(path);
                return <SavedImage key={src} path={path} src={src} />;
              })}</div>}
              {pin ? <SkillPinView pin={pin} /> : <div className="txt" dir="auto">{item.text}</div>}
            </>
          ) : (
            <div className="reask">
              <textarea
                ref={box}
                dir="auto"
                data-action-change="turn.edit"
                data-action-keydown="turn.resend"
                data-target={item.id}
                value={draft}
                rows={1}
                readOnly={sending}
                {...ime.handlers}
                aria-keyshortcuts={sendAria(shortcut, touchKeyboard())}
                title={sendHint(shortcut, false, touchKeyboard())}
                aria-label={t("chat.rewrite_this_message")}
                onChange={(ev) => setDraft(ev.target.value)}
                onKeyDown={(ev) => {
                  if (ime.isIme(ev.nativeEvent)) {
                    if (ev.key === "Enter") ev.preventDefault();
                    if (ev.key === "Escape") ev.stopPropagation();
                    return;
                  }
                  if (ev.key === "Escape") {
                    ev.preventDefault();
                    ev.stopPropagation();
                    setDraft(null);
                  } else if (sendsOnKey(ev, shortcut, touchKeyboard())) {
                    ev.preventDefault();
                    resend();
                  }
                }}
              />
              {failed && <div className="txt bad">{failed}</div>}
              <div className="reask-ft">
                <button
                  className="btn"
                  data-action="turn.resend"
                  data-target={item.id}
                  disabled={sending || !draft.trim()}
                  onClick={resend}
                >
                  {sending ? t("chat.sending") : t("chat.send_the_edit")}
                </button>
                <button className="dismiss" data-action="turn.edit" data-value="cancel" onClick={() => setDraft(null)}>
                  {t("common.cancel")}
                </button>
                <span className="hint">{t("chat.everything_after_this_turn_is")}</span>
              </div>
            </div>
          )}
        </div>
        <div className="user-acts">
          {item.at && <time className="user-time" dateTime={new Date(item.at).toISOString()} title={new Date(item.at).toLocaleString()}>{clock(item.at)}</time>}
          <CopyButton text={item.text} iconOnly showFeedback label={t("common.copy")} />
          {reopen && (
            <button type="button" className="reask-open" data-action="turn.edit" data-target={item.id}
              title={t("chat.rewrite_this_message_and_send")} aria-label={t("chat.rewrite")}
              onClick={() => setDraft(item.text)}>
              <StudioIcon name="edit" />
            </button>
          )}
          {rewind && (
            <RewindControl cp={cp!} compact onPrepare={onPrepareRewind!} onCommit={(planId) => onCommitRewind!(planId, item.text)} />
          )}
        </div>
      </div>
    </div>
  );
}
