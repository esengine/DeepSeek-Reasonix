import { useCallback, useEffect, useMemo, useRef, useState, type CSSProperties } from "react";
import { Check, ChevronDown } from "lucide-react";

import { app } from "../lib/bridge";
import { asArray } from "../lib/array";
import { useT } from "../lib/i18n";
import { PROJECT_COLOR_OPTIONS, projectColorValue, type ProjectColorKey } from "../lib/projectColors";
import type { MCPToolView, SettingsView, SkillView, SubagentProfileInput } from "../lib/types";

import { InlineConfirmButton } from "./InlineConfirmButton";
import { AnchoredPopover } from "./AnchoredPopover";
import { CopyButton } from "./CopyButton";
import { allRefs, EFFORT_PRESETS, ModelPicker, toRef } from "./SettingsPanel";
import { Tooltip } from "./Tooltip";

const NAME_PATTERN = /^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$/;

function subagentScopeLabel(scope: string, t: ReturnType<typeof useT>): string {
  switch (scope) {
    case "builtin":
      return t("caps.skillScopeBuiltin");
    case "project":
      return t("caps.skillScopeProject");
    case "custom":
      return t("caps.skillScopeCustom");
    case "global":
      return t("caps.skillScopeGlobal");
    default:
      return scope;
  }
}

function toolsSummaryLabel(allowedTools: string[] | undefined, t: ReturnType<typeof useT>): string {
  if (!allowedTools || allowedTools.length === 0) return t("subagents.allTools");
  return t("subagents.toolCount", { n: allowedTools.length });
}

function builtinDescription(name: string, fallback: string, t: ReturnType<typeof useT>): string {
  switch (name) {
    case "explore":
      return t("subagents.builtinExploreDescription");
    case "research":
      return t("subagents.builtinResearchDescription");
    case "review":
      return t("subagents.builtinReviewDescription");
    case "security-review":
      return t("subagents.builtinSecurityReviewDescription");
    default:
      return fallback;
  }
}

function shortModelRef(ref: string): string {
  const parts = ref.split("/");
  return parts[parts.length - 1] || ref;
}

