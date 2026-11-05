import { useRef, useState } from "react";
import { t } from "../i18n";
import type { AgentPort, PluginExport, PluginItem, PluginPackage } from "../port/port";
import { Switch } from "./Switch";
import { CopyButton } from "./CopyButton";
import { reason } from "../i18n/kernel";
import { onRowItself, usePickRow } from "./extpick";

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
            aria-label={t("ext.search_installed_packages")} placeholder={t("ext.search_package_names_skills_commands")}
            data-action="extensions.search" onChange={(e) => setQ(e.target.value)} />
          {q && <button className="act ghost" data-action="extensions.clear-search" onClick={() => { setQ(""); search.current?.focus({ preventScroll: true }); }}>{t("common.clear_search")}</button>}
          <span className="cnt" aria-live="polite" aria-atomic="true">{t("ext.packages_shown_total", { shown, total: packages.length })}</span>
        </div>
      )}
      {packages.length > 0 && query && shown === 0 && <div className="empty">{t("ext.no_matching_installed_packages")}</div>}
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
  add(p.skills?.length, "ext.n_skills");
  add(p.commands?.length, "ext.n_commands");
  add(p.agents?.length, "subagent.group.count");
  add(p.prompts?.length, "ext.n_prompts");
  add(p.themes?.length, "ext.n_palettes");
  add(p.hooks?.length, "ext.n_hooks");
  add(p.mcpServers?.length, "settings.n_services");
  if (p.runtime) parts.push(t("ext.long_running_process"));
  return parts.join(" · ");
}

export function Package({
  p, hidden, port, onDone, onReloadError, onReloaded, updating, onUpdate, detail = false,
}: {
  p: PluginPackage; port: AgentPort; onDone: () => void; updating: string; onUpdate: () => void;
  hidden: boolean;
  // Drawn as the extensions page's detail pane: everything at once, no fold.
  detail?: boolean;
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
          {t("ext.update")}
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
        {t(busy === "export" ? "ext.packing" : "ext.export")}
      </button>
      <button className="act ghost" aria-label={t("common.remove_name", { name: p.name })} disabled={locked} onClick={() => setConfirming(true)}>
        {t("common.remove")}
      </button>
    </span>
  );
  const toggle = (
      <Switch
        data-action="extensions.enabled"
        data-target={p.name}
        on={p.enabled}
        busy={locked}
        label={`${t(p.enabled ? "common.close" : "ext.enable")} ${p.name}`}
        onClick={() => void run("toggle", async () => {
          const out = await port.setPluginEnabled(p.name, !p.enabled);
          if (out.reloadError) onReloadError(out.reloadError);
          else onReloaded();
        })}
      />
  );

  const confirm = confirming && (
    <div className="confirm">
      <span className="q">
        {t("ext.delete_name_the_skills_commands", { name: p.name })}
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
            if (!out.ok) setFailed(out.error || out.next || t("ext.it_could_not_be_removed"));
          })
        }
      >
        {t(busy === "remove" ? "ext.removing_2" : "common.delete")}
      </button>
    </div>
  );

  const head = (
    <>
      <i className="pip" />
      <span className="srv-main">
        <span className="nm">{p.name}</span>
        <span className="srv-sub">
          <span className="fold" title={summary(p) || undefined}>{summary(p) || t("ext.nothing_usable_inside")}</span>
          {meta && <span className="meta" title={meta}>{meta}</span>}
        </span>
      </span>
      {toggle}
    </>
  );

  const notes = (
    <>
      {p.error && <div className="why">{p.error}</div>}
      {failed && <div className="why" role="alert">{failed}</div>}
      {p.status === "disabled_incompatible" && (
        <div className="why">{p.statusReason || t("ext.this_package_does_not_work")}</div>
      )}
      {p.warnings?.map((w) => (
        <div className="why" key={w}>
          {w}
        </div>
      ))}
      {exported && (
        <div className="why" role="status">
          {exported.savedTo ? t("ext.saved_to_path", { path: exported.savedTo }) : t("ext.exported")}
          {" "}
          {exported.required.length
            ? t("ext.the_key_values_were_stripped", { names: exported.required.join("、") })
            : t("ext.this_package_asks_for_no")}
        </div>
      )}
      {exported?.savedTo && (
        <div className="acts">
          <CopyButton text={exported.savedTo} label={t("common.copy_path")} />
        </div>
      )}
    </>
  );

  const row = usePickRow("package", p.name);
  const peek = <PackagePeek p={p} />;
  if (detail) {
    return (
      <div className="srv ext-detail" data-extension-name={p.name} data-st={p.enabled ? "ready" : "disabled"} aria-busy={locked}>
        <div className="ext-detail-hd">
          <i className="pip" />
          <b>{p.name}</b>
          {p.version && <span className="ext-ver">{p.version}</span>}
          <span className="ext-chip" data-on={p.enabled ? "" : undefined}>{t(p.enabled ? "ext.state_enabled" : "ext.state_disabled")}</span>
        </div>
        {p.description && <p className="ext-detail-ds">{p.description}</p>}
        {actions}
        {confirm}
        {notes}
        <dl className="ext-facts">
          {p.source && <><dt>{t("common.source")}</dt><dd>{p.source}</dd></>}
          <dt>{t("ext.location")}</dt><dd>{p.root}</dd>
          {p.manifestKind && <><dt>{t("ext.format")}</dt><dd>{p.manifestKind}</dd></>}
        </dl>
        <h4 className="ext-detail-sub">{t("ext.it_provides")}</h4>
        {peek}
      </div>
    );
  }
  return (
    <details className="srv" hidden={hidden} data-extension-name={p.name} data-picked={row?.on ? "" : undefined} data-st={p.enabled ? "ready" : "disabled"} aria-busy={locked} open={confirming || !!failed || !!exported || !!p.error || undefined}>
      <summary data-action-click={row ? "extensions.pick" : undefined} data-target={row ? p.name : undefined}
        onClick={row ? (e) => { e.preventDefault(); if (onRowItself(e.target)) row.pick(); } : undefined}>{head}</summary>
      {confirm}
      {notes}
      {actions}
      {peek}
    </details>
  );
}

function PackagePeek({ p }: { p: PluginPackage }) {
  return (
      <div className="peek">
        {/* What executes comes first: it is the part of a package a user has to
            know about, and it is also the part they cannot see from a name. */}
        {p.runtime && (
          <div className="row" data-run>
            <span className="d">▸</span>
            <span>{t("ext.long_running_process")}</span>
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
        <Contributions label={t("ext.skills")} items={p.skills} />
        <Contributions label={t("ext.commands")} items={p.commands} />
        <Contributions label={t("ext.subagents")} items={p.agents} />
        <Contributions label={t("ext.prompts")} items={p.prompts} />
        <Contributions label={t("ext.themes")} items={p.themes} />
        {p.skipped?.map((s, index) => (
          <div className="row" key={index}>
            <span className="d">·</span>
            <span>{s.capability}</span>
            <span className="sc">{t("ext.unusable_why", { why: s.reason })}</span>
          </div>
        ))}
      </div>
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
          {it.invocation && <CopyButton key={it.invocation} text={it.invocation} iconOnly label={t("ext.copy_invocation_command", { command: it.invocation })} />}
        </div>
      ))}
    </div>
  );
}
