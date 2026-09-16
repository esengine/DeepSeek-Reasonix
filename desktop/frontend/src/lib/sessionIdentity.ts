import type { SessionRef } from "./sessionRef";

export type SessionIdentityRef = Readonly<SessionRef>;

export type SessionIdentity = Readonly<{
  session?: SessionIdentityRef | null;
  sessionPath?: string;
  sessionGeneration?: number;
}>;

export type SessionHydrationOptions<Item, SurfacePolicy extends string> = SessionIdentity & Readonly<{
  skipHistory?: boolean;
  placeholderItems?: Item[];
  preserveCachedHistory?: boolean;
  freshSnapshot?: boolean;
  sessionRevision?: number;
  sessionDigest?: string;
  cancelHydrateGeneration?: number;
  deferResetUntilHistory?: boolean;
  surfacePolicy?: SurfacePolicy;
  recoveryCurrent?: () => boolean;
}>;

export function sessionIdentityFields(identity: SessionIdentity | undefined): SessionIdentity {
  return {
    session: identity?.session,
    sessionPath: identity?.sessionPath,
    sessionGeneration: identity?.sessionGeneration,
  };
}

function identityBaseKey(identity: SessionIdentity | undefined): string {
  const ref = identity?.session;
  if (ref?.sessionId) return `s\0${ref.hostId || "local"}\0${ref.sessionId}`;
  const path = identity?.sessionPath?.trim() || "";
  return path ? `p\0${path}` : "";
}

function sameGeneration(target: SessionIdentity, current: SessionIdentity | undefined): boolean {
  const targetGeneration = target.sessionGeneration;
  const currentGeneration = current?.sessionGeneration;
  return targetGeneration == null || currentGeneration == null || targetGeneration === currentGeneration;
}

/** Canonical SessionRef first; legacy path is only the compatibility identity. */
export function sameSessionIdentity(
  target: SessionIdentity | undefined,
  current: SessionIdentity | undefined,
): boolean {
  if (!target || !current) return false;
  const key = identityBaseKey(target);
  return !!(key && key === identityBaseKey(current) && sameGeneration(target, current));
}

/** Stable cache/fence key. An empty string means the identity is not yet proven. */
export function sessionIdentityStableKey(identity: SessionIdentity | undefined): string {
  const key = identityBaseKey(identity);
  return key ? `${key}\0${identity?.sessionGeneration ?? 0}` : "";
}

/** True when an in-flight hydrate still addresses the live Session identity. */
export function hydrateIdentityCurrent(
  load: SessionIdentity,
  current: SessionIdentity | undefined,
): boolean {
  if (!identityBaseKey(load)) {
    return sameGeneration(load, current);
  }
  return sameSessionIdentity(load, current);
}
