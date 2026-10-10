import { t } from "../i18n";
import type { AgentPort } from "../port/port";
import { COMMUNITY } from "./communityLinks";

export function AuthorLine({ port }: { port: Pick<AgentPort, "openExternal"> }) {
  return (
    <div className="comm-author">
      <span className="comm-k">{t("作者与维护者")}</span>
      <a href={COMMUNITY.authorUrl} data-action="community.author" onClick={(e) => { e.preventDefault(); void port.openExternal(COMMUNITY.authorUrl).catch(() => {}); }}>{COMMUNITY.author}</a>
    </div>
  );
}
