import type { Context } from "hono";
import { z } from "zod";
import type { AppEnv } from "../env";
import { ApiError } from "../http/errors";

// A capability slug: lowercase, 1–64 chars of [a-z0-9._-], starting and ending
// with an alphanumeric. Matches the skill-name rules install_source enforces.
const slug = z
  .string()
  .trim()
  .toLowerCase()
  .regex(/^[a-z0-9](?:[a-z0-9._-]*[a-z0-9])?$/, "Use 1–64 chars: letters, digits, '.', '_', '-'.")
  .max(64);

const httpUrl = z.string().trim().url().max(500);

export const PublishSchema = z
  .object({
    kind: z.enum(["skill", "mcp"]),
    name: slug,
    summary: z.string().trim().max(200).default(""),
    description: z.string().trim().max(8000).default(""),
    source: z.string().trim().min(1).max(500),
    installKind: z.enum(["auto", "skill", "mcp"]).default("auto"),
    version: z.string().trim().max(40).default(""),
    homepage: z.union([httpUrl, z.literal("")]).default(""),
    repoUrl: z.union([httpUrl, z.literal("")]).default(""),
    tags: z.array(z.string().trim().min(1).max(30)).max(8).default([]),
    manifest: z.string().max(16000).default(""),
    contentHash: z.string().trim().max(128).default(""),
    riskLevel: z.string().trim().max(20).default(""),
  })
  .strict();

export type PublishInput = z.infer<typeof PublishSchema>;

export const ListQuerySchema = z.object({
  kind: z.enum(["skill", "mcp", "all"]).default("all"),
  q: z.string().trim().max(100).default(""),
  sort: z.enum(["new", "trending", "installs"]).default("new"),
  limit: z.coerce.number().int().min(1).max(100).default(24),
  offset: z.coerce.number().int().min(0).max(10000).default(0),
});

function firstIssue(error: z.ZodError): string {
  const issue = error.issues[0];
  if (!issue) return "Some fields are invalid.";
  const path = issue.path.join(".");
  return path ? `${path}: ${issue.message}` : issue.message;
}

export async function parseBody<S extends z.ZodTypeAny>(c: Context<AppEnv>, schema: S): Promise<z.infer<S>> {
  let raw: unknown;
  try {
    raw = await c.req.json();
  } catch {
    throw new ApiError(400, "invalid_json", "Request body must be valid JSON.");
  }
  const result = schema.safeParse(raw);
  if (!result.success) throw new ApiError(422, "invalid_input", firstIssue(result.error));
  return result.data;
}

export function parseQuery<S extends z.ZodTypeAny>(c: Context<AppEnv>, schema: S): z.infer<S> {
  const params = Object.fromEntries(new URL(c.req.url).searchParams);
  const result = schema.safeParse(params);
  if (!result.success) throw new ApiError(422, "invalid_input", firstIssue(result.error));
  return result.data;
}