// SubagentsSettingsPage is a self-contained subagent-profile management page
// embedded inside the settings centre. A "subagent profile" is a skill file
// with runAs=subagent + invocation=manual (see internal/skill): it stays
// invocable by name but is excluded from the pinned Skills index, so the
// model never discovers or auto-invokes a profile the user configured for
// their own deliberate use. This reads the same app.SkillsSettings() list
// the Skills tab uses and filters client-side — the underlying file is the
// same, just shown through a different lens — rather than adding a
// redundant, parallel backend list endpoint.
export function SubagentsSettingsPage({ s, onUseInChat }: { s: SettingsView; onUseInChat: (command: string) => void }) {
  const t = useT();
  const [skills, setSkills] = useState<SkillView[] | null>(null);
  const [tools, setTools] = useState<MCPToolView[]>([]);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [query, setQuery] = useState("");
  const [adding, setAdding] = useState(false);
  const [editingSkill, setEditingSkill] = useState<SkillView | null>(null);
  const formOpen = adding || editingSkill !== null;

  const reload = useCallback(async () => {
    const [settingsView, availableTools] = await Promise.all([
      app.SkillsSettings().catch(() => ({ skills: [], skillRoots: [] })),
      app.AvailableSubagentTools().catch(() => []),
    ]);
    setSkills(asArray<SkillView>(settingsView?.skills).filter((sk) => sk.runAs === "subagent"));
    setTools(asArray<MCPToolView>(availableTools));
  }, []);
  useEffect(() => { void reload(); }, [reload]);

  const mutate = async (fn: () => Promise<unknown>) => {
    setBusy(true);
    setErr(null);
    try {
      await fn();
      await reload();
      return true;
    } catch (e) {
      setErr(String((e as Error)?.message ?? e));
      return false;
    } finally {
      setBusy(false);
    }
  };

  const filtered = useMemo(() => {
    if (!skills) return [];
    const q = query.trim().toLowerCase();
    if (!q) return skills;
    return skills.filter((sk) => `${sk.name} ${sk.description}`.toLowerCase().includes(q));
  }, [skills, query]);

  const builtins = useMemo(() => filtered.filter((sk) => sk.scope === "builtin"), [filtered]);
  // This page creates only project/global profiles. Manual subagents loaded
  // from configured custom paths remain visible, but their external ownership
  // is preserved: they are read-only here and managed from the Skills page.
  const custom = useMemo(
    () => filtered.filter((sk) => (sk.scope === "project" || sk.scope === "global") && sk.invocationMode === "manual"),
    [filtered],
  );
  const external = useMemo(
    () => filtered.filter((sk) => sk.scope === "custom" && sk.invocationMode === "manual"),
    [filtered],
  );

  if (!skills) return <div className="empty">{t("caps.loading")}</div>;

  return (
    <section className="mem-section">
      {err && <div className="banner banner--error">{err}</div>}
      {!formOpen && (
        <div className="cap-search subagents-toolbar">
          <input
            className="mem-input"
            type="search"
            placeholder={t("subagents.search")}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
          />
          <button
            className="btn btn--small"
            type="button"
            disabled={busy}
            onClick={() => { setEditingSkill(null); setAdding(true); }}
          >
            {t("subagents.new")}
          </button>
          <button className="btn btn--small" type="button" disabled={busy} onClick={() => void mutate(() => app.RefreshSkills())}>
            {t("caps.refreshSkills")}
          </button>
        </div>
      )}

      {adding && (
        <SubagentProfileForm
          s={s}
          tools={tools}
          existingNames={skills.map((sk) => sk.name)}
          busy={busy}
          onCancel={() => setAdding(false)}
          onSave={(input) =>
            mutate(() => app.CreateSubagentProfile(input)).then((ok) => {
              if (ok) setAdding(false);
            })
          }
        />
      )}

      {editingSkill && (
        <SubagentProfileForm
          s={s}
          tools={tools}
          existingNames={skills.map((sk) => sk.name)}
          busy={busy}
          editingSkill={editingSkill}
          onCancel={() => setEditingSkill(null)}
          onSave={(input) =>
            mutate(() => app.UpdateSubagentProfile(editingSkill.name, editingSkill.scope, input)).then((ok) => {
              if (ok) setEditingSkill(null);
            })
          }
        />
      )}

      {!formOpen && (
        <>
          <section className="subagents-profile-group" aria-labelledby="subagents-custom-title">
            <div className="cap-skills-head">
              <div className="cap-skills-head__copy">
                <h3 id="subagents-custom-title" className="cap-skills-head__title">{t("subagents.customTitle")}</h3>
                <div className="cap-skills-head__summary">{t("subagents.customHint")}</div>
              </div>
            </div>
            {custom.length === 0 && external.length === 0 ? (
              <div className="mem-empty">{query.trim() ? t("subagents.noMatches") : t("subagents.noCustom")}</div>
            ) : (
              <div className="cap-skills">
                {custom.map((sk) => (
                  <CustomSubagentRow
                    key={`${sk.scope}:${sk.name}`}
                    skill={sk}
                    busy={busy}
                    onEdit={() => { setAdding(false); setEditingSkill(sk); }}
                    onDelete={() => void mutate(() => app.DeleteSubagentProfile(sk.name, sk.scope))}
                    onUseInChat={onUseInChat}
                  />
                ))}
                {external.map((sk) => (
                  <CustomSubagentRow key={`${sk.scope}:${sk.name}`} skill={sk} busy={busy} externallyManaged onUseInChat={onUseInChat} />
                ))}
              </div>
            )}
          </section>

          <section className="subagents-profile-group" aria-labelledby="subagents-builtin-title">
            <div className="cap-skills-head">
              <div className="cap-skills-head__copy">
                <h3 id="subagents-builtin-title" className="cap-skills-head__title">{t("subagents.builtinTitle")}</h3>
                <div className="cap-skills-head__summary">{t("subagents.builtinHint")}</div>
              </div>
            </div>
            {builtins.length > 0 && (
              <div className="cap-skills">
                {builtins.map((sk) => (
                  <BuiltinSubagentRow
                    key={sk.name}
                    skill={sk}
                    s={s}
                    busy={busy}
                    onSetModel={(ref) => void mutate(() => app.SetSubagentProfileModel(sk.name, ref))}
                    onSetEffort={(level) => void mutate(() => app.SetSubagentProfileEffort(sk.name, level))}
                    onReset={() => void mutate(async () => {
                      if (sk.configuredModel) await app.SetSubagentProfileModel(sk.name, "");
                      if (sk.configuredEffort) await app.SetSubagentProfileEffort(sk.name, "");
                    })}
                    onUseInChat={onUseInChat}
                  />
                ))}
              </div>
            )}
          </section>
        </>
      )}
    </section>
  );
}

