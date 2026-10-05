import { t } from "../i18n";
import { ICON, NAV, SETTINGS, type Section } from "./prefsnav";

interface Props {
  section: Section;
  value?: string;
  danger?: boolean;
}

export function SettingsHeading({ section, value, danger }: Props) {
  const group = NAV.find(([, items]) => items.some(([id]) => id === section));
  const name = group?.[1].find(([id]) => id === section)?.[1] ?? section;
  const count = SETTINGS.filter((entry) => entry.section === section).length;

  return (
    <header className="prefs-intro">
      <span className="prefs-intro-mark" aria-hidden="true">
        <svg viewBox="0 0 16 16">{ICON[section]}</svg>
      </span>
      <span className="prefs-intro-copy">
        <span className="prefs-intro-overline">{t(group?.[0] ?? "")}</span>
        <h2>{t(name)}</h2>
      </span>
      <span className="prefs-intro-meta" data-danger={danger ? "" : undefined}>
        {value && <strong title={value}>{value}</strong>}
        <span>{t("设置项：{n}", { n: count })}</span>
      </span>
    </header>
  );
}
