import type { McpEntry } from "../../port/port";
import { t } from "../../i18n";

// Only the servers that need a decision get a row. A healthy MCP fleet is the
// least interesting thing on this rail: it is all green and never changes, and
// where its tools came from is already stamped on the tool card that used them.
// The endpoint's own error text stays out: it is written for a terminal, and
// nothing here can act on it. Settings prints it beside the switch and the
// retry, so the row carries the way there instead — named, not hover-revealed,
// because a row that can be pressed and does not say so reads as a dead status.

// The rail reports, it does not enumerate — the same cap the delegate and file
// panels take, at the count two-line rows stop fitting beside everything else.
const SHOWN = 3;

export function Mcp({ servers, onOpen }: { servers: McpEntry[]; onOpen: () => void }) {
  // A server the repository declared and nobody has answered for needs a
  // decision as much as a broken one does — and unlike a broken one, it has
  // never run, so nothing else on screen would show it is missing.
  const waiting = servers.filter((s) => s.state === "failed" || s.state === "pending");
  if (waiting.length === 0) return null;
  const pending = waiting.filter((s) => s.state === "pending").length;
  const shown = waiting.slice(0, SHOWN);
  const count = pending === waiting.length
    ? t("{n} 个待批准", { n: pending })
    : pending === 0
      ? t("{n} 个连不上", { n: waiting.length })
      : t("{n} 个连不上 · {p} 个待批准", { n: waiting.length - pending, p: pending });

  return (
    <div className="block" data-b="mcp">
      <div className="lbl">
        {t("外部服务")}<span className="c">{count}</span>
      </div>
      <div className="srvs">
        {shown.map((s) => (
          <button className="srvrow" key={s.name} data-action="chrome.settings" onClick={onOpen}
            title={s.state === "pending" ? t("这个服务由仓库声明，到设置里决定是否允许它运行") : t("到设置的 MCP 面板里修复")}>
            <span className="hd">
              <i className="pip" data-s={s.state} />
              <span className="nm">{s.name}</span>
            </span>
            <span className="fix">{s.state === "pending" ? t("去批准") : t("去修复")}</span>
          </button>
        ))}
        {waiting.length > shown.length && (
          <span className="more">{t("还有 {n} 个，都在设置里", { n: waiting.length - shown.length })}</span>
        )}
      </div>
    </div>
  );
}
