import type { Translator } from "../lib/i18n";
import { mcpListAttribution, type McpListAttribution } from "../lib/mcpListAttribution";
import type { Item } from "../lib/useController";

export function McpListLayers({ items, t }: { items?: Item[]; t: Translator }) {
  const counts: McpListAttribution = mcpListAttribution(items ?? []);
  const total = counts.sharedHost + counts.diskCache + counts.remote;
  return (
    <section className="context-panel__section context-panel__mcp-list">
      <div className="context-panel__section-head">
        <h3>{t("context.mcpListTitle")}</h3>
      </div>
      {total === 0 ? (
        <p className="context-panel__mcp-empty">{t("context.mcpListEmpty")}</p>
      ) : (
        <div className="context-panel__mcp-rows">
          <McpListRow label={t("context.mcpListSharedHost")} value={counts.sharedHost} />
          <McpListRow label={t("context.mcpListDiskCache")} value={counts.diskCache} />
          <McpListRow label={t("context.mcpListRemote")} value={counts.remote} />
        </div>
      )}
    </section>
  );
}

function McpListRow({ label, value }: { label: string; value: number }) {
  return (
    <div className="context-panel__mcp-row">
      <span>{label}</span>
      <strong>{value}</strong>
    </div>
  );
}
