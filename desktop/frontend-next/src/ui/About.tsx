import { t } from "../i18n";
import type { AgentPort } from "../port/port";
import { AuthorLine } from "./AuthorLine";
import { Community } from "./Community";
import { Group } from "./Group";
import { Logs } from "./Logs";
import { Sponsor } from "./Sponsor";
import { Versions } from "./Versions";

export function About({ port }: { port: AgentPort }) {
  return (
    <>
      <Group id="versions"
        title={t("common.version")}
        hint={t("about.the_installed_version_available_updates")}
      >
        <Versions port={port} />
      </Group>
      <Group id="logs"
        title={t("about.logs_and_abnormal_exit_records")}
        hint={t("about.after_a_problem_or_a")}
      >
        <Logs port={port} />
      </Group>
      <AuthorLine port={port} />
      <Group id="community"
        title={t("common.community_and_contributors")}
        hint={t("about.join_the_community_for_help")}
      >
        <Community port={port} />
      </Group>
      <Group id="sponsor" title={t("about.support_the_project")}>
        <Sponsor port={port} />
      </Group>
    </>
  );
}
