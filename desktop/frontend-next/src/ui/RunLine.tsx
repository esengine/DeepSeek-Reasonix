import type { ReactNode } from "react";
import { t } from "../i18n";
import { RMark } from "./RMark";
import { RunClock } from "./RunClock";
import { RunTokens } from "./RunTokens";

export function RunLine({ label, running, blocked, since, sent, received, estimated, children }: {
  label: string;
  running: boolean;
  blocked: boolean;
  since?: number;
  sent: number;
  received: number;
  estimated: boolean;
  children?: ReactNode;
}) {
  return (
    <div className="compose-context">
      {children}
      <div
        className="studio-runstate"
        role="status"
        aria-live="polite"
        data-running={running && !blocked ? "" : undefined}
        data-waiting={blocked ? "" : undefined}
        data-idle={running || blocked ? undefined : ""}
      >
        <span className="studio-runlabel"><RMark /><span className="sr-only">{t(label)}</span>{running && since !== undefined && <RunClock since={since} />}</span>
        <RunTokens sent={sent} received={received} estimated={estimated} />
      </div>
    </div>
  );
}
