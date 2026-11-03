import { useRef, useState } from "react";
import { t } from "../i18n";
import type { AgentPort, PluginExport, PluginItem, PluginPackage } from "../port/port";
import { Switch } from "./Switch";
import { CopyButton } from "./CopyButton";
import { reason } from "../i18n/kernel";

interface Props {
  port: AgentPort;
  packages: PluginPackage[];
  onChanged: () => void;
  onReloadError?: (message: string) => void;
  onReloaded?: () => void;
  // Only one package can be mid-update: the confirmation is a full pane, and
  // two of them open at once would be two plans competing for one answer.
  updating: string;
  onUpdate: (name: string) => void;
}

export function Packages({ port, packages, onChanged, onReloadError, onReloaded, updating, onUpdate }: Props) {
  const [q, setQ] = useState("");
  const search = useRef<HTMLInputElement>(null);
  const [connection, setConnection] = useState({ port, generation: 0 });
  const currentConnection = useRef(connection);
  currentConnection.current = connection;
  if (connection.port !== port) setConnection({ port, generation: connection.generation + 1 });
  const query = q.trim().toLowerCase();
  const rows = packages.map((p) => ({ p, shown: matches(p, query) }));
  const shown = rows.filter((row) => row.shown).length;
  return (
    <>
      {packages.length > 0 && (
        <div className="msearch">
          <input ref={search} type="search" value={q} spellCheck={false}
            aria-label={t("packages.searchInstalledPackages")} placeholder={t("packages.searchPackageNamesSkillsCommands")}
            data-action="extensions.search" onChange={(e) => setQ(e.target.value)} />
          {q && <button className="act ghost" data-action="extensions.clear-search" onClick={() => { setQ(""); search.current?.focus({ preventScroll: true }); }}>{t("common.clearSearch")}</button>}
          <span className="cnt" aria-live="polite" aria-atomic="true">{t("packages.packages", { shown, total: packages.length })}</span>
        </div>
      )}
      {packages.length > 0 && query && shown === 0 && <div className="empty">{t("packages.noMatchingInstalledPackages")}</div>}
      {rows.map(({ p, shown }) => (
        <Package
          key={`${connection.generation}:${p.name}`}
          p={p}
          hidden={!shown}
          port={port}
          onDone={() => {
            if (currentConnection.current === connection) onChanged();
          }}
          onReloadError={(message) => {
            if (currentConnection.current === connection) onReloadError?.(message);
          }}
          onReloaded={() => {
            if (currentConnection.current === connection) onReloaded?.();
          }}
          updating={updating}
          onUpdate={() => onUpdate(p.name)}
        />
      ))}
    </>
  );
}

function matches(p: PluginPackage, query: string): boolean {
  if (!query) return true;
  const fields = [
    p.name,
    ...[p.skills, p.commands, p.agents, p.prompts, p.themes].flatMap((items) =>
      items?.flatMap((item) => [item.name, item.invocation, item.description]) ?? []),
    ...(p.mcpServers?.flatMap((server) => [server.name, server.displayName, server.description, server.command, server.url, server.transport]) ?? []),
    ...(p.hooks?.flatMap((hook) => [hook.event, hook.description, hook.command, hook.contextFile]) ?? []),
    p.runtime && [p.runtime.command, ...(p.runtime.args ?? [])].join(" "),
    ...(p.runtime?.tools ?? []),
  ];
  return fields.some((field) => field?.toLowerCase().includes(query));
}

// The summary line answers "what did this bring", and the two halves of that
// answer are not equal: skills and commands are entries in a menu, while hooks,
// servers and a runtime are code that runs. Counting them together would let
// the second hide inside the first.
function summary(p: PluginPackage): string {
  const parts: string[] = [];
  // 单位跟着数量走一句译文，而不是数字加一个词——英文里那样拼不出复数。
  const add = (n: number | undefined, unit: string) => {
    if (n) parts.push(t(unit, { n }));
  };
  add(p.skills?.length, "common.skills");
  add(p.commands?.length, "common.commands");
  add(p.agents?.length, "common.subagents");
  add(p.prompts?.length, "common.prompts");
  add(p.themes?.length, "common.palettes");
  add(p.hooks?.length, "common.hooks");
  add(p.mcpServers?.length, "common.services");
  if (p.runtime) parts.push(t("common.longRunningProcess"));
  return parts.join(" · ");
}

