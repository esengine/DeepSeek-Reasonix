import { useState } from "react";
import { t } from "../i18n";
import { setShowsCompositionPicker, useShowsCompositionPicker } from "../state/compositionPicker";
import { Group } from "./Group";
import { Switch } from "./Switch";

export function CompositionPickerSetting() {
  const enabled = useShowsCompositionPicker();
  const [error, setError] = useState(false);
  return <Group id="composition-picker" title={t("persona.show_mode_picker")}
    hint={t("persona.off_by_default_enable_to")}
    action={<Switch label={t("persona.show_mode_picker")} data-action="agent-preset.show-picker" on={enabled}
      onClick={() => setError(!setShowsCompositionPicker(!enabled))} />}>
    {error && <p role="alert">{t("persona.could_not_save_the_mode")}</p>}
  </Group>;
}
