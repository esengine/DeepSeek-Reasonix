import { useEffect, useId, useState } from "react";
import { t } from "../i18n";
import type {
  AgentComposition,
  AgentPresetCatalog,
  CompositionChoice,
  CompositionKind,
  CompositionSelection,
  CompositionModule,
} from "../port/composition";
import { COMPOSITION_KINDS } from "../port/composition";
import { Switch } from "./Switch";
import { StudioIcon } from "./StudioIcon";
import { CompositionModulePicker } from "./CompositionModulePicker";

const labels: Record<CompositionKind, string> = {
  tools: "common.tool",
  skills: "common.skills2",
  mcp: "common.mCPServers",
  extensions: "common.extension",
  hooks: "compositionEditor.automationHooks",
  commands: "compositionEditor.slashCommands",
};

export function CompositionEditor({
  draft,
  modules,
  disabled,
  creating = false,
  onChange,
  onValidityChange,
}: {
  draft: AgentComposition;
  modules: AgentPresetCatalog["modules"];
  disabled: boolean;
  creating?: boolean;
  onChange: (d: AgentComposition) => void;
  onValidityChange: (valid: boolean) => void;
}) {
  const [valid, setValid] = useState<Record<string, boolean>>({});
  const helpId = useId();
  useEffect(() => onValidityChange(Object.values(valid).every(Boolean)), [valid, onValidityChange]);
  const persona = draft.persona;
  const updatePersona = (patch: Partial<AgentComposition["persona"]>) =>
    onChange({ ...draft, persona: { ...persona, ...patch } });
  return (
    <div className="composition-editor">
      <label className="pers-field">
        <span className="lb">{t("compositionEditor.presetName")}</span>
        <input
          className="in"
          aria-label={t("compositionEditor.presetName")}
          data-action-change="agent-preset.name"
          disabled={disabled}
          value={draft.id.startsWith("builtin:") ? t(draft.name) : draft.name}
          onChange={(e) => onChange({ ...draft, name: e.target.value })}
        />
      </label>
      <label className="pers-field">
        <span className="lb">{t("compositionEditor.purpose")}</span>
        <input
          className="in"
          data-action-change="agent-preset.description"
          disabled={disabled}
          value={draft.id.startsWith("builtin:") ? t(draft.description) : draft.description}
          onChange={(e) => onChange({ ...draft, description: e.target.value })}
        />
      </label>
      <label className="pers-field">
        <span className="lb">{t("compositionEditor.personaAndInstructions")}</span>
        <span className="ds" id={`${helpId}-persona`}>{t("compositionEditor.describeTheRoleApproachAnd")}</span>
        <textarea
          aria-label={t("compositionEditor.personaAndInstructions")}
          aria-describedby={`${helpId}-persona`}
          data-action-change="persona.prefix"
          rows={5}
          maxLength={32768}
          disabled={disabled}
          value={persona.prefix}
          onChange={(e) => updatePersona({ prefix: e.target.value })}
        />
      </label>
      <label className="pers-field">
        <span className="lb">{t("common.additionalInstructions")}</span>
        <span className="ds" id={`${helpId}-suffix`}>{t("common.placedAfterTheBuiltIn")}</span>
        <textarea
          aria-label={t("common.additionalInstructions")}
          aria-describedby={`${helpId}-suffix`}
          data-action-change="persona.suffix"
          rows={5}
          maxLength={32768}
          disabled={disabled}
          value={persona.suffix}
          onChange={(e) => updatePersona({ suffix: e.target.value })}
        />
      </label>
      <div className="lrow">
        <span className="tx">
          <span className="lb">{t("common.doNotInjectOptionalRuntime")}</span>
          <span className="ds">{t("common.whenOnTheProbedEnvironment")}</span>
        </span>
        <Switch
          data-action="persona.runtime-context"
          label={t("common.doNotInjectOptionalRuntime")}
          on={!persona.include_runtime_context}
          busy={disabled}
          onClick={() => updatePersona({ include_runtime_context: !persona.include_runtime_context })}
        />
      </div>
      <div className="composition-capabilities">
        <h3>{t("compositionEditor.capabilities")}</h3>
        <p className="ds">{t("compositionEditor.inheritCurrentToolsSkillsAnd")}</p>
        <details>
          <summary><StudioIcon name="settings" />{t("common.tool")}<span>{t(draft.tools.mode === "inherit" ? "compositionEditor.inheritCurrentConfiguration" : "compositionEditor.selectedModulesOnly")}</span><StudioIcon name="chevron" /></summary>
          <ModuleEditor kind="tools" choices={modules?.tools ?? []} value={draft.tools} disabled={disabled}
            onValidityChange={(ok) => setValid(prev => ({ ...prev, tools: ok }))}
            onChange={(value) => onChange({ ...draft, tools: value })} />
        </details>
        <details>
          <summary><StudioIcon name="layers" />{t("compositionEditor.skillsAndExtensions")}<span>{t(COMPOSITION_KINDS.filter(k => k !== "tools").every(k => draft[k].mode === "inherit") ? "compositionEditor.inheritCurrentConfiguration" : "common.custom")}</span><StudioIcon name="chevron" /></summary>
          {COMPOSITION_KINDS.filter(kind => kind !== "tools").map((kind) => (
            <ModuleEditor key={kind} kind={kind} choices={modules?.[kind] ?? []} value={draft[kind]} disabled={disabled}
              onValidityChange={(ok) => setValid(prev => ({ ...prev, [kind]: ok }))}
              onChange={(value) => onChange({ ...draft, [kind]: value })} />
          ))}
        </details>
      </div>
      <label className="pers-field">
        <span className="lb">{t("compositionEditor.storageScope")}</span>
        <select aria-label={t("compositionEditor.storageScope")} aria-describedby={`${helpId}-scope`} disabled={disabled || !creating} data-action-change="agent-preset.scope"
          value={draft.id.split(":")[0]} onChange={(e) => onChange({ ...draft, id: `${e.target.value}:${draft.id.split(":")[1]}` })}>
          {draft.id.startsWith("builtin:") && <option value="builtin">{t("common.builtIn")}</option>}
          <option value="user">{t("common.personal")}</option><option value="project">{t("common.sharedWithProject")}</option>
        </select>
        <span className="ds" id={`${helpId}-scope`}>{t("compositionEditor.personalSavedOnThisMachine")}</span>
      </label>
      <label className="pers-field">
        <span className="lb">{t("compositionEditor.presetID")}</span>
        <input className="in" aria-label={t("compositionEditor.presetID")} aria-describedby={`${helpId}-id`} disabled={disabled || !creating} data-action-change="agent-preset.id"
          value={draft.id.split(":")[1]} onChange={(e) => onChange({ ...draft, id: `${draft.id.split(":")[0]}:${e.target.value}` })} />
        <span className="ds" id={`${helpId}-id`}>{t("compositionEditor.generatedAutomaticallyToSaveAnd")}</span>
      </label>
    </div>
  );
}