function SubagentInvocation({ name, onUseInChat }: { name: string; onUseInChat: (command: string) => void }) {
  const t = useT();
  const command = `/${name} `;
  const example = t("subagents.invocationExample", { name });
  return (
    <div className="subagents-invocation">
      <div className="subagents-invocation__command">
        <span>{t("subagents.invocationLabel")}</span>
        <code>{example}</code>
        <CopyButton text={example} label={t("subagents.copyInvocation")} className="subagents-invocation__copy" />
      </div>
      <button className="btn btn--small" type="button" onClick={() => onUseInChat(command)}>
        {t("subagents.useInChat")}
      </button>
    </div>
  );
}

function EffortPicker({
  value,
  inheritedValue,
  disabled,
  ariaLabel,
  onPick,
}: {
  value: string;
  inheritedValue: string;
  disabled: boolean;
  ariaLabel: string;
  onPick: (level: string) => void;
}) {
  const t = useT();
  const [open, setOpen] = useState(false);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const selectedLabel = value || t("subagents.inheritDefault");
  const effectiveValue = value || inheritedValue;
  const pick = (level: string) => {
    setOpen(false);
    if (level !== value) onPick(level);
  };

  return (
    <div className="settings-model-picker subagents-effort-picker">
      <button
        ref={triggerRef}
        type="button"
        className="settings-model-picker__trigger"
        disabled={disabled}
        aria-label={ariaLabel}
        aria-haspopup="listbox"
        aria-expanded={open}
        onClick={() => setOpen((next) => !next)}
      >
        <span className="settings-model-picker__selected">
          <span>{selectedLabel}</span>
          <small>{t("subagents.effectiveValue", { value: effectiveValue })}</small>
        </span>
        <ChevronDown size={16} className={`settings-model-picker__chev${open ? " settings-model-picker__chev--open" : ""}`} />
      </button>
      <AnchoredPopover
        open={open && !disabled}
        anchorRef={triggerRef}
        onClose={() => setOpen(false)}
        className="settings-model-picker__menu subagents-effort-picker__menu"
        placement="bottom"
        style={{ width: triggerRef.current?.getBoundingClientRect().width }}
      >
        <div className="settings-model-picker__list" role="listbox">
          <button
            type="button"
            role="option"
            aria-selected={value === ""}
            className={`settings-model-picker__option settings-model-picker__option--pinned${value === "" ? " settings-model-picker__option--selected" : ""}`}
            onClick={() => pick("")}
          >
            <span>
              <strong>{t("subagents.inheritDefault")}</strong>
              <small>{t("subagents.effectiveValue", { value: inheritedValue })}</small>
            </span>
            {value === "" && <Check size={14} />}
          </button>
          {EFFORT_PRESETS.map((level) => (
            <button
              key={level}
              type="button"
              role="option"
              aria-selected={level === value}
              className={`settings-model-picker__option${level === value ? " settings-model-picker__option--selected" : ""}`}
              onClick={() => pick(level)}
            >
              <span>
                <strong>{level}</strong>
                <small>{t("subagents.effectiveValue", { value: level })}</small>
              </span>
              {level === value && <Check size={14} />}
            </button>
          ))}
        </div>
      </AnchoredPopover>
    </div>
  );
}

