import { useEffect, useState } from "react";
import { t } from "../i18n";
import { useFileDrop } from "./filedrop";
import type { AgentPort, PluginAction, PluginPackage, PluginPlan } from "../port/port";
import { reason } from "../i18n/kernel";

// Installing and importing are the same act from two doors, so there is one
// box: a link, or a folder picked off this machine.
const PLACEHOLDER = `https://github.com/acme/review-kit

~/projects/my-plugin`;

const RISK_TITLE: Record<string, string> = {
  high: "这些会在你机器上跑东西",
  medium: "这些会改变可用的能力",
  low: "这些只是加文件",
};

const ORDER = ["high", "medium", "low"];

interface Props {
  port: AgentPort;
  onClose: () => void;
  onInstalled: () => void;
  // Set when this is an update: the package being replaced. Its source is
  // already known, so the box is skipped and the plan is read against what is
  // installed — a new version's new hook is the thing worth seeing.
  updating?: PluginPackage;
}

export function AddPlugin({ port, onClose, onInstalled, updating }: Props) {
  const [text, setText] = useState(updating?.source ?? "");
  const [plan, setPlan] = useState<PluginPlan | null>(null);
  const [done, setDone] = useState<PluginPlan | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const request = (planId?: string) => ({
    source: text.trim(),
    name: updating?.name,
    replace: !!updating,
    planId,
  });

  const look = async () => {
    setBusy(true);
    setError("");
    try {
      const p = await port.planPlugin(request());
      // "Nothing installable here" is the plan's own answer, and its reason is
      // more useful than anything this component could invent.
      if (!p.actions?.length) {
        setError(p.error || p.next || t("这个来源里没有能装的东西"));
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
    setError("");
    try {
      const out = await port.installPlugin(request(plan.planId));
      setDone(out);
      if (out.applied) onInstalled();
    } catch (e) {
      setError(reason(e));
    } finally {
      setBusy(false);
    }
  };

  const pick = async () => {
    const dir = await port.pickFolder();
    if (dir) setText(dir);
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
  const drop = useFileDrop((d) => d.paths[0] && setText(d.paths[0]), setOver);

  if (done) {
    return (
      <div className="addpkg" data-stage="done">
        <Outcome plan={done} />
        <div className="acts">
          <button className="act" onClick={onClose}>
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
      <div className="addpkg" data-stage="confirm">
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
            <span className="why">{gained.length ? t("这一版新增了：{list}", { list: gained.join("、") }) : t("没有新增会执行的东西。")}</span>
          </div>
        )}
        {groups.map((g) => (
          <section className="rgrp" key={g.level} data-lvl={g.level}>
            <h3>{RISK_TITLE[g.level] ?? g.level}</h3>
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
          <span className="note">{t(updating ? "覆盖已装的那一份" : "装到「我的」，所有项目都能用")}</span>
          <button className="act" onClick={() => (updating ? onClose() : setPlan(null))}>
            {t(updating ? "取消" : "返回")}
          </button>
          <button className="act" data-primary disabled={busy} onClick={() => void install()}>
            {t(busy ? (updating ? "更新中…" : "安装中…") : updating ? "更新" : "装上")}
          </button>
        </div>
        {error && <div className="why">{error}</div>}
      </div>
    );
  }

  // An update never shows the box: its source is not something to re-enter, and
  // a failure there is about the recorded source, not about what was typed.
  if (updating) {
    return (
      <div className="addpkg" data-stage="reading">
        <div className="find" data-lvl={error ? "err" : undefined}>
          <span className="t">{error ? t("读不到 {name} 的来源", { name: updating.name }) : t("正在读取 {name} 的来源…", { name: updating.name })}</span>
          <span className="why">{error || updating.source}</span>
        </div>
        <div className="acts">
          <button className="act" onClick={onClose}>
            {t(error ? "关掉" : "取消")}
          </button>
        </div>
      </div>
    );
  }

  return (
    <div className="addpkg" data-stage="paste" ref={drop} data-over={over ? "" : undefined}>
      <textarea
        className="paste"
        rows={3}
        autoFocus
        value={text}
        placeholder={PLACEHOLDER}
        onChange={(e) => setText(e.target.value)}
        onKeyDown={(e) => {
          if ((e.metaKey || e.ctrlKey) && e.key === "Enter") void look();
        }}
      />
      <div className="acts">
        <span className="note">{t("一个仓库地址，或者把文件夹拖进来")}</span>
        <button className="act" onClick={() => void pick()}>
          {t("选文件夹")}
        </button>
        <button className="act" onClick={onClose}>
          {t("取消")}
        </button>
        <button className="act" data-primary disabled={!text.trim() || busy} onClick={() => void look()}>
          {t(busy ? "读取中…" : "看看是什么")}
        </button>
      </div>
      {error && <div className="why">{error}</div>}
    </div>
  );
}

// The plan's own riskReasons are written for the model that reads the tool's
// JSON. What the user needs is the same facts as rows they can scan, so the
// structured counts are rendered here and the kernel's wording stays behind a
// fold — where a reason this UI does not know about yet is still readable.
function Candidate({ a }: { a: PluginAction }) {
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
          <span className="lb">{r.label}</span>
          <span className="dt">{r.detail}</span>
          {r.why && <span className="why">{r.why}</span>}
        </div>
      ))}
      {adds.length > 0 && (
        <div className="risk">
          <span className="lb">{t("会加进来")}</span>
          <span className="dt">{adds.join(" · ")}</span>
        </div>
      )}
      {Object.keys(a.env ?? {}).map((k) => (
        <div className="risk" data-kind="secret" key={k}>
          <span className="lb">{t("要你填")}</span>
          <span className="dt">{k}</span>
        </div>
      ))}
      {a.skippedCapabilities?.map((s) => (
        <div className="risk" key={s.capability + s.reason}>
          <span className="lb">{t("用不了")}</span>
          <span className="dt">{s.capability}</span>
          <span className="why">{s.reason}</span>
        </div>
      ))}
      {a.riskReasons?.length ? (
        <details className="reasons">
          <summary>{t("核心给出的判定（{n}）", { n: a.riskReasons.length })}</summary>
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
      why: "它跑在 Reasonix 内部，能读整个会话、绕过权限、直接操作这台机器",
    });
  }
  if (a.hookCount) {
    out.push({ label: "自动化钩子", detail: `${a.hookCount} 条`, why: "会在会话的生命周期里自动执行" });
  }
  if (a.toolCount) {
    out.push({ label: "外部服务", detail: `${a.toolCount} 个 MCP 服务`, why: "它给 agent 的能力和内置工具一样真实" });
  }
  if (a.kind === "mcp" && a.command) {
    out.push({ label: "会启动", detail: [a.command, ...(a.args ?? [])].join(" ") });
  }
  if (a.kind === "mcp" && a.url) {
    out.push({ label: "会连到", detail: a.url });
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
  if (plugin?.runtime && !current.runtime) out.push("一个常驻进程");
  const hooks = (plugin?.hookCount ?? 0) - (current.hooks?.length ?? 0);
  if (hooks > 0) out.push(`${hooks} 条钩子`);
  const servers = (plugin?.toolCount ?? 0) - (current.mcpServers?.length ?? 0);
  if (servers > 0) out.push(`${servers} 个外部服务`);
  // A separate MCP action installs a server of its own, outside the package.
  const standalone = actions.filter((a) => a.kind === "mcp").length;
  if (standalone > 0) out.push(`${standalone} 个独立的 MCP 服务`);
  return out;
}

function contributes(a: PluginAction): string[] {
  const parts: string[] = [];
  const add = (n: number | undefined, unit: string) => {
    if (n) parts.push(`${n} ${unit}`);
  };
  add(a.skillCount, "个技能");
  add(a.commandCount, "个命令");
  add(a.agentCount, "个子代理");
  add(a.promptCount, "个提示词");
  add(a.themeCount, "套配色");
  return parts;
}

// "Written" and "running" are different outcomes: a reload refused mid-turn
// leaves the package on disk and out of this session, and saying so is the
// difference between waiting and re-installing.
function Outcome({ plan }: { plan: PluginPlan }) {
  const state = plan.reloadError ? "action_required" : plan.ok ? "ready" : "issue";
  return (
    <div className="outcome" data-state={state}>
      <i className="pip" />
      <span className="nm">{plan.actions?.[0]?.name || "安装"}</span>
      <span className="dt">
        {state === "ready" && "装好了，下一轮就能用"}
        {state === "action_required" && "装好了，但这一轮还在跑：等它结束或新建会话后生效"}
        {state === "issue" && (plan.error || plan.next || "没装上")}
      </span>
    </div>
  );
}
