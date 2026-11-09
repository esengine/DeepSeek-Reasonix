import type { StudioIconName } from "../StudioIcon";
import type { ReactNode } from "react";
import { Sym } from "../Sym";

export function NoteCard({ icon, level, gone, lead, strong, meta, tail, action, children }: {
  icon: StudioIconName;
  level?: "warn" | "err";
  gone?: boolean;
  lead: ReactNode;
  strong?: ReactNode;
  meta?: ReactNode;
  tail?: ReactNode;
  action?: ReactNode;
  children?: ReactNode;
}) {
  return (
    <div className="note" data-lvl={level} data-lead={strong ? "" : undefined} data-gone={gone ? "" : undefined}>
      <div className="note-hd">
        <Sym icon={icon} />
        <span className="note-nm">{lead}</span>
        {strong && <b className="note-strong">{strong}</b>}
        {meta && <span className="note-meta">{meta}</span>}
        {tail}
        {action}
      </div>
      {children}
    </div>
  );
}
