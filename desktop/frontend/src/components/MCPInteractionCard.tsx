import { useEffect, useMemo, useState } from "react";
import type { WireMCPInteraction } from "../lib/types";
import { useI18n } from "../lib/i18n";
import { structuredFieldIssue, type StructuredFieldValue } from "../lib/structuredFormSchema";
import { PromptBadge, PromptShelf } from "./PromptShelf";
import {
  StructuredForm,
  coerceStructuredValues,
  initialStructuredValues,
  parseStructuredSchema,
} from "./StructuredForm";
import "./MCPInteractionCard.css";

function hostOfURL(raw: string | undefined): string {
  if (!raw) return "";
  try {
    return new URL(raw).host;
  } catch {
    return "";
  }
}

// MCPInteractionCard renders one server-initiated elicitation. Missing or
// unknown mode falls back to form, preserving pre-2025-11-25 behavior.
export function MCPInteractionCard({
  interaction,
  busy,
  onAnswer,
  onOpenLink,
}: {
  interaction: WireMCPInteraction;
  busy: boolean;
  onAnswer: (id: string, action: "accept" | "decline" | "cancel", content?: Record<string, unknown>) => void;
  onOpenLink?: (url: string) => void;
}) {
  const { t } = useI18n();
  const mode = interaction.mode === "url" ? "url" : "form";
  const schema = useMemo(() => parseStructuredSchema(interaction.requestedSchema), [interaction.requestedSchema]);
  const [values, setValues] = useState<Record<string, StructuredFieldValue>>(() => initialStructuredValues(schema.fields));
  const [openedLink, setOpenedLink] = useState(false);
  const [showErrors, setShowErrors] = useState(false);
  const [focusInvalidNonce, setFocusInvalidNonce] = useState(0);

  useEffect(() => {
    setValues(initialStructuredValues(schema.fields));
    setOpenedLink(false);
    setShowErrors(false);
  }, [interaction.id, schema.fields]);

  const firstIssue = schema.fields.find((field) => structuredFieldIssue(field, values[field.key]));
  const formBlocked = schema.unsupported || firstIssue !== undefined;
  const messageID = `mcp-interaction-${interaction.id}-message`;
  const targetHost = hostOfURL(interaction.url);

  const submit = () => {
    if (formBlocked) {
      setShowErrors(true);
      setFocusInvalidNonce((nonce) => nonce + 1);
      return;
    }
    onAnswer(interaction.id, "accept", coerceStructuredValues(schema.fields, values).content);
  };

  const openLink = () => {
    if (!interaction.url) return;
    setOpenedLink(true);
    onOpenLink?.(interaction.url);
  };

  const validationHint = schema.unsupported
    ? t("mcp.interaction.unsupportedSchema")
    : showErrors && firstIssue
      ? firstIssue.required
        ? t("mcp.interaction.required", { label: firstIssue.label })
        : t("mcp.interaction.invalidValue", { label: firstIssue.label })
      : t("mcp.interaction.reviewHint");

  return (
    <PromptShelf
      className="mcp-interaction-shelf"
      titleId={`mcp-interaction-${interaction.id}`}
      descriptionId={messageID}
      title={t("mcp.interaction.title")}
      badges={
        <>
          <PromptBadge>{interaction.server}</PromptBadge>
          <PromptBadge>{mode === "url" ? t("mcp.interaction.modeUrl") : t("mcp.interaction.modeForm")}</PromptBadge>
        </>
      }
      decision
      footer={
        mode === "url" ? (
          <div className="prompt-shelf-bar">
            <button
              type="button"
              className="btn btn--small prompt-shelf-bar__quiet"
              onClick={() => onAnswer(interaction.id, "decline")}
              disabled={busy}
            >
              {t("mcp.interaction.decline")}
            </button>
            <span className="prompt-shelf-bar-hint" aria-live="polite">
              {openedLink ? t("mcp.interaction.urlOpenedHint") : t("mcp.interaction.urlHint")}
            </span>
            <div className="prompt-shelf-bar-actions" role="group" aria-label={t("mcp.interaction.actions")}>
              <button type="button" className="btn btn--small" onClick={() => onAnswer(interaction.id, "cancel")} disabled={busy}>
                {t("mcp.interaction.cancel")}
              </button>
              <button type="button" className="btn btn--small" onClick={openLink} disabled={busy || !interaction.url}>
                {t("mcp.interaction.openUrl", { host: targetHost || t("mcp.interaction.link") })}
              </button>
              <button type="button" className="btn btn--small btn--primary" onClick={() => onAnswer(interaction.id, "accept")} disabled={busy}>
                {t("mcp.interaction.accept")}
              </button>
            </div>
          </div>
        ) : (
          <div className="prompt-shelf-bar">
            <button
              type="button"
              className="btn btn--small prompt-shelf-bar__quiet"
              onClick={() => onAnswer(interaction.id, "decline")}
              disabled={busy}
            >
              {t("mcp.interaction.decline")}
            </button>
            <span className="prompt-shelf-bar-hint" aria-live="polite">{validationHint}</span>
            <div className="prompt-shelf-bar-actions" role="group" aria-label={t("mcp.interaction.actions")}>
              <button type="button" className="btn btn--small" onClick={() => onAnswer(interaction.id, "cancel")} disabled={busy}>
                {t("mcp.interaction.cancel")}
              </button>
              <button type="button" className="btn btn--small btn--primary" onClick={submit} disabled={busy || schema.unsupported}>
                {t("mcp.interaction.submit")}
              </button>
            </div>
          </div>
        )
      }
    >
      <p id={messageID} className="mcp-interaction-message">{interaction.message}</p>
      {mode === "url" ? (
        <div className="mcp-interaction-url">
          <span>{interaction.server}</span>
          <span aria-hidden="true">→</span>
          <strong>{targetHost || t("mcp.interaction.unknownHost")}</strong>
        </div>
      ) : schema.fields.length > 0 ? (
        <>
          {schema.unsupported ? (
            <p className="mcp-interaction-noschema" role="alert">{t("mcp.interaction.unsupportedSchema")}</p>
          ) : null}
          <StructuredForm
            fields={schema.fields}
            values={values}
            onChange={setValues}
            disabled={busy}
            showErrors={showErrors}
            focusInvalidNonce={focusInvalidNonce}
          />
          <p className="mcp-interaction-privacy">{t("mcp.interaction.formPrivacyHint")}</p>
        </>
      ) : schema.unsupported ? (
        <p className="mcp-interaction-noschema" role="alert">{t("mcp.interaction.unsupportedSchema")}</p>
      ) : (
        <p className="mcp-interaction-noschema">{t("mcp.interaction.confirmOnly")}</p>
      )}
    </PromptShelf>
  );
}
