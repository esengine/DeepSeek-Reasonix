import { useState } from "react";
import { t } from "../i18n";
import { setShowsCompositionPicker, useShowsCompositionPicker } from "../state/compositionPicker";
import { Group } from "./Group";
import { Switch } from "./Switch";

export function CompositionPickerSetting() {
  const enabled = useShowsCompositionPicker();
  const [error, setError] = useState(false);
  return <Group id="composition-picker" title={t("common.showModePicker")}
    hint={t("compositionPickerSetting.offByDefaultEnableTo")}
    action={<Switch label={t("common.showModePicker")} data-action="agent-preset.show-picker" on={enabled}
      onClick={() => setError(!setShowsCompositionPicker(!enabled))} />}>
    {error && <p role="alert">{t("compositionPickerSetting.couldNotSaveTheMode")}</p>}
  </Group>;
}
