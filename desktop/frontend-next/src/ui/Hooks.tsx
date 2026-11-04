import { useEffect, useMemo, useState } from "react";
import { useEscape } from "./dismiss";
import { t } from "../i18n";
import type { AgentPort, HookCatalog, HookDryRun, HookEntry } from "../port/port";
import { reason } from "../i18n/kernel";

// Nobody arrives wanting a "PreToolUse hook with an anchored matcher". They
// arrive wanting the agent to run their formatter. The recipe is the product;
// the event, the matcher and the exit-code contract are its implementation.
interface Recipe {
  id: string;
  title: string;
  desc: string;
  event: string;
  match?: string;
  command: string;
}

const RECIPES: Recipe[] = [
  {
    id: "format",
    title: "ext.format_files_after_they_are",
    desc: "ext.runs_a_formatter_after_every",
    event: "PostToolUse",
    match: "edit_file|write_file|multi_edit",
    command: "gofmt -w . 2>/dev/null || true",
  },
  {
    id: "guard-secrets",
    title: "ext.ask_me_before_writing_a",
    desc: "ext.stops_on_paths_like_env",
    event: "PreToolUse",
    match: "edit_file|write_file",
    command: `grep -q '"\\.env' <<< "$REASONIX_HOOK_PAYLOAD" && exit 2 || exit 0`,
  },
  {
    id: "test-before-stop",
    title: "ext.run_the_tests_before_wrapping",
    desc: "ext.runs_the_tests_at_the",
    event: "Stop",
    command: "go test ./... 2>&1 | tail -5",
  },
];

const SCOPE_LABEL = { user: "ext.mine", project: "ext.this_project" } as const;

const EVENT_LABELS = new Map<string, () => string>([
  ["PreToolUse", () => t("ext.before_a_tool_runs")],
  ["PostToolUse", () => t("ext.after_a_tool_runs")],
  ["PostToolUseFailure", () => t("ext.after_a_tool_fails")],
  ["PermissionRequest", () => t("ext.when_permission_is_requested")],
  ["UserPromptSubmit", () => t("ext.when_a_message_is_submitted")],
  ["Stop", () => t("ext.when_the_turn_ends")],
  ["StopFailure", () => t("ext.when_the_turn_fails")],
  ["PostLLMCall", () => t("ext.after_a_model_response_completes")],
  ["SessionStart", () => t("ext.when_a_session_starts")],
  ["SessionEnd", () => t("ext.when_a_session_ends")],
  ["SubagentStart", () => t("ext.when_a_subagent_starts")],
  ["SubagentStop", () => t("ext.when_a_subagent_ends")],
  ["Notification", () => t("ext.when_your_attention_is_needed")],
  ["PreCompact", () => t("ext.before_context_is_compacted")],
]);

function eventLabel(event: string): string {
  const label = EVENT_LABELS.get(event);
  return label ? `${label()} (${event})` : event;
}

interface Props {
  port: AgentPort;
  onChanged: () => void;
}

interface Trial {
  rule: HookEntry;
  result?: HookDryRun;
  error?: string;
}

