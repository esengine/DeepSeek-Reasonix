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
  auto: "common.auto", disabled: "common.close", none: "composer.no_reasoning", minimal: "composer.minimal", low: "composer.fast", medium: "composer.balanced", high: "composer.deep", xhigh: "composer.extra_deep", max: "composer.maximum",
};

export function effortShort(value?: string, modes?: ModelMode[]): string {
  const id = (value || "auto").toLowerCase();
  const on = modes?.find((m) => m.active);
  return `${t(EFFORT_LABELS[id] ?? id)}${on ? ` · ${modeLabel(on)}` : ""}`;
}

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
  "model_mode.pro": "composer.pro_mode",
  "model_mode.pro.hint": "composer.slower_the_model_does_more",
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
    auto: "composer.effort_desc.auto",
    disabled: "composer.effort_desc.disabled",
    none: "composer.effort_desc.none",
    minimal: "composer.effort_desc.minimal",
    low: "composer.effort_desc.low",
    medium: "composer.effort_desc.medium",
    high: "composer.effort_desc.high",
    xhigh: "composer.effort_desc.xhigh",
    max: "composer.effort_desc.max",
  } as Record<string, string>)[id] ?? value);
  // A model that cannot switch thinking off still reasons at its cheapest
  // level, and still bills for it. That is the rung a saved "off" lands on, so
  // it is the one that would otherwise read as free.
  if (forcedThinking && cheapest !== "" && id === cheapest.toLowerCase()) {
    return `${base} · ${t("composer.thinking_stays_on_and_is")}`;
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
    { value: "__effort-heading", label: t("composer.reasoning_effort"), right: modelLabel, header: true },
    ...(declared ? [] : [{ value: onDeclare, label: t("composer.declare_reasoning_levels"), desc: t("composer.this_endpoint_reported_no_reasoning") }]),
    ...efforts.map((value) => ({
      value,
      label: effortLabel(value),
      meta: effortApi(value),
      badge: value === "auto" ? t("composer.recommended") : undefined,
      recommended: value === "auto",
      strength: value === "disabled" || value === "none" ? 0 : ({ auto: 1, low: 1, medium: 2, high: 3, xhigh: 4, max: 4 } as Record<string, number>)[value.toLowerCase()] ?? 1,
      desc: effortDescription(value, forcedThinking, cheapest),
    })),
    ...modes.map((mode, i) => ({
      value: MODE_ROW + mode.id,
      label: modeLabel(mode),
      desc: MODE_TEXT[mode.hintKey] ? t(MODE_TEXT[mode.hintKey]) : undefined,
      badge: mode.costlier ? t("composer.costs_more") : undefined,
      toggle: mode.active,
      divide: i === 0,
    })),
    ...(declared ? [{ value: "__effort-note", label: t("composer.only_levels_actually_supported_by"), header: true }] : []),
  ];
}
