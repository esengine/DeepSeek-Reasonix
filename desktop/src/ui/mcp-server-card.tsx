import { useState } from "react";
import { t } from "../i18n";
import { I } from "../icons";
import type { McpSpecInfo } from "../protocol";
import { Tooltip } from "./tooltip";

export type McpServerCardMode = "settings" | "context";

export function mcpStatusLabel(spec: McpSpecInfo): string {
  if (spec.status === "failed") return mcpFailureLabel(spec);
  switch (spec.status) {
    case "connected":
      return t("settings.mcpStatus.connected");
    case "handshake":
      return t("settings.mcpStatus.handshake");
    case "disabled":
      return t("settings.mcpStatus.disabled");
    case "configured":
      return t("settings.mcpStatus.configured");
  }
}

export function mcpFailureLabel(spec: McpSpecInfo): string {
  switch (spec.statusHint) {
    case "missing-token":
      return t("settings.mcpFailure.missingToken");
    case "auth":
      return t("settings.mcpFailure.auth");
    case "command":
      return t("settings.mcpFailure.command");
    case "network":
      return t("settings.mcpFailure.network");
    default:
      return t("settings.mcpFailure.unknown");
  }
}

export function mcpEditLabel(spec: McpSpecInfo): string {
  void spec;
  return t("settings.mcpEdit");
}

export function McpServerCard({
  spec,
  mode = "settings",
  onEdit,
  onRetry,
  onRemove,
}: {
  spec: McpSpecInfo;
  mode?: McpServerCardMode;
  onEdit?: (spec: McpSpecInfo) => void;
  onRetry?: (raw: string) => void;
  onRemove?: (raw: string) => void;
}) {
  const [toolsOpen, setToolsOpen] = useState(false);
  const [detailOpen, setDetailOpen] = useState(false);
  const tools = spec.tools ?? [];
  const hasTools = tools.length > 0;
  const canExpandTools = hasTools || spec.status === "connected";
  const displayName = spec.name ?? "(anonymous)";
  const status = mcpStatusLabel(spec);
  const toolCount = typeof spec.toolCount === "number" ? spec.toolCount : tools.length;
  const statusText = `${status}${toolCount ? ` · ${t("contextPanel.mcpTools", { count: toolCount })}` : ""}`;
  const statusTooltip =
    spec.status === "failed" && spec.statusReason
      ? `${statusText}\n${spec.statusReason}`
      : statusText;

  return (
    <div className="scard mcp-server-card" data-mode={mode} data-status={spec.status}>
      <div className="top">
        <span className="ico">
          <I.wrench size={14} />
        </span>
        <div className="mcp-spec-body">
          <div className="nm">{displayName}</div>
          <div className="sub mcp-spec-summary" title={spec.summary}>
            {spec.summary}
          </div>
          <Tooltip content={statusTooltip} className="mcp-status-tooltip">
            <span className="mcp-spec-status" data-status={spec.status}>
              {statusText}
            </span>
          </Tooltip>
        </div>
        <div className="mcp-card-actions">
          {canExpandTools ? (
            <button type="button" className="btn ghost" onClick={() => setToolsOpen((v) => !v)}>
              <I.chev size={13} className={toolsOpen ? "rot" : ""} />
              {toolsOpen ? t("mcpCard.hideTools") : t("mcpCard.showTools")}
            </button>
          ) : null}
          {onEdit && !spec.parseError ? (
            <button type="button" className="btn ghost" onClick={() => onEdit(spec)}>
              <I.pencil size={13} />
              {mcpEditLabel(spec)}
            </button>
          ) : null}
          {onRetry && spec.status === "failed" ? (
            <button type="button" className="btn ghost" onClick={() => onRetry(spec.raw)}>
              <I.refresh size={13} />
              {t("settings.mcpRetry")}
            </button>
          ) : null}
          {spec.status === "failed" && spec.statusReason ? (
            <button type="button" className="btn ghost" onClick={() => setDetailOpen((v) => !v)}>
              {detailOpen ? t("settings.mcpHideDetail") : t("settings.mcpDetail")}
            </button>
          ) : null}
          {onRemove ? (
            <button
              type="button"
              className="btn ghost mcp-remove"
              style={{ color: "var(--danger)" }}
              onClick={() => onRemove(spec.raw)}
            >
              {t("settings.mcpRemove")}
            </button>
          ) : null}
        </div>
      </div>

      {spec.parseError ? (
        <div className="desc" style={{ color: "var(--danger)" }}>
          {t("settings.parseError", { error: spec.parseError })}
        </div>
      ) : null}

      {toolsOpen ? (
        <div className="mcp-tools-detail">
          <div className="mcp-tools-detail-title">
            {hasTools
              ? t("mcpCard.availableTools", { count: tools.length })
              : t("mcpCard.noTools")}
          </div>
          {hasTools ? (
            <div className="mcp-tools-grid">
              {tools.map((tool) => (
                <div className="mcp-tool-chip" key={tool.registeredName} title={tool.description ?? tool.registeredName}>
                  <span className="mcp-tool-name">{tool.registeredName}</span>
                  {tool.description ? <span className="mcp-tool-desc">{tool.description}</span> : null}
                </div>
              ))}
            </div>
          ) : null}
        </div>
      ) : null}

      {detailOpen && spec.statusReason ? (
        <div className="mcp-error-detail">
          <div className="mcp-error-detail-title">{t("settings.mcpDetailTitle")}</div>
          <pre>{spec.statusReason}</pre>
        </div>
      ) : null}
    </div>
  );
}
