import { useEffect, useId, useRef } from "react";
import { useI18n } from "../lib/i18n";
import {
  structuredFieldIssue,
  type StructuredField,
  type StructuredFieldIssue,
  type StructuredFieldValue,
} from "../lib/structuredFormSchema";

export {
  coerceStructuredValues,
  initialStructuredValues,
  missingStructuredRequired,
  normalizeStructuredSchema,
  parseStructuredSchema,
} from "../lib/structuredFormSchema";
export type { StructuredField, StructuredFieldIssue, StructuredFieldValue } from "../lib/structuredFormSchema";

function inputType(field: StructuredField): "text" | "email" | "url" | "date" | "number" {
  if (field.kind === "number" || field.kind === "integer") return "number";
  if (field.format === "email") return "email";
  if (field.format === "uri") return "url";
  if (field.format === "date") return "date";
  // RFC 3339 date-times can include a timezone, which datetime-local drops.
  return "text";
}

// StructuredForm renders the complete MCP flat-schema subset. It owns no
// protocol state: the caller decides when validation becomes visible and when
// values are submitted.
export function StructuredForm({
  fields,
  values,
  onChange,
  disabled,
  showErrors = false,
  focusInvalidNonce = 0,
}: {
  fields: StructuredField[];
  values: Record<string, StructuredFieldValue>;
  onChange: (next: Record<string, StructuredFieldValue>) => void;
  disabled?: boolean;
  showErrors?: boolean;
  focusInvalidNonce?: number;
}) {
  const { t } = useI18n();
  const formID = useId().replace(/:/g, "");
  const controls = useRef(new Map<string, HTMLElement>());

  useEffect(() => {
    const first = fields.find((field) => field.kind !== "unsupported");
    if (first) controls.current.get(first.key)?.focus();
  }, [fields]);

  useEffect(() => {
    if (!focusInvalidNonce) return;
    const firstInvalid = fields.find((field) => structuredFieldIssue(field, values[field.key]));
    if (firstInvalid) controls.current.get(firstInvalid.key)?.focus();
  }, [fields, focusInvalidNonce, values]);

  const set = (key: string, value: StructuredFieldValue | undefined) => {
    const next = { ...values };
    if (value === undefined) delete next[key];
    else next[key] = value;
    onChange(next);
  };

  const messageFor = (field: StructuredField, issue: StructuredFieldIssue): string => {
    if (issue === "required") return t("mcp.interaction.fieldRequired");
    if (issue === "tooShort") return t("mcp.interaction.fieldTooShort", { min: field.minLength ?? 0 });
    if (issue === "tooLong") return t("mcp.interaction.fieldTooLong", { max: field.maxLength ?? 0 });
    if (issue === "belowMinimum") return t("mcp.interaction.fieldBelowMinimum", { min: field.minimum ?? 0 });
    if (issue === "aboveMaximum") return t("mcp.interaction.fieldAboveMaximum", { max: field.maximum ?? 0 });
    if (issue === "tooFewItems") return t("mcp.interaction.fieldTooFewItems", { min: field.minItems ?? 0 });
    if (issue === "tooManyItems") return t("mcp.interaction.fieldTooManyItems", { max: field.maxItems ?? 0 });
    if (issue === "unsupported") return t("mcp.interaction.fieldUnsupported");
    return t("mcp.interaction.fieldInvalid");
  };

  return (
    <div className="structured-form">
      {fields.map((field, index) => {
        const raw = values[field.key];
        const fieldID = `${formID}-field-${index}`;
        const hintID = field.description ? `${fieldID}-hint` : undefined;
        const issue = structuredFieldIssue(field, raw);
        const errorID = showErrors && issue ? `${fieldID}-error` : undefined;
        const describedBy = [hintID, errorID].filter(Boolean).join(" ") || undefined;
        const setControl = (node: HTMLElement | null) => {
          if (node) controls.current.set(field.key, node);
          else controls.current.delete(field.key);
        };
        const label = (
          <>
            {field.label}
            {field.required ? <span className="structured-form-required" aria-hidden="true"> *</span> : null}
          </>
        );
        const help = (
          <>
            {field.description ? <span id={hintID} className="structured-form-hint">{field.description}</span> : null}
            {showErrors && issue ? <span id={errorID} className="structured-form-error" role="alert">{messageFor(field, issue)}</span> : null}
          </>
        );

        if (field.kind === "unsupported") {
          return (
            <div key={field.key} className="structured-form-field structured-form-field--wide">
              <span className="structured-form-label">{label}</span>
              <span className="structured-form-unsupported" role="alert">{t("mcp.interaction.fieldUnsupported")}</span>
            </div>
          );
        }

        if (field.kind === "boolean") {
          return (
            <div key={field.key} className="structured-form-field structured-form-field--wide">
              <label className="structured-form-checkbox-row" htmlFor={fieldID}>
                <input
                  ref={setControl}
                  id={fieldID}
                  type="checkbox"
                  className="structured-form-checkbox"
                  checked={raw === true}
                  disabled={disabled}
                  aria-required={field.required || undefined}
                  aria-invalid={showErrors && issue ? true : undefined}
                  aria-describedby={describedBy}
                  onChange={(event) => set(field.key, event.target.checked)}
                />
                <span className="structured-form-label">{label}</span>
              </label>
              {help}
            </div>
          );
        }

        if (field.kind === "multi-enum" && field.options) {
          const selected = Array.isArray(raw) ? raw : [];
          return (
            <fieldset
              key={field.key}
              className="structured-form-field structured-form-field--wide structured-form-options"
              aria-required={field.required || undefined}
              aria-invalid={showErrors && issue ? true : undefined}
              aria-describedby={describedBy}
            >
              <legend className="structured-form-label">{label}</legend>
              {field.options.map((option, optionIndex) => (
                <label key={option.value} className="structured-form-option">
                  <input
                    ref={optionIndex === 0 ? setControl : undefined}
                    type="checkbox"
                    checked={selected.includes(option.value)}
                    disabled={disabled}
                    onChange={(event) => {
                      const next = event.target.checked
                        ? [...selected, option.value]
                        : selected.filter((value) => value !== option.value);
                      set(field.key, next);
                    }}
                  />
                  <span>{option.label}</span>
                </label>
              ))}
              {help}
            </fieldset>
          );
        }

        const stringRaw = raw === undefined || Array.isArray(raw) ? "" : String(raw);
        return (
          <div key={field.key} className="structured-form-field">
            <label className="structured-form-label" htmlFor={fieldID}>{label}</label>
            {field.kind === "enum" && field.options ? (
              <select
                ref={setControl}
                id={fieldID}
                className="structured-form-control"
                value={stringRaw}
                disabled={disabled}
                required={field.required}
                aria-required={field.required || undefined}
                aria-invalid={showErrors && issue ? true : undefined}
                aria-describedby={describedBy}
                onChange={(event) => set(field.key, event.target.value || undefined)}
              >
                <option value="">{t("mcp.interaction.chooseOption")}</option>
                {field.options.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}
              </select>
            ) : (
              <input
                ref={setControl}
                id={fieldID}
                type={inputType(field)}
                className="structured-form-control"
                value={stringRaw}
                disabled={disabled}
                required={field.required}
                aria-required={field.required || undefined}
                aria-invalid={showErrors && issue ? true : undefined}
                aria-describedby={describedBy}
                min={field.minimum}
                max={field.maximum}
                step={field.kind === "integer" ? 1 : field.kind === "number" ? "any" : undefined}
                minLength={field.minLength}
                maxLength={field.maxLength}
                inputMode={field.kind === "integer" ? "numeric" : field.kind === "number" ? "decimal" : undefined}
                placeholder={field.format === "date-time" ? "2026-08-29T12:00:00Z" : undefined}
                onChange={(event) => set(field.key, event.target.value || undefined)}
              />
            )}
            {help}
          </div>
        );
      })}
    </div>
  );
}
