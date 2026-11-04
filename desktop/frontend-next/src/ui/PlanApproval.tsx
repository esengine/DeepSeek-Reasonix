import { useEffect, useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort, PlanApprovalSettings } from "../port/port";
import { Switch } from "./Switch";

// One switch over whether a submitted plan waits for the user's click. Off is
// the default. On releases only that click: tool approvals, the sandbox and
// deny rules still apply while the plan runs, and the transcript notes each
// automatic start.
export function PlanApproval({ port }: { port: AgentPort }) {
  const [state, setState] = useState<PlanApprovalSettings | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    port
      .planApproval()
      .then(setState)
      .catch(() => setState(null));
  }, [port]);

  if (!state) return <div className="empty">{t("plan.could_not_read_the_plan")}</div>;

  const flip = async () => {
    setBusy(true);
    setError("");
    try {
      setState(await port.savePlanApproval(!state.auto));
    } catch (e) {
      setError(reason(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="box">
      <div className="lrow">
        <span className="tx">
          <span className="lb">{t("plan.approve_plans_automatically")}</span>
          <span className="ds">{t("plan.a_submitted_plan_starts_without")}</span>
        </span>
        <Switch
          data-action="plan-approval.auto"
          on={state.auto}
          busy={busy}
          label={t("plan.approve_plans_automatically")}
          onClick={() => void flip()}
        />
      </div>
      {error && <div className="why">{error}</div>}
    </div>
  );
}

// The plan-related settings share this entry point.
export { DefaultMode } from "./DefaultMode";
