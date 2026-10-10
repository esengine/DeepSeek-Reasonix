import { useEffect, useMemo, useState } from "react";
import { useEscape } from "./dismiss";
import { t } from "../i18n";
import type { AgentPort, PermissionLists, PermissionRules } from "../port/port";
import { Switch } from "./Switch";
import { reason } from "../i18n/kernel";

type List = "deny" | "ask" | "allow";

// The three verdicts, in the order the gate reads them. A rule's verdict is a
// property of the rule, not the box it lives in — which is why the editor below
// groups by tool and lets the verdict be changed in place. Filing rules into
// three separate lists made "this one is too strict" a delete-and-retype.
const LEVELS: [List, string, string][] = [
  ["deny", "拒绝", "命中即终止：全部放行下同样拦截，不显示审批框"],
  ["ask", "询问", "命中即暂停并请求确认，即使已开启自动批准"],
  ["allow", "放行", "命中后不再请求确认"],
];

const MODES: [string, string, string][] = [
  ["ask", "请求确认", "未被任何规则命中的写入操作，执行前请求一次确认"],
  ["allow", "放行", "未被命中的写入操作直接执行"],
  ["deny", "拒绝", "未被命中的写入操作一律不执行"],
];

// The tools worth offering by name. `file_mutation` is not a tool but the gate's
// own alias for every tool that writes a file, and it is the one most people
// actually want — so it leads, under the name of what it does.
const TOOLS: [string, string][] = [
  ["bash", "命令"],
  ["file_mutation", "写文件（全部写入工具）"],
  ["read_file", "读文件"],
  ["write_file", "整份写入"],
  ["edit_file", "改动文件"],
  ["web_fetch", "抓取网页"],
  ["grep", "全文搜索"],
  ["glob", "查找文件"],
];

// A grant a person gave an approval card lands here too, under the group the
// gate files it in. They are not offered above, because a rule typed for one of
// these is only honoured when it names its subject exactly.
const GRANTED: [string, string][] = [
  ["Computer", "操作应用（逐个应用授权）"],
  ["Browser", "浏览器（逐个来源授权）"],
];

const TOOL_LABEL = new Map([...TOOLS, ...GRANTED]);

interface Recipe {
  id: string;
  title: string;
  desc: string;
  list: List;
  rules: string[];
}

const RECIPES: Recipe[] = [
  {
    id: "secrets",
    title: "禁止访问 .env",
    desc: "文件工具读写 .env 一律拒绝。bash 中的 cat 属于另一条路径，本规则不覆盖",
    list: "deny",
    rules: ["file_mutation(*.env*)", "read_file(*.env*)"],
  },
  { id: "push", title: "禁止推送到远端", desc: "本地修改不受限制，推送始终由你执行", list: "deny", rules: ["bash(git push:*)"] },
  {
    id: "history",
    title: "禁止改写 git 历史",
    desc: "rebase 与 reset 可能丢弃尚未推送的提交",
    list: "deny",
    rules: ["bash(git rebase:*)", "bash(git reset:*)"],
  },
  { id: "tests", title: "运行测试无需确认", desc: "测试命令直接放行，其余保持不变", list: "allow", rules: ["bash(go test:*)", "bash(npm test:*)", "bash(pytest:*)"] },
];

// A rule is "tool" or "tool(subject)". Splitting it is what lets the tool name
// be said once per group instead of once per row — with twenty bash rules, the
// prefix was three quarters of every line and none of the information.
function splitRule(rule: string): { tool: string; pattern: string } {
  const at = rule.indexOf("(");
  if (at < 0 || !rule.endsWith(")")) return { tool: rule.trim(), pattern: "" };
  return { tool: rule.slice(0, at).trim(), pattern: rule.slice(at + 1, -1) };
}

const joinRule = (tool: string, pattern: string) => (pattern.trim() ? `${tool.trim()}(${pattern.trim()})` : tool.trim());

