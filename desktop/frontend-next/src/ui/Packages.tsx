import { useRef, useState } from "react";
import { t } from "../i18n";
import type { AgentPort, PluginExport, PluginItem, PluginPackage } from "../port/port";
import { Switch } from "./Switch";
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
  const [connection, setConnection] = useState({ port, generation: 0 });
  const currentConnection = useRef(connection);
  currentConnection.current = connection;
  if (connection.port !== port) setConnection({ port, generation: connection.generation + 1 });
  return (
    <>
      {packages.map((p) => (
        <Package
          key={`${connection.generation}:${p.name}`}
          p={p}
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
  add(p.skills?.length, "{n} 个技能");
  add(p.commands?.length, "{n} 个命令");
  add(p.agents?.length, "{n} 个子代理");
  add(p.prompts?.length, "{n} 个提示词");
  add(p.themes?.length, "{n} 套配色");
  add(p.hooks?.length, "{n} 条钩子");
  add(p.mcpServers?.length, "{n} 个服务");
  if (p.runtime) parts.push(t("常驻进程"));
  return parts.join(" · ");
}

function Package({
  p, port, onDone, onReloadError, onReloaded, updating, onUpdate,
}: {
  p: PluginPackage; port: AgentPort; onDone: () => void; updating: string; onUpdate: () => void;
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
          {t("更新")}
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
        {t(busy === "export" ? "打包中…" : "导出")}
      </button>
      <button className="act ghost" aria-label={t("移除 {name}", { name: p.name })} disabled={locked} onClick={() => setConfirming(true)}>
        {t("移除")}
      </button>
      <Switch
        data-action="extensions.enabled"
        data-target={p.name}
        on={p.enabled}
        busy={locked}
        label={`${t(p.enabled ? "关闭" : "启用")} ${p.name}`}
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
        {t("删除 {name}？其提供的技能、命令与服务将一并移除。若只是暂时停用，关闭开关即可。", { name: p.name })}
      </span>
      <button className="act" disabled={locked} onClick={() => setConfirming(false)}>
        {t("取消")}
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
            if (out.applied && out.reloadError) onReloadError(out.reloadError);
            if (!out.ok) setFailed(out.error || out.next || t("没能删掉"));
          })
        }
      >
        {t(busy === "remove" ? "移除中…" : "删除")}
      </button>
    </div>
  );

  const head = (
    <>
      <i className="pip" />
      <span className="nm" title={p.name}>{p.name}</span>
      <span className="fold" title={summary(p) || undefined}>{summary(p) || t("没有可用的贡献")}</span>
      <span className="meta" title={meta || undefined}>{meta}</span>
      {actions}
    </>
  );

  const notes = (
    <>
      {p.error && <div className="why">{p.error}</div>}
      {failed && <div className="why" role="alert">{failed}</div>}
      {p.status === "disabled_incompatible" && (
        <div className="why">{p.statusReason || t("这个包和当前版本不兼容，已经被停用。")}</div>
      )}
      {p.warnings?.map((w) => (
        <div className="why" key={w}>
          {w}
        </div>
      ))}
      {exported && (
        <div className="why" role="status">
          {exported.savedTo ? t("已保存至 {path}。", { path: exported.savedTo }) : t("导出完成。")}
          {" "}
          {exported.required.length
            ? t("里面的密钥值已经去掉，装它的人要自己提供：{names}", { names: exported.required.join("、") })
            : t("该包不需要填写任何密钥。")}
        </div>
      )}
    </>
  );

  return (
    <details className="srv" data-extension-name={p.name} data-st={p.enabled ? "ready" : "disabled"} aria-busy={locked} open={confirming || !!failed || !!exported || undefined}>
      <summary>{head}</summary>
      {confirm}
      {notes}
      <div className="peek">
        {/* What executes comes first: it is the part of a package a user has to
            know about, and it is also the part they cannot see from a name. */}
        {p.runtime && (
          <div className="row" data-run>
            <span className="d">▸</span>
            <span>{t("常驻进程")}</span>
            <span className="sc">{[p.runtime.command, ...(p.runtime.args ?? [])].join(" ")}</span>
          </div>
        )}
        {p.runtime?.tools?.map((name) => (
          <div className="row" data-run key={"tool:" + name}>
            <span className="d">▸</span>
            <span>{t("工具")}</span>
            <span className="sc">{name}</span>
          </div>
        ))}
        {p.hooks?.map((h) => (
          <div className="row" data-run key={h.event + h.command}>
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
        <Contributions items={p.skills} />
        <Contributions items={p.commands} />
        <Contributions items={p.agents} />
        <Contributions items={p.prompts} />
        <Contributions items={p.themes} />
        {p.skipped?.map((s, index) => (
          <div className="row" key={index}>
            <span className="d">·</span>
            <span>{s.capability}</span>
            <span className="sc">{t("用不了：{why}", { why: s.reason })}</span>
          </div>
        ))}
      </div>
    </details>
  );
}

function Contributions({ items }: { items?: PluginItem[] }) {
  if (!items?.length) return null;
  return (
    <>
      {items.map((it, index) => (
        <div className="row" key={index}>
          <span className="d">·</span>
          <span>{it.invocation || it.name}</span>
          <span className="sc">{it.description}</span>
        </div>
      ))}
    </>
  );
}
