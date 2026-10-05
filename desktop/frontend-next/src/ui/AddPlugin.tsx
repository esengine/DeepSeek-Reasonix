import { useEffect, useId, useRef, useState } from "react";
import { useEscape } from "./dismiss";
import { current as language, plural, t } from "../i18n";
import { useFileDrop } from "./filedrop";
import type { AgentPort, PluginAction, PluginPackage, PluginPlan } from "../port/port";
import { reason } from "../i18n/kernel";

// Installing and importing are the same act from two doors, so there is one
// box: a link, or a folder picked off this machine.
const PLACEHOLDER = `https://github.com/acme/review-kit

~/projects/my-plugin`;

const RISK_TITLE: Record<string, string> = {
  high: "以下项目会在本机运行程序",
  medium: "以下项目会改变可用能力",
  low: "以下项目仅添加文件",
};

export const ORDER = ["high", "medium", "low"];

interface Props {
  port: AgentPort;
  onClose: () => void;
  onInstalled: () => void;
  // Set when this is an update: the package being replaced. Its source is
  // already known, so the box is skipped and the plan is read against what is
  // installed — a new version's new hook is the thing worth seeing.
  updating?: PluginPackage;
  // A source someone already chose, such as a plugin named in a restored backup.
  source?: string;
  onApplying?: (applying: boolean) => void;
}

