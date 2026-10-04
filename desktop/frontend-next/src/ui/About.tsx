import { t } from "../i18n";
import type { AgentPort } from "../port/port";
import { AuthorLine } from "./AuthorLine";
import { Community } from "./Community";
import { Group } from "./Group";
import { Sponsor } from "./Sponsor";
import { Versions } from "./Versions";

export function About({ port }: { port: AgentPort }) {
  return (
    <>
      <Group id="versions"
        title={t("版本")}
        hint={t("当前安装的版本、可用更新，以及出现问题时如何回退。更新下载好后由你决定何时重启；回退后会固定在所选版本，不再提示新版本。")}
      >
        <Versions port={port} />
      </Group>
      <AuthorLine port={port} />
      <Group id="community"
        title={t("社区与贡献者")}
        hint={t("遇到问题、想交流用法或想参与进来，可以加入社区；下面是为 Reasonix 提交过代码的人。")}
      >
        <Community port={port} />
      </Group>
      <Group id="sponsor" title={t("赞助")}>
        <Sponsor port={port} />
      </Group>
    </>
  );
}
