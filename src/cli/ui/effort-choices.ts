import type { ReasoningEffort } from "../../config.js";
import { isDeepSeekHost } from "../../loop/errors.js";

const ALL: readonly ReasoningEffort[] = ["low", "medium", "high", "max"];
const STANDARD: readonly ReasoningEffort[] = ["low", "medium", "high"];

/** `max` is a DeepSeek-only reasoning extension; non-DeepSeek hosts 400 on it (#1794). */
export function effortChoicesForBaseUrl(
  baseUrl: string | undefined | null,
): readonly ReasoningEffort[] {
  return isDeepSeekHost(baseUrl) ? ALL : STANDARD;
}

export function effortArgsHintFor(choices: readonly ReasoningEffort[]): string {
  return `<${choices.join("|")}>`;
}
