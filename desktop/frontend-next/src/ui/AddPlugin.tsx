import { useEffect, useId, useRef, useState } from "react";
import { ModPreviewBlock } from "./ModPreviewBlock";
import { useEscape } from "./dismiss";
import { current as language, plural, t } from "../i18n";
import { useFileDrop } from "./filedrop";
import type { AgentPort, PluginAction, PluginPackage, PluginPlan } from "../port/port";
import { reason } from "../i18n/kernel";
import { CopyButton } from "./CopyButton";

// Installing and importing are the same act from two doors, so there is one
// box: a link, or a folder picked off this machine.
const PLACEHOLDER = `https://github.com/acme/review-kit

~/projects/my-plugin`;

const RISK_TITLE: Record<string, string> = {
  high: "ext.these_run_programs_on_this",
  medium: "ext.these_change_what_is_available",
  low: "ext.these_only_add_files",
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
  // Opened to import a folder: the picker comes up at once.
  pickFirst?: boolean;
}

export function AddPlugin(props: Props) {
  const [connection, setConnection] = useState({ port: props.port, generation: 0 });
  if (connection.port !== props.port) setConnection({ port: props.port, generation: connection.generation + 1 });
  return <PluginInput key={connection.generation} {...props} />;
}

function PluginInput({ port, onClose, onInstalled, updating, source, onApplying, pickFirst }: Props) {
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
        setError(p.error || p.next || t("ext.there_is_nothing_installable_at"));
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
  const picked = useRef(false);
  useEffect(() => {
    if (!pickFirst || picked.current) return;
    picked.current = true;
    void pick();
  });

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
              {t(updating ? "common.retry" : "common.back")}
            </button>
          )}
          <button className="act" data-action="extensions.finish" ref={done.ok ? apply : undefined} onClick={onClose}>
            {t("common.done")}
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
                : t("ext.reinstall_name", { name: updating.name })}
            </span>
            <span className="why">{gained.length ? t("ext.this_version_adds_list", { list: gained.join(language() === "zh" ? "、" : " · ") }) : t("ext.nothing_new_that_runs")}</span>
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
        <PreviewCut shown={plan.previewTruncated} hidden={plan.hiddenActions} />
        {plan.warnings?.map((wmsg) => (
          <div className="why" key={wmsg}>
            {wmsg}
          </div>
        ))}
        <div className="acts">
          <span className="note">{t(updating ? "ext.replace_the_installed_copy" : "ext.install_to_mine_available_in")}</span>
          <button
            className="act"
            data-action={updating ? "extensions.cancel" : "extensions.back"}
            disabled={busy}
            onClick={() => (updating ? onClose() : setPlan(null))}
          >
            {t(updating ? "common.cancel" : "common.back")}
          </button>
          <button
            className="act"
            data-action={updating ? "extensions.update" : "extensions.install"}
            ref={apply}
            data-primary
            disabled={busy}
            onClick={() => void install()}
          >
            {t(busy ? (updating ? "ext.updating" : "ext.installing") : updating ? "ext.update" : "ext.install")}
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
          <span className="t">{error ? t("ext.cannot_read_name_s_source", { name: updating.name }) : t("ext.reading_name_s_source", { name: updating.name })}</span>
          <span className="why">{error || updating.source}</span>
        </div>
        <div className="acts">
          {error && <button className="act" data-action="extensions.inspect" data-primary ref={apply} disabled={busy} onClick={() => void look()}>{t("common.retry")}</button>}
          <button className="act" data-action="extensions.cancel" ref={error ? undefined : apply} onClick={onClose}>
            {t(error ? "ext.close" : "common.cancel")}
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
        data-action-change="extensions.inspect"
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
        <span className="note" id={sourceLabel}>{t("ext.a_repository_address_or_drop")}</span>
        <button className="act" data-action="extensions.pick-folder" disabled={busy} onClick={() => void pick()}>
          {t("common.choose_a_folder")}
        </button>
        <button className="act" data-action="extensions.cancel" onClick={onClose}>
          {t("common.cancel")}
        </button>
        <button
          className="act"
          data-action="extensions.inspect"
          data-primary
          disabled={!text.trim() || busy}
          onClick={() => void look()}
        >
          {t(busy ? "ext.reading" : "ext.see_what_it_is")}
        </button>
      </div>
      {error && <div className="why" role="alert">{error}</div>}
    </div>
  );
}

