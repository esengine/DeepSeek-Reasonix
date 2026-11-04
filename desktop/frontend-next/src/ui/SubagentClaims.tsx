import { useEffect, useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort, SubagentClaimsSettings } from "../port/port";
import { Switch } from "./Switch";

// One switch over how a sub-agent that named no write_paths holds its claim.
// On is the default and trades the fence for concurrency; off is strict, the
// whole workspace. It says what that costs beside the switch.
export function SubagentClaims({ port, onChanged }: { port: AgentPort; onChanged: () => void }) {
  const [state, setState] = useState<SubagentClaimsSettings | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    port
      .subagentClaims()
      .then(setState)
      .catch(() => setState(null));
  }, [port]);

  if (!state) return <div className="empty">{t("subagent.could_not_read_the_sub")}</div>;

  const flip = async () => {
    setBusy(true);
    setError("");
    try {
      setState(await port.saveSubagentClaims(!state.optimistic));
      onChanged();
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
          <span className="lb">{t("subagent.optimistic_mode_efficiency_first")}</span>
          <span className="ds">{t("subagent.a_sub_agent_that_names")}</span>
        </span>
        <Switch
          data-action="subagent-claims.optimistic"
          on={state.optimistic}
          busy={busy}
          label={t("subagent.optimistic_mode_efficiency_first")}
          onClick={() => void flip()}
        />
      </div>
      {error && <div className="why">{error}</div>}
    </div>
  );
}