export function Hooks({ port, onChanged }: Props) {
  const [cat, setCat] = useState<HookCatalog | null>(null);
  const [scope, setScope] = useState<"user" | "project">("user");
  const [expert, setExpert] = useState(false);
  useEscape(expert, () => setExpert(false));
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [tried, setTried] = useState<Record<string, Trial>>({});
  const mine = useMemo(() => cat?.hooks.filter((h) => h.scope === (scope === "user" ? "global" : "project")) ?? [], [cat, scope]);

  const reload = () => {
    port
      .hooks()
      .then((c) => {
        setCat(c);
        if (!c.projectPath && scope === "project") setScope("user");
      })
      .catch(() => setCat(null));
  };
  useEffect(reload, [port]); // eslint-disable-line react-hooks/exhaustive-deps

  if (!cat) return <div className="empty">{t("ext.could_not_read_the_hooks")}</div>;

  const plugin = cat.hooks.filter((h) => h.scope === "plugin");
  const broken = cat.sources.filter((s) => s.status === "malformed" || s.status === "unreadable");

  const save = async (next: HookEntry[]) => {
    setBusy("save");
    setError("");
    try {
      await port.saveHooks(scope, next);
      reload();
      onChanged();
    } catch (e) {
      setError(reason(e));
    } finally {
      setBusy("");
    }
  };

  const has = (r: Recipe) => mine.some((h) => h.event === r.event && h.command === r.command);
  const toggleRecipe = (r: Recipe) => {
    const on = has(r);
    const next = on
      ? mine.filter((h) => !(h.event === r.event && h.command === r.command))
      : [...mine, { event: r.event, match: r.match, command: r.command, description: r.title, scope }];
    void save(next as HookEntry[]);
  };

  const tryOne = async (h: HookEntry, key: string) => {
    const trial: Trial = { rule: h };
    setTried((t) => ({ ...t, [key]: trial }));
    setBusy(key);
    setError("");
    try {
      const result = await port.dryRunHook(h);
      setTried((t) => t[key] === trial ? { ...t, [key]: { ...trial, result } } : t);
    } catch (e) {
      const error = reason(e);
      setTried((t) => t[key] === trial ? { ...t, [key]: { ...trial, error } } : t);
    } finally {
      setBusy("");
    }
  };

  return (
    <div className="hooks">
      <div className="scope" role="radiogroup" aria-label={t("ext.where_these_rules_live")}>
        {(["user", "project"] as const).map((s) => (
          <button
            key={s}
            role="radio"
            aria-checked={scope === s}
            disabled={s === "project" && !cat.projectPath}
            onClick={() => setScope(s)}
          >
            {t(SCOPE_LABEL[s])}
            <i>{t(s === "user" ? "ext.this_machine_only" : "ext.written_into_the_repository_so")}</i>
          </button>
        ))}
      </div>
      <p className="path">{scope === "user" ? cat.globalPath : cat.projectPath || t("ext.no_project_open")}</p>

      {broken.map((s) => (
        <div className="why" key={s.path}>
          {s.path} {t("ext.could_not_be_read")}{s.parseError || s.status}
        </div>
      ))}

      <div className="recipes">
        {RECIPES.map((r) => (
          <div className="recipe" key={r.id} data-on={has(r) ? "" : undefined}>
            <div className="tx">
              <span className="lb">{t(r.title)}</span>
              <span className="ds">{t(r.desc)}</span>
            </div>
            <button
              className="sw"
                data-action="hooks.recipe"
                data-target={r.id}
              role="switch"
              aria-checked={has(r)}
              aria-label={t(r.title)}
              disabled={!!busy}
              onClick={() => toggleRecipe(r)}
            >
              <i />
            </button>
          </div>
        ))}
      </div>

      <button className="more" aria-expanded={expert} onClick={() => setExpert((v) => !v)}>
        {t(expert ? "common.collapse" : "ext.write_one_manually")}
        <span className="c">{t("common.n_rules", { n: mine.length })}</span>
      </button>

      {expert && (
        <Expert
          cat={cat}
          scope={scope}
          rules={mine}
          plugin={plugin}
          busy={busy}
          tried={tried}
          onSave={save}
          onTry={tryOne}
        />
      )}
      {error && <div className="why">{error}</div>}
    </div>
  );
}

