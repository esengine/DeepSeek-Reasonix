import { t } from "../i18n";
import type { AgentPort } from "../port/port";
import { ChangedFilesSetting } from "./ChangedFilesSetting";
import { FeedbackLevelNoticeSetting } from "./FeedbackLevelNoticeSetting";
import { Folding } from "./Folding";
import { FoldWorkSetting } from "./FoldWorkSetting";
import { Group } from "./Group";
import { NewAnswerSection } from "./NewAnswerSection";
import { OfficePreviewSection } from "./OfficePreviewSection";
import { PlanProgressSection } from "./PlanProgressSection";
import { PresentFiles } from "./PresentFiles";
import { SectionCard } from "./SectionCard";
import { SettingDeep } from "./SettingDeep";

export function ChatDisplaySettings({ port }: { port: AgentPort }) {
  return (
    <>
      <SectionCard title={t("settings.process_card")}>
        <FoldWorkSetting port={port} />
        <SettingDeep id="folding" title={t("chat.transcript_folding")} hint={t("settings.older_turns_fold_to_a_line")} act={t("safety.details")}>
          <section className="prefs-card"><Folding /></section>
        </SettingDeep>
        <PlanProgressSection />
      </SectionCard>
      <SectionCard title={t("settings.files_and_previews_card")}>
        <ChangedFilesSetting port={port} />
        <Group id="present-files" inline title={t("settings.ai_file_delivery_cards")} hint={t("settings.generated_files_as_clickable_cards")}>
          <PresentFiles port={port} />
        </Group>
        <OfficePreviewSection />
      </SectionCard>
      <SectionCard title={t("settings.notices_card")}>
        <NewAnswerSection />
        <FeedbackLevelNoticeSetting port={port} />
      </SectionCard>
    </>
  );
}
