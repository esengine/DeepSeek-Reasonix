import type { ReactNode } from "react";
import { StudioIcon, type StudioIconName } from "./StudioIcon";

/** One empty or failed state of a workbench page, as the WB-Parts board draws
 *  them: what it is, why, and what can be done about it. */
export function WorkbenchState({ icon, title, detail, alert = false, children }: {
  icon: StudioIconName; title: string; detail?: string; alert?: boolean; children?: ReactNode;
}) {
  return (
    <div className="workbench-state" role={alert ? "alert" : "status"}>
      <StudioIcon name={icon} />
      <b>{title}</b>
      {detail && <p>{detail}</p>}
      {children && <div className="workbench-state-acts">{children}</div>}
    </div>
  );
}
