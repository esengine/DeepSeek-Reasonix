import { useMemo } from "react";
import { t } from "../i18n";
import type { ModelEntry, RoleAssignments, RoleOverride } from "../port/port";
import { activeKind, contextLabel, groupVendors, type Vendor } from "./Models";
import { orderAccounts, useProviderOrder } from "../state/providerorder";

type RoleKey = keyof RoleAssignments["roles"];
type Answers = "chat" | "decision" | "search" | "vision";

// Decision is the one job that cannot follow the main model: it asks a question
// set, which a chat model has no answer for, so its row offers the decision
// sources and says so when there are none.
const ROLES: [RoleKey, string, string, Answers][] = [
  ["planner", "common.plan", "settings.drafts_a_plan_read_only", "chat"],
  ["subagent", "common.subagents", "usage.work_sent_out", "chat"],
  ["vision", "models.vision", "usage.reads_images_the_main_model", "chat"],
  ["guardian", "models.review", "usage.audits_this_turn_independently", "chat"],
  ["title", "session.auto_name", "usage.generate_short_conversation_titles", "chat"],
  ["decision", "models.decision", "usage.the_backend_system_one_asks", "decision"],
];

const shown = (m: ModelEntry) => m.modelAlias?.trim() || m.modelShort || m.model;
const answersOf = (m: ModelEntry): Answers => (m.answers === "decision" ? "decision" : "chat");
const answersMatch = (m: ModelEntry, answers: Answers) =>
  answers === "search" ? m.webSearch === true : answers === "vision" ? m.vision === true && answersOf(m) === "chat" : answersOf(m) === answers;

// Only what the config or the catalog declares: an inferred "reads images" sends
// the user to a request the endpoint rejects.
function traits(m?: ModelEntry): string {
  if (!m) return "";
  const ctx = contextLabel(m.contextWindow);
  return [m.vision ? t("common.reads_images") : "", m.efforts && m.efforts.length > 1 ? t("usage.adjustable_reasoning") : "", ctx ? t("usage.size_context", { size: ctx }) : ""]
    .filter(Boolean)
    .join(" · ");
}

interface Props {
  models: ModelEntry[];
  roles: RoleAssignments | null;
  main?: string;
  busy: string;
  // Which protocol each account is showing, as chosen on the services page.
  protocol: Record<string, string>;
  onMain: (ref: string) => void;
  effort?: string;
  onEffort?: (effort: string) => void;
  onRole: (role: string, ref: string, effort?: string) => void;
  // Entries that win over a role's own row, so the row is not what runs.
  overrides?: Record<string, RoleOverride[]>;
  onClearOverride?: (role: string, key: string) => void;
}

