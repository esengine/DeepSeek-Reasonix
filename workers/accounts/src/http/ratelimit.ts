import type { MiddlewareHandler } from "hono";
import type { AppEnv } from "../env";
import { ApiError } from "./errors";

// Per-IP throttle for sensitive auth endpoints. The binding may be absent under
// local `wrangler dev`, in which case the limiter is simply skipped.
export const authRateLimit: MiddlewareHandler<AppEnv> = async (c, next) => {
  const limiter = c.env.AUTH_LIMITER;
  if (limiter) {
    const ip = c.req.header("cf-connecting-ip") ?? "unknown";
    const { success } = await limiter.limit({ key: ip });
    if (!success) {
      throw new ApiError(429, "rate_limited", "Too many attempts. Please wait a minute and try again.");
    }
  }
  await next();
};
