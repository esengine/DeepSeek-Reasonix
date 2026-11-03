import { t } from "../i18n";
import type { AgentPort } from "../port/port";
import { AuthorLine } from "./AuthorLine";
import { Community } from "./Community";
import { Group } from "./Group";
import { Logs } from "./Logs";
import { Sponsor } from "./Sponsor";
import { UpdateMode } from "./UpdateMode";
import { Versions } from "./Versions";

export function About({ port }: { port: AgentPort }) {
  return (
    <>
      <Group id="versions"
        title={t("common.version")}
        hint={t("about.theInstalledVersionAvailableUpdates")}
      >
        <Versions port={port} />
      </Group>
      <Group id="update-mode"
        title={t("common.updateMode")}
        hint={t("about.choosesWhetherTheWindowDoes")}
      >
        <UpdateMode port={port} />
      </Group>
      <Group id="logs"
        title={t("common.logsAndAbnormalExitRecords")}
        hint={t("about.afterAProblemOrA")}
      >
        <Logs port={port} />
      </Group>
      <AuthorLine port={port} />
      <Group id="community"
        title={t("common.communityAndContributors")}
        hint={t("about.joinTheCommunityForHelp")}
      >
        <Community port={port} />
      </Group>
      <Group id="sponsor" title={t("common.supportTheProject")}>
        <Sponsor port={port} />
      </Group>
    </>
  );
}
