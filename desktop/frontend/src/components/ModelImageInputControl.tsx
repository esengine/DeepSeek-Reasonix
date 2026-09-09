import { useId } from "react";
import { useT } from "../lib/i18n";
import { imageInputHardBlocked, imageInputState, type ImageInputMode } from "../lib/providerImageInput";
import type { ProviderModelCapabilityView } from "../lib/types";

export function ModelImageInputControl({ model, mode, capability, baseURL, fallback, disabled, onChange }: {
  model: string; mode: ImageInputMode; capability?: ProviderModelCapabilityView;
  baseURL?: string; fallback?: string; disabled: boolean; onChange?: (mode: ImageInputMode) => void;
}) {
  const t = useT();
  const groupName = useId();
  const blocked = imageInputHardBlocked(baseURL, model, capability);
  const state = blocked ? "unsupported" : imageInputState(mode, capability, fallback);
  const status = blocked ? t("settings.imageInputProtocolBadge")
    : mode === "on" ? t("settings.imageInputManualOn")
    : mode === "off" ? t("settings.imageInputManualOff")
    : capability?.automaticSource === "legacy" ? t("settings.imageInputLegacy")
    : state === "supported" ? t("settings.imageInputSupported")
    : state === "unsupported" ? t("settings.imageInputUnsupported") : t("settings.imageInputUnknown");
  const statusTone = blocked ? "restricted" : mode === "on" || state === "supported" ? "supported"
    : mode === "off" || state === "unsupported" ? "unsupported" : "unknown";
  const options: Array<{ value: ImageInputMode; label: string }> = [
    { value: "auto", label: t("settings.imageInputAuto") },
    { value: "on", label: t("settings.imageInputOn") },
    { value: "off", label: t("settings.imageInputOff") },
  ];
  return <div className="provider-image-input">
    <div className="provider-image-input__head">
      <span className="provider-image-input__label">{t("settings.imageInputLabel")}</span>
      <div className="set-seg provider-image-input__modes" role="radiogroup" aria-label={t("settings.imageInputModeAria", { model })}>
        {options.map((option) => {
          const optionDisabled = disabled || !onChange || (blocked && option.value === "on");
          return <label key={option.value} className={`set-seg__btn provider-image-input__mode${mode === option.value ? " set-seg__btn--on" : ""}${optionDisabled ? " provider-image-input__mode--disabled" : ""}`}>
            <input className="sr-only" type="radio" name={groupName} value={option.value} checked={mode === option.value}
              disabled={optionDisabled} onChange={() => onChange?.(option.value)} />
            <span>{option.label}</span>
          </label>;
        })}
      </div>
    </div>
    <div className="provider-image-input__meta">
      <span className={`provider-image-input__status provider-image-input__status--${statusTone}`}>
        <span className="provider-image-input__status-dot" aria-hidden="true" />{status}
      </span>
      {(blocked || (mode === "auto" && state === "unknown")) && (
        <span className="provider-image-input__detail">{blocked ? t("settings.imageInputProtocolBlocked") : t("settings.imageInputUnknownHint")}</span>
      )}
    </div>
  </div>;
}
