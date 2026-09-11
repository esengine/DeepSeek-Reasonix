import type { ProviderCatalog } from "./providerCatalogTypes";

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

// Only rewrite a standard API suffix on an explicit protocol selection. Custom
// paths, queries and fragments are user-owned and must stay byte-for-byte intact.
export function providerRequestURLForFormatChange(previousKind: string, nextKind: string, requestUrl: string): string {
  if (previousKind === nextKind || !requestUrl) return requestUrl;
  try {
    const url = new URL(requestUrl);
    if (url.search || url.hash) return requestUrl;
    const previousSuffix = previousKind === "anthropic" ? "/messages"
      : previousKind === "responses" ? "/responses" : previousKind === "openai" ? "/chat/completions" : "";
    if (!previousSuffix || !url.pathname.endsWith(previousSuffix)) return requestUrl;
    const nextSuffix = nextKind === "anthropic" ? "/messages"
      : nextKind === "responses" ? "/responses" : nextKind === "openai" ? "/chat/completions" : "";
    if (!nextSuffix) return requestUrl;
    url.pathname = url.pathname.slice(0, -previousSuffix.length) + nextSuffix;
    return url.toString();
  } catch { return requestUrl; }
}

function normalizedProtocol(kind: string): string {
  const normalized = kind.trim().toLowerCase();
  return normalized === "dashscope-responses" ? "responses" : normalized;
}

function protocolSuffix(kind: string): string {
  switch (normalizedProtocol(kind)) {
    case "anthropic": return "/messages";
    case "responses": return "/responses";
    case "openai": return "/chat/completions";
    default: return "";
  }
}

export function providerCatalogRequestURL(catalog: ProviderCatalog | undefined, kind: string): string {
  const route = catalog?.protocols?.[normalizedProtocol(kind)];
  return route?.baseUrl ? providerRequestURLFromConfig(kind, route.baseUrl, "") : "";
}

function normalizedExactURL(value: string): string {
  try {
    const parsed = new URL(value.trim());
    if (parsed.search || parsed.hash || parsed.username || parsed.password) return "";
    parsed.pathname = parsed.pathname.replace(/\/+$/, "") || "/";
    return parsed.toString();
  } catch {
    return "";
  }
}

// Switch to a catalog route only when the current URL is one of that catalog's
// registered official routes. Custom gateways and query-bearing routes keep the
// conservative suffix-only behavior.
export function providerRequestURLForCatalogFormatChange(
  previousKind: string,
  nextKind: string,
  requestUrl: string,
  catalog?: ProviderCatalog,
): string {
  const current = normalizedExactURL(requestUrl);
  if (current && catalog?.protocols) {
    const matchesOfficialRoute = Object.entries(catalog.protocols).some(([kind, route]) =>
      normalizedExactURL(providerRequestURLFromConfig(kind, route.baseUrl, "")) === current,
    );
    if (matchesOfficialRoute) {
      const next = providerCatalogRequestURL(catalog, nextKind);
      if (next) return next;
    }
  }
  if (catalog?.protocols) return requestUrl;
  return providerRequestURLForFormatChange(previousKind, nextKind, requestUrl);
}

export interface ProviderEndpointMismatchDetail {
  mismatch: boolean;
  recommendedUrl: string;
}

export function providerEndpointMismatchDetail(
  kind: string,
  address: string,
  catalog?: ProviderCatalog,
): ProviderEndpointMismatchDetail {
  const recommendedUrl = providerCatalogRequestURL(catalog, kind);
  let parsed: URL;
  try { parsed = new URL(address); } catch { return { mismatch: false, recommendedUrl }; }
  if (parsed.search || parsed.hash || parsed.username || parsed.password) return { mismatch: false, recommendedUrl };
  const path = parsed.pathname.replace(/\/+$/, "");
  const expected = protocolSuffix(kind);
  if (!expected) return { mismatch: false, recommendedUrl };
  if (recommendedUrl && normalizedExactURL(address) === normalizedExactURL(recommendedUrl)) return { mismatch: false, recommendedUrl };
  const knownSuffix = ["/messages", "/chat/completions", "/responses"].find(suffix => path.endsWith(suffix));
  if (knownSuffix && !path.endsWith(expected)) return { mismatch: true, recommendedUrl };
  if (!catalog?.protocols || !path.endsWith(expected)) return { mismatch: false, recommendedUrl };

  const selectedRoute = catalog.protocols[normalizedProtocol(kind)];
  if (!selectedRoute) return { mismatch: false, recommendedUrl };
  let selectedBase: URL;
  try { selectedBase = new URL(selectedRoute.baseUrl); } catch { return { mismatch: false, recommendedUrl }; }
  if (selectedBase.host.toLowerCase() !== parsed.host.toLowerCase()) return { mismatch: false, recommendedUrl };
  for (const [otherKind, route] of Object.entries(catalog.protocols)) {
    if (normalizedProtocol(otherKind) === normalizedProtocol(kind)) continue;
    let foreignBase: URL;
    try { foreignBase = new URL(providerRequestURLFromConfig(otherKind, route.baseUrl, "")); } catch { continue; }
    if (foreignBase.host.toLowerCase() !== parsed.host.toLowerCase()) continue;
    const otherSuffix = protocolSuffix(otherKind);
    const foreignRequestPath = foreignBase.pathname.replace(/\/+$/, "");
    const foreignRoot = foreignRequestPath.slice(0, -otherSuffix.length);
    if (!foreignRoot || foreignRoot === "/") continue;
    if (path === `${foreignRoot}${expected}`) {
      return { mismatch: true, recommendedUrl };
    }
  }
  return { mismatch: false, recommendedUrl };
}
