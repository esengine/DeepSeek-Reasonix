import { useRef, useState } from "react";
import { t } from "../i18n";
import type { AgentPort, McpEntry } from "../port/port";
import { Exception } from "./CapabilityScope";
import { Clip } from "./Clip";
import { Switch } from "./Switch";
import { reason } from "../i18n/kernel";
import { listMatcher } from "./listsearch";
import "../styles/mcpheld.css";

// One external service: whether it is answering, what it brought, and the two
// acts that change that — switching it off and letting it try again.
// standby is the ordinary state of a working server, not a lesser kind of
// connected: its tools are in the catalog and the process starts on the first
// call. Calling that "not connected" beside a Reconnect button reads as broken.
const MCP_STATE: Record<string, string> = {
  ready: "common.connected2",
  connecting: "common.connecting",
  failed: "common.unreachable",
  disabled: "serverRow.disabled",
  standby: "serverRow.standbyStartsOnFirstCall",
  idle: "common.notConnected",
  // Project declarations require approval before the server can launch.
  pending: "serverRow.awaitingApprovalDeclaredByThe",
};

// Why a pending server waits, keyed by the kernel's code. The state label
// already says a project declared it; a code with no entry adds nothing.
const PENDING_WHY: Record<string, string> = {
  changed_since_enabled: "serverRow.whatItLaunchesChangedSince",
};

// How a withheld tool differs from what the user approved, keyed by the kernel's
// class. A class with no entry is shown as the kernel wrote it.
const HELD_CLASS: Record<string, string> = {
  added: "common.added",
  changed: "serverRow.definitionChanged",
  unverifiable: "serverRow.cannotBeChecked",
};

// A tag only when the schema carries the server's tools or config asks it to:
// deferred is the default, and a badge on every row is a badge on none.
function loadTag(m: McpEntry): { text: string; pending: boolean } | null {
  if (m.alwaysLoad) return { text: m.inSchema ? t("serverRow.alwaysLoaded") : t("serverRow.alwaysLoadedNotYetIn"), pending: !m.inSchema };
  if (m.inSchema) return { text: t("serverRow.alwaysLoadedBeingRemoved"), pending: true };
  return null;
}

