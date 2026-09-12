// Adapted from DeepSeek Harness c291e7961a, ui-chat/ContextInjectionRow.tsx (MIT).
import { useState, type ReactNode } from "react";
import { ClipboardList } from "lucide-react";
import { DisclosureRow } from "./DisclosureRow";
import css from "./ContextInjectionRow.styles";

export function ContextInjectionRow({ title, summary, children, beforeToggle }: {
  title: string; summary?: string; children: ReactNode; beforeToggle?: () => void;
}) {
  const [open, setOpen] = useState(false);
  return <DisclosureRow className={css.root} icon={<ClipboardList size={14} />} title={title}
    chevronClassName={css.chevron} open={open} expandable expandOnRowClick keepContentWhenOpen
    onToggle={() => { beforeToggle?.(); setOpen(value => !value); }}
    collapsedContent={summary && <><span className={css.sep} aria-hidden /><span className={css.summary}>{summary}</span></>}>
    <div className={css.body}>{children}</div>
  </DisclosureRow>;
}
