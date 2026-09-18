import { useCallback, useState } from "react";
import { app } from "../lib/bridge";
import { useI18n, type Locale } from "../lib/i18n";
import type { WireImageIdentity, WireImageRecoveryAction } from "../lib/imageRecoveryTypes";
import "./ImageRecoveryNotice.css";

type Props = {
  tabId?: string;
  recoveryId: string;
  recovery: WireImageRecoveryAction;
};

const copy = {
  en: {
    title: "Choose unavailable images",
    body: "The provider rejected an image but did not identify which one. Select only the images to exclude from future model requests.",
    candidate: (request: number, image: number) => `Request image ${request} (image ${image} in its message)`,
    action: "Exclude selected images",
    saving: "Saving...",
    saved: "The selected images were excluded. Send a new message when you are ready to continue.",
    unavailable: "Image recovery is unavailable for this tab.",
  },
  zh: {
    title: "选择不可用的图片",
    body: "提供方拒绝了图片，但未指出具体是哪一张。请只勾选需要从后续模型请求中隔离的图片。",
    candidate: (request: number, image: number) => `请求图片 ${request}（所在消息中的第 ${image} 张）`,
    action: "隔离所选图片",
    saving: "正在保存…",
    saved: "所选图片已隔离。需要继续时请发送一条新消息。",
    unavailable: "当前标签无法执行图片恢复。",
  },
  "zh-TW": {
    title: "選擇不可用的圖片",
    body: "提供方拒絕了圖片，但未指出具體是哪一張。請只勾選需要從後續模型請求中隔離的圖片。",
    candidate: (request: number, image: number) => `請求圖片 ${request}（所在訊息中的第 ${image} 張）`,
    action: "隔離所選圖片",
    saving: "正在儲存…",
    saved: "所選圖片已隔離。需要繼續時請傳送一則新訊息。",
    unavailable: "目前分頁無法執行圖片恢復。",
  },
} satisfies Record<Locale, {
  title: string;
  body: string;
  candidate: (request: number, image: number) => string;
  action: string;
  saving: string;
  saved: string;
  unavailable: string;
}>;

export default function ImageRecoveryNotice({ tabId, recoveryId, recovery }: Props) {
  const { locale } = useI18n();
  const text = copy[locale];
  const [selected, setSelected] = useState<Record<string, boolean>>({});
  const [busy, setBusy] = useState(false);
  const [resolved, setResolved] = useState(false);
  const [error, setError] = useState("");
  const keyOf = (identity: WireImageIdentity) => `${identity.messageId}:${identity.imageOrdinal}:${identity.contentDigest}`;
  const choices = recovery.candidates.filter(candidate => selected[keyOf(candidate.identity)]).map(candidate => candidate.identity);
  const isolate = useCallback(async () => {
    if (!tabId) throw new Error(text.unavailable);
    await app.ResolveImageRecoveryForTab(tabId, recoveryId, choices);
  }, [choices, recoveryId, tabId, text.unavailable]);
  return <div className="chat-notice chat-notice--image-recovery" role="status" data-level="warn">
    <div><strong>{text.title} </strong>{text.body}</div>
    <div className="chat-notice__image-options">
      {recovery.candidates.map((candidate, index) => {
        const key = keyOf(candidate.identity);
        return <label key={key} className="chat-notice__image-option">
          <input type="checkbox" checked={Boolean(selected[key])} disabled={busy || resolved}
            onChange={event => setSelected(current => ({ ...current, [key]: event.target.checked }))} />
          <span>{text.candidate(index + 1, candidate.identity.imageOrdinal + 1)}</span>
        </label>;
      })}
    </div>
    {error && <div role="alert">{error}</div>}
    {resolved ? <div>{text.saved}</div> : <button className="btn" disabled={busy || choices.length === 0} onClick={() => {
      setBusy(true);
      setError("");
      void isolate().then(() => setResolved(true), reason => setError(String((reason as Error)?.message ?? reason))).finally(() => setBusy(false));
    }}>{busy ? text.saving : text.action}</button>}
  </div>;
}
