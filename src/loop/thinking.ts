/** True when the model emits reasoning_content and requires it round-tripped on follow-ups. */
export function isThinkingModeModel(model: string): boolean {
  if (model.includes("reasoner")) return true;
  if (model === "deepseek-v4-flash" || model === "deepseek-v4-pro") return true;
  return false;
}

/** Pins extra_body.thinking.type; `undefined` lets third-party endpoints skip the field. */
export function thinkingModeForModel(model: string): "enabled" | "disabled" | undefined {
  if (model === "deepseek-chat") return "disabled";
  if (model.includes("reasoner")) return "enabled";
  if (model === "deepseek-v4-flash" || model === "deepseek-v4-pro") return "enabled";
  return undefined;
}

/** Strip hallucinated tool-call envelopes — `tools: undefined` doesn't always force prose. */
export function stripHallucinatedToolMarkup(s: string): string {
  let out = s;
  // DeepSeek's DSML envelope (full-width "｜" is the form R1 emits in practice).
  out = out.replace(/<｜DSML｜function_calls>[\s\S]*?<\/?｜DSML｜function_calls>/g, "");
  out = out.replace(/<\|DSML\|function_calls>[\s\S]*?<\/?\|DSML\|function_calls>/g, "");
  out = out.replace(/<function_calls>[\s\S]*?<\/function_calls>/g, "");
  // Lone unpaired DSML opener left over after R1 truncates mid-call.
  out = out.replace(/<｜DSML｜[\s\S]*$/g, "");
  return out.trim();
}