export function AddPlugin({ port, onClose, onInstalled, updating, source, onApplying }: Props) {
  const sourceLabel = useId();
  const [text, setText] = useState(updating?.source ?? source ?? "");
  const [plan, setPlan] = useState<PluginPlan | null>(null);
  const [done, setDone] = useState<PluginPlan | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const apply = useRef<HTMLButtonElement>(null);
  useEscape(true, () => { if (!updating || !plan || !busy) onClose(); });
  useEffect(() => {
    const readingUpdate = !!updating?.name && !done && !plan;
    if (readingUpdate || (!busy && (done || (updating?.name && plan)))) apply.current?.focus();
  }, [updating?.name, plan, done, busy, error]);

  const request = (planId?: string) => ({
    source: text.trim(),
    name: updating?.name,
    replace: !!updating,
    planId,
  });

  const look = async () => {
    if (busy || !text.trim()) return;
    setBusy(true);
    setError("");
    try {
      const p = await port.planPlugin(request());
      // "Nothing installable here" is the plan's own answer, and its reason is
      // more useful than anything this component could invent.
      if (!p.actions?.length) {
        setError(p.error || p.next || t("该来源中没有可安装的内容"));
        setPlan(null);
        return;
      }
      setPlan(p);
    } catch (e) {
      setPlan(null);
      setError(reason(e));
    } finally {
      setBusy(false);
    }
  };

  const install = async () => {
    if (!plan) return;
    setBusy(true);
    onApplying?.(true);
    setError("");
    try {
      const out = await port.installPlugin(request(plan.planId));
      setDone(out);
      if (out.applied) onInstalled();
    } catch (e) {
      setError(reason(e));
    } finally {
      setBusy(false);
      onApplying?.(false);
    }
  };

  const pick = async () => {
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      const dir = await port.pickFolder();
      if (dir) setText(dir);
    } catch (e) {
      setError(reason(e));
    } finally {
      setBusy(false);
    }
  };

  // An update has nothing to type: its source was recorded at install. Going
  // straight to the plan is what makes the button say "update" and not "fill
  // in where this came from again".
  useEffect(() => {
    if (updating?.source) void look();
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  // A dropped folder is a source typed for you. It fills the box rather than
  // planning straight away: dropping is how you point at something, not how
  // you agree to install it. A tab has no path to fill it with, so nothing is
  // typed there and the box still takes an address.
  const [over, setOver] = useState(false);
  const drop = useFileDrop((d) => !busy && d.paths[0] && setText(d.paths[0]), setOver);

  if (done) {
    return (
      <div className="addpkg" data-stage="done">
        <Outcome plan={done} />
        <div className="acts">
          {!done.ok && (
            <button className="act" data-action={updating ? "extensions.inspect" : "extensions.back"} data-primary ref={apply} disabled={busy} onClick={() => {
              setDone(null);
              setPlan(null);
              if (updating) void look();
            }}>
              {t(updating ? "重试" : "返回")}
            </button>
          )}
          <button className="act" data-action="extensions.finish" ref={done.ok ? apply : undefined} onClick={onClose}>
            {t("完成")}
          </button>
        </div>
      </div>
    );
  }

  if (plan) {
    const groups = ORDER.map((level) => ({
      level,
      actions: (plan.actions ?? []).filter((a) => (a.riskLevel || "low") === level),
    })).filter((g) => g.actions.length > 0);
    const gained = updating ? newlyGained(updating, plan.actions ?? []) : [];
    return (
      <div className="addpkg" data-stage="confirm" aria-busy={busy}>
        {/* What this version brings that the installed one did not. A package
            that was only skills last time and starts a process this time is
            the case the whole confirmation exists for. */}
        {updating && (
          <div className="find" data-lvl={gained.length ? "warn" : undefined}>
            <span className="t">
              {version(plan.actions) && updating.version
                ? `${updating.name} ${updating.version} → ${version(plan.actions)}`
                : t("重装 {name}", { name: updating.name })}
            </span>
            <span className="why">{gained.length ? t("本版本新增：{list}", { list: gained.join(language() === "zh" ? "、" : " · ") }) : t("没有新增的可执行内容。")}</span>
          </div>
        )}
        {groups.map((g) => (
          <section className="rgrp" key={g.level} data-lvl={g.level}>
            <h3>{t(RISK_TITLE[g.level] ?? g.level)}</h3>
            {g.actions.map((a, i) => (
              <Candidate a={a} key={`${a.kind}:${a.name}:${i}`} />
            ))}
          </section>
        ))}
        {plan.warnings?.map((wmsg) => (
          <div className="why" key={wmsg}>
            {wmsg}
          </div>
        ))}
        <div className="acts">
          <span className="note">{t(updating ? "覆盖已安装的版本" : "安装到「我的」，所有项目均可使用")}</span>
          <button
            className="act"
            data-action={updating ? "extensions.cancel" : "extensions.back"}
            disabled={busy}
            onClick={() => (updating ? onClose() : setPlan(null))}
          >
            {t(updating ? "取消" : "返回")}
          </button>
          <button
            className="act"
            data-action={updating ? "extensions.update" : "extensions.install"}
            ref={apply}
            data-primary
            disabled={busy}
            onClick={() => void install()}
          >
            {t(busy ? (updating ? "更新中…" : "安装中…") : updating ? "更新" : "安装")}
          </button>
        </div>
        {error && <div className="why" role="alert">{error}</div>}
      </div>
    );
  }

  // An update never shows the box: its source is not something to re-enter, and
  // a failure there is about the recorded source, not about what was typed.
  if (updating) {
    return (
      <div className="addpkg" data-stage="reading" aria-busy={busy}>
        <div className="find" data-lvl={error ? "err" : undefined}>
          <span className="t">{error ? t("无法读取 {name} 的来源", { name: updating.name }) : t("正在读取 {name} 的来源…", { name: updating.name })}</span>
          <span className="why">{error || updating.source}</span>
        </div>
        <div className="acts">
          {error && <button className="act" data-action="extensions.inspect" data-primary ref={apply} disabled={busy} onClick={() => void look()}>{t("重试")}</button>}
          <button className="act" data-action="extensions.cancel" ref={error ? undefined : apply} onClick={onClose}>
            {t(error ? "关掉" : "取消")}
          </button>
        </div>
      </div>
    );
  }

  return (
    <div className="addpkg" data-stage="paste" ref={drop} data-over={over ? "" : undefined} aria-busy={busy}>
      <textarea
        className="paste"
        aria-labelledby={sourceLabel}
        data-action-keydown="extensions.inspect"
        rows={3}
        autoFocus
        value={text}
        disabled={busy}
        placeholder={PLACEHOLDER}
        onChange={(e) => setText(e.target.value)}
        onKeyDown={(e) => {
          if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
            e.preventDefault();
            void look();
          }
        }}
      />
      <div className="acts">
        <span className="note" id={sourceLabel}>{t("仓库地址，或将文件夹拖入此处")}</span>
        <button className="act" data-action="extensions.pick-folder" disabled={busy} onClick={() => void pick()}>
          {t("选文件夹")}
        </button>
        <button className="act" data-action="extensions.cancel" onClick={onClose}>
          {t("取消")}
        </button>
        <button
          className="act"
          data-action="extensions.inspect"
          data-primary
          disabled={!text.trim() || busy}
          onClick={() => void look()}
        >
          {t(busy ? "读取中…" : "查看内容")}
        </button>
      </div>
      {error && <div className="why" role="alert">{error}</div>}
    </div>
  );
}

