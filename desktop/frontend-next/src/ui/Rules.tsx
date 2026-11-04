import { useEffect, useMemo, useState } from "react";
import { useEscape } from "./dismiss";
import { t } from "../i18n";
import type { AgentPort, PermissionLists, PermissionRules } from "../port/port";
import { Switch } from "./Switch";
import { reason, savedNotApplied } from "../i18n/kernel";
import { SaveNote } from "./SaveNote";

type List = "deny" | "ask" | "allow";

// The three verdicts, in the order the gate reads them. A rule's verdict is a
// property of the rule, not the box it lives in — which is why the editor below
// groups by tool and lets the verdict be changed in place. Filing rules into
// three separate lists made "this one is too strict" a delete-and-retype.
const LEVELS: [List, string, string][] = [
  ["deny", "common.refuse", "safety.a_match_is_final_it"],
  ["ask", "safety.ask", "safety.a_match_stops_for_you"],
  ["allow", "common.allow", "safety.a_match_stops_asking"],
];

const MODES: [string, string, string][] = [
  ["ask", "safety.ask_for_confirmation", "safety.a_write_no_rule_matched"],
  ["allow", "common.allow", "safety.a_write_matched_by_no"],
  ["deny", "common.refuse", "safety.a_write_no_list_matched"],
];

// The tools worth offering by name. `file_mutation` is not a tool but the gate's
// own alias for every tool that writes a file, and it is the one most people
// actually want — so it leads, under the name of what it does.
const TOOLS: [string, string][] = [
  ["bash", "chat.commands"],
  ["file_mutation", "safety.writing_files_every_write_tool"],
  ["read_file", "safety.reading_files"],
  ["write_file", "safety.whole_file_writes"],
  ["edit_file", "safety.file_edits"],
  ["web_fetch", "safety.fetching_web_pages"],
  ["grep", "safety.full_text_search"],
  ["glob", "safety.finding_files"],
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
    title: "safety.keep_env_off_limits",
    desc: "safety.file_tools_are_refused_outright",
    list: "deny",
    rules: ["file_mutation(*.env*)", "read_file(*.env*)"],
  },
  { id: "push", title: "safety.never_push", desc: "safety.change_anything_locally_pushing_stays", list: "deny", rules: ["bash(git push:*)"] },
  {
    id: "history",
    title: "safety.never_rewrite_git_history",
    desc: "safety.rebase_and_reset_can_eat",
    list: "deny",
    rules: ["bash(git rebase:*)", "bash(git reset:*)"],
  },
  { id: "tests", title: "safety.tests_without_asking", desc: "safety.test_commands_go_straight_through", list: "allow", rules: ["bash(go test:*)", "bash(npm test:*)", "bash(pytest:*)"] },
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
  bash: "safety.matched_by_command_words",
  grep: "safety.matched_against_the_search_pattern",
  glob: "safety.matched_against_the_search_pattern",
  web_fetch: "safety.matched_against_the_url",
  Computer: "比对应用 id，放行必须写全；通配符只拦不放",
  Browser: "比对页面来源（协议://主机:端口）",
};
const pathMatching = "safety.matched_by_path_crosses";

// What is unusual about this one rule — and nothing when there is nothing. The
// common shape is `x:*`, and printing "commands starting with x" on thirteen of
// fifteen rows is noise, not information. Saying it only where the shape
// differs makes the row that has something to say the one that stands out.
function odd(tool: string, pattern: string): string {
  const p = pattern.trim();
  if (!p) return t("safety.every_call_to_this_tool");
  if (tool === "bash") {
    if (p.endsWith(":*")) return "";
    if (p.includes("*")) return t("safety.matches_the_whole_command_not");
    return t("safety.exactly_this_command_one_more");
  }
  if (p.includes("*")) return "";
  return MATCHING[tool] ? t("safety.exactly_this_one_value") : t("safety.exactly_this_one_path");
}

