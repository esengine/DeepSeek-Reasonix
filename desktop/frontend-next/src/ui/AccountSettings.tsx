import { t } from "../i18n";
import type { AccountState, AgentPort } from "../port/port";
import { Account } from "./Account";
import { AuthorLine } from "./AuthorLine";
import { Community } from "./Community";
import { Group } from "./Group";
import { Logs } from "./Logs";
import { SectionCard } from "./SectionCard";
import { Sponsor } from "./Sponsor";
import { TraceExport } from "./TraceExport";
import { Versions } from "./Versions";

interface Props {
  port: AgentPort;
  homePort: AgentPort;
  state: AccountState | null;
  unread: string;
  reload: () => void;
}

export function AccountSettings({ port, homePort, state, unread, reload }: Props) {
  return (
    <>
      <SectionCard title={t("settings.account")}>
        <Group id="account" pill={t(state?.signedIn ? "settings.account_info" : "settings.sign_in")} title={t("settings.account")} hint={t("settings.account_hint")}>
          <Account port={port} state={state} unread={unread} reload={reload} />
        </Group>
      </SectionCard>
      <SectionCard title={t("settings.version_and_diagnostics")}>
        <Group id="versions" pill={t("settings.versions_and_updates")} title={t("common.version")} hint={t("settings.versions_hint")}>
          <Versions port={homePort} />
        </Group>
        <Group id="logs" pill={t("safety.details")} title={t("about.logs_and_abnormal_exit_records")} hint={t("settings.logs_hint")}>
          <Logs port={homePort} />
        </Group>
        <TraceExport port={port} />
      </SectionCard>
      <SectionCard title={t("settings.about")}>
        <p className="srow-desc">{t("settings.about_note")}</p>
        <AuthorLine port={homePort} />
        <Group id="community" pill={t("settings.look_at")} title={t("common.community_and_contributors")} hint={t("about.join_the_community_for_help")}>
          <Community port={homePort} />
        </Group>
        <Group id="sponsor" pill={t("settings.look_at")} title={t("about.support_the_project")}>
          <Sponsor port={homePort} />
        </Group>
      </SectionCard>
    </>
  );
}
