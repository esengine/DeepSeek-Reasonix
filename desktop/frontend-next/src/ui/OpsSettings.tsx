import { t } from "../i18n";
import { EscStopSetting } from "./EscStopSetting";
import { ComposerHintSection } from "./ComposerHintSection";
import { GlobalShortcutSection } from "./GlobalShortcutSection";
import { KeysSheet } from "./KeysSheet";
import { LiveGraceSection } from "./LiveGraceSection";
import { LiveStreamBudgetSection } from "./LiveStreamBudgetSection";
import { NavEntriesSection } from "./NavEntriesSection";
import { NavRailSection } from "./NavRailSection";
import { OpenSessionLimitSection } from "./OpenSessionLimitSection";
import { PageSleepSetting } from "./PageSleepSetting";
import { RunningMessageSetting } from "./RunningMessageSetting";
import { SectionCard } from "./SectionCard";
import { SessionQuickSection } from "./SessionQuickSection";
import { SessionTabsSetting } from "./SessionTabsSetting";

export function OpsSettings() {
  return (
    <>
      <SectionCard title={t("settings.keyboard_shortcuts")}>
        <KeysSheet />
        <GlobalShortcutSection />
        <EscStopSetting />
      </SectionCard>
      <SectionCard title={t("settings.tabs_and_navigation_card")}>
        <SessionTabsSetting />
        <SessionQuickSection />
        <NavRailSection />
        <NavEntriesSection />
        <LiveGraceSection />
      </SectionCard>
      <SectionCard title={t("settings.while_running_card")}>
        <RunningMessageSetting />
        <ComposerHintSection />
      </SectionCard>
      <SectionCard title={t("settings.performance_card")}>
        <OpenSessionLimitSection />
        <LiveStreamBudgetSection />
        <PageSleepSetting />
      </SectionCard>
    </>
  );
}
