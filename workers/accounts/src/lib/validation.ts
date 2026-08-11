import type { Context } from "hono";
import { z } from "zod";
import type { AppEnv } from "../env";
import { MAX_PASSWORD, MIN_PASSWORD } from "../config";
import { ApiError } from "../http/errors";

const email = z.string().trim().toLowerCase().email().max(254);
const password = z.string().min(MIN_PASSWORD, `Password must be at least ${MIN_PASSWORD} characters.`).max(MAX_PASSWORD);

export const RegisterSchema = z.object({
  email,
  password,
  displayName: z.string().trim().max(80).optional(),
});

export const LoginSchema = z.object({
  email,
  password: z.string().min(1).max(MAX_PASSWORD),
});

export const ForgotSchema = z.object({ email });
export const ResendSchema = z.object({ email });

export const ResetSchema = z.object({
  token: z.string().min(10).max(256),
  password,
});

export const VerifyQuerySchema = z.object({
  token: z.string().min(10).max(256),
});

export const ProfileSchema = z
  .object({
    displayName: z.string().trim().max(80).optional(),
    bio: z.string().trim().max(500).optional(),
    avatarUrl: z.union([z.string().url().max(500), z.literal("")]).optional(),
    handle: z.string().trim().min(3).max(30).optional(),
  })
  .strict();

export const PasswordChangeSchema = z.object({
  currentPassword: z.string().min(1).max(MAX_PASSWORD),
  newPassword: password,
});

const userCode = z.string().trim().min(4).max(20);

export const DevicePollSchema = z.object({ deviceCode: z.string().min(10).max(256) });
export const DeviceApproveSchema = z.object({ userCode });
export const DeviceCodeQuerySchema = z.object({ userCode });

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
