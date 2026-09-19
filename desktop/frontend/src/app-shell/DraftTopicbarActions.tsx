import { Plug, Trash2 } from "lucide-react";

import { draftSubmissionLocksEditing, type SessionDraftSurface } from "../app-runtime/useSessionDraftSurface";
import { Tooltip } from "../components/Tooltip";
import type { Translator } from "../lib/i18n";

/** Keeps optional draft controls available without turning the landing surface
 * into a management page. */
export function DraftTopicbarActions(props: {
  t: Translator;
  draft: SessionDraftSurface;
  onSetMCPEnabled(server: SessionDraftSurface["servers"][number], enabled: boolean): void;
  onDiscard(): void;
}) {
  const { draft, t } = props;
  const editingLocked = draft.preparingSubmission || draftSubmissionLocksEditing(draft.operation);
  const enabledServers = draft.servers.filter((server) => server.enabled && !draft.settings.disabledMcp[server.name]).length;
  return <div className="topicbar__actions draft-topicbar-actions">
    {draft.servers.length > 0 ? <details className="draft-topicbar-mcp">
      <summary className="topicbar__action-btn topicbar__action-btn--utility" aria-label={t("draft.mcpTitle")}>
        <Plug size={15} aria-hidden="true" />
        <span>{enabledServers}/{draft.servers.length}</span>
      </summary>
      <div className="draft-topicbar-mcp__menu" role="group" aria-label={t("draft.mcpTitle")}>
        {draft.servers.map((server) => {
          const available = server.enabled;
          const selected = available && !draft.settings.disabledMcp[server.name];
          return <label key={server.name}>
            <input type="checkbox" checked={selected} disabled={!available || editingLocked}
              onChange={(event) => props.onSetMCPEnabled(server, event.currentTarget.checked)} />
            <span>{server.name}</span>
            <small>{available ? t(selected ? "draft.mcpEnabled" : "draft.mcpDisabled") : t("draft.mcpUnavailable")}</small>
          </label>;
        })}
      </div>
    </details> : null}
    <Tooltip label={t("draft.discard")}>
      <button className="topicbar__action-btn topicbar__action-btn--icon topicbar__action-btn--utility" type="button"
        aria-label={t("draft.discard")} disabled={editingLocked} onClick={props.onDiscard}>
        <Trash2 size={15} aria-hidden="true" />
      </button>
    </Tooltip>
  </div>;
}