function ModuleEditor({
  kind,
  choices,
  value,
  disabled,
  onChange,
  onValidityChange,
}: {
  kind: CompositionKind;
  choices: CompositionChoice[];
  value: CompositionSelection;
  disabled: boolean;
  onChange: (value: CompositionSelection) => void;
  onValidityChange: (valid: boolean) => void;
}) {
  const [jsonErrors, setJsonErrors] = useState<Record<number, string>>({});
  const [rawParameters, setRawParameters] = useState<Record<number, string>>({});
  const validate = (errors: Record<number, string>) => {
    setJsonErrors(errors);
    onValidityChange(Object.values(errors).every((v) => !v));
  };
  const changeMembers = (members: CompositionModule[]) => {
    const remap = <T,>(source: Record<number, T>): Record<number, T> =>
      Object.fromEntries(
        members.flatMap((member, index) => {
          const old = value.members.indexOf(member);
          return old in source ? [[index, source[old]]] : [];
        }),
      );
    validate(remap(jsonErrors));
    setRawParameters(remap(rawParameters));
    onChange({ ...value, members });
  };
  return (
    <fieldset className="composition-modules" disabled={disabled}>
      <legend>{t(labels[kind])}</legend>
      {kind === "hooks" && (
        <p className="ds">{t("compositionEditor.safetyHooksAndPermissionRules")}</p>
      )}
      <div className="composition-module-controls">
      <select
        aria-label={`${t(labels[kind])} ${t("compositionEditor.selectionMode")}`}
        data-action-change="agent-preset.module-mode"
        data-target={kind}
        value={value.mode}
        onChange={(e) => {
          validate({});
          setRawParameters({});
          onChange({ mode: e.target.value as "inherit" | "select", members: [] });
        }}
      >
        <option value="inherit">{t("compositionEditor.followCurrentConfigurationIncludingR")}</option>
        <option value="select">{t("compositionEditor.useSelectedModulesOnlyEmpty")}</option>
      </select>
      {value.mode === "select" && <CompositionModulePicker kind={kind} label={t(labels[kind])} choices={choices} members={value.members} disabled={disabled} onChange={changeMembers} />}
      </div>
      {value.mode === "select" && (
        <>
          {value.members.map((module, index) => (
            <div className="composition-module" key={index}>
              <div className="composition-toolbar">
                <input
                  className="in"
                  aria-label={`${t(labels[kind])} ID ${index + 1}`}
                  data-action-change="agent-preset.module-id"
                  data-target={kind}
                  value={module.id}
                  onChange={(e) =>
                    onChange({
                      ...value,
                      members: value.members.map((m, i) => (i === index ? { ...m, id: e.target.value } : m)),
                    })
                  }
                />
                <button
                  type="button"
                  className="act"
                  data-action="agent-preset.module-remove"
                  data-target={kind}
                  onClick={() => changeMembers(value.members.filter((_, i) => i !== index))}
                >
                  {t("common.remove")}
                </button>
              </div>
              {(kind === "mcp" || kind === "extensions") && (
                <label className="pers-field">
                  <span className="ds">{t("compositionEditor.perCompositionParametersJSONUse")}</span>
                  <textarea
                    aria-label={`${t(labels[kind])} ${t("compositionEditor.parameters")} ${index + 1}`}
                    data-action-change="agent-preset.module-parameters"
                    data-target={kind}
                    rows={3}
                    value={rawParameters[index] ?? JSON.stringify(module.parameters ?? {}, null, 2)}
                    onChange={(e) => {
                      setRawParameters({ ...rawParameters, [index]: e.target.value });
                      try {
                        const parsed: unknown = JSON.parse(e.target.value);
                        if (!parsed || typeof parsed !== "object" || Array.isArray(parsed))
                          throw new Error("object required");
                        validate({ ...jsonErrors, [index]: "" });
                        onChange({
                          ...value,
                          members: value.members.map((m, i) =>
                            i === index ? { ...m, parameters: parsed as Record<string, unknown> } : m,
                          ),
                        });
                      } catch {
                        validate({ ...jsonErrors, [index]: t("compositionEditor.parametersMustBeAValid") });
                      }
                    }}
                  />
                  {jsonErrors[index] && <span role="alert">{jsonErrors[index]}</span>}
                </label>
              )}
            </div>
          ))}
          <details className="composition-manual">
          <summary>{t("compositionEditor.advancedEnterAModuleID")}</summary>
          <p className="ds">{t("compositionEditor.useOnlyForModulesNot")}</p>
          <button
            type="button"
            className="act"
            data-action="agent-preset.module-add"
            data-target={kind}
            onClick={() => onChange({ ...value, members: [...value.members, { id: "" }] })}
          >
            {t("compositionEditor.addBlankID")}
          </button>
          </details>
        </>
      )}
    </fieldset>
  );
}