function Expert({
  cat, scope, rules, plugin, busy, tried, onSave, onTry,
}: {
  cat: HookCatalog;
  scope: "user" | "project";
  rules: HookEntry[];
  plugin: HookEntry[];
  busy: string;
  tried: Record<string, Trial>;
  onSave: (next: HookEntry[]) => void;
  onTry: (h: HookEntry, key: string) => void;
}) {
  const [draft, setDraft] = useState<HookEntry[]>(rules);
  useEffect(() => setDraft(rules), [rules]);

  const patch = (i: number, p: Partial<HookEntry>) =>
    setDraft((d) => d.map((h, k) => (k === i ? { ...h, ...p } : h)));
  const info = (event: string) => cat.events.find((e) => e.name === event);

  return (
    <div className="expert">
      {draft.map((h, i) => {
        const meta = info(h.event);
        const key = `${scope}-${i}`;
        const trial = tried[key]?.rule === h ? tried[key] : undefined;
        return (
          <div className="rule" key={i} data-blocking={meta?.blocking ? "" : undefined}>
            <div className="line">
              <select value={h.event} onChange={(e) => patch(i, { event: e.target.value })}>
                {cat.events.map((e) => (
                  <option key={e.name} value={e.name}>
                    {eventLabel(e.name)}
                  </option>
                ))}
              </select>
              {meta?.usesMatch && (
                <input
                  className="match"
                  value={h.match ?? ""}
                  placeholder={t("ext.which_tools_to_match_regex")}
                  onChange={(e) => patch(i, { match: e.target.value })}
                />
              )}
              {meta?.blocking && <i className="warn">{t("ext.can_stop_the_agent")}</i>}
                <button className="act ghost" data-action="hooks.remove" onClick={() => onSave(draft.filter((_, k) => k !== i))}>
                {t("common.delete")}
              </button>
            </div>
            <input
              className="cmd"
              value={h.command}
              placeholder={t("ext.the_command_to_run")}
              onChange={(e) => patch(i, { command: e.target.value })}
            />
            {h.issues?.map((msg) => (
              <div className="why" key={msg}>
                {msg}
              </div>
            ))}
            <div className="line">
                <button className="act" data-action="hooks.run-once" disabled={busy === key} onClick={() => onTry(h, key)}>
                {t(busy === key ? "ext.running" : "ext.dry_run_once")}
              </button>
              {/* The command really runs. Saying so is the difference between a
                  button that checks syntax and one that might delete a file. */}
              <span className="note">{t("ext.really_runs_this_command")}</span>
            </div>
            {trial?.result && <DryRun res={trial.result} blocking={!!meta?.blocking} />}
            {trial?.error && <div className="why">{trial.error}</div>}
          </div>
        );
      })}

      <div className="line">
        <button
          className="act"
          onClick={() => setDraft((d) => [...d, { event: "PostToolUse", command: "", scope }])}
        >
          {t("common.add_a_rule")}
        </button>
            <button className="act" data-action="hooks.save" data-primary disabled={busy === "save"} onClick={() => onSave(draft)}>
          {t(busy === "save" ? "common.saving" : "common.save")}
        </button>
      </div>

      {plugin.length > 0 && (
        <div className="fromplugin">
          <span className="lb">{t("ext.n_from_plugins", { n: plugin.length })}</span>
          {plugin.map((h, i) => (
            <div className="hookrow" key={i}>
              <span className="ev" title={eventLabel(h.event)}>{eventLabel(h.event)}</span>
              <span className="cm">{h.command}</span>
              <span className="sc">{t("common.read_only")}</span>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

// The exit code is not the answer; what it does on this event is. A dry run that
// only printed "exit 2" would leave the user exactly as unsure as before.
function DryRun({ res, blocking }: { res: HookDryRun; blocking: boolean }) {
  const verdict = res.timedOut
    ? "ext.timed_out_this_step_counts"
    : res.blocks
      ? "这一次它会挡住这一步"
      : res.decision === "block" && !blocking
        ? "这个事件挡不住东西，只会提醒你"
        : res.decision === "warn"
          ? "会作为提醒显示出来"
          : "将放行";
  return (
    <span className="dryrun" data-blocks={res.blocks ? "" : undefined}>
      <b>{verdict}</b>
      <span className="meta">
        exit {res.exitCode} · {res.durationMs}ms
      </span>
      {(res.stdout || res.stderr) && <span className="out">{(res.stdout || res.stderr || "").slice(0, 160)}</span>}
    </span>
  );
}