// The plan's own riskReasons are written for the model that reads the tool's
// JSON. What the user needs is the same facts as rows they can scan, so the
// structured counts are rendered here and the kernel's wording stays behind a
// fold — where a reason this UI does not know about yet is still readable.
export function Candidate({ a }: { a: PluginAction }) {
  const meta = [a.version, a.manifestKind, a.transport].filter(Boolean).join(" · ");
  const runs = executes(a);
  const adds = contributes(a);
  return (
    <div className="cand">
      <div className="cand-hd">
        <span className="nm">{a.name || a.kind}</span>
        {meta && <span className="meta">{meta}</span>}
      </div>
      {runs.map((r) => (
        <div className="risk" data-kind="shell" key={r.label}>
          <span className="lb">{t(r.label)}</span>
          <span className="dt">{r.detail}</span>
          {r.why && <span className="why">{t(r.why)}</span>}
        </div>
      ))}
      {adds.length > 0 && (
        <div className="risk">
          <span className="lb">{t("将添加")}</span>
          <span className="dt">{adds.join(" · ")}</span>
        </div>
      )}
      {Object.keys(a.env ?? {}).map((k) => (
        <div className="risk" data-kind="secret" key={k}>
          <span className="lb">{t("需填写")}</span>
          <span className="dt">{k}</span>
        </div>
      ))}
      {a.skippedCapabilities?.map((s) => (
        <div className="risk" key={s.capability + s.reason}>
          <span className="lb">{t("不可用")}</span>
          <span className="dt">{s.capability}</span>
          <span className="why">{s.reason}</span>
        </div>
      ))}
      {a.riskReasons?.length ? (
        <details className="reasons">
          <summary>{t("内核给出的判定（{n}）", { n: a.riskReasons.length })}</summary>
          {a.riskReasons.map((r) => (
            <p key={r}>{r}</p>
          ))}
        </details>
      ) : null}
    </div>
  );
}

// What will execute. A process, a hook and an external server are each their
// own row: "one more skill" and "one more process" are not the same decision,
// and a single count would let the second hide inside the first.
function executes(a: PluginAction): { label: string; detail: string; why?: string }[] {
  const out: { label: string; detail: string; why?: string }[] = [];
  if (a.runtime) {
    out.push({
      label: "常驻进程",
      detail: [a.runtime.command, ...(a.runtime.args ?? [])].join(" "),
      why: "运行于 Reasonix 内部，可读取整个会话、绕过权限并直接操作本机",
    });
  }
  if (a.runtime?.tools?.length) {
    out.push({ label: "新增工具", detail: a.runtime.tools.join(language() === "zh" ? "、" : ", "), why: "agent 可以调用，每次调用照常经过权限确认" });
  }
  if (a.hookCount) {
    out.push({ label: "自动化钩子", detail: language() === "zh" ? t("{n} 条", { n: a.hookCount }) : plural(a.hookCount, "1 条钩子", "{n} 条钩子"), why: "在会话生命周期内自动执行" });
  }
  if (a.toolCount) {
    out.push({ label: "外部服务", detail: plural(a.toolCount, "1 个 MCP 服务", "{n} 个 MCP 服务"), why: "为 agent 提供的能力与内置工具等同" });
  }
  if (a.kind === "mcp" && a.command) {
    out.push({ label: "将启动", detail: [a.command, ...(a.args ?? [])].join(" ") });
  }
  if (a.kind === "mcp" && a.url) {
    out.push({ label: "将连接至", detail: a.url });
  }
  return out;
}