// One row per job: what it is for, which model does it, and which service that
// model is reached through.
export function ModelUsage({ models, roles, main, busy, protocol, effort = "auto", onEffort, onMain, onRole, overrides, onClearOverride }: Props) {
  const order = useProviderOrder();
  const vendors = useMemo(() => orderAccounts(groupVendors(models), order), [models, order]);
  const serviceOf = (ref?: string) => vendors.find((v) => Object.values(v.byKind).some((list) => list.some((m) => m.ref === ref)))?.label ?? "";
  const byRef = (ref?: string) => models.find((m) => m.ref === ref);

  if (models.length === 0) return <div className="empty">{t("usage.could_not_read_the_model")}</div>;

  const current = byRef(main);
  const shown = models.find((m) => m.default)?.ref || main;
  const shownModel = byRef(shown);
  // What an attachment reaches: the vision role if assigned, else the sub-agent
  // it would be handed to, else the main model.
  const assignments = roles?.roles;
  const visionRef = assignments?.vision || assignments?.subagent || main;
  const visionModel = byRef(visionRef);

  return (
    <>
      <div className="usage" role="table" aria-label={t("usage.model_uses")}>
        <div className="usage-row usage-hd" role="row">
          <span role="columnheader">{t("usage.use")}</span>
          <span role="columnheader">{t("usage.model")}</span>
          <span role="columnheader">{t("usage.effort")}</span>
          <span role="columnheader">{t("usage.connections")}</span>
        </div>
        <div className="usage-row" role="row">
          <span className="usage-job" role="rowheader"><b>{t("common.default_model")}</b><small>{t("usage.this_conversation_and_most_tasks")}</small></span>
          <span role="cell">
            <Choices vendors={vendors} protocol={protocol} main={main} value={shown ?? ""} answers="chat"
              label={t("common.default_model")} disabled={busy !== ""} onPick={onMain} />
          </span>
          <span className="usage-effort" role="cell"><span className="usage-effort-label">{t("usage.effort")}</span><Efforts model={current} value={effort} label={t("usage.default_model_reasoning_effort")}
            action="reasoning.effort" disabled={busy !== "" || !onEffort} onPick={(level) => onEffort?.(level)} /></span>
          <span className="usage-conn" role="cell">{serviceOf(shown)}<small>{traits(shownModel)}</small></span>
        </div>
        {roles && ROLES.map(([key, name, tag, answers]) => {
          const set = roles.roles[key];
          const roleModel = byRef(set || (key === "title" || answers === "decision" ? "" : main));
          const roleEffort = roles.efforts?.[key] || "auto";
          const offered = models.filter((m) => answersMatch(m, answers));
          const none = answers === "decision" && offered.length === 0;
          return (
            <div className="usage-row" role="row" key={key}>
              <span className="usage-job" role="rowheader"><b>{t(name)}</b><small>{t(tag)}</small></span>
              <span role="cell">
                <Choices vendors={vendors} protocol={protocol} main={main} value={set} answers={key === "vision" ? "vision" : answers}
                  label={t(name)} role disabled={busy !== "" || none}
                  empty={t(key === "title" ? "usage.do_not_use" : answers === "chat" ? "usage.follow_the_main_model" : none ? "usage.no_source_yet" : "usage.do_not_use")}
                  onPick={(ref) => onRole(key, ref, "auto")} />
                {(overrides?.[key] ?? []).map((o) => (
                  <span className="usage-override" key={o.key} data-scope={o.scope}>
                    <span>{t("usage.key_is_pinned_to_model", { key: o.key, model: o.model })}</span>
                    {o.scope === "user" ? (
                      <button type="button" data-action="roles.override.clear" disabled={busy !== ""} onClick={() => onClearOverride?.(key, o.key)}>
                        {t("usage.follow_this_choice")}
                      </button>
                    ) : (
                      <small>{t("usage.set_by_the_project_config")}</small>
                    )}
                  </span>
                ))}
              </span>
              <span className="usage-effort" role="cell"><span className="usage-effort-label">{t("usage.effort")}</span><Efforts model={roleModel} value={roleEffort} label={t("usage.name_reasoning_effort", { name: t(name) })}
                action="roles.effort" fixedOff={key === "title"} disabled={busy !== "" || answers === "decision"}
                onPick={(level) => onRole(key, set || main || "", level)} /></span>
              <span className="usage-conn" role="cell" data-follow={!set && answers === "chat" && key !== "title" ? "" : undefined}>
                {set ? serviceOf(set) : key === "title" ? t("usage.use_message_preview") : answers === "chat" ? t("usage.follows_the_default") : none ? t("usage.add_a_decision_source_under") : ""}
              </span>
            </div>
          );
        })}
        {roles && <SearchUsage models={models} vendors={vendors} protocol={protocol} main={main} roles={roles} busy={busy} onRole={onRole} />}
      </div>
      {!roles && <div className="empty">{t("usage.could_not_read_the_role")}</div>}
      <p className="note">
        {visionModel?.vision
          ? t("usage.images_the_main_model_cannot_2", { model: visionModel.model })
          : t("usage.images_the_main_model_cannot")}
      </p>
    </>
  );
}

function searchReason(reason?: string) {
  switch (reason) {
    case "bad_ref": return t("usage.the_search_model_must_be");
    case "not_added": return t("usage.the_search_models_connection_has");
    case "model_removed": return t("usage.the_search_model_is_no");
    case "unsupported": return t("usage.the_search_models_protocol_has");
    case "no_credentials": return t("usage.the_search_models_connection_has_2");
    default: return "";
  }
}