// How this tool's rules are matched, said once on the group header. What a rule
// is compared against is whichever argument the call carries — bash exposes its
// command, the file tools their path, grep and glob their search pattern — so
// the same glob means different things per tool and the header has to say which.
const MATCHING: Record<string, string> = {
  bash: "按命令的词比对",
  grep: "比对的是搜索式，而非路径",
  glob: "比对的是搜索式，而非路径",
  web_fetch: "比对的是网址",
  Computer: "比对应用 id，放行必须写全；通配符只拦不放",
  Browser: "比对页面来源（协议://主机:端口）",
};
const pathMatching = "按路径匹配，* 能跨过 /";

// What is unusual about this one rule — and nothing when there is nothing. The
// common shape is `x:*`, and printing "commands starting with x" on thirteen of
// fifteen rows is noise, not information. Saying it only where the shape
// differs makes the row that has something to say the one that stands out.
function odd(tool: string, pattern: string): string {
  const p = pattern.trim();
  if (!p) return t("这个工具的每一次调用");
  if (tool === "bash") {
    if (p.endsWith(":*")) return "";
    if (p.includes("*")) return t("匹配完整命令，而非按词前缀");
    return t("仅匹配该命令本身，附加任何参数即不匹配");
  }
  if (p.includes("*")) return "";
  return MATCHING[tool] ? t("仅匹配该值") : t("仅匹配该路径");
}

// The add form is the one place the full sentence belongs: nothing is on screen
// yet to compare against, and the rule has not been saved.
function explain(tool: string, pattern: string): string {
  const p = pattern.trim();
  if (!p) return t("这个工具的每一次调用");
  if (tool === "bash") {
    if (p.endsWith(":*")) return t("以 {cmd} 开头的命令，不限参数", { cmd: p.slice(0, -2) });
    if (p.includes("*")) return t("命令整体匹配 {pat}", { pat: p });
    return t("仅为 {cmd} 这条命令", { cmd: p });
  }
  if (p.includes("*")) return t("路径匹配 {pat} 的调用（* 可跨越 /）", { pat: p });
  return t("仅为 {path} 这个路径", { path: p });
}

interface Group {
  tool: string;
  rows: { rule: string; pattern: string; level: List }[];
}

const ORDER: Record<List, number> = { deny: 0, ask: 1, allow: 2 };

function group(lists: PermissionLists, query: string): Group[] {
  const q = query.trim().toLocaleLowerCase();
  const byTool = new Map<string, Group>();
  for (const level of ["deny", "ask", "allow"] as List[]) {
    for (const rule of lists[level]) {
      const { tool, pattern } = splitRule(rule);
      if (q && !rule.toLocaleLowerCase().includes(q)) continue;
      let g = byTool.get(tool);
      if (!g) {
        g = { tool, rows: [] };
        byTool.set(tool, g);
      }
      g.rows.push({ rule, pattern, level });
    }
  }
  for (const g of byTool.values()) g.rows.sort((a, b) => ORDER[a.level] - ORDER[b.level] || a.pattern.localeCompare(b.pattern));
  // Most-governed tool first: that is the one being read.
  return [...byTool.values()].sort((a, b) => b.rows.length - a.rows.length || a.tool.localeCompare(b.tool));
}

