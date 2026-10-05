export interface RateLimiter {
  limit(opts: { key: string }): Promise<{ success: boolean }>;
}

export interface Bindings {
  DB: D1Database;
  POST_LIMITER?: RateLimiter;
  APP_ORIGIN: string;
  ALLOWED_ORIGINS: string;
  ID_ORIGIN: string;
  ADMIN_EMAILS?: string;
}

// A signed-in forum member, resolved from the shared identity + local state.
export interface Member {
  email: string;
  handle: string;
  emailVerified: boolean;
  trust: number; // 0 new · 1 basic · 2 member · 3 regular · 4 leader
  role: "member" | "moderator" | "admin";
  silencedUntil: string | null;
}

export type AppEnv = { Bindings: Bindings; Variables: { member: Member | null } };