function SearchUsage({ models, vendors, protocol, main, roles, busy, onRole }: {
  models: ModelEntry[]; vendors: Vendor[]; protocol: Record<string, string>; main?: string;
  roles: RoleAssignments; busy: string; onRole: (role: string, ref: string) => void;
}) {
  const value = roles.web_search, effective = roles.web_search_effective ?? "";
  const ref = !value || value.toLowerCase() === "auto" ? "" : value;
  const none = !models.some((m) => m.webSearch);
  const serviceOf = (r: string) => vendors.find((v) => Object.values(v.byKind).flat().some((m) => m.ref === r))?.label ?? r;
  const reason = searchReason(roles.web_search_reason);
  const used = ref ? (reason || serviceOf(ref)) : effective
    ? t("usage.automatic_the_conversation_models_built", { model: serviceOf(effective) })
    : t("usage.the_conversation_model_has_no");
  return (
    <div className="usage-row" role="row">
      <span className="usage-job" role="rowheader"><b>{t("common.web_search")}</b><small>{t("usage.model_for_independent_search_requests")}</small></span>
      <span role="cell">
        <Choices vendors={vendors} protocol={protocol} main={main} value={ref} answers="search"
          label={t("common.web_search")} role disabled={busy !== ""} empty={t("common.pick_one_automatically")}
          onPick={(next) => onRole("web_search", next)} />
      </span>
      <span className="usage-effort" role="cell">{t("usage.not_applicable")}</span>
      <span className="usage-conn" role="cell">
        {used}
        {none && <small>{t("usage.no_search_models_available")}</small>}
        {roles.web_search_source === "project" && <small>{effective
          ? t("usage.project_override_model_is_used", { model: serviceOf(effective) })
          : t("usage.the_project_config_overrides_this")}</small>}
      </span>
    </div>
  );
}

function Efforts({ model, value, label, action, fixedOff = false, disabled, onPick }: {
  model?: ModelEntry; value: string; label: string; action: "roles.effort" | "reasoning.effort"; fixedOff?: boolean; disabled: boolean; onPick: (level: string) => void;
}) {
  const levels = [...new Set(["auto", ...(model?.efforts ?? [])])];
  const supported = !!model?.efforts?.some((level) => level !== "auto");
  if (fixedOff) {
    return <select className="usage-pick" aria-label={label} data-action={action === "roles.effort" ? "roles.effort" : "reasoning.effort"} value="auto" disabled>
      <option value="auto">{t("usage.not_applicable")}</option>
    </select>;
  }
  return (
    <select className="usage-pick" aria-label={label} data-action={action === "roles.effort" ? "roles.effort" : "reasoning.effort"} value={levels.includes(value) ? value : "auto"}
      disabled={disabled || !supported} onChange={(e) => onPick(e.target.value)}>
      {!supported ? <option value="auto">{t("usage.not_applicable")}</option>
        : levels.map((level) => <option key={level} value={level}>{level === "auto" ? t("common.auto") : level}</option>)}
    </select>
  );
}

function Choices({
  vendors, protocol, main, value, answers, label, role = false, disabled, empty, onPick,
}: {
  vendors: Vendor[]; protocol: Record<string, string>; main?: string; value: string; answers: Answers;
  label: string; role?: boolean; disabled: boolean; empty?: string; onPick: (ref: string) => void;
}) {
  // Each service offers the models of the protocol it is on, and a model already
  // chosen stays listed even when its service is showing the other protocol.
  const groups = vendors
    .map((v) => {
      const kind = protocol[v.key] ?? activeKind(v, main);
      const shown = (answers === "search" ? Object.values(v.byKind).flat() : v.byKind[kind] ?? []).filter((m) => answersMatch(m, answers) && !(answers === "search" && m.notAdded));
      const held = Object.values(v.byKind).flat().find((m) => m.ref === value && answersMatch(m, answers) && !shown.includes(m));
      return { v, rows: held ? [held, ...shown] : shown };
    })
    .filter((g) => g.rows.length > 0);
  return (
    <select className="usage-pick" aria-label={label} data-action={role ? "roles.model" : "model.select"} value={value} disabled={disabled}
      onChange={(e) => onPick(e.target.value)}>
      {empty !== undefined && <option value="">{empty}</option>}
      {answers === "search" && value && !groups.some((g) => g.rows.some((m) => m.ref === value)) && <option value={value}>{t("usage.model_unavailable", { model: value })}</option>}
      {answers === "vision" && value && !groups.some((g) => g.rows.some((m) => m.ref === value)) && <option value={value}>{t("usage.model_not_declared_to_read", { model: value })}</option>}
      {groups.map(({ v, rows }) => (
        <optgroup key={v.key} label={v.label}>
          {rows.map((m) => (
            <option key={m.ref} value={m.ref}>{m.vision ? `${shown(m)} · ${t("common.reads_images")}` : shown(m)}</option>
          ))}
        </optgroup>
      ))}
    </select>
  );
}
