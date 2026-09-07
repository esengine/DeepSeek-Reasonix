import { useEffect, useMemo, useState } from "react";
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
  ["deny", "拒绝", "命中就没得商量：全放行也拦得住，批准框不会出现"],
  ["ask", "询问", "命中就停下来等你，哪怕自动批准开着"],
  ["allow", "放行", "命中就不再问你"],
];

const MODES: [string, string, string][] = [
  ["ask", "问我", "没被任何规矩说中的写操作，动手前问一次"],
  ["allow", "放行", "没被说中的写操作直接做"],
  ["deny", "拒绝", "没被说中的写操作一律不做"],
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
  ["web_fetch", "抓网页"],
  ["grep", "全文搜索"],
  ["glob", "找文件"],
];

const TOOL_LABEL = new Map(TOOLS);

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
    title: "不许动 .env",
    desc: "文件工具读或写 .env 一律拒绝。bash 里的 cat 走另一条路，这条管不到它",
    list: "deny",
    rules: ["file_mutation(*.env*)", "read_file(*.env*)"],
  },
  { id: "push", title: "不许推到远端", desc: "本地怎么改都行，推出去永远留给你", list: "deny", rules: ["bash(git push:*)"] },
  {
    id: "history",
    title: "不许改写 git 历史",
    desc: "rebase 和 reset 能吃掉还没推的提交",
    list: "deny",
    rules: ["bash(git rebase:*)", "bash(git reset:*)"],
  },
  { id: "tests", title: "跑测试不用问", desc: "测试命令直接放行，其余照旧", list: "allow", rules: ["bash(go test:*)", "bash(npm test:*)", "bash(pytest:*)"] },
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
  grep: "比的是搜索式，不是路径",
  glob: "比的是搜索式，不是路径",
  web_fetch: "比的是网址",
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
    if (p.includes("*")) return t("整条命令匹配，不是按词前缀");
    return t("正好这一条命令，多一个参数就不算");
  }
  if (p.includes("*")) return "";
  return MATCHING[tool] ? t("正好这一个值") : t("正好这一个路径");
}

// The add form is the one place the full sentence belongs: nothing is on screen
// yet to compare against, and the rule has not been saved.
function explain(tool: string, pattern: string): string {
  const p = pattern.trim();
  if (!p) return t("这个工具的每一次调用");
  if (tool === "bash") {
    if (p.endsWith(":*")) return t("以 {cmd} 开头的命令，带什么参数都算", { cmd: p.slice(0, -2) });
    if (p.includes("*")) return t("命令整体匹配 {pat}", { pat: p });
    return t("正好是 {cmd} 这条命令", { cmd: p });
  }
  if (p.includes("*")) return t("路径匹配 {pat} 的调用（* 能跨过 /）", { pat: p });
  return t("正好是 {path} 这个路径", { path: p });
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
  const [shut, setShut] = useState<Set<string>>(new Set());

  useEffect(() => {
    port.permissions().then(setRules).catch(() => setRules(null));
  }, [port]);

  const lists: PermissionLists | null = rules && { mode: rules.mode, deny: rules.deny, ask: rules.ask, allow: rules.allow };
  const groups = useMemo(() => (lists ? group(lists, query) : []), [lists, query]);

  if (!rules || !lists) return <div className="empty">{t("读不到权限配置。")}</div>;

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
          <span className="t">{t("这个项目自带一份权限配置")}</span>
          <span className="why">
            {t("{path} 里也写了 permissions，实际生效的是它。这里的改动会存下来，但要等它不再声明才用得上。", { path: rules.shadowedBy })}
          </span>
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
        <span className="k">{t("剩下的写操作")}</span>
        <div className="seg fit" data-text role="radiogroup" aria-label={t("剩下的写操作")}>
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

      {/* The table's own header: how big the boundary is, how to find one row in
          it, and the single way in. Three separate add boxes made one act look
          like three. */}
      <div className="rhead">
        <span className="n">
          {total ? t("{n} 条规矩", { n: total }) : t("还没有额外的规矩")}
          {total > 0 && (
            <span className="mix">
              <i data-k="deny" title={t("拒绝")}>{counts.deny}</i>
              <i data-k="ask" title={t("询问")}>{counts.ask}</i>
              <i data-k="allow" title={t("放行")}>{counts.allow}</i>
            </span>
          )}
        </span>
        {total > 6 && (
          <input className="find-rule" value={query} placeholder={t("筛一下…")} onChange={(e) => setQuery(e.target.value)} />
        )}
        <button className="act" onClick={() => setAdding((v) => !v)}>{t(adding ? "取消" : "加一条")}</button>
      </div>

      {adding && <AddRule busy={!!busy} onAdd={(rule, level) => { add(rule, level); setAdding(false); }} />}

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
                <span className="how">{t(MATCHING[g.tool] ?? pathMatching)}</span>
                <span className="cnt">{t("{n} 条", { n: g.rows.length })}</span>
              </button>
              {!closed &&
                g.rows.map((row) => (
                  <div className="rrow" key={row.rule} data-k={row.level}>
                    <i className="dot" aria-hidden="true" />
                    <span className="pat">
                      <code>{row.pattern || t("整个工具")}</code>
                      {odd(g.tool, row.pattern) && <span className="says">{odd(g.tool, row.pattern)}</span>}
                    </span>
                    <select value={row.level} data-action="permissions.rule-level" disabled={!!busy} aria-label={t("{rule} 的处理方式", { rule: row.rule })}
                      onChange={(e) => move(row.rule, e.target.value as List)}>
                      {LEVELS.map(([id, label]) => (
                        <option key={id} value={id}>{t(label)}</option>
                      ))}
                    </select>
                    <button className="act ghost" data-action="permissions.remove-rule" data-target={row.rule}
                      disabled={!!busy} aria-label={t("删掉 {rule}", { rule: row.rule })}
                      onClick={() => drop(row.rule)}>
                      {t("删掉")}
                    </button>
                  </div>
                ))}
            </section>
          );
        })}
        {total > 0 && groups.length === 0 && <div className="empty">{t("没有匹配的规矩。")}</div>}
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
          {t("加上")}
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
