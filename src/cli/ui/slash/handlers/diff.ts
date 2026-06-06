import { type DiffDisplay, loadDiffDisplay, saveDiffDisplay } from "@/config.js";
import { t } from "../../../../i18n/index.js";
import type { SlashHandler } from "../dispatch.js";

const DIFF_MODES: readonly DiffDisplay[] = ["summary", "full", "none"];

const diff: SlashHandler = (args, _loop, ctx) => {
  const mode = (args[0] ?? "").toLowerCase();
  if (!mode) {
    const current = loadDiffDisplay(ctx.configPath);
    return { info: t("handlers.diff.diffStatus", { current }) };
  }
  if (!DIFF_MODES.includes(mode as DiffDisplay)) {
    return { info: t("handlers.diff.diffInvalid", { mode, choices: DIFF_MODES.join(", ") }) };
  }
  saveDiffDisplay(mode as DiffDisplay, ctx.configPath);
  return { info: t("handlers.diff.diffSet", { mode }) };
};

export const handlers: Record<string, SlashHandler> = {
  diff,
};