// The add form is the one place the full sentence belongs: nothing is on screen
// yet to compare against, and the rule has not been saved.
function explain(tool: string, pattern: string): string {
  const p = pattern.trim();
  if (!p) return t("safety.every_call_to_this_tool");
  if (tool === "bash") {
    if (p.endsWith(":*")) return t("safety.commands_starting_with_cmd_whatever", { cmd: p.slice(0, -2) });
    if (p.includes("*")) return t("safety.commands_matching_pat_as_a", { pat: p });
    return t("safety.exactly_the_command_cmd", { cmd: p });
  }
  if (p.includes("*")) return t("safety.calls_whose_path_matches_pat", { pat: p });
  return t("safety.exactly_the_path_path", { path: p });
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
  const [note, setNote] = useState<{ text: string; unapplied: boolean } | null>(null);
  const [query, setQuery] = useState("");
  const [adding, setAdding] = useState(false);
  useEscape(adding, () => setAdding(false));
  const [shut, setShut] = useState<Set<string>>(new Set());

  useEffect(() => {
    port.permissions().then(setRules).catch(() => setRules(null));
  }, [port]);

  const lists: PermissionLists | null = rules && { mode: rules.mode, deny: rules.deny, ask: rules.ask, allow: rules.allow };
  const groups = useMemo(() => (lists ? group(lists, query) : []), [lists, query]);

  if (!rules || !lists) return <div className="empty">{t("safety.cannot_read_the_permission_config")}</div>;

  const dormant = new Set((rules.dormant ?? []).map((d) => `${d.list}\u0000${d.rule}`));
  const counts = { deny: rules.deny.length, ask: rules.ask.length, allow: rules.allow.length };
  const total = counts.deny + counts.ask + counts.allow;

  const apply = async (what: string, next: PermissionLists) => {
    setBusy(what);
    setNote(null);
    try {
      setRules(await port.savePermissions(next));
      onChanged();
    } catch (e) {
      setNote({ text: reason(e), unapplied: savedNotApplied(e) });
    } finally {
      setBusy("");
    }
  };

  const revoke = async (rule: string) => {
    setBusy(rule || "all");
    setNote(null);
    try {
      setRules(await port.revokeSessionGrant(rule));
    } catch (e) {
      setNote({ text: reason(e), unapplied: savedNotApplied(e) });
    } finally {
      setBusy("");
    }
  };

  const revokeRemembered = async (rule: string) => {
    setBusy(`remembered:${rule}`);
    setNote(null);
    try {
      setRules(await port.revokeRememberedProjectRule(rule));
      onChanged();
    } catch (e) {
      setNote({ text: reason(e), unapplied: savedNotApplied(e) });
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
          <span className="t">{t("safety.this_project_ships_its_own")}</span>
          <span className="why">
            {t("safety.path_declares_permissions_too_and", { path: rules.shadowedBy })}
          </span>
        </div>
      )}

      {!!rules.dormant?.length && (
        <div className="find" data-lvl="warn" role="status">
          <span className="t">{t("safety.n_rule_s_name_no", { n: rules.dormant.length })}</span>
          <span className="why">{t("safety.a_shell_command_is_written")}</span>
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
        <span className="k">{t("safety.everything_else_that_writes")}</span>
        <div className="seg fit" data-text role="radiogroup" aria-label={t("safety.everything_else_that_writes")}>
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
          <span className="t">{t("safety.could_not_read_remembered_permission")}</span>
          <span className="why">{t("safety.check_or_repair_the_grant")}</span>
          <span className="path">{rules.rememberedPath}</span>
        </div>
      )}

      {!!rules.remembered?.length && (
        <section className="rtable" aria-label={t("safety.remembered_allow_rules_for_this")}>
          <div className="rhead">{t("safety.remembered_allow_rules_for_this")}</div>
          {rules.remembered.map((rule) => (
            <div className="rrow" data-k="allow" key={rule}>
              <i className="dot" aria-hidden="true" />
              <span className="pat"><code>{rule}</code></span>
              <button className="act ghost" data-action="permissions.revoke-remembered" data-target={rule}
                disabled={!!busy} aria-label={t("safety.take_back_rule", { rule })}
                onClick={() => void revokeRemembered(rule)}>{t("safety.take_back")}</button>
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
          {total ? t("common.n_rules", { n: total }) : t("safety.no_rules_of_your_own")}
          {total > 0 && (
            <span className="mix">
              <i data-k="deny" title={t("common.refuse")}>{counts.deny}</i>
              <i data-k="ask" title={t("safety.ask")}>{counts.ask}</i>
              <i data-k="allow" title={t("common.allow")}>{counts.allow}</i>
            </span>
          )}
        </span>
        {total > 6 && (
          <input className="find-rule" value={query} placeholder={t("safety.filter")} onChange={(e) => setQuery(e.target.value)} />
        )}
        <button className="act" onClick={() => setAdding((v) => !v)}>{t(adding ? "common.cancel" : "common.add_a_rule")}</button>
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
                <span className="cnt">{t("common.n_2", { n: g.rows.length })}</span>
              </button>
              {!closed &&
                g.rows.map((row) => (
                  <div className="rrow" key={row.rule} data-k={row.level}>
                    <i className="dot" aria-hidden="true" />
                    <span className="pat">
                      <code>{row.pattern || t("safety.the_whole_tool")}</code>
                      {dormant.has(`${row.level}\u0000${row.rule}`)
                        ? <span className="says" data-dormant="">{t("safety.no_tool_is_named_tool", { tool: g.tool })}</span>
                        : odd(g.tool, row.pattern) && <span className="says">{odd(g.tool, row.pattern)}</span>}
                    </span>
                    <select data-size="sm" value={row.level} data-action="permissions.rule-level" disabled={!!busy} aria-label={t("safety.what_happens_on_rule", { rule: row.rule })}
                      onChange={(e) => move(row.rule, e.target.value as List)}>
                      {LEVELS.map(([id, label]) => (
                        <option key={id} value={id}>{t(label)}</option>
                      ))}
                    </select>
                    <button className="act ghost" data-action="permissions.remove-rule" data-target={row.rule}
                      disabled={!!busy} aria-label={t("safety.delete_rule", { rule: row.rule })}
                      onClick={() => drop(row.rule)}>
                      {t("common.delete")}
                    </button>
                  </div>
                ))}
            </section>
          );
        })}
        {total > 0 && groups.length === 0 && <div className="empty">{t("safety.no_rule_matches_that")}</div>}
      </div>

      {total > 0 && <p className="path">{rules.path}</p>}
      <SaveNote note={note} />
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
        <select value={tool} disabled={busy} aria-label={t("common.tool")} onChange={(e) => setTool(e.target.value)}>
          {TOOLS.map(([id, label]) => (
            <option key={id} value={id}>{`${id} · ${t(label)}`}</option>
          ))}
        </select>
        <input
          value={pattern}
              data-action-keydown="permissions.add-rule"
          placeholder={t(tool === "bash" ? "safety.e_g_git_push_empty" : "safety.e_g_env_empty_the")}
          disabled={busy}
          onChange={(e) => setPattern(e.target.value)}
          onKeyDown={(e) => e.key === "Enter" && rule && onAdd(rule, level)}
        />
        <select value={level} disabled={busy} aria-label={t("safety.verdict")} onChange={(e) => setLevel(e.target.value as List)}>
          {LEVELS.map(([id, label]) => (
            <option key={id} value={id}>{t(label)}</option>
          ))}
        </select>
        <button className="act" data-primary data-action="permissions.add-rule" disabled={busy || !rule} onClick={() => onAdd(rule, level)}>
          {t("common.add")}
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
        <span className="n">{t("safety.n_more_allowed_for_this", { n: rules.length })}</span>
        <span className="how">{t("safety.this_session_only_not_written")}</span>
        <button className="act" data-action="permissions.revoke-session" data-value="all" disabled={!!busy}
          onClick={() => onRevoke("")}>{t("safety.take_all_back")}</button>
      </div>
      {rules.map((rule) => (
        <div className="g-row" key={rule}>
          <code>{rule}</code>
          <button className="act" data-action="permissions.revoke-session" data-target={rule} disabled={!!busy}
            aria-label={t("safety.take_back_rule", { rule })} onClick={() => onRevoke(rule)}>{t("safety.take_back")}</button>
        </div>
      ))}
    </section>
  );
}

// The tool switches sit beside the rules that govern the same tools.
export { ToolList } from "./ToolList";