function version(actions?: PluginAction[]): string {
  return actions?.find((a) => a.kind === "plugin")?.version ?? "";
}

// newlyGained compares what runs, not what the package totals. Losing a hook is
// not something to warn about, and one more skill is not either — the question
// an update has to answer is whether it now does something it could not before.
function newlyGained(current: PluginPackage, actions: PluginAction[]): string[] {
  const plugin = actions.find((a) => a.kind === "plugin");
  const out: string[] = [];
  if (plugin?.runtime && !current.runtime) out.push(t("一个常驻进程"));
  const tools = (plugin?.runtime?.tools ?? []).filter((name) => !current.runtime?.tools?.includes(name));
  if (tools.length > 0) out.push(t("工具 {names}", { names: tools.join(language() === "zh" ? "、" : ", ") }));
  const hooks = (plugin?.hookCount ?? 0) - (current.hooks?.length ?? 0);
  if (hooks > 0) out.push(plural(hooks, "一条钩子", "{n} 条钩子"));
  const servers = (plugin?.toolCount ?? 0) - (current.mcpServers?.length ?? 0);
  if (servers > 0) out.push(plural(servers, "一个外部服务", "{n} 个外部服务"));
  // A separate MCP action installs a server of its own, outside the package.
  const standalone = actions.filter((a) => a.kind === "mcp").length;
  if (standalone > 0) out.push(plural(standalone, "一个独立的 MCP 服务", "{n} 个独立的 MCP 服务"));
  return out;
}

function contributes(a: PluginAction): string[] {
  const parts: string[] = [];
  const add = (n: number | undefined, one: string, many: string) => {
    if (n) parts.push(plural(n, one, many));
  };
  add(a.skillCount, "1 个技能", "{n} 个技能");
  add(a.commandCount, "1 个命令", "{n} 个命令");
  add(a.agentCount, "1 个子代理", "{n} 个子代理");
  add(a.promptCount, "1 个提示词", "{n} 个提示词");
  add(a.themeCount, "1 套配色", "{n} 套配色");
  return parts;
}

// "Written" and "running" are different outcomes: a reload refused mid-turn
// leaves the package on disk and out of this session, and saying so is the
// difference between waiting and re-installing.
export function Outcome({ plan }: { plan: PluginPlan }) {
  const state = !plan.ok ? "issue" : plan.reloadError ? "action_required" : "ready";
  const failed = plan.actions?.filter((action) => action.status === "failed") ?? [];
  return <>
    <div className="outcome" data-state={state}>
      <i className="pip" />
      <span className="nm">{plan.actions?.[0]?.name || t("安装")}</span>
      <span className="dt">
        {state === "ready" && t("装好了，下一轮就能用")}
        {state === "action_required" && t("装好了，但运行时未重载：{reason}。请用「重载运行时」重试。", { reason: plan.reloadError! })}
        {state === "issue" && (plan.error || (failed.length ? t("有项目未安装成功，原因见下方。") : plan.next || t("没装上")))}
      </span>
    </div>
    {failed.map((action, i) => (
      <div className="why" role="alert" key={`${action.kind}:${action.name}:${i}`}>
        {action.name || action.kind}: {action.error || action.next || t("没装上")}
        {action.error && action.next && <div>{action.next}</div>}
      </div>
    ))}
    {state === "issue" && plan.reloadError && (
      <div className="why" role="status">
        {t("运行时未重载：{reason}。请用「重载运行时」重试。", { reason: plan.reloadError })}
      </div>
    )}
  </>;
}
