import { useEffect, useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort, ToolSettings } from "../port/port";
import { Group } from "./Group";
import { Switch } from "./Switch";
import { toolBlurb } from "./toolBlurbs";

// Every tool the model is offered, each with a switch. Turning one off removes
// it from every request in conversations opened afterwards and refuses a call to
// it; a tool another feature needs has no switch and says which feature.
// What needs a tool, by the kernel's feature id; an id this build does not know is shown as sent.
function feature(id: string): string {
  if (id === "goal_mode") return t("toolList.goalMode");
  if (id === "deferred_mcp_tools") return t("toolList.loadingDeferredMCPTools");
  if (id === "plan_steps") return t("toolList.planStepTracking");
  if (id === "turn_exit") return t("toolList.endingATurnTheHost");
  return id;
}

export function ToolList({ port }: { port: AgentPort }) {
  const [state, setState] = useState<ToolSettings | null>(null);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");

  useEffect(() => {
    port
      .toolSettings()
      .then(setState)
      .catch(() => setState(null));
  }, [port]);

  const group = (body: React.ReactNode) => (
    <Group id="tool-list" title={t("common.toolsTheModelCanUse")} hint={t("toolList.aToolYouTurnOff")}>
      {body}
    </Group>
  );

  if (!state) return group(<div className="empty">{t("toolList.couldNotReadTheTool")}</div>);

  const flip = async (name: string, disabled: boolean) => {
    setBusy(name);
    setError("");
    try {
      setState(await port.saveToolDisabled(name, disabled));
    } catch (e) {
      setError(reason(e));
    } finally {
      setBusy("");
    }
  };

  return group(
    <div className="box">
      {state.tools.map((tool) => (
        <div className="lrow" key={tool.name}>
          <span className="tx">
            <span className="lb">{tool.name}</span>
            <span className="ds">
              {tool.requiredBy
                ? t("toolList.needsItSoItCannot", { feature: feature(tool.requiredBy) })
                : tool.byProject
                  ? t("toolList.thisProjectSConfigFile")
                  : (toolBlurb(tool.name) ?? tool.description)}
            </span>
          </span>
          {!tool.requiredBy && !tool.byProject && (
            <Switch
              data-action="tool-list.toggle"
              on={!tool.disabled}
              busy={busy === tool.name}
              label={tool.name}
              onClick={() => void flip(tool.name, !tool.disabled)}
            />
          )}
        </div>
      ))}
      {error && <div className="why">{error}</div>}
    </div>,
  );
}
