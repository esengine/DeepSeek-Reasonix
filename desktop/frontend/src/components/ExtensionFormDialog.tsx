import { useEffect, useId, useMemo, useRef, useState } from "react";
import { useT } from "../lib/i18n";
import type { ExtensionFormState } from "../lib/useController";
import type { WireExtensionFormField } from "../lib/types";
import { PromptAction, PromptBadge, PromptShelf } from "./PromptShelf";

// ExtensionFormDialog renders one extension-published form surface as the
// footer's decision shelf: structured fields (confirm / input / select /
// multiselect) with required validation, a submit bar, and cancel. Submitting
// delivers the values to the owning sidecar; cancelling reports
// values{"cancelled": true} (wired by the caller).
export function ExtensionFormDialog({
  surface,
  busy = false,
  onSubmit,
  onCancel,
}: {
  surface: ExtensionFormState;
  busy?: boolean;
  onSubmit: (values: Record<string, unknown>) => void;
  onCancel: () => void;
}) {
  const t = useT();
  const titleId = useId();
  const shelfRef = useRef<HTMLDivElement | null>(null);
  const onCancelRef = useRef(onCancel);
  onCancelRef.current = onCancel;
  const [values, setValues] = useState<Record<string, unknown>>(() => initialFormValues(surface.form.fields));

  useEffect(() => {
    shelfRef.current?.focus();
    const onKeyDown = (event: globalThis.KeyboardEvent) => {
      if (event.key !== "Escape") return;
      const target = event.target instanceof Element ? event.target : null;
      const tag = target?.tagName.toLowerCase();
      if (tag === "input" || tag === "textarea" || (target instanceof HTMLElement && target.isContentEditable)) return;
      event.preventDefault();
      onCancelRef.current();
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, []);

  const missing = useMemo(() => missingRequiredLabels(surface.form.fields, values), [surface.form.fields, values]);
  const setField = (key: string, value: unknown) => setValues((current) => ({ ...current, [key]: value }));
  const toggleMulti = (key: string, option: string) => {
    setValues((current) => {
      const selected = Array.isArray(current[key]) ? (current[key] as string[]) : [];
      const next = selected.includes(option) ? selected.filter((entry) => entry !== option) : [...selected, option];
      return { ...current, [key]: next };
    });
  };

  const hint = missing.length > 0 ? t("ext.form.required", { label: missing[0] }) : " ";
  const { form } = surface;
  return (
    <PromptShelf
      decision
      barRef={shelfRef}
      titleId={titleId}
      title={form.title || t("ext.form.defaultTitle")}
      badges={<PromptBadge>{surface.pluginId}</PromptBadge>}
      actionsRole="group"
      footer={
        <div className="decision-confirm-bar">
          <div className="decision-confirm-bar__hint">{hint}</div>
          <button type="button" className="btn btn--small" onClick={onCancel} disabled={busy}>
            {t("common.cancel")}
          </button>
          <button
            type="button"
            className="btn btn--small btn--primary decision-confirm-bar__confirm"
            onClick={() => onSubmit(values)}
            disabled={busy || missing.length > 0}
          >
            {t("ext.form.submit")}
          </button>
        </div>
      }
    >
      <div className="extension-form">
        {form.message ? <p className="prompt-shelf__note">{form.message}</p> : null}
        {form.fields.map((field) => (
          <ExtensionFormFieldRow
            key={field.key}
            field={field}
            value={values[field.key]}
            disabled={busy}
            onChange={(value) => setField(field.key, value)}
            onToggleOption={(option) => toggleMulti(field.key, option)}
          />
        ))}
      </div>
    </PromptShelf>
  );
}

function fieldLabel(field: WireExtensionFormField): string {
  return field.label || field.key;
}

function fieldKind(field: WireExtensionFormField): string {
  return field.kind || "input";
}

function initialFormValues(fields: WireExtensionFormField[]): Record<string, unknown> {
  const values: Record<string, unknown> = {};
  for (const field of fields) {
    switch (fieldKind(field)) {
      case "confirm":
        values[field.key] = field.default === true;
        break;
      case "multiselect":
        values[field.key] = Array.isArray(field.default) ? field.default.map(String) : [];
        break;
      default:
        values[field.key] = field.default === undefined || field.default === null ? "" : String(field.default);
    }
  }
  return values;
}

function missingRequiredLabels(fields: WireExtensionFormField[], values: Record<string, unknown>): string[] {
  const missing: string[] = [];
  for (const field of fields) {
    if (!field.required) continue;
    const value = values[field.key];
    const present =
      fieldKind(field) === "confirm"
        ? value === true
        : fieldKind(field) === "multiselect"
          ? Array.isArray(value) && value.length > 0
          : typeof value === "string" && value.trim() !== "";
    if (!present) missing.push(fieldLabel(field));
  }
  return missing;
}

function ExtensionFormFieldRow({
  field,
  value,
  disabled,
  onChange,
  onToggleOption,
}: {
  field: WireExtensionFormField;
  value: unknown;
  disabled: boolean;
  onChange: (value: unknown) => void;
  onToggleOption: (option: string) => void;
}) {
  const label = fieldLabel(field);
  const kind = fieldKind(field);
  if (kind === "confirm") {
    return (
      <label className="extension-form__row extension-form__row--check">
        <input type="checkbox" checked={value === true} disabled={disabled} onChange={(event) => onChange(event.target.checked)} />
        <span>{label}{field.required ? " *" : ""}</span>
      </label>
    );
  }
  if (kind === "select" || kind === "multiselect") {
    const options = field.options ?? [];
    const selected = kind === "select" ? (typeof value === "string" && value !== "" ? [value] : []) : Array.isArray(value) ? (value as string[]) : [];
    return (
      <div className="extension-form__row">
        <div className="extension-form__label">{label}{field.required ? " *" : ""}</div>
        <div className="extension-form__options" role={kind === "select" ? "listbox" : "group"} aria-label={label}>
          {options.map((option) => (
            <PromptAction
              key={option}
              keyLabel=""
              label={option}
              role={kind === "select" ? "option" : "button"}
              selected={selected.includes(option)}
              disabled={disabled}
              onClick={() => (kind === "select" ? onChange(option) : onToggleOption(option))}
            />
          ))}
        </div>
      </div>
    );
  }
  return (
    <label className="extension-form__row">
      <span className="extension-form__label">{label}{field.required ? " *" : ""}</span>
      <input
        type="text"
        className="extension-form__input"
        value={typeof value === "string" ? value : ""}
        disabled={disabled}
        onChange={(event) => onChange(event.target.value)}
      />
    </label>
  );
}
