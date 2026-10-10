import type { ModelEntry } from "./port";

// The catalogue the model rows read: two protocols onto one host, plus a second
// vendor carrying the only model that reads images — the two shapes the picker
// has to render correctly. The effort list stays the caller's, so a test can
// hand in its own.
export function mockModels(efforts: string[]): ModelEntry[] {
  return [
    {
      ref: "deepseek/deepseek-v4-pro", provider: "deepseek", model: "deepseek-v4-pro",
      kind: "openai", vendor: "api.deepseek.com", keyEnv: "DEEPSEEK_API_KEY", active: true, efforts, effort: "high", webSearch: true,
      contextWindow: 131072, price: { input: 2, output: 8, currency: "CNY" },
    },
    {
      ref: "deepseek-anthropic/deepseek-v4-pro", provider: "deepseek-anthropic",
      model: "deepseek-v4-pro", kind: "anthropic", vendor: "api.deepseek.com", keyEnv: "DEEPSEEK_API_KEY",
      efforts, effort: "high", contextWindow: 131072,
    },
    {
      ref: "deepseek/deepseek-flash", provider: "deepseek", model: "deepseek-flash",
      kind: "openai", vendor: "api.deepseek.com", keyEnv: "DEEPSEEK_API_KEY", efforts, effort: "high",
      contextWindow: 131072, price: { input: 0.5, output: 2, currency: "CNY" },
    },
    {
      ref: "kimi/kimi-k2-vision", provider: "kimi", model: "kimi-k2-vision",
      kind: "openai", vendor: "api.moonshot.cn", keyEnv: "KIMI_API_KEY", vision: true, contextWindow: 262144,
    },
    {
      ref: "myrelay/gpt-4o", provider: "myrelay", model: "gpt-4o", kind: "openai",
      vendor: "relay.example.com", keyEnv: "MYRELAY_API_KEY", vision: true, contextWindow: 131072,
    },
    {
      ref: "myrelay/claude-sonnet-4", provider: "myrelay", model: "claude-sonnet-4", kind: "openai",
      vendor: "relay.example.com", keyEnv: "MYRELAY_API_KEY", contextWindow: 200000,
    },
    {
      ref: "myrelay-work/gpt-4o", provider: "myrelay-work", model: "gpt-4o", kind: "openai",
      vendor: "relay.example.com", keyEnv: "MYRELAY_WORK_API_KEY", contextWindow: 131072,
    },
  ];
}
