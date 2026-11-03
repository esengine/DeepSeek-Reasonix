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
    ? t("mcp.awaitingApproval", { n: pending })
    : pending === 0
      ? t("mcp.unreachable", { n: waiting.length })
      : t("mcp.unreachableAwaitingApproval", { n: waiting.length - pending, p: pending });

  return (
    <div className="block" data-b="mcp">
      <div className="lbl">
        {t("common.externalServices")}<span className="c">{count}</span>
      </div>
      <div className="srvs">
        {shown.map((s) => (
          <button className="srvrow" key={s.name} data-action="chrome.settings" onClick={onOpen}
            title={s.state === "pending" ? t("mcp.thisServerWasDeclaredBy") : t("mcp.fixItInTheMCP")}>
            <span className="hd">
              <i className="pip" data-s={s.state} />
              <span className="nm">{s.name}</span>
            </span>
            <span className="fix">{s.state === "pending" ? t("mcp.approve") : t("mcp.fix")}</span>
          </button>
        ))}
        {waiting.length > shown.length && (
          <span className="more">{t("mcp.moreAllInSettings", { n: waiting.length - shown.length })}</span>
        )}
      </div>
    </div>
  );
}
