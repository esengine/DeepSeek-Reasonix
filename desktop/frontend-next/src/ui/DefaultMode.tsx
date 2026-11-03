import { useEffect, useState, type ReactNode } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort, DefaultModeSettings } from "../port/port";
import { Group } from "./Group";
import { Switch } from "./Switch";

// One switch over how a new conversation starts: in plan mode, or as usual. Off
// is the default. It changes only the starting mode; the mode picker still
// switches it, and a conversation that already exists keeps its own.
export function DefaultMode({ port }: { port: AgentPort }) {
  const [state, setState] = useState<DefaultModeSettings | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    port
      .defaultMode()
      .then(setState)
      .catch(() => setState(null));
  }, [port]);

  const group = (body: ReactNode) => (
    <Group id="default-mode" title={t("common.howNewConversationsStart")} hint={t("defaultMode.whichModeANewConversation")}>
      {body}
    </Group>
  );

  if (!state) return group(<div className="empty">{t("defaultMode.couldNotReadTheSetting")}</div>);

  const flip = async () => {
    setBusy(true);
    setError("");
    try {
      setState(await port.saveDefaultMode(!state.plan));
    } catch (e) {
      setError(reason(e));
    } finally {
      setBusy(false);
    }
  };

  return group(
    <div className="box">
      <div className="lrow">
        <span className="tx">
          <span className="lb">{t("defaultMode.startNewConversationsInPlan")}</span>
          <span className="ds">{t("defaultMode.aNewConversationPlansFirst")}</span>
        </span>
        <Switch
          data-action="default-mode.plan"
          on={state.plan}
          busy={busy}
          label={t("defaultMode.startNewConversationsInPlan")}
          onClick={() => void flip()}
        />
      </div>
      {error && <div className="why">{error}</div>}
    </div>,
  );
}
