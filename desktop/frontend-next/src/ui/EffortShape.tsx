import { t } from "../i18n";
import { effortExample } from "./provider_compat";

export const EXAMPLE_LEVEL = "high";

export function EffortShape({ field, level, className = "tip" }: { field?: string; level?: string; className?: string }) {
  if (!field) return null;
  return (
    <i className={className} data-effort-field={field}>
      {t("composer.this_interface_type_sends_the", { field, example: effortExample(field, level || EXAMPLE_LEVEL) })}
    </i>
  );
}
