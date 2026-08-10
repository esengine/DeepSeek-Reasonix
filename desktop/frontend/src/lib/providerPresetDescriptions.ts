import type { DictKey } from "./i18n";

export const opencodeGoPresetDescriptionKeys = {
  "opencode-go": "settings.addProvider.preset.opencodeGoDesc",
  "opencode-go-anthropic": "settings.addProvider.preset.opencodeGoAnthropicDesc",
  "opencode-go-deepseek-anthropic": "settings.addProvider.preset.opencodeGoDeepSeekAnthropicDesc",
  "opencode-go-deepseek-responses": "settings.addProvider.preset.opencodeGoDeepSeekResponsesDesc",
} as const satisfies Record<string, DictKey>;
