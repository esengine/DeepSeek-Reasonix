import type { MiddlewareHandler } from "hono";
import { cors } from "hono/cors";
import type { AppEnv } from "../env";

// CORS with credentials: only origins listed in ALLOWED_ORIGINS get an
// Access-Control-Allow-Origin header, and it always echoes the exact origin
// (never "*", which is incompatible with cookies).
export const corsMiddleware: MiddlewareHandler<AppEnv> = (c, next) => {
  const allowed = (c.env.ALLOWED_ORIGINS ?? "")
    .split(",")
    .map((s) => s.trim())
    .filter(Boolean);
  return cors({
    origin: (origin) => (allowed.includes(origin) ? origin : null),
    credentials: true,
    allowMethods: ["GET", "POST", "DELETE", "OPTIONS"],
    allowHeaders: ["Content-Type", "Authorization"],
    maxAge: 86400,
  })(c, next);
};
