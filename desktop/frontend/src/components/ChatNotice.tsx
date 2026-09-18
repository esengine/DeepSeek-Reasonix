import { lazy, Suspense, useState } from "react";
import type { ChatNode } from "../lib/chatViewSource";
import type { ChatScrollController } from "../lib/chatScrollController";
import { useT } from "../lib/i18n";
import type { ChatActions } from "./ChatNodes";
import { ContextInjectionRow } from "./harness-chat/ContextInjectionRow";

const ImageRecoveryNotice = lazy(() => import("./ImageRecoveryNotice"));

type Props = {
  node: Extract<ChatNode, { kind: "notice" }>;
  actions: ChatActions;
  scroll: ChatScrollController;
  tabId?: string;
};

function NoticeDisclosure({ label, children }: { label: string; children: import("react").ReactNode }) {
  const [open, setOpen] = useState(false);
  return <details className="chat-notice" open={open} onToggle={event => setOpen(event.currentTarget.open)}>
    <summary>{label}</summary>{open && children}
  </details>;
}

export default function ChatNotice({ node, actions, scroll, tabId }: Props) {
  const t = useT();
  const item = node.item;
  const summary = item.completionSummary;
  if (item.code === "capability_proxy_audit") return <NoticeDisclosure label={t("chat.details")}><pre>{item.text}{"\n"}{item.detail}</pre></NoticeDisclosure>;
  if (summary && !summary.mutations && !summary.changed_files && !summary.checks_passed && !summary.checks_failed) return null;
  if (item.action === "isolate_images" && item.imageRecovery && item.recoveryId) return <Suspense fallback={<div className="chat-notice" role="status" data-level="warn">{t("chat.loading")}</div>}>
    <ImageRecoveryNotice tabId={tabId} recoveryId={item.recoveryId} recovery={item.imageRecovery} />
  </Suspense>;
  if (item.level === "warn" || item.action === "recover_context") return <div className="chat-notice" role="status" data-level={item.level}>
    {item.title && <strong>{item.title} </strong>}{item.text}
    {summary && <details className="chat-notice__details"><summary>{t("chat.details")}</summary><pre>{JSON.stringify(summary, null, 2)}</pre></details>}
    {item.detail && <NoticeDisclosure label={t("chat.details")}><pre>{item.detail}</pre></NoticeDisclosure>}
    {item.action === "recover_context" && item.recoveryId && <button className="btn" onClick={() => actions.recover(item.recoveryId!)}>{t("notice.protocolRecoveryAction")}</button>}
  </div>;
  return <ContextInjectionRow title={item.title || t(item.decisionReceipt ? "chat.decision" : summary ? "chat.record" : "chat.notice")}
    summary={item.decisionReceipt ? undefined : item.text.split("\n")[0]} beforeToggle={scroll.beforeChange}>
    <pre>{item.text}{item.detail ? `\n${item.detail}` : ""}{summary ? `\n${JSON.stringify(summary, null, 2)}` : ""}</pre>
  </ContextInjectionRow>;
}