function BuiltinSubagentRow({
  skill,
  s,
  busy,
  onSetModel,
  onSetEffort,
  onReset,
  onUseInChat,
}: {
  skill: SkillView;
  s: SettingsView;
  busy: boolean;
  onSetModel: (ref: string) => void;
  onSetEffort: (level: string) => void;
  onReset: () => void;
  onUseInChat: (command: string) => void;
}) {
  const t = useT();
  const toolsLabel = toolsSummaryLabel(skill.allowedTools, t);
  const inheritedModel = shortModelRef(toRef(s.subagentModel || s.defaultModel, s)) || t("common.auto");
  const inheritedEffort = s.subagentEffort || t("common.auto");
  const overridden = Boolean(skill.configuredModel || skill.configuredEffort);
  return (
    <div className="cap-skill-card subagents-builtin-card">
      <div className="cap-skill-card__top">
        <span className="cap-skill-card__head">
          <span className="cap-skill-card__icon">/</span>
          <span className="cap-skill-card__main">
            <span className="cap-skill-card__command">{skill.name}</span>
            <span className="cap-skill-card__badges">
              <span className="cap-skill-badge cap-skill-badge--builtin">{t("caps.skillScopeBuiltin")}</span>
              <Tooltip label={(skill.allowedTools ?? []).join(", ") || t("subagents.allTools")}>
                <span className="cap-skill-badge">{toolsLabel}</span>
              </Tooltip>
            </span>
          </span>
        </span>
      </div>
      <div className="cap-skill-card__desc">{builtinDescription(skill.name, skill.description, t)}</div>
      <SubagentInvocation name={skill.name} onUseInChat={onUseInChat} />
      <div className="subagents-builtin-overrides">
        <div className="subagents-builtin-overrides__field">
          <span className="subagents-builtin-overrides__field-label">{t("subagents.model")}</span>
          <ModelPicker
            s={s}
            refs={allRefs(s)}
            value={toRef(skill.configuredModel ?? "", s)}
            disabled={busy}
            ariaLabel={`${skill.name}: ${t("subagents.model")}`}
            emptyOptionLabel={t("subagents.inheritDefault")}
            emptyOptionHint={t("subagents.effectiveValue", { value: inheritedModel })}
            onPick={onSetModel}
          />
        </div>
        <div className="subagents-builtin-overrides__field">
          <span className="subagents-builtin-overrides__field-label">{t("subagents.effort")}</span>
          <EffortPicker
            value={skill.configuredEffort ?? ""}
            disabled={busy}
            inheritedValue={inheritedEffort}
            ariaLabel={`${skill.name}: ${t("subagents.effort")}`}
            onPick={onSetEffort}
          />
        </div>
        <div className="subagents-builtin-overrides__status">
          {overridden ? (
            <button className="btn btn--small subagents-reset-override" type="button" disabled={busy} onClick={onReset}>
              <span className="subagents-reset-override__state">{t("subagents.overridden")}</span>
              <span aria-hidden="true">·</span>
              <span>{t("subagents.resetOverride")}</span>
            </button>
          ) : (
            <Tooltip label={t("subagents.builtinReadOnlyHint")}>
              <span className="subagents-inherit-badge">{t("subagents.inherited")}</span>
            </Tooltip>
          )}
        </div>
      </div>
    </div>
  );
}

function CustomSubagentRow({
  skill,
  busy,
  onEdit,
  onDelete,
  externallyManaged = false,
  onUseInChat,
}: {
  skill: SkillView;
  busy: boolean;
  onEdit?: () => void;
  onDelete?: () => void;
  externallyManaged?: boolean;
  onUseInChat: (command: string) => void;
}) {
  const t = useT();
  const toolsLabel = toolsSummaryLabel(skill.allowedTools, t);
  const accent = projectColorValue(skill.color);
  return (
    <div className="cap-skill-card">
      <div className="cap-skill-card__top">
        <span className="cap-skill-card__head">
          {accent && <span className="subagents-color-dot" style={{ "--project-accent": accent } as CSSProperties} aria-hidden="true" />}
          <span className="cap-skill-card__main">
            <span className="cap-skill-card__command">/{skill.name}</span>
            <span className="cap-skill-card__badges">
              <span className={`cap-skill-badge cap-skill-badge--${skill.scope}`}>{subagentScopeLabel(skill.scope, t)}</span>
              {skill.model && <span className="cap-skill-badge">{skill.model}</span>}
              <Tooltip label={(skill.allowedTools ?? []).join(", ") || t("subagents.allTools")}>
                <span className="cap-skill-badge">{toolsLabel}</span>
              </Tooltip>
            </span>
          </span>
        </span>
        {externallyManaged ? (
          <Tooltip label={t("subagents.externalManagedHint")}>
            <span className="cap-skill-badge">{t("subagents.externalManaged")}</span>
          </Tooltip>
        ) : (
          <span className="subagents-row-actions">
            <button className="btn btn--small" type="button" disabled={busy} onClick={() => onEdit?.()}>
              {t("common.edit")}
            </button>
            <InlineConfirmButton
              label={t("common.delete")}
              confirmLabel={t("subagents.confirmDelete")}
              cancelLabel={t("common.cancel")}
              disabled={busy}
              danger
              onConfirm={() => onDelete?.()}
            />
          </span>
        )}
      </div>
      <div className="cap-skill-card__desc">{skill.description}</div>
      <SubagentInvocation name={skill.name} onUseInChat={onUseInChat} />
    </div>
  );
}

