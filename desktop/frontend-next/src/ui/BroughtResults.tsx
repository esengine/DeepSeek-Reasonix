import { t } from "../i18n";
import type { Item } from "../state/session";
import type { ExtensionSurface } from "../port/wire";
import type { AgentPort, RewindPlan, RewindResult } from "../port/port";
import { ToolCard } from "./cards/ToolCard";
import { StudioIcon } from "./StudioIcon";

/** The calls a reader brought to the end, each drawn from the transcript's own
 *  record of it. The original card stays where it happened. */
export function BroughtResults({ items, ids, onDismiss, takeovers, onExtInvoke, onPrepareFileRevert, onCommitFileRevert }: {
  items: Item[];
  ids: string[];
  onDismiss: (id: string) => void;
  takeovers: Record<string, ExtensionSurface>;
  onExtInvoke: (name: string) => void;
  onPrepareFileRevert: (path: string) => Promise<RewindPlan>;
  onCommitFileRevert: (planId: string, resolution?: string) => Promise<RewindResult>;
  port?: Pick<AgentPort, "stopToolCall">;
}) {
  const drawn = ids.flatMap((id) => {
    const it = items.find((x) => x.t === "tool" && x.tool.id === id);
    return it && it.t === "tool" ? [it] : [];
  });
  if (drawn.length === 0) return null;
  return (
    <section className="brought" aria-label={t("chat.brought_to_the_end")}>
      {drawn.map((it) => (
        <div className="brought-item" key={it.tool.id}>
          <div className="brought-bar">
            <span>{t("chat.brought_to_the_end")}</span>
            <button type="button" className="brought-close" data-action="tool.unbring" aria-label={t("common.remove")} title={t("common.remove")} onClick={() => onDismiss(it.tool.id!)}>
              <StudioIcon name="close" />
            </button>
          </div>
          <div className="activity-body brought-line">
            <ToolCard
              tool={it.tool}
              running={it.running}
              children={it.children}
              takeover={it.tool.id ? takeovers[`tool:${it.tool.id}`] : undefined}
              onExtInvoke={onExtInvoke}
              onPrepareFileRevert={onPrepareFileRevert}
              onCommitFileRevert={onCommitFileRevert}
              pinned
            />
          </div>
        </div>
      ))}
    </section>
  );
}