// The tool list is the long half and the least urgent, so it folds behind its
// own count — the same idiom a long file read uses in the transcript.
export function ServerRow({
  m, port, onDone, root, live,
}: {
  m: McpEntry; port: AgentPort; onDone: () => void; root: string; live: boolean;
}) {
  const [owner, setOwner] = useState({ port, root });
  const currentOwner = useRef(owner);
  currentOwner.current = owner;
  const [busy, setBusy] = useState("");
  const [failed, setFailed] = useState("");
  const [confirming, setConfirming] = useState(false);
  const [q, setQ] = useState("");
  const search = useRef<HTMLInputElement>(null);
  if (owner.port !== port || owner.root !== root) {
    setOwner({ port, root });
    setBusy("");
    setFailed("");
    setConfirming(false);
    setQ("");
  }
  // 状态码是主机答上来的事实，画出来即可；它是不是「需要你重新认证」还要看自动
  // 刷新有没有跑过，那件事目前无人知道。m.error 是外部服务器自己写的文本，只作
  // 详情显示,不参与任何判断。
  const meta = [t(MCP_STATE[m.state] ?? m.state), m.transport, m.source, m.httpStatus ? `HTTP ${m.httpStatus}` : ""]
    .filter(Boolean)
    .join(" · ");

  const run = async (what: string, fn: () => Promise<unknown>) => {
    setBusy(what);
    setFailed("");
    try {
      const r = (await fn()) as { error?: string } | void;
      if (currentOwner.current === owner && r && typeof r === "object" && r.error) setFailed(r.error);
    } catch (e) {
      if (currentOwner.current === owner) setFailed(reason(e));
    } finally {
      if (currentOwner.current === owner) setBusy("");
      onDone();
    }
  };

  const actions = (
    <span className="acts">
      {live && m.enabled && m.state !== "ready" && m.state !== "pending" && (
        <button className="act" data-action="mcp.retry" data-target={m.name} disabled={!!busy} onClick={() => void run("retry", () => port.reconnectMcp(m.name))}>
          {t(busy === "retry" ? "common.connecting3" : m.state === "standby" ? "serverRow.connectNow" : "serverRow.reconnect")}
        </button>
      )}
      {/* The server answered 401: the status is a fact the host drew, and signing in
          is the one thing that answers it, so it sits beside the retry. */}
      {live && m.enabled && m.state === "failed" && m.httpStatus === 401 && (
        <button className="act" data-action="mcp.signin" data-target={m.name} disabled={!!busy} onClick={() => void run("signin", () => port.signinMcp(m.name))}>
          {t(busy === "signin" ? "serverRow.waitingForTheBrowserSign" : "common.signIn")}
        </button>
      )}
      {/* Removal is the one action here that cannot be undone by clicking again,
          so it asks — and the question names the file it is about to edit. */}
      {live && (
        <button className="act ghost" data-action="mcp.remove" data-target={m.name} aria-label={t("common.remove2", { name: m.name })} disabled={!!busy} onClick={() => setConfirming(true)}>
          {t("common.remove")}
        </button>
      )}
      <Switch
        data-action="mcp.enabled"
        data-target={m.name}
        on={m.enabled}
        busy={!!busy}
        label={t(m.enabled ? "common.disable" : "common.enable", { name: m.name })}
        onClick={() => void run("toggle", () => port.setMcpEnabled(m.name, !m.enabled, "project", root || undefined))}
      />
    </span>
  );

  const confirm = confirming && (
    <div className="confirm">
      <span className="q">
        {t("serverRow.deleteFromIfYouOnly", { name: m.name, where: m.source || t("serverRow.theConfig") })}
      </span>
      <button className="act" data-action="mcp.remove" data-target={m.name} data-value="cancel" disabled={!!busy} onClick={() => setConfirming(false)}>
        {t("common.cancel")}
      </button>
      <button
        className="act danger"
        data-action="mcp.remove"
        data-target={m.name}
        data-value="confirm"
        disabled={!!busy}
        onClick={() =>
          void run("remove", async () => {
            const r = await port.removeMcp(m.name);
            if (currentOwner.current !== owner) return;
            setConfirming(false);
            // A lower-precedence declaration with the same name may have taken
            // over; saying so beats a list that looks like the delete failed.
            if (r.stillConfigured) setFailed(t("serverRow.anotherDeclarationOfTheSame"));
          })
        }
      >
        {t(busy === "remove" ? "common.removing" : "common.remove")}
      </button>
    </div>
  );

  const heldTools = m.held ?? [];
  // The approval is for the exact set the kernel withheld; the digest it sent is
  // handed back, and a set that changed meanwhile is refused rather than waved through.
  const held = heldTools.length > 0 && !!m.heldDigest && (
    <div className="srv-held" role="group" aria-label={t("serverRow.toolsWaitingForYourApproval")}>
      <span className="ds">
        {t("serverRow.ofThisServiceSTools", { n: heldTools.length + (m.heldMore ?? 0) })}
      </span>
      <ul>
        {heldTools.map((h) => (
          <li key={h.name}>
            <b>{h.name}</b>
            <i>{t(HELD_CLASS[h.class] ?? h.class)}</i>
          </li>
        ))}
      </ul>
      {!!m.heldMore && <span className="hn">{t("serverRow.moreAreNotListedApproving", { n: m.heldMore })}</span>}
      {live && (
        <span className="acts">
          <button className="act" data-action="mcp.trust" data-target={m.name} disabled={!!busy} onClick={() => void run("trust", () => port.trustMcp(m.name, m.heldDigest!))}>
            {t(busy === "trust" ? "serverRow.approving" : "serverRow.approveAndReconnect")}
          </button>
          {/* Following is asked for after an approval, never instead of one: the
              kernel owes the approval first, and a refused approval stops here. */}
          <button className="act" data-action="mcp.follow" data-target={m.name} disabled={!!busy}
            onClick={() => void run("follow", async () => {
              const approved = await port.trustMcp(m.name, m.heldDigest!);
              if (approved.error) return approved;
              await port.followMcp(m.name, true);
            })}>
            {t(busy === "follow" ? "serverRow.approving" : "serverRow.acceptAndAcceptThisServer")}
          </button>
        </span>
      )}
    </div>
  );
  const asked = m.scopeUpgrade ?? [];
  // The scope names are the server's words: shown as written, never read for meaning.
  const upgrade = asked.length > 0 && (
    <div className="srv-held" role="group" aria-label={t("serverRow.thisServerAsksForMore")}>
      <span className="ds">{t("serverRow.thisServerRefusedACall")}</span>
      <code>{asked.join(" ")}</code>
      {live && (
        <button className="act" data-action="mcp.upgrade" data-target={m.name} disabled={!!busy} onClick={() => void run("upgrade", () => port.upgradeMcp(m.name))}>
          {t(busy === "upgrade" ? "serverRow.waitingForTheBrowserAuthorization" : "serverRow.grantMoreAccess")}
        </button>
      )}
    </div>
  );
  const following = m.followsChanges && (
    <div className="srv-held" role="group" aria-label={t("serverRow.changesFromThisServerAre")}>
      <span className="ds">{t("serverRow.thisServerSChangesAre")}</span>
      {live && (
        <button className="act" data-action="mcp.unfollow" data-target={m.name} disabled={!!busy} onClick={() => void run("unfollow", () => port.followMcp(m.name, false))}>
          {t(busy === "unfollow" ? "common.stopping" : "serverRow.stopFollowing")}
        </button>
      )}
    </div>
  );
  const tools = m.toolList ?? [];
  const searchable = tools.length > 5;
  const matches = listMatcher(searchable ? q : "");
  const shown = tools.filter((tool) => matches([tool.name, tool.description, tool.error]));
  const tag = loadTag(m);
  // The tool list is what gets loaded, so the choice sits above it; a server
  // whose tools are not known yet has nothing to load either way.
  const load = live && m.enabled && tools.length > 0 && (
    <div className="load">
      <span className="lb">{t("serverRow.loading")}</span>
      <div className="seg" data-text role="group" aria-label={t("serverRow.loading")}>
        {(["deferred", "always"] as const).map((mode) => (
          <button key={mode} data-action="mcp.load" data-target={m.name} data-value={mode}
            aria-pressed={(mode === "always") === !!m.alwaysLoad} disabled={!!busy}
            onClick={() => {
              if ((mode === "always") !== !!m.alwaysLoad) void run("load", () => port.setMcpLoad(m.name, mode));
            }}>
            {t(mode === "always" ? "serverRow.alwaysLoaded" : "serverRow.onDemand")}
          </button>
        ))}
      </div>
      <span className="hn">
        {t(m.alwaysLoad
          ? "serverRow.connectedAtStartupTheTools"
          : "serverRow.searchedForAndLoadedWhen")}
      </span>
    </div>
  );
  const head = (
    <>
      <i className="pip" />
      <Clip className="nm">{m.name}</Clip>
      {tools.length ? <Clip className="fold">{t("serverRow.tools", { n: m.tools })}</Clip> : null}
      {tag && <i className="ld" data-pending={tag.pending ? "" : undefined}>{tag.text}</i>}
      <Clip className="meta">{meta}</Clip>
      {m.localOverride && <Exception onClear={() => void run("clear", () => port.clearMcpOverride(m.name, root || undefined))} busy={!!busy} />}
      {actions}
    </>
  );
  // 服务是干什么的，只有服务自己说了算：MCP 握手里的那段自述。它没写，这里就
  // 没有 —— 拿名字或配置凑一句出来，等于替它编。
  const about = (!!m.description || m.remembered || (!tools.length && m.state !== "connecting")) && (
    <div className="srv-ab">
      <span className="ds">{m.description || t("serverRow.thisServiceDoesNotDescribe")}</span>
      {m.remembered && (
        <i className="w" title={t("serverRow.nothingIsConnectedRightNow")}>
          {t(m.stale ? "serverRow.fromTheLastTimeIt2" : "serverRow.fromTheLastTimeIt")}
        </i>
      )}
    </div>
  );
  // Approving a repository's server approves what it runs, so the line it
  // launches sits beside the switch that approves it.
  const launch = m.state === "pending" && !!m.launch && (
    <div className="srv-ab" data-launch>
      <span className="ds">{t("common.willStart")} · <code>{m.launch}</code></span>
    </div>
  );
  const pendingWhy = m.state === "pending" && m.pendingReason ? PENDING_WHY[m.pendingReason] : undefined;
  const why = failed || (pendingWhy ? t(pendingWhy) : m.error);
  if (!tools.length) {
    return (
      <div className="srv mcp-server" data-st={m.state} data-local={m.localOverride ? "" : undefined} aria-busy={!!busy}>
        <div className="srv-hd">{head}</div>
        {about}
        {launch}
        {held}{upgrade}{following}
        {why && <div className="why" role={failed ? "alert" : undefined}>{why}</div>}
        {confirm}
      </div>
    );
  }
  return (
    // Asking to remove has to open the row: the confirmation lives inside the
    // fold. A row with withheld tools opens too, so the approval is not behind a click.
    <details className="srv mcp-server" data-st={m.state} data-local={m.localOverride ? "" : undefined} aria-busy={!!busy} open={confirming || heldTools.length > 0 || undefined}>
      <summary>{head}</summary>
      {about}
      {launch}
      {held}{upgrade}{following}
      {why && <div className="why" role={failed ? "alert" : undefined}>{why}</div>}
      {confirm}
      {load}
      <div className="peek">
        {searchable && <div className="msearch">
          <input ref={search} type="search" data-action="mcp.tool-search" data-target={m.name}
            value={q} spellCheck={false} aria-label={t("serverRow.searchToolsIn", { name: m.name })}
            placeholder={t("serverRow.searchToolsIn", { name: m.name })} onChange={(e) => setQ(e.target.value)} />
          {q && <button className="act" data-action="mcp.tool-search-clear" data-target={m.name}
            onClick={() => { setQ(""); search.current?.focus(); }}>{t("common.clear")}</button>}
        </div>}
        {tools.length > 0 && shown.length === 0 && <div className="empty" role="status">{t("serverRow.noMatchingTool")}</div>}
        {shown.map((tool) => (
          // 一行一个工具：它叫什么、它自己说它干什么、以及这一刀下去会不会动
          // 你的东西。schema 被拒的那些照列，但写明为什么调不了。
          <div className="trow" key={tool.name} data-bad={tool.error ? "" : undefined}>
            <span className="nm">{tool.name}</span>
            <Clip className="ds">{tool.error || tool.description || t("common.noDescriptionWritten")}</Clip>
            <span className="face">
              {tool.destructive ? <i className="dg">{t("serverRow.modifiesData")}</i> : tool.readOnly ? <i className="ro">{t("common.readOnly")}</i> : null}
            </span>
          </div>
        ))}
      </div>
    </details>
  );
}

// How a skill can start is the thing the slash list cannot say: a slash name
// means you can call it, "auto" means the model may start it on its own, and
// those are separate permissions. Both is the norm, so only the row that is
// missing one says anything — a badge on every row is a badge on none.