export function Rules({ port, onChanged }: { port: AgentPort; onChanged: () => void }) {
  const [rules, setRules] = useState<PermissionRules | null>(null);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [query, setQuery] = useState("");
  const [adding, setAdding] = useState(false);
  useEscape(adding, () => setAdding(false));
  const [shut, setShut] = useState<Set<string>>(new Set());

  useEffect(() => {
    port.permissions().then(setRules).catch(() => setRules(null));
  }, [port]);

  const lists: PermissionLists | null = rules && { mode: rules.mode, deny: rules.deny, ask: rules.ask, allow: rules.allow };
  const groups = useMemo(() => (lists ? group(lists, query) : []), [lists, query]);

  if (!rules || !lists) return <div className="empty">{t("无法读取权限配置。")}</div>;

  const dormant = new Set((rules.dormant ?? []).map((d) => `${d.list}\u0000${d.rule}`));
  const counts = { deny: rules.deny.length, ask: rules.ask.length, allow: rules.allow.length };
  const total = counts.deny + counts.ask + counts.allow;

  const apply = async (what: string, next: PermissionLists) => {
    setBusy(what);
    setError("");
    try {
      setRules(await port.savePermissions(next));
      onChanged();
    } catch (e) {
      setError(reason(e));
    } finally {
      setBusy("");
    }
  };

  const revoke = async (rule: string) => {
    setBusy(rule || "all");
    setError("");
    try {
      setRules(await port.revokeSessionGrant(rule));
    } catch (e) {
      setError(reason(e));
    } finally {
      setBusy("");
    }
  };

  const revokeRemembered = async (rule: string) => {
    setBusy(`remembered:${rule}`);
    setError("");
    try {
      setRules(await port.revokeRememberedProjectRule(rule));
      onChanged();
    } catch (e) {
      setError(reason(e));
    } finally {
      setBusy("");
    }
  };

  const without = (l: PermissionLists, rule: string): PermissionLists => ({
    ...l,
    deny: l.deny.filter((r) => r !== rule),
    ask: l.ask.filter((r) => r !== rule),
    allow: l.allow.filter((r) => r !== rule),
  });
  // Changing a verdict is a move between lists, not a delete and a retype.
  const move = (rule: string, to: List) => {
    const next = without(lists, rule);
    void apply(rule, { ...next, [to]: [...next[to], rule] });
  };
  const drop = (rule: string) => void apply(rule, without(lists, rule));
  const add = (rule: string, level: List) => {
    if (lists[level].includes(rule)) return;
    const next = without(lists, rule);
    void apply(rule, { ...next, [level]: [...next[level], rule] });
  };

  const has = (r: Recipe) => r.rules.every((rule) => lists[r.list].includes(rule));
  const toggleRecipe = (r: Recipe) => {
    const on = has(r);
    let next = lists;
    for (const rule of r.rules) next = without(next, rule);
    if (!on) next = { ...next, [r.list]: [...next[r.list], ...r.rules] };
    void apply(r.id, next);
  };

  return (
    <div className="rules">
      {rules.shadowedBy && (
        <div className="find" data-lvl="warn" role="status">
          <span className="t">{t("当前项目自带权限配置")}</span>
          <span className="why">
            {t("{path} 中同样声明了 permissions，实际生效的是该文件。此处的修改会被保存，但需待其不再声明后才会生效。", { path: rules.shadowedBy })}
          </span>
        </div>
      )}

      {!!rules.dormant?.length && (
        <div className="find" data-lvl="warn" role="status">
          <span className="t">{t("有 {n} 条规则没有对应的工具，不会生效", { n: rules.dormant.length })}</span>
          <span className="why">{t("命令要写成 Bash(命令:*)；下方标出的规则可以删除或改写。")}</span>
        </div>
      )}

      <div className="recipes">
        {RECIPES.map((r) => (
          <div className="recipe" key={r.id} data-on={has(r) ? "" : undefined}>
            <div className="tx">
              <span className="lb">{t(r.title)}</span>
              <span className="ds">{t(r.desc)}</span>
            </div>
            <Switch data-action="permissions.recipe" data-target={r.id} on={has(r)} busy={busy === r.id} label={t(r.title)} onClick={() => toggleRecipe(r)} />
          </div>
        ))}
      </div>

      <div className="fallback">
        <span className="k">{t("其余写入操作")}</span>
        <div className="seg fit" data-text role="radiogroup" aria-label={t("其余写入操作")}>
          {MODES.map(([id, label]) => (
            <button key={id} role="radio" data-action="permissions.mode" data-value={id}
              aria-checked={rules.mode === id} disabled={!!busy}
              onClick={() => void apply("mode", { ...lists, mode: id })}>
              {t(label)}
            </button>
          ))}
        </div>
      </div>
      <p className="note">{t(MODES.find(([id]) => id === rules.mode)?.[2] ?? "")}</p>

      {rules.rememberedErrorCode && (
        <div className="find" data-lvl="warn" role="status">
          <span className="t">{t("已记住的权限规则无法读取")}</span>
          <span className="why">{t("请检查或修复用户目录中的授权记录文件。")}</span>
          <span className="path">{rules.rememberedPath}</span>
        </div>
      )}

      {!!rules.remembered?.length && (
        <section className="rtable" aria-label={t("当前项目记住的放行规则")}>
          <div className="rhead">{t("当前项目记住的放行规则")}</div>
          {rules.remembered.map((rule) => (
            <div className="rrow" data-k="allow" key={rule}>
              <i className="dot" aria-hidden="true" />
              <span className="pat"><code>{rule}</code></span>
              <button className="act ghost" data-action="permissions.revoke-remembered" data-target={rule}
                disabled={!!busy} aria-label={t("收回 {rule}", { rule })}
                onClick={() => void revokeRemembered(rule)}>{t("收回")}</button>
            </div>
          ))}
          <p className="path">{rules.rememberedPath}</p>
        </section>
      )}

      {/* The table's own header: how big the boundary is, how to find one row in
          it, and the single way in. Three separate add boxes made one act look
          like three. */}
      <div className="rhead">
        <span className="n">
          {total ? t("{n} 条规则", { n: total }) : t("尚无额外规则")}
          {total > 0 && (
            <span className="mix">
              <i data-k="deny" title={t("拒绝")}>{counts.deny}</i>
              <i data-k="ask" title={t("询问")}>{counts.ask}</i>
              <i data-k="allow" title={t("放行")}>{counts.allow}</i>
            </span>
          )}
        </span>
        {total > 6 && (
          <input className="find-rule" value={query} placeholder={t("筛选…")} onChange={(e) => setQuery(e.target.value)} />
        )}
        <button className="act" onClick={() => setAdding((v) => !v)}>{t(adding ? "取消" : "添加规则")}</button>
      </div>

      {adding && <AddRule busy={!!busy} onAdd={(rule, level) => { add(rule, level); setAdding(false); }} />}

      <Granted rules={rules.granted ?? []} busy={busy} onRevoke={revoke} />

      <div className="rtable">
        {groups.map((g) => {
          const closed = shut.has(g.tool);
          return (
            <section className="rgroup" key={g.tool}>
              <button className="rg-hd" aria-expanded={!closed}
                onClick={() => setShut((s) => { const n = new Set(s); if (closed) n.delete(g.tool); else n.add(g.tool); return n; })}>
                <i className="caret" aria-hidden="true" />
                <code>{g.tool}</code>
                <span className="what">{t(TOOL_LABEL.get(g.tool) ?? "")}</span>
                {!g.rows.every((row) => dormant.has(`${row.level}\u0000${row.rule}`)) && <span className="how">{t(MATCHING[g.tool] ?? pathMatching)}</span>}
                <span className="cnt">{t("{n} 条", { n: g.rows.length })}</span>
              </button>
              {!closed &&
                g.rows.map((row) => (
                  <div className="rrow" key={row.rule} data-k={row.level}>
                    <i className="dot" aria-hidden="true" />
                    <span className="pat">
                      <code>{row.pattern || t("整个工具")}</code>
                      {dormant.has(`${row.level}\u0000${row.rule}`)
                        ? <span className="says" data-dormant="">{t("没有名为 {tool} 的工具，这条规则不会生效", { tool: g.tool })}</span>
                        : odd(g.tool, row.pattern) && <span className="says">{odd(g.tool, row.pattern)}</span>}
                    </span>
                    <select value={row.level} data-action="permissions.rule-level" disabled={!!busy} aria-label={t("{rule} 的处理方式", { rule: row.rule })}
                      onChange={(e) => move(row.rule, e.target.value as List)}>
                      {LEVELS.map(([id, label]) => (
                        <option key={id} value={id}>{t(label)}</option>
                      ))}
                    </select>
                    <button className="act ghost" data-action="permissions.remove-rule" data-target={row.rule}
                      disabled={!!busy} aria-label={t("删除 {rule}", { rule: row.rule })}
                      onClick={() => drop(row.rule)}>
                      {t("删除")}
                    </button>
                  </div>
                ))}
            </section>
          );
        })}
        {total > 0 && groups.length === 0 && <div className="empty">{t("没有匹配的规则。")}</div>}
      </div>

      {total > 0 && <p className="path">{rules.path}</p>}
      {error && <div className="why">{error}</div>}
    </div>
  );
}

