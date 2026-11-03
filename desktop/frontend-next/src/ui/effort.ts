import { t } from "../i18n";
import type { ModelEntry, ModelMode } from "../port/port";

// Only the ladder the kernel would accept for the model in hand. A fixed list
// here offered rungs a given model does not have, and picking one looked like
// the control was dead: the request was refused downstream, with nothing on the
// composer to say so. The kernel's list already opens with "auto" — prepending
// another one put the same rung in the menu twice. The fallback is for a model
// that declares nothing at all, and only then.
const EFFORT_FALLBACK = ["auto", "low", "medium", "high", "xhigh", "max"];

export function effortsFor(models: ModelEntry[], ref?: string): string[] {
  const model = models.find((m) => m.ref === ref);
  // A model that is in the list and names no levels has none: the host omits
  // the field exactly when it would refuse every level but auto. Filling that
  // in from the fallback is what put a full ladder in front of a relay whose
  // every rung came back refused. The fallback is for a list not answered yet.
  if (model) return model.efforts ?? [];
  return EFFORT_FALLBACK;
}

/** Whether the model in hand cannot switch thinking off, so even its cheapest
 *  level reasons and is billed. Absent is "nothing said so", never "no". */
export function forcesThinkingFor(models: ModelEntry[], ref?: string): boolean {
  return models.find((m) => m.ref === ref)?.forcesThinking === true;
}

/** The cheapest real level — where a model that cannot switch thinking off
 *  still reasons, and where a saved "off" lands. The ladder opens with auto. */
export function cheapestEffort(efforts: string[]): string {
  return efforts.find((value) => value.toLowerCase() !== "auto") ?? "";
}

// xhigh and max are distinct rungs on the ladders that carry both, so they
// cannot share a name.
const EFFORT_LABELS: Record<string, string> = {
  auto: "common.auto", disabled: "common.close", none: "effort.noReasoning", minimal: "effort.minimal", low: "effort.fast", medium: "effort.balanced", high: "effort.deep", xhigh: "effort.extraDeep", max: "effort.maximum",
};

export function effortReading(value?: string, modes?: ModelMode[]): string {
  const id = (value || "auto").toLowerCase();
  const label = EFFORT_LABELS[id] ?? id;
  const raw = id === "auto" ? "Auto" : id.charAt(0).toUpperCase() + id.slice(1);
  const on = modes?.find((m) => m.active);
  return `${t(label)} · ${raw}${on ? ` · ${modeLabel(on)}` : ""}`;
}

// The kernel names a mode's strings by key; the wording is ours. A key this
// build has no words for shows the mode's id rather than nothing.
const MODE_TEXT: Record<string, string> = {
  "model_mode.pro": "effort.proMode",
  "model_mode.pro.hint": "effort.slowerTheModelDoesMore",
};

export function modeLabel(mode: ModelMode): string {
  return MODE_TEXT[mode.labelKey] ? t(MODE_TEXT[mode.labelKey]) : mode.id;
}

const MODE_ROW = "__mode:";

/** What a row of the effort menu asks for. A mode row flips that mode, so the
 *  answer is the mode to set — "" when it is the one already on. */
export function routeEffortPick(
  value: string,
  modes: ModelMode[] | undefined,
  act: { declare: () => void; effort: (level: string) => void; mode: (mode: string) => void },
) {
  const mode = value.startsWith(MODE_ROW) ? modes?.find((m) => m.id === value.slice(MODE_ROW.length)) : undefined;
  if (mode) act.mode(mode.active ? "" : mode.id);
  else if (value === "__effort-declare") act.declare();
  else if (!value.startsWith(MODE_ROW)) act.effort(value);
}

export function effortLabel(value: string): string {
  const id = value.toLowerCase();
  return t(EFFORT_LABELS[id] ?? id);
}

export function effortApi(value: string): string {
  const id = value.toLowerCase();
  return id === "xhigh" ? "XHigh" : id.charAt(0).toUpperCase() + id.slice(1);
}

export function effortDescription(value: string, forcedThinking = false, cheapest = ""): string {
  const id = value.toLowerCase();
  const base = t(({
    auto: "effort.useTheModelDefaultOr",
    disabled: "effort.doNotSendAnEffort",
    none: "effort.answersWithoutReasoningFastestFor",
    minimal: "effort.lighterThanFastGLM5",
    low: "effort.lightReasoningForRewritingExtraction",
    medium: "effort.balanceSpeedAndReliabilityFor",
    high: "effort.spendMoreTimeAnalyzingComplex",
    xhigh: "effort.moreReasoningThanDeepFor",
    max: "effort.forTheHardestProblemsWith",
  } as Record<string, string>)[id] ?? value);
  // A model that cannot switch thinking off still reasons at its cheapest
  // level, and still bills for it. That is the rung a saved "off" lands on, so
  // it is the one that would otherwise read as free.
  if (forcedThinking && cheapest !== "" && id === cheapest.toLowerCase()) {
    return `${base} · ${t("effort.thinkingStaysOnAndIs")}`;
  }
  return base;
}

/** The rows the effort picker offers. An endpoint that reported no levels has
 *  not said "none" — it has said nothing, so rather than invent a rung the one
 *  row here points at where the capability is declared. */
export function effortMenu(efforts: string[], modelLabel: string, onDeclare: string, modes: ModelMode[] = [], forcedThinking = false) {
  const declared = efforts.length > 0;
  const cheapest = cheapestEffort(efforts);
  return [
    { value: "__effort-heading", label: t("common.reasoningEffort"), right: modelLabel, header: true },
    ...(declared ? [] : [{ value: onDeclare, label: t("effort.declareReasoningLevels"), desc: t("effort.thisEndpointReportedNoReasoning") }]),
    ...efforts.map((value) => ({
      value,
      label: effortLabel(value),
      meta: effortApi(value),
      badge: value === "auto" ? t("common.recommended") : undefined,
      recommended: value === "auto",
      strength: value === "disabled" || value === "none" ? 0 : ({ auto: 1, low: 1, medium: 2, high: 3, xhigh: 4, max: 4 } as Record<string, number>)[value.toLowerCase()] ?? 1,
      desc: effortDescription(value, forcedThinking, cheapest),
    })),
    ...modes.map((mode, i) => ({
      value: MODE_ROW + mode.id,
      label: modeLabel(mode),
      desc: MODE_TEXT[mode.hintKey] ? t(MODE_TEXT[mode.hintKey]) : undefined,
      badge: mode.costlier ? t("effort.costsMore") : undefined,
      toggle: mode.active,
      divide: i === 0,
    })),
    ...(declared ? [{ value: "__effort-note", label: t("effort.onlyLevelsActuallySupportedBy"), right: t("effort.perModel"), header: true }] : []),
  ];
}
