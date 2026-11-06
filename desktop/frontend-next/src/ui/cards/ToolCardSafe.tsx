import type { ComponentProps } from "react";
import { t } from "../../i18n";
import type { StateTool } from "../../state/outputcap";
import { Boundary } from "../Boundary";
import { ToolCard as Card } from "./ToolCard";

// The first string the call was given: the path or command it acted on.
function target(args: string | undefined): string {
  try {
    const parsed: unknown = JSON.parse(args ?? "");
    if (parsed && typeof parsed === "object") {
      for (const value of Object.values(parsed)) if (typeof value === "string" && value) return value.split("\n")[0].slice(0, 120);
    }
  } catch {
    // arguments that do not parse have no target to name
  }
  return "";
}

function Simplified({ tool }: { tool: StateTool }) {
  const to = target(tool.args);
  return (
    <div className="toolfb" role="group">
      <span className="toolfb-nm">{tool.name}{to && <> · {to}</>}</span>
      <span className="srcfb-tag">{t("chat.simplified_view")}</span>
    </div>
  );
}

/** One call's card, falling back to a single line when drawing it throws, so a
 *  card that cannot be drawn costs that call its detail and nothing else. */
export function ToolCard(props: ComponentProps<typeof Card>) {
  return (
    <Boundary retryKey={[props.tool.args, props.tool.output, props.running]} fallback={<Simplified tool={props.tool} />}>
      <Card {...props} />
    </Boundary>
  );
}