export function PreviewCut({ shown, hidden }: { shown?: boolean; hidden?: number }) {
  if (!shown && !hidden) return null;
  return (
    <div className="why" data-testid="preview-cut">
      {hidden ? t("ext.n_more_steps_are_not", { n: hidden }) : null}
      {hidden && shown ? " " : null}
      {shown ? t("ext.some_text_is_too_long") : null}
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
      {a.kind === "plugin" && a.commit && (
        <div className="risk">
          <span className="lb">{t("ext.resolved_commit")}</span>
          <span className="dt">{a.commit}</span>
          <CopyButton text={a.commit} label={t("ext.copy_commit")} iconOnly showFeedback />
        </div>
      )}
      {runs.map((r) => (
        <div className="risk" data-kind="shell" key={r.label}>
          <span className="lb">{t(r.label)}</span>
          <span className="dt">{r.detail}</span>
          {r.why && <span className="why">{t(r.why)}</span>}
        </div>
      ))}
      {a.mod && <ModPreviewBlock mod={a.mod} />}
      {adds.length > 0 && (
        <div className="risk">
          <span className="lb">{t("ext.will_be_added")}</span>
          <span className="dt">{adds.join(" · ")}</span>
        </div>
      )}
      {Object.keys(a.env ?? {}).map((k) => (
        <div className="risk" data-kind="secret" key={k}>
          <span className="lb">{t("ext.needs_filling_in")}</span>
          <span className="dt">{k}</span>
        </div>
      ))}
      {a.skippedCapabilities?.map((s) => (
        <div className="risk" key={s.capability + s.reason}>
          <span className="lb">{t("ext.unusable")}</span>
          <span className="dt">{s.capability}</span>
          <span className="why">{s.reason}</span>
        </div>
      ))}
      {a.previewTruncated && (
        <div className="risk">
          <span className="lb">{t("ext.shortened")}</span>
          <span className="dt">{t("ext.some_text")}</span>
        </div>
      )}
      {a.riskReasons?.length ? (
        <details className="reasons">
          <summary>{t("ext.the_kernels_assessment_n", { n: a.riskReasons.length })}</summary>
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
      label: "ext.long_running_process",
      detail: [a.runtime.command, ...(a.runtime.args ?? [])].join(" "),
      why: "ext.runs_inside_reasonix_can_read",
    });
  }
  if (a.runtime?.tools?.length) {
    out.push({ label: "ext.new_tools", detail: a.runtime.tools.join(language() === "zh" ? "、" : ", "), why: "ext.the_agent_can_call_them" });
  }
  if (a.hookCount) {
    out.push({ label: "ext.automatic_hooks", detail: language() === "zh" ? t("common.n_2", { n: a.hookCount }) : plural(a.hookCount, "ext.n_1_hook", "ext.n_hooks"), why: "ext.run_automatically_during_the_session" });
  }
  if (a.toolCount) {
    out.push({ label: "ext.external_services", detail: plural(a.toolCount, "ext.n_1_mcp_service", "ext.n_mcp_services"), why: "ext.provides_capabilities_equivalent_to_the" });
  }
  if (a.kind === "mcp" && a.command) {
    out.push({ label: "ext.will_start", detail: [a.command, ...(a.args ?? [])].join(" ") });
  }
  if (a.kind === "mcp" && a.url) {
    out.push({ label: "ext.will_connect_to", detail: a.url });
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
  if (plugin?.runtime && !current.runtime) out.push(t("ext.a_long_running_process"));
  const tools = (plugin?.runtime?.tools ?? []).filter((name) => !current.runtime?.tools?.includes(name));
  if (tools.length > 0) out.push(t("ext.tools_names", { names: tools.join(language() === "zh" ? "、" : ", ") }));
  const hooks = (plugin?.hookCount ?? 0) - (current.hooks?.length ?? 0);
  if (hooks > 0) out.push(plural(hooks, "ext.a_hook", "ext.n_hooks"));
  const servers = (plugin?.toolCount ?? 0) - (current.mcpServers?.length ?? 0);
  if (servers > 0) out.push(plural(servers, "ext.an_external_service", "ext.n_external_services"));
  // A separate MCP action installs a server of its own, outside the package.
  const standalone = actions.filter((a) => a.kind === "mcp").length;
  if (standalone > 0) out.push(plural(standalone, "ext.a_standalone_mcp_service", "ext.n_standalone_mcp_services"));
  return out;
}

function contributes(a: PluginAction): string[] {
  const parts: string[] = [];
  const add = (n: number | undefined, one: string, many: string) => {
    if (n) parts.push(plural(n, one, many));
  };
  add(a.skillCount, "ext.n_1_skill", "ext.n_skills");
  add(a.commandCount, "ext.n_1_command", "ext.n_commands");
  add(a.agentCount, "ext.n_1_subagent", "subagent.group.count");
  add(a.promptCount, "ext.n_1_prompt", "ext.n_prompts");
  add(a.themeCount, "ext.n_1_palette", "ext.n_palettes");
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
      <span className="nm">{plan.actions?.[0]?.name || t("ext.install")}</span>
      <span className="dt">
        {state === "ready" && t("ext.installed_the_next_turn_can")}
        {state === "action_required" && t("ext.installed_but_the_runtime_did", { reason: plan.reloadError! })}
        {state === "issue" && (plan.error || (failed.length ? t("ext.not_all_items_were_installed") : plan.next || t("ext.not_installed")))}
      </span>
    </div>
    {failed.map((action, i) => (
      <div className="why" role="alert" key={`${action.kind}:${action.name}:${i}`}>
        {action.name || action.kind}: {action.error || action.next || t("ext.not_installed")}
        {action.error && action.next && <div>{action.next}</div>}
      </div>
    ))}
    {state === "issue" && plan.reloadError && (
      <div className="why" role="status">
        {t("ext.the_runtime_did_not_reload", { reason: plan.reloadError })}
      </div>
    )}
  </>;
}