// Adding is where the syntax has to be learnable: the tool is a list rather than
// something to remember, and the line underneath says what the rule will catch
// before it is saved.
function AddRule({ busy, onAdd }: { busy: boolean; onAdd: (rule: string, level: List) => void }) {
  const [tool, setTool] = useState("bash");
  const [pattern, setPattern] = useState("");
  const [level, setLevel] = useState<List>("deny");
  const rule = joinRule(tool, pattern);

  return (
    <div className="raddbox">
      <div className="line">
        <select value={tool} disabled={busy} aria-label={t("工具")} onChange={(e) => setTool(e.target.value)}>
          {TOOLS.map(([id, label]) => (
            <option key={id} value={id}>{`${id} · ${t(label)}`}</option>
          ))}
        </select>
        <input
          value={pattern}
              data-action-keydown="permissions.add-rule"
          placeholder={t(tool === "bash" ? "例如 git push:*　（留空 = 整个工具）" : "例如 *.env*　（留空 = 整个工具）")}
          disabled={busy}
          onChange={(e) => setPattern(e.target.value)}
          onKeyDown={(e) => e.key === "Enter" && rule && onAdd(rule, level)}
        />
        <select value={level} disabled={busy} aria-label={t("处理方式")} onChange={(e) => setLevel(e.target.value as List)}>
          {LEVELS.map(([id, label]) => (
            <option key={id} value={id}>{t(label)}</option>
          ))}
        </select>
        <button className="act" data-primary data-action="permissions.add-rule" disabled={busy || !rule} onClick={() => onAdd(rule, level)}>
          {t("添加")}
        </button>
      </div>
      <p className="preview">
        <code>{rule || "…"}</code>
        <span>{t(LEVELS.find(([id]) => id === level)?.[1] ?? "")}</span>
        <span className="says">{explain(tool, pattern)}</span>
      </p>
    </div>
  );
}

// What a prompt allowed for this session. It is in no file, so a person reading
// the table above is reading less than the agent may currently do — and until
// there was somewhere to take one back, "allow for this session" could only be
// undone by ending the session, which costs the work as well as the grant.
function Granted({ rules, busy, onRevoke }: { rules: string[]; busy: string; onRevoke: (rule: string) => void }) {
  if (!rules.length) return null;
  return (
    <section className="granted">
      <div className="g-hd">
        <span className="n">{t("本次会话另外允许了 {n} 项", { n: rules.length })}</span>
        <span className="how">{t("只在这个会话里有效，没有写进文件")}</span>
        <button className="act" data-action="permissions.revoke-session" data-value="all" disabled={!!busy}
          onClick={() => onRevoke("")}>{t("全部收回")}</button>
      </div>
      {rules.map((rule) => (
        <div className="g-row" key={rule}>
          <code>{rule}</code>
          <button className="act" data-action="permissions.revoke-session" data-target={rule} disabled={!!busy}
            aria-label={t("收回 {rule}", { rule })} onClick={() => onRevoke(rule)}>{t("收回")}</button>
        </div>
      ))}
    </section>
  );
}
