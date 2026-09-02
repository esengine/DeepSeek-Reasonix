import type { ContextInfo, ContextPanelInfo, WireUsage } from "./types";

// contextSessionCache picks the session-cumulative cache hit/miss pair for the
// panel's session average. The shared ContextInfo is refreshed after every
// usage event and also drives StatusBar, so prefer it over the panel's
// independently throttled snapshot. Panel telemetry remains the all-sources
// fallback for callers without live context; executor-only wire counters only
// bridge the pre-refresh gap. The pair always comes from one source so the
// computed rate never mixes scopes.
export function contextSessionCache(
  info?: Pick<ContextPanelInfo, "sessionCacheHitTokens" | "sessionCacheMissTokens"> | null,
  context?: Pick<ContextInfo, "cacheHitTokens" | "cacheMissTokens">,
  usage?: Pick<WireUsage, "sessionCacheHitTokens" | "sessionCacheMissTokens">,
): { hit: number; miss: number } {
  const ctxHit = context?.cacheHitTokens ?? 0;
  const ctxMiss = context?.cacheMissTokens ?? 0;
  if (ctxHit + ctxMiss > 0) return { hit: ctxHit, miss: ctxMiss };
  const infoHit = info?.sessionCacheHitTokens ?? 0;
  const infoMiss = info?.sessionCacheMissTokens ?? 0;
  if (infoHit + infoMiss > 0) return { hit: infoHit, miss: infoMiss };
  return { hit: usage?.sessionCacheHitTokens ?? 0, miss: usage?.sessionCacheMissTokens ?? 0 };
}
