import { savePreset, saveReasoningEffort } from "@/config.js";
import { t } from "@/i18n/index.js";
import { PRESETS } from "../../presets.js";
import type { SlashHandler } from "../dispatch.js";

function inferPresetFromModel(id: string): "auto" | "flash" | "pro" | null {
  if (id === "deepseek-v4-pro") return "pro";
  if (id === "deepseek-v4-flash") return "flash";
  return null;
}

const model: SlashHandler = (args, loop, ctx) => {
  const id = args[0];
  const known = ctx.models ?? null;
  if (!id) {
    return { openModelPicker: true };
  }
  // Manual model pick = explicit pin: disable auto-escalate so flash doesn't
  // get bumped, and persist the inferred preset so a relaunch keeps the choice.
  loop.configure({ model: id, autoEscalate: false });
  ctx.dispatch?.({ type: "session.model.change", model: id });
  const inferred = inferPresetFromModel(id);
  ctx.dispatch?.({ type: "session.preset.change", preset: inferred });
  if (inferred) {
    try {
      savePreset(inferred);
    } catch {
      /* disk full / perms — runtime change still took effect */
    }
  }
  if (known && known.length > 0 && !known.includes(id)) {
    return {
      info: t("handlers.model.modelNotInCatalog", { id, list: known.join(", ") }),
    };
  }
  return { info: t("handlers.model.modelSet", { id }) };
};

const preset: SlashHandler = (args, loop, ctx) => {
  const name = (args[0] ?? "").toLowerCase();
  const apply = (
    presetName: "auto" | "flash" | "pro",
    p: (typeof PRESETS)[keyof typeof PRESETS],
  ) => {
    loop.configure({
      model: p.model,
      autoEscalate: p.autoEscalate,
      reasoningEffort: p.reasoningEffort,
    });
    ctx.dispatch?.({ type: "session.model.change", model: p.model });
    ctx.dispatch?.({ type: "session.preset.change", preset: presetName });
    try {
      saveReasoningEffort(p.reasoningEffort);
      savePreset(presetName);
    } catch {
      /* disk full / perms — runtime change still took effect */
    }
  };
  if (name === "auto") {
    apply("auto", PRESETS.auto);
    return { info: t("handlers.model.presetAuto") };
  }
  if (name === "flash") {
    apply("flash", PRESETS.flash);
    return { info: t("handlers.model.presetFlash") };
  }
  if (name === "pro") {
    apply("pro", PRESETS.pro);
    return { info: t("handlers.model.presetPro") };
  }
  if (name === "") {
    return { openModelPicker: true };
  }
  return { info: t("handlers.model.presetUsage") };
};

const ESCALATION_MODEL_ID = "deepseek-v4-pro";

const pro: SlashHandler = (args, loop, ctx) => {
  const arg = (args[0] ?? "").toLowerCase();
  if (arg === "off" || arg === "cancel" || arg === "disarm") {
    if (!loop.proArmed) {
      return { info: t("handlers.model.proNothingArmed") };
    }
    if (ctx.disarmPro) ctx.disarmPro();
    else loop.disarmPro();
    return { info: t("handlers.model.proDisarmed") };
  }
  if (arg && arg !== "on" && arg !== "arm") {
    return { info: t("handlers.model.proUsage") };
  }
  if (ctx.armPro) ctx.armPro();
  else loop.armProForNextTurn();
  return {
    info: t("handlers.model.proArmed", { model: ESCALATION_MODEL_ID }),
  };
};

const budget: SlashHandler = (args, loop) => {
  const arg = args[0]?.trim() ?? "";
  if (arg === "") {
    if (loop.budgetUsd === null) {
      return { info: t("handlers.model.budgetNoCap") };
    }
    const spent = loop.stats.totalCost;
    const pct = (spent / loop.budgetUsd) * 100;
    return {
      info: t("handlers.model.budgetStatus", {
        spent: spent.toFixed(4),
        cap: loop.budgetUsd.toFixed(2),
        pct: pct.toFixed(1),
      }),
    };
  }
  if (arg === "off" || arg === "none" || arg === "0") {
    loop.setBudget(null);
    return { info: t("handlers.model.budgetOff") };
  }
  const cleaned = arg.replace(/^\$/, "");
  const usd = Number(cleaned);
  if (!Number.isFinite(usd) || usd <= 0) {
    return { info: t("handlers.model.budgetUsage", { arg }) };
  }
  loop.setBudget(usd);
  const spent = loop.stats.totalCost;
  if (spent >= usd) {
    return {
      info: t("handlers.model.budgetExhausted", {
        cap: usd.toFixed(2),
        spent: spent.toFixed(4),
      }),
    };
  }
  return {
    info: t("handlers.model.budgetSet", {
      cap: usd.toFixed(2),
      spent: spent.toFixed(4),
    }),
  };
};

export const handlers: Record<string, SlashHandler> = {
  model,
  preset,
  pro,
  budget,
};
