import { MessageSquare, Settings as SettingsIcon } from "lucide-react";
import { CopyButton } from "../components/CopyButton";
import { useT, type Translator } from "../lib/i18n";
import { sidebarImScopeLabel, sidebarImSessionTarget, type SidebarImConnection } from "./SidebarIm";

type SidebarImConnectionDetailProps = {
  connection: SidebarImConnection;
  onClose: () => void;
  onOpenSession: () => void;
  onOpenSettings: () => void;
  onManageAllowlist: () => void;
};

function sidebarImSessionLabel(connection: SidebarImConnection, translate: Translator): string {
  const target = sidebarImSessionTarget(connection);
  if (!target) {
    return connection.remoteId ? translate("botDetail.readOnlyChannel") : translate("botDetail.noSession");
  }
  if (connection.sessionSource === "auto") return translate("botDetail.readOnlyChannel");
  if (target.kind === "path") return target.value.split(/[\\/]/).pop() || target.value;
  return target.value;
}

function sidebarImAccessModeLabel(connection: SidebarImConnection, translate: Translator): string {
  if (connection.allowAll) return translate("botDetail.accessAllowAll");
  if (connection.allowlistEnabled) return translate("botDetail.accessWhitelist");
  return translate("botDetail.accessDisabled");
}

function sidebarImAccessStatusLabel(connection: SidebarImConnection, translate: Translator): string {
  if (connection.allowAll) return translate("botDetail.accessOpen");
  if (!connection.remoteId) return translate("botDetail.accessUnknown");
  return connection.allowlistMatched ? translate("botDetail.accessMatched") : translate("botDetail.accessMissing");
}

function sidebarImAccessStatusClass(connection: SidebarImConnection): string {
  if (connection.allowAll || connection.allowlistMatched) return "ok";
  if (!connection.remoteId) return "muted";
  return "warn";
}

export function SidebarImConnectionDetail({ connection, onClose, onOpenSession, onOpenSettings, onManageAllowlist }: SidebarImConnectionDetailProps) {
  const translate = useT();
  const target = sidebarImSessionTarget(connection);
  const accessStatusClass = sidebarImAccessStatusClass(connection);
  return (
    <div className="bot-detail">
      <section className="bot-detail__summary">
        <div className={`bot-detail__avatar bot-detail__avatar--${connection.platform}`} aria-hidden="true">
          {connection.platform === "qq" ? "Q" : connection.platform === "weixin" ? "微" : connection.platform === "lark" ? "L" : "飞"}
        </div>
        <div className="bot-detail__summary-main">
          <span>{translate("botDetail.subtitle")}</span>
          <h2>{connection.title}</h2>
          <div className="bot-detail__chips">
            <span>{connection.platformLabel}</span>
            <span>{connection.statusLabel}</span>
            <span>{sidebarImScopeLabel(connection, translate)}</span>
          </div>
        </div>
        <div className="bot-detail__summary-actions">
          <button type="button" className="btn btn--primary btn--small bot-detail__primary" disabled={!target} title={target ? undefined : translate("botDetail.openDisabled")} onClick={onOpenSession}>
            <MessageSquare size={14} />
            {translate("botDetail.openSession")}
          </button>
          <button type="button" className="btn btn--secondary btn--small" onClick={onOpenSettings}>
            <SettingsIcon size={14} />
            {translate("botDetail.manage")}
          </button>
          <button type="button" className="btn btn--secondary btn--small" onClick={onClose}>
            {translate("botDetail.close")}
          </button>
        </div>
      </section>

      <section className="bot-detail__panel bot-detail__panel--access" aria-label={translate("botDetail.access")}>
        <div className="bot-detail__section-head">
          <span>{translate("botDetail.access")}</span>
          <div className="bot-detail__section-actions">
            {connection.remoteId ? (
              <CopyButton text={connection.remoteId} label={translate("botDetail.copyRemoteId")} />
            ) : null}
            <button type="button" className="btn btn--secondary btn--small" onClick={onManageAllowlist}>
              {translate("botDetail.manageAllowlist")}
            </button>
          </div>
        </div>
        <div className="bot-detail__access-grid">
          <div>
            <span>{translate("botDetail.accessMode")}</span>
            <strong>{sidebarImAccessModeLabel(connection, translate)}</strong>
          </div>
          <div>
            <span>{translate("botDetail.accessCurrentUser")}</span>
            <code title={connection.remoteId || undefined}>{connection.remoteId || "—"}</code>
          </div>
          <div>
            <span>{translate("botDetail.accessStatus")}</span>
            <strong className={`bot-detail__access-status bot-detail__access-status--${accessStatusClass}`}>
              {sidebarImAccessStatusLabel(connection, translate)}
            </strong>
          </div>
        </div>
        <div className="bot-detail__allowlist">
          <span>{translate("botDetail.channelAllowlistUsers")}</span>
          <div className="bot-detail__id-list">
            {connection.allowlistUsers.length > 0 ? (
              connection.allowlistUsers.map((id) => (
                <code
                  key={id}
                  className={id === connection.remoteId ? "bot-detail__id-list-item--active" : ""}
                  title={id}
                >
                  {id}
                </code>
              ))
            ) : (
              <em>{translate("botDetail.emptyAllowlistUsers")}</em>
            )}
          </div>
        </div>
      </section>

      <section className="bot-detail__panel bot-detail__panel--facts" aria-label={translate("botDetail.summary")}>
        <div className="bot-detail__section-head">
          <span>{translate("botDetail.summary")}</span>
        </div>
        <div className="bot-detail__facts">
          <div>
            <span>{translate("botDetail.remoteId")}</span>
            <code>{connection.remoteId || "—"}</code>
          </div>
          <div>
            <span>{translate("botDetail.localTopic")}</span>
            <strong>{sidebarImSessionLabel(connection, translate)}</strong>
          </div>
          <div>
            <span>{translate("botDetail.scope")}</span>
            <strong>{sidebarImScopeLabel(connection, translate)}</strong>
          </div>
        </div>
      </section>
    </div>
  );
}
