import type { Context } from "hono";
import { HTTPException } from "hono/http-exception";
import type { ContentfulStatusCode } from "hono/utils/http-status";
import type { AppEnv } from "../env";

// A client-safe error: the code/message pair is sent verbatim to the caller, so
// never put internal detail in here. `extra` adds sibling fields to the body
// for callers that act on the code and need a record, such as a pending row.
export class ApiError extends Error {
  constructor(
    public readonly status: ContentfulStatusCode,
    public readonly code: string,
    message: string,
    public readonly extra?: Record<string, unknown>,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

export function errorHandler(err: Error, c: Context<AppEnv>): Response {
  if (err instanceof ApiError) {
    return c.json({ error: { code: err.code, message: err.message }, ...err.extra }, err.status);
  }
  if (err instanceof SyntaxError) {
    return c.json({ error: { code: "invalid_json", message: "Request body must be valid JSON." } }, 400);
  }
  if (err instanceof HTTPException) {
    return err.getResponse();
  }
  console.error("unhandled error:", err);
  return c.json({ error: { code: "internal", message: "Something went wrong." } }, 500);
}

export function notFoundHandler(c: Context<AppEnv>): Response {
  return c.json({ error: { code: "not_found", message: "Not found." } }, 404);
}
