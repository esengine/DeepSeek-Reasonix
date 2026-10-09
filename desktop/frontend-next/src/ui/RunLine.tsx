import { t } from "../i18n";
import { RMark } from "./RMark";
import { RunTokens } from "./RunTokens";

export function RunLine({ label, running, blocked, sent, received, estimated }: {
  label: string;
  running: boolean;
  blocked: boolean;
  sent: number;
  received: number;
  estimated: boolean;
}) {
  return (
    <div
      className="studio-runstate"
      role="status"
      aria-live="polite"
      data-running={running && !blocked ? "" : undefined}
      data-waiting={blocked ? "" : undefined}
      data-idle={running || blocked ? undefined : ""}
    >
      <span className="studio-runlabel"><RMark /><span>{t(label)}</span></span>
      <RunTokens sent={sent} received={received} estimated={estimated} />
    </div>
  );
}