function Package({
  p, hidden, port, onDone, onReloadError, onReloaded, updating, onUpdate,
}: {
  p: PluginPackage; port: AgentPort; onDone: () => void; updating: string; onUpdate: () => void;
  hidden: boolean;
  onReloadError: (message: string) => void;
  onReloaded: () => void;
}) {
  const [busy, setBusy] = useState("");
  const [failed, setFailed] = useState("");
  const [confirming, setConfirming] = useState(false);
  const [exported, setExported] = useState<PluginExport | null>(null);
  const locked = !!busy || updating === p.name;

  const run = async (what: string, fn: () => Promise<unknown>) => {
    setBusy(what);
    setFailed("");
    try {
      await fn();
    } catch (e) {
      setFailed(reason(e));
    } finally {
      setBusy("");
      onDone();
    }
  };

  const meta = [p.version, p.manifestKind, p.source].filter(Boolean).join(" · ");
  const actions = (
    <span className="acts">
      {/* Only a package that recorded where it came from can be fetched again;
          one installed from a folder that has since moved cannot. */}
      {p.source && (
        <button className="act ghost" data-action="extensions.update" disabled={locked || !!updating} onClick={onUpdate}>
          {t("common.update")}
        </button>
      )}
      {/* Export and install are one door seen from both sides, so this button
          says what the archive is for rather than naming a format. */}
      <button
        className="act ghost"
        data-action="extensions.export"
        disabled={locked}
        onClick={() =>
          void run("export", async () => {
            setExported(null);
            setExported(await port.exportPlugin(p.name));
          })
        }
      >
        {t(busy === "export" ? "packages.packing" : "common.export")}
      </button>
      <button className="act ghost" aria-label={t("common.remove2", { name: p.name })} disabled={locked} onClick={() => setConfirming(true)}>
        {t("common.remove")}
      </button>
      <Switch
        data-action="extensions.enabled"
        data-target={p.name}
        on={p.enabled}
        busy={locked}
        label={`${t(p.enabled ? "common.close" : "packages.enable")} ${p.name}`}
        onClick={() => void run("toggle", async () => {
          const out = await port.setPluginEnabled(p.name, !p.enabled);
          if (out.reloadError) onReloadError(out.reloadError);
          else onReloaded();
        })}
      />
    </span>
  );

  const confirm = confirming && (
    <div className="confirm">
      <span className="q">
        {t("packages.deleteTheSkillsCommandsAnd", { name: p.name })}
      </span>
      <button className="act" disabled={locked} onClick={() => setConfirming(false)}>
        {t("common.cancel")}
      </button>
      <button
        className="act danger"
        data-action="extensions.remove"
        data-target={p.name}
        disabled={locked}
        onClick={() =>
          void run("remove", async () => {
            const out = await port.removePlugin(p.name);
            setConfirming(false);
            if (out.applied) {
              if (out.reloadError) onReloadError(out.reloadError);
              else onReloaded();
            }
            if (!out.ok) setFailed(out.error || out.next || t("packages.itCouldNotBeRemoved"));
          })
        }
      >
        {t(busy === "remove" ? "common.removing" : "common.delete")}
      </button>
    </div>
  );

  const head = (
    <>
      <i className="pip" />
      <span className="nm" title={p.name}>{p.name}</span>
      <span className="fold" title={summary(p) || undefined}>{summary(p) || t("packages.nothingUsableInside")}</span>
      <span className="meta" title={meta || undefined}>{meta}</span>
      {actions}
    </>
  );

  const notes = (
    <>
      {p.error && <div className="why">{p.error}</div>}
      {failed && <div className="why" role="alert">{failed}</div>}
      {p.status === "disabled_incompatible" && (
        <div className="why">{p.statusReason || t("packages.thisPackageDoesNotWork")}</div>
      )}
      {p.warnings?.map((w) => (
        <div className="why" key={w}>
          {w}
        </div>
      ))}
      {exported && (
        <div className="why" role="status">
          {exported.savedTo ? t("packages.savedTo", { path: exported.savedTo }) : t("packages.exported")}
          {" "}
          {exported.required.length
            ? t("packages.theKeyValuesWereStripped", { names: exported.required.join("、") })
            : t("packages.thisPackageAsksForNo")}
        </div>
      )}
      {exported?.savedTo && (
        <div className="acts">
          <CopyButton text={exported.savedTo} label={t("common.copyPath")} />
        </div>
      )}
    </>
  );

  return (
    <details className="srv" hidden={hidden} data-extension-name={p.name} data-st={p.enabled ? "ready" : "disabled"} aria-busy={locked} open={confirming || !!failed || !!exported || undefined}>
      <summary>{head}</summary>
      {confirm}
      {notes}
      <div className="peek">
        {/* What executes comes first: it is the part of a package a user has to
            know about, and it is also the part they cannot see from a name. */}
        {p.runtime && (
          <div className="row" data-run>
            <span className="d">▸</span>
            <span>{t("common.longRunningProcess")}</span>
            <span className="sc">{[p.runtime.command, ...(p.runtime.args ?? [])].join(" ")}</span>
          </div>
        )}
        {p.runtime?.tools?.map((name) => (
          <div className="row" data-run key={"tool:" + name}>
            <span className="d">▸</span>
            <span>{t("common.tool")}</span>
            <span className="sc">{name}</span>
          </div>
        ))}
        {p.hooks?.map((h, index) => (
          <div className="row" data-run key={index}>
            <span className="d">▸</span>
            <span>{h.event}</span>
            <span className="sc">{h.description || h.command || h.contextFile}</span>
          </div>
        ))}
        {p.mcpServers?.map((s) => (
          <div className="row" data-run key={s.name}>
            <span className="d">▸</span>
            <span>{s.displayName || s.name}</span>
            <span className="sc">{s.description || s.command || s.url || s.transport}</span>
          </div>
        ))}
        <Contributions label={t("packages.skills")} items={p.skills} />
        <Contributions label={t("packages.commands")} items={p.commands} />
        <Contributions label={t("packages.subagents")} items={p.agents} />
        <Contributions label={t("packages.prompts")} items={p.prompts} />
        <Contributions label={t("packages.themes")} items={p.themes} />
        {p.skipped?.map((s, index) => (
          <div className="row" key={index}>
            <span className="d">·</span>
            <span>{s.capability}</span>
            <span className="sc">{t("packages.unusable", { why: s.reason })}</span>
          </div>
        ))}
      </div>
    </details>
  );
}

function Contributions({ label, items }: { label: string; items?: PluginItem[] }) {
  if (!items?.length) return null;
  return (
    <div className="contributions" role="group" aria-label={label}>
      <h4 className="kind">{label}</h4>
      {items.map((it, index) => (
        <div className="row" key={index}>
          <span className="d">·</span>
          <span>{it.invocation || it.name}</span>
          <span className="sc">{it.description}</span>
          {it.invocation && <CopyButton key={it.invocation} text={it.invocation} iconOnly label={t("common.copyInvocation", { command: it.invocation })} />}
        </div>
      ))}
    </div>
  );
}
