import type { StudioIconName } from "../StudioIcon";
import type { ReactNode } from "react";
import { Sym } from "../Sym";

/** The inline step of the transcript grammar: a bare status icon, the title, the
 *  mono subject and the meta on one row over a hairline, with an optional detail
 *  line under it. No box, no icon square, no rail. */
export function LineRow({ icon, title, subject, subjectTitle, meta, action, detail, level, gone }: {
  icon: StudioIconName;
  title: ReactNode;
  subject?: ReactNode;
  subjectTitle?: string;
  meta?: ReactNode;
  action?: ReactNode;
  detail?: ReactNode;
  level?: "ok" | "warn" | "err";
  gone?: boolean;
}) {
  return (
    <div className="line-row" data-lvl={level} data-gone={gone ? "" : undefined}>
      <div className="lr-main">
        <Sym icon={icon} />
        <span className="lr-nm">{title}</span>
        {subject && <span className="lr-arg" title={subjectTitle}>{subject}</span>}
        {meta && <span className="lr-meta">{meta}</span>}
        {action}
      </div>
      {detail && <div className="lr-detail">{detail}</div>}
    </div>
  );
}
