import { useEffect, useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort, PlanApprovalSettings } from "../port/port";
import { SegChoice, SettingRow } from "./SettingRow";

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
    <>
      <SettingRow id="plan-approval" title={t("settings.plan_approval")} desc={t("settings.show_plans_before_running")} more={t("plan.a_submitted_plan_starts_without")}>
        <SegChoice label={t("settings.plan_approval")} value={state.auto ? "auto" : "ask"} action="plan-approval.auto" disabled={busy}
          onPick={() => void flip()} options={[["ask", t("settings.every_time")], ["auto", t("settings.not_needed")]]} />
      </SettingRow>
      {error && <div className="why">{error}</div>}
    </>
  );
}

// The plan-related settings share this entry point.
export { DefaultMode } from "./DefaultMode";
