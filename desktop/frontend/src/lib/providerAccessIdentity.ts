import type { ProviderView } from "./types";

export function isOpenCodeGoProviderName(name: string): boolean {
  const base = name.trim().split("--")[0];
  switch (base) {
    case "opencode-go":
    case "opencode-go-anthropic":
    case "opencode-go-responses":
    case "opencode-go-deepseek-anthropic":
    case "opencode-go-deepseek-responses":
      return true;
    default:
      return false;
  }
}

export function canonicalOfficialProviderName(name: string): string {
  switch (name.trim()) {
    case "deepseek-flash":
    case "deepseek-pro":
      return "deepseek";
    default:
      return name.trim();
  }
}

export function providerGroupID(p: ProviderView): string {
  if (p.providerId === "deepseek") return "builtin:deepseek";
  if (p.providerId === "opencode-go" || isOpenCodeGoProviderName(p.name)) return "custom:opencode-go";
  if (p.providerId) return `family:${p.providerId}`;
  if (p.name === "opencode-zen-anthropic") return "custom:opencode-zen";
  return `custom:${p.name}`;
}
