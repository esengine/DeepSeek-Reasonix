import {
  type ReasoningEffort,
  isReasoningEffort,
  saveModel,
  saveReasoningEffort,
} from "@/config.js";
import { t } from "@/i18n/index.js";
import { effortChoicesForBaseUrl } from "../../effort-choices.js";
import type { SlashHandler } from "../dispatch.js";

const model: SlashHandler = (args, loop, ctx) => {
  const id = args[0];
  const known = ctx.models ?? null;
  if (!id) {
    return { openModelPicker: true };
  }
  loop.configure({ model: id });
  ctx.dispatch?.({ type: "session.model.change", model: id });
  try {
    saveModel(id);
  } catch {
    /* disk full / perms — runtime change still took effect */
  }
  if (known && known.length > 0 && !known.includes(id)) {
    return {
      info: t("handlers.model.modelNotInCatalog", { id, list: known.join(", ") }),
    };
  }
  return { info: t("handlers.model.modelSet", { id }) };
};

const effort: SlashHandler = (args, loop) => {
  const choices = effortChoicesForBaseUrl(loop.client.baseUrl);
  const list = choices.join(" | ");
  const usageKey =
    choices.length === 4 ? "handlers.model.effortUsage" : "handlers.model.effortUsageNoMax";
  const raw = (args[0] ?? "").toLowerCase();
  if (raw === "") {
    return {
      info: t("handlers.model.effortStatus", { current: loop.reasoningEffort, list }),
    };
  }
  if (!isReasoningEffort(raw) || !choices.includes(raw as ReasoningEffort)) {
    return { info: t(usageKey, { list }) };
  }
  const next: ReasoningEffort = raw;
  loop.configure({ reasoningEffort: next });
  try {
    saveReasoningEffort(next);
  } catch {
    /* disk full / perms — runtime change still took effect */
  }
  return { info: t("handlers.model.effortSet", { effort: next }) };
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
  effort,
  budget,
};
