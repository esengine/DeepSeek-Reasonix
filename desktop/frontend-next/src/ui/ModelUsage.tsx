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
  ["planner", "common.plan", "common.draftsAPlanReadOnly", "chat"],
  ["subagent", "common.subagents2", "modelUsage.workSentOut", "chat"],
  ["vision", "common.vision", "modelUsage.readsImagesTheMainModel", "chat"],
  ["guardian", "common.review", "modelUsage.auditsThisTurnIndependently", "chat"],
  ["title", "common.autoName", "modelUsage.generateShortConversationTitles", "chat"],
  ["decision", "common.decision", "modelUsage.theBackendSystemOneAsks", "decision"],
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
  return [m.vision ? t("common.readsImages") : "", m.efforts && m.efforts.length > 1 ? t("modelUsage.adjustableReasoning") : "", ctx ? t("modelUsage.context", { size: ctx }) : ""]
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

  if (models.length === 0) return <div className="empty">{t("modelUsage.couldNotReadTheModel")}</div>;

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
      <div className="usage" role="table" aria-label={t("modelUsage.modelUses")}>
        <div className="usage-row usage-hd" role="row">
          <span role="columnheader">{t("modelUsage.use")}</span>
          <span role="columnheader">{t("modelUsage.model")}</span>
          <span role="columnheader">{t("common.effort")}</span>
          <span role="columnheader">{t("common.connections")}</span>
        </div>
        <div className="usage-row" role="row">
          <span className="usage-job" role="rowheader"><b>{t("common.defaultModel")}</b><small>{t("modelUsage.thisConversationAndMostTasks")}</small></span>
          <span role="cell">
            <Choices vendors={vendors} protocol={protocol} main={main} value={shown ?? ""} answers="chat"
              label={t("common.defaultModel")} disabled={busy !== ""} onPick={onMain} />
          </span>
          <span className="usage-effort" role="cell"><span className="usage-effort-label">{t("common.effort")}</span><Efforts model={current} value={effort} label={t("modelUsage.defaultModelReasoningEffort")}
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
                  empty={t(key === "title" ? "modelUsage.doNotUse" : answers === "chat" ? "modelUsage.followTheMainModel" : none ? "modelUsage.noSourceYet" : "modelUsage.doNotUse")}
                  onPick={(ref) => onRole(key, ref, "auto")} />
                {(overrides?.[key] ?? []).map((o) => (
                  <span className="usage-override" key={o.key} data-scope={o.scope}>
                    <span>{t("modelUsage.isPinnedToByConfiguration", { key: o.key, model: o.model })}</span>
                    {o.scope === "user" ? (
                      <button type="button" data-action="roles.override.clear" disabled={busy !== ""} onClick={() => onClearOverride?.(key, o.key)}>
                        {t("modelUsage.followThisChoice")}
                      </button>
                    ) : (
                      <small>{t("modelUsage.setByTheProjectConfig")}</small>
                    )}
                  </span>
                ))}
              </span>
              <span className="usage-effort" role="cell"><span className="usage-effort-label">{t("common.effort")}</span><Efforts model={roleModel} value={roleEffort} label={t("modelUsage.reasoningEffort", { name: t(name) })}
                action="roles.effort" fixedOff={key === "title"} disabled={busy !== "" || answers === "decision"}
                onPick={(level) => onRole(key, set || main || "", level)} /></span>
              <span className="usage-conn" role="cell" data-follow={!set && answers === "chat" && key !== "title" ? "" : undefined}>
                {set ? serviceOf(set) : key === "title" ? t("modelUsage.useMessagePreview") : answers === "chat" ? t("modelUsage.followsTheDefault") : none ? t("modelUsage.addADecisionSourceUnder") : ""}
              </span>
            </div>
          );
        })}
        {roles && <SearchUsage models={models} vendors={vendors} protocol={protocol} main={main} roles={roles} busy={busy} onRole={onRole} />}
      </div>
      {!roles && <div className="empty">{t("modelUsage.couldNotReadTheRole")}</div>}
      <p className="note">
        {visionModel?.vision
          ? t("modelUsage.imagesTheMainModelCannot", { model: visionModel.model })
          : t("modelUsage.imagesTheMainModelCannot2")}
      </p>
    </>
  );
}

function searchReason(reason?: string) {
  switch (reason) {
    case "bad_ref": return t("modelUsage.theSearchModelMustBe");
    case "not_added": return t("modelUsage.theSearchModelSConnection");
    case "model_removed": return t("modelUsage.theSearchModelIsNo");
    case "unsupported": return t("modelUsage.theSearchModelSProtocol");
    case "no_credentials": return t("modelUsage.theSearchModelSConnection2");
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
    ? t("modelUsage.automaticTheConversationModelS", { model: serviceOf(effective) })
    : t("modelUsage.theConversationModelHasNo");
  return (
    <div className="usage-row" role="row">
      <span className="usage-job" role="rowheader"><b>{t("common.webSearch2")}</b><small>{t("modelUsage.modelForIndependentSearchRequests")}</small></span>
      <span role="cell">
        <Choices vendors={vendors} protocol={protocol} main={main} value={ref} answers="search"
          label={t("common.webSearch2")} role disabled={busy !== ""} empty={t("common.pickOneAutomatically")}
          onPick={(next) => onRole("web_search", next)} />
      </span>
      <span className="usage-effort" role="cell">{t("modelUsage.notApplicable")}</span>
      <span className="usage-conn" role="cell">
        {used}
        {none && <small>{t("modelUsage.noSearchModelsAvailable")}</small>}
        {roles.web_search_source === "project" && <small>{effective
          ? t("modelUsage.projectOverrideIsUsedThis", { model: serviceOf(effective) })
          : t("modelUsage.theProjectConfigOverridesThis")}</small>}
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
      <option value="auto">{t("modelUsage.notApplicable")}</option>
    </select>;
  }
  return (
    <select className="usage-pick" aria-label={label} data-action={action === "roles.effort" ? "roles.effort" : "reasoning.effort"} value={levels.includes(value) ? value : "auto"}
      disabled={disabled || !supported} onChange={(e) => onPick(e.target.value)}>
      {!supported ? <option value="auto">{t("modelUsage.notApplicable")}</option>
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
      {answers === "search" && value && !groups.some((g) => g.rows.some((m) => m.ref === value)) && <option value={value}>{t("modelUsage.unavailable", { model: value })}</option>}
      {answers === "vision" && value && !groups.some((g) => g.rows.some((m) => m.ref === value)) && <option value={value}>{t("modelUsage.notDeclaredToReadImages", { model: value })}</option>}
      {groups.map(({ v, rows }) => (
        <optgroup key={v.key} label={v.label}>
          {rows.map((m) => (
            <option key={m.ref} value={m.ref}>{m.vision ? `${shown(m)} · ${t("common.readsImages")}` : shown(m)}</option>
          ))}
        </optgroup>
      ))}
    </select>
  );
}