function ColorSwatchPicker({ value, onChange }: { value: ProjectColorKey; onChange: (key: ProjectColorKey) => void }) {
  const t = useT();
  return (
    <div className="subagents-color-grid" role="group" aria-label={t("subagents.color")}>
      {PROJECT_COLOR_OPTIONS.filter((opt) => opt.key !== "").map((opt) => (
        <button
          key={opt.key}
          type="button"
          className={`subagents-color-swatch${value === opt.key ? " subagents-color-swatch--selected" : ""}`}
          style={{ "--project-accent": opt.value } as CSSProperties}
          aria-pressed={value === opt.key}
          aria-label={opt.key}
          onClick={() => onChange(value === opt.key ? "" : opt.key)}
        />
      ))}
    </div>
  );
}

function ToolMultiSelect({
  tools,
  selected,
  onChange,
}: {
  tools: MCPToolView[];
  selected: Set<string>;
  onChange: (next: Set<string>) => void;
}) {
  const t = useT();
  const selectedToolCount = tools.reduce((count, tool) => count + Number(selected.has(tool.name)), 0);
  const allSelected = tools.length > 0 && selectedToolCount === tools.length;
  const toggle = (name: string, checked: boolean) => {
    const next = new Set(selected);
    if (checked) next.add(name);
    else next.delete(name);
    onChange(next);
  };
  return (
    <div className="subagents-tool-grid" role="group" aria-label={t("subagents.customToolsOption")}>
      <div className="subagents-tool-grid__actions">
        <span>{t("subagents.selectedToolCount", { n: selectedToolCount, total: tools.length })}</span>
        <button type="button" disabled={allSelected} onClick={() => onChange(new Set(tools.map((tool) => tool.name)))}>
          {t("subagents.selectAllTools")}
        </button>
        <button type="button" disabled={selected.size === 0} onClick={() => onChange(new Set())}>
          {t("subagents.clearTools")}
        </button>
      </div>
      {tools.map((tool) => (
        <Tooltip key={tool.name} label={tool.description}>
          <label className="subagents-tool-option">
            <input type="checkbox" checked={selected.has(tool.name)} onChange={(e) => toggle(tool.name, e.target.checked)} />
            <span>{tool.name}</span>
          </label>
        </Tooltip>
      ))}
    </div>
  );
}

export function selectToolsOnFirstCustomUse(
  selected: ReadonlySet<string>,
  tools: MCPToolView[],
  hasUsedCustomMode: boolean,
): Set<string> {
  if (hasUsedCustomMode) return new Set(selected);
  return new Set(tools.map((tool) => tool.name));
}

