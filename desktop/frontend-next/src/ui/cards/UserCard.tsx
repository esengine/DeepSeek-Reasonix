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
    {failed ? <span>{t("图片不可用")}</span> : <ImagePreview src={src} name={name} className="user-image-trigger">
      <img src={src} alt={name} loading="lazy" width={240} height={160} onError={() => setFailed(true)} />
    </ImagePreview>}
  </div>;
}

// The thumbnail stands for a stored image, so the token that carried it into
// the turn is not drawn again as prose. A removed token leaves a placeholder,
// and only the whitespace beside one is collapsed; any other message text is
// returned byte for byte, so paragraph breaks and indentation survive.
function stripAttachmentTokens(text: string, paths: string[]): string {
  const covered = paths.filter(Boolean).map((p) => p.replace(/[.*+()?^${}|[\]\\]/g, "\\$&"));
  if (covered.length === 0) return text;
  const token = new RegExp("(?:^|\\s)@(?:" + covered.join("|") + ")(?=$|\\s)", "g");
  const HOLD = "\u0000";
  return text
    .replace(token, HOLD)
    .replace(/^\u0000[ \t]?/, "")
    .replace(/\u0000[ \t]/g, " ")
    .replace(/\u0000/g, "");
}

export function UserCard({
  item,
  port,
  cp,
  onResend,
  onPrepareRewind,
  onCommitRewind,
}: {
  item: Extract<Item, { t: "user" }>;
  port?: Pick<AgentPort, "workspaceImageURL">;
  cp?: Checkpoint;
  onResend?: (turn: number, text: string) => Promise<void>;
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
  // What the row says is the words without the paths the thumbnails already
  // stand for; empty means the message was only its attachments, so no bubble.
  const shown = useMemo(() => stripAttachmentTokens(item.text, images), [item.text, images]);

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

  return (
    <div className="call" data-k="me" data-pending={item.pending ? "" : undefined}>
      <div className="g">
        <span className="sym">{t("你")}</span>
      </div>
      <div className="c">
        {(item.steer || source) && <div className="hl user-hl">
          {/* It reached the model inside a turn already running, which is why
              there is no checkpoint on this row to rewind to. */}
          {item.steer && <span className="steermark">{t("插话")}</span>}
          {source && <span className="viamark">{source}</span>}
        </div>}
        <div className="out">
          {draft === null ? (
            <>
              {port && images.length > 0 && <div className="user-images">{images.map((path) => {
                const src = port.workspaceImageURL(path);
                return <SavedImage key={src} path={path} src={src} />;
              })}</div>}
              {pin ? <SkillPinView pin={pin} /> : shown ? <div className="txt" dir="auto">{shown}</div> : null}
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
                aria-label={t("改写这条消息")}
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
                  {sending ? t("正在重发…") : t("改完重发")}
                </button>
                <button className="dismiss" data-action="turn.edit" data-value="cancel" onClick={() => setDraft(null)}>
                  {t("取消")}
                </button>
                <span className="hint">{t("这一轮之后的记录会被丢弃")}</span>
              </div>
            </div>
          )}
        </div>
        <div className="user-acts">
          {item.at && <time className="user-time" dateTime={new Date(item.at).toISOString()} title={new Date(item.at).toLocaleString()}>{clock(item.at)}</time>}
          <CopyButton text={item.text} iconOnly showFeedback label={t("复制")} />
          {reopen && (
            <button type="button" className="reask-open" data-action="turn.edit" data-target={item.id}
              title={t("改写这条消息并重新发送")} aria-label={t("改写")}
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
