import { useEffect, useId, useState } from "react";
import { t } from "../i18n";
import { reason } from "../i18n/kernel";
import type { AgentPort } from "../port/port";
import type { PersonaDraft, PersonaPreset, PersonaSettings } from "../port/boundary";
import { Group } from "./Group";
import { Switch } from "./Switch";

// The kernel refuses a prefix over 32 KiB and a total over 64 KiB; the box
// bounds the field it can see instead of letting a huge paste grow silently.
const LIMIT = 32768;

const BLANK: PersonaDraft = { prefix: "", suffix: "", include_runtime_context: true };

const draftOf = (s: PersonaSettings): PersonaDraft => ({
  prefix: s.prefix,
  suffix: s.suffix,
  include_runtime_context: s.include_runtime_context,
});

const sameDraft = (a: PersonaDraft, b: PersonaDraft) =>
  a.prefix === b.prefix && a.suffix === b.suffix &&
  a.include_runtime_context === b.include_runtime_context;

const isPreset = (p: PersonaPreset, d: PersonaDraft) =>
  sameDraft({ prefix: p.prefix, suffix: p.suffix, include_runtime_context: p.include_runtime_context }, d);

// Presets are served with their prompt text and a stable id; the words the
// page shows about them are this interface's, so they go through t() here.
function presetName(id: string): string {
  switch (id) {
    case "engineer": return t("persona.rigorousEngineer");
    case "concise": return t("persona.concise");
    case "mentor": return t("persona.mentor");
    case "critic": return t("persona.critic");
    default: return id;
  }
}

function presetAbout(id: string): string {
  switch (id) {
    case "engineer": return t("persona.verifyBeforeConcludingMakeThe");
    case "concise": return t("persona.answerBrieflyAndDirectlySkip");
    case "mentor": return t("persona.explainTradeOffsAndReasoning");
    case "critic": return t("persona.reviewAdversariallyNameTheFailure");
    default: return "";
  }
}

export function Persona({ port, onDirtyChange, onBusyChange, onSaved }: { port: AgentPort; onDirtyChange?: (dirty: boolean) => void; onBusyChange?: (busy: boolean) => void; onSaved?: () => void }) {
  const [state, setState] = useState<PersonaSettings | null>(null);
  const [draft, setDraft] = useState<PersonaDraft>(BLANK);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [saved, setSaved] = useState(false);
  const prefixId = useId();
  const suffixId = useId();

  const adopt = (s: PersonaSettings) => {
    setState(s);
    setDraft(draftOf(s));
    setSaved(false);
  };

  useEffect(() => {
    port.persona().then(adopt).catch(() => setState(null));
  }, [port]);

  const change = (patch: Partial<PersonaDraft>) => {
    setDraft((d) => ({ ...d, ...patch }));
    setSaved(false);
    setError("");
  };

  const save = async () => {
    if (!state) return;
    setBusy(true);
    setError("");
    try {
      adopt(await port.savePersonaSettings(draft));
      setSaved(true);
      onSaved?.();
    } catch (e) {
      setError(reason(e));
    } finally {
      setBusy(false);
    }
  };

  const preset = state?.presets.find((p) => isPreset(p, draft)) ?? null;
  const blank = !draft.prefix && !draft.suffix;
  // A draft the presets do not describe is the user's own; saying so is the
  // answer to "which of these am I looking at".
  const now = !state ? undefined : preset ? presetName(preset.id) : blank ? undefined : t("common.custom");
  const changed = !!state && !sameDraft(draft, draftOf(state));
  useEffect(() => { onDirtyChange?.(changed); }, [onDirtyChange, changed]);
  useEffect(() => { onBusyChange?.(busy); }, [onBusyChange, busy]);

  return (
    <Group
      id="persona"
      title={t("common.systemPersona")}
      now={now}
      hint={t("persona.theSystemPersonaShapesThe")}
    >
      {!state ? (
        <div className="empty">{t("persona.couldnTReadThePersona")}</div>
      ) : (
        <div className="box">
          {state.presets.length > 0 && (
            <>
              <div className="seg" data-text role="group" aria-label={t("common.presets")}>
                {state.presets.map((p) => (
                  <button
                    key={p.id}
                    type="button"
                    data-action="persona.preset"
                    data-value={p.id}
                    aria-pressed={preset?.id === p.id}
                    disabled={busy}
                    onClick={() => change({
                      prefix: p.prefix,
                      suffix: p.suffix,
                      include_runtime_context: p.include_runtime_context,
                    })}
                  >
                    {presetName(p.id)}
                  </button>
                ))}
              </div>
              <p className="persona-name">
                {preset
                  ? presetAbout(preset.id)
                  : blank
                    ? t("persona.noPresetAppliedFillIn")
                    : t("persona.thePresetTextHasBeen")}
              </p>
            </>
          )}

          <div className="pers-field">
            <label className="lb" htmlFor={prefixId}>{t("persona.startOfTheSystemPrompt")}</label>
            <span className="ds">{t("persona.leadsTheSystemPromptAhead")}</span>
            <textarea
              id={prefixId}
              dir="auto"
              rows={5}
              maxLength={LIMIT}
              spellCheck={false}
              aria-label={t("persona.startOfTheSystemPrompt")}
              data-action-change="persona.prefix"
              value={draft.prefix}
              disabled={busy}
              onChange={(e) => change({ prefix: e.target.value })}
            />
          </div>

          <div className="pers-field">
            <label className="lb" htmlFor={suffixId}>{t("common.additionalInstructions")}</label>
            <span className="ds" id={`${suffixId}-help`}>{t("common.placedAfterTheBuiltIn")}</span>
            <textarea
              id={suffixId}
              dir="auto"
              rows={5}
              maxLength={LIMIT}
              spellCheck={false}
              aria-label={t("common.additionalInstructions")}
              aria-describedby={`${suffixId}-help`}
              data-action-change="persona.suffix"
              value={draft.suffix}
              disabled={busy}
              onChange={(e) => change({ suffix: e.target.value })}
            />
          </div>

          <div className="lrow">
            <span className="tx">
              <span className="lb">{t("common.doNotInjectOptionalRuntime")}</span>
              <span className="ds">{t("common.whenOnTheProbedEnvironment")}</span>
            </span>
            <Switch
              data-action="persona.runtime-context"
              on={!draft.include_runtime_context}
              busy={busy}
              label={t("common.doNotInjectOptionalRuntime")}
              onClick={() => change({ include_runtime_context: !draft.include_runtime_context })}
            />
          </div>

          <div className="lrow">
            <span className="tx">
              <span className="lb">{t("common.save")}</span>
              <span className="ds">{t("persona.writesTheUserConfigApplies")}</span>
            </span>
            <button className="act" data-action="persona.save" disabled={busy || !changed} onClick={() => void save()}>
              {t(busy ? "common.saving2" : "common.save")}
            </button>
          </div>

          {saved && (
            <div className="find" data-lvl="ok" role="status">
              <span className="t">{t("persona.savedAppliesAtTheNext")}</span>
              <span className="why"><code>{state.path}</code></span>
            </div>
          )}
          {error && (
            <div className="find" data-lvl="err" role="alert">
              <span className="t">{t("common.thatDidNotGoThrough")}</span>
              <span className="why">{error}</span>
            </div>
          )}
        </div>
      )}
    </Group>
  );
}