function SubagentProfileForm({
  s,
  tools,
  existingNames,
  busy,
  editingSkill,
  onCancel,
  onSave,
}: {
  s: SettingsView;
  tools: MCPToolView[];
  existingNames: string[];
  busy: boolean;
  editingSkill?: SkillView;
  onCancel: () => void;
  onSave: (input: SubagentProfileInput) => Promise<unknown>;
}) {
  const t = useT();
  const formRef = useRef<HTMLDivElement>(null);
  const isEditing = Boolean(editingSkill);
  const [name, setName] = useState(editingSkill?.name ?? "");
  const [description, setDescription] = useState(editingSkill?.description ?? "");
  const [color, setColor] = useState<ProjectColorKey>((editingSkill?.color as ProjectColorKey) ?? "");
  const [model, setModel] = useState(editingSkill?.model ?? "");
  const [effort, setEffort] = useState(editingSkill?.effort ?? "");
  const [toolMode, setToolMode] = useState<"all" | "custom">(
    editingSkill?.allowedTools && editingSkill.allowedTools.length > 0 ? "custom" : "all",
  );
  const [selectedTools, setSelectedTools] = useState<Set<string>>(() => new Set(editingSkill?.allowedTools ?? []));
  const hasUsedCustomMode = useRef(Boolean(editingSkill?.allowedTools?.length));
  const [systemPrompt, setSystemPrompt] = useState(editingSkill?.body ?? "");
  const [readOnly, setReadOnly] = useState(Boolean(editingSkill?.readOnly));
  const [scope, setScope] = useState<"global" | "project">(editingSkill?.scope === "project" ? "project" : "global");
  const [tryTask, setTryTask] = useState("");
  const [tryRunning, setTryRunning] = useState(false);
  const [tryResult, setTryResult] = useState<string | null>(null);
  const [tryError, setTryError] = useState<string | null>(null);

  useEffect(() => {
    formRef.current?.scrollIntoView({ block: "start" });
  }, []);

  const trimmedName = name.trim();
  // Editing keeps its own name fixed, so it can never collide with itself.
  const otherNames = isEditing ? existingNames.filter((n) => n !== editingSkill?.name) : existingNames;
  const nameTaken = trimmedName !== "" && otherNames.some((n) => n.toLowerCase() === trimmedName.toLowerCase());
  const nameValid = trimmedName === "" || NAME_PATTERN.test(trimmedName);
  const promptReady = systemPrompt.trim() !== "";
  const toolsReady = toolMode === "all" || selectedTools.size > 0;
  const ready = trimmedName !== "" && nameValid && !nameTaken && description.trim() !== "" && promptReady && toolsReady;

  const currentInput = (): SubagentProfileInput => ({
    name: trimmedName,
    description: description.trim(),
    systemPrompt: systemPrompt.trim(),
    color: color || undefined,
    model,
    effort,
    allowedTools: toolMode === "custom" ? Array.from(selectedTools) : [],
    readOnly,
    scope,
  });

  const submit = () => {
    void onSave(currentInput());
  };

  const runTry = async () => {
    setTryRunning(true);
    setTryError(null);
    setTryResult(null);
    try {
      setTryResult(await app.TrySubagentProfile(currentInput(), tryTask.trim()));
    } catch (e) {
      setTryError(String((e as Error)?.message ?? e));
    } finally {
      setTryRunning(false);
    }
  };

  return (
    <div ref={formRef} className="prov-card prov-card--edit">
      <button className="subagents-form-back" type="button" onClick={onCancel} disabled={busy}>
        <span aria-hidden="true">←</span> {t("subagents.backToList")}
      </button>
      <div className="cap-skills-head__title">{isEditing ? t("subagents.editTitle") : t("subagents.newTitle")}</div>
      <label className="set-label">{t("subagents.name")}</label>
      <input
        className="mem-input"
        placeholder={t("subagents.namePlaceholder")}
        value={name}
        disabled={isEditing}
        onChange={(e) => setName(e.target.value)}
      />
      {trimmedName !== "" && !nameValid && <div className="subagents-field-error">{t("subagents.nameInvalid")}</div>}
      {nameTaken && <div className="subagents-field-error">{t("subagents.nameTaken")}</div>}

      <label className="set-label">{t("subagents.color")}</label>
      <ColorSwatchPicker value={color} onChange={setColor} />

      <label className="set-label">{t("settings.subagentModel")}</label>
      <ModelPicker
        s={s}
        refs={allRefs(s)}
        value={toRef(model, s)}
        disabled={busy}
        emptyOptionLabel={t("settings.subagentModelDefault")}
        emptyOptionHint={t("common.auto")}
        onPick={(ref) => setModel(ref)}
      />

      <label className="set-label">{t("settings.subagentEffort")}</label>
      <select className="mem-select set-grow" value={effort} disabled={busy} onChange={(e) => setEffort(e.target.value)}>
        <option value="">{t("settings.subagentEffortDefault")}</option>
        {EFFORT_PRESETS.map((level) => (
          <option key={level} value={level}>
            {level}
          </option>
        ))}
      </select>

      <label className="set-label">{t("subagents.description")}</label>
      <input
        className="mem-input"
        placeholder={t("subagents.descriptionPlaceholder")}
        value={description}
        onChange={(e) => setDescription(e.target.value)}
      />

      <label className="set-label">{t("subagents.tools")}</label>
      <div className="subagents-tool-scope-row">
        <select
          className="mem-select"
          value={toolMode}
          disabled={busy}
          onChange={(e) => {
            const nextMode = e.target.value === "custom" ? "custom" : "all";
            if (nextMode === "custom") {
              setSelectedTools(selectToolsOnFirstCustomUse(selectedTools, tools, hasUsedCustomMode.current));
              hasUsedCustomMode.current = true;
            }
            setToolMode(nextMode);
          }}
        >
          <option value="all">{t("subagents.allToolsOption")}</option>
          <option value="custom">{t("subagents.customToolsOption")}</option>
        </select>
        <span>{t(toolMode === "all" ? "subagents.allToolsHint" : "subagents.customToolsHint")}</span>
      </div>
      {toolMode === "custom" && <ToolMultiSelect tools={tools} selected={selectedTools} onChange={setSelectedTools} />}
      {toolMode === "custom" && !toolsReady && <div className="subagents-field-error">{t("subagents.selectAtLeastOneTool")}</div>}

      <label className="set-label">{t("subagents.readOnly")}</label>
      <div className="set-seg" role="group" aria-label={t("subagents.readOnly")}>
        <button
          type="button"
          className={`set-seg__btn${!readOnly ? " set-seg__btn--on" : ""}`}
          disabled={busy}
          onClick={() => setReadOnly(false)}
        >
          {t("subagents.readOnlyOff")}
        </button>
        <button
          type="button"
          className={`set-seg__btn${readOnly ? " set-seg__btn--on" : ""}`}
          disabled={busy}
          onClick={() => setReadOnly(true)}
        >
          {t("subagents.readOnlyOn")}
        </button>
      </div>
      <div className="set-hint">{t("subagents.readOnlyHint")}</div>

      <label className="set-label">{t("subagents.systemPrompt")}</label>
      <textarea
        className="mem-textarea"
        rows={6}
        placeholder={t("subagents.systemPromptPlaceholder")}
        value={systemPrompt}
        onChange={(e) => setSystemPrompt(e.target.value)}
      />

      <label className="set-label">{t("subagents.tryIt")}</label>
      <div className="subagents-tryit-row">
        <input
          className="mem-input"
          placeholder={t("subagents.tryItPlaceholder")}
          value={tryTask}
          disabled={tryRunning}
          onChange={(e) => setTryTask(e.target.value)}
        />
        <button
          className="btn btn--small"
          type="button"
          onClick={() => (tryRunning ? void app.CancelTrySubagentProfile() : void runTry())}
          disabled={!tryRunning && (!promptReady || tryTask.trim() === "")}
        >
          {tryRunning ? t("subagents.cancelRun") : t("subagents.run")}
        </button>
      </div>
      {tryError && <div className="banner banner--error">{tryError}</div>}
      {tryResult && <pre className="subagents-tryit-result">{tryResult}</pre>}

      <label className="set-label">{t("subagents.scope")}</label>
      <select
        className="mem-select set-grow"
        value={scope}
        disabled={busy || isEditing}
        onChange={(e) => setScope(e.target.value === "project" ? "project" : "global")}
      >
        <option value="global">{t("caps.skillScopeGlobal")}</option>
        <option value="project">{t("caps.skillScopeProject")}</option>
      </select>

      <div className="subagents-hint">{t("subagents.manualInvocationHint", { name: trimmedName || "…" })}</div>

      <div className="prov-card__actions">
        <button className="btn btn--small" onClick={onCancel} disabled={busy}>
          {t("common.cancel")}
        </button>
        <button className="btn btn--primary btn--small" onClick={submit} disabled={busy || !ready}>
          {t("common.save")}
        </button>
      </div>
    </div>
  );
}
