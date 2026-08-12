export interface ProviderEndpointConfig {
  kind: string;
  baseUrl: string;
  requestUrl?: string;
  chatUrl?: string;
}

export function trimmedBaseURL(value: string): string {
  return value.trim().replace(/\/+$/, "");
}

export function providerRequestURLFromConfig(
  kind: string,
  baseUrl: string,
  requestUrl: string,
  legacyChatUrl = "",
): string {
  const exactRequestURL = requestUrl.trim();
  if (exactRequestURL) return exactRequestURL;
  if (kind.trim().toLowerCase() === "openai") {
    const legacyOpenAIRequestURL = legacyChatUrl.trim().replace(/\/+$/, "");
    if (legacyOpenAIRequestURL) return legacyOpenAIRequestURL;
  }
  const base = trimmedBaseURL(baseUrl);
  if (!base) return "";
  switch (kind.trim().toLowerCase()) {
    case "anthropic":
      return base.endsWith("/v1") ? `${base}/messages` : `${base}/v1/messages`;
    case "responses":
    case "dashscope-responses":
      return `${base}/responses`;
    default:
      return `${base}/chat/completions`;
  }
}

export function providerBaseURLFromRequestURL(kind: string, requestUrl: string): string {
  const exactRequestURL = requestUrl.trim();
  if (!exactRequestURL) return "";
  const normalizedKind = kind.trim().toLowerCase();
  const suffixes = normalizedKind === "anthropic"
    ? ["/v1/messages"]
    : normalizedKind === "responses" || normalizedKind === "dashscope-responses"
      ? ["/responses"]
      : ["/chat/completions"];
  try {
    const parsed = new URL(exactRequestURL);
    const pathname = parsed.pathname.replace(/\/+$/, "");
    const suffix = suffixes.find((candidate) => pathname.endsWith(candidate));
    parsed.pathname = suffix ? pathname.slice(0, -suffix.length) || "/" : pathname || "/";
    parsed.search = "";
    parsed.hash = "";
    return trimmedBaseURL(parsed.toString());
  } catch {
    const suffix = suffixes.find((candidate) => exactRequestURL.endsWith(candidate));
    if (suffix) return trimmedBaseURL(exactRequestURL.slice(0, -suffix.length));
  }
  return trimmedBaseURL(exactRequestURL);
}

export function providerBaseURLForSave(
  initial: ProviderEndpointConfig | undefined,
  effectiveKind: string,
  effectiveRequestUrl: string,
): string {
  const requestUrl = effectiveRequestUrl.trim();
  if (initial) {
    const initialRequestUrl = providerRequestURLFromConfig(
      initial.kind,
      initial.baseUrl,
      initial.requestUrl ?? "",
      initial.chatUrl ?? "",
    );
    const kindUnchanged = initial.kind.trim().toLowerCase() === effectiveKind.trim().toLowerCase();
    if (kindUnchanged && initialRequestUrl === requestUrl) {
      return initial.baseUrl.trim();
    }
  }
  return providerBaseURLFromRequestURL(effectiveKind, requestUrl);
}
