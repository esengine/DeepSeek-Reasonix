import type { Context } from "hono";
import { z } from "zod";
import type { AppEnv } from "../env";
import { CONFIG_BACKUP_MAX_BYTES, MAX_PASSWORD, MIN_PASSWORD } from "../config";
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

const remoteCapability = z.enum(["terminal", "tasks", "logs", "files", "desktop"]);
const remoteDeviceId = z.string().regex(/^[0-9a-f]{64}$/);

export const RemoteDeviceRegisterSchema = z.object({
  name: z.string().trim().min(1).max(64),
  platform: z.enum(["macos", "windows", "linux"]),
  publicKey: z.string().regex(/^[A-Za-z0-9_-]{43}$/),
  capabilities: z.array(remoteCapability).min(1).max(5).transform((items) => [...new Set(items)]),
}).strict();

export const RemoteDeviceRenameSchema = z.object({
  name: z.string().trim().min(1).max(64),
}).strict();

export const RemoteGrantIssueSchema = z.object({
  targetDeviceId: remoteDeviceId,
  scopes: z.array(remoteCapability).min(1).max(5).transform((items) => [...new Set(items)]),
}).strict();

export const RemoteAttachmentIssueSchema = z.object({
  targetDeviceId: remoteDeviceId,
  ciphertextBytes: z.number().int().positive().max(20 * 1024 * 1024),
}).strict();

export const RemoteDeviceAuthenticateSchema = z.object({
  deviceId: remoteDeviceId,
  deviceCredential: z.string().regex(/^[0-9a-f]{64}$/),
}).strict();

export const RemoteGrantConsumeSchema = z.object({
  ticket: z.string().regex(/^[0-9a-f]{64}$/),
}).strict();

const base64urlCoordinate = z.string().regex(/^[A-Za-z0-9_-]{43}$/);
const controllerId = z.string().regex(/^rc_[A-Za-z0-9_-]{43}$/);

// Exactly the public members of an RFC 7638 canonical P-256 key. A private
// member, ext, key_ops or any extra member is refused rather than ignored.
export const ControllerJwkSchema = z.object({
  kty: z.literal("EC"),
  crv: z.literal("P-256"),
  x: base64urlCoordinate,
  y: base64urlCoordinate,
}).strict();

export const ControllerChallengeSchema = z.object({
  host: remoteDeviceId,
  purpose: z.enum(["enroll"]).default("enroll"),
}).strict();

export const ControllerEnrollSchema = z.object({
  host: remoteDeviceId,
  nonce: z.string().regex(/^[0-9a-f]{64}$/),
  publicKey: ControllerJwkSchema,
  signature: z.string().regex(/^[A-Za-z0-9_-]{86}$/),
  uaClass: z.string().max(32).default("other"),
  claimsId: controllerId.optional(),
  controllerId: controllerId.optional(),
}).strict();

export const ControllerResolveSchema = z.object({
  action: z.enum(["add", "replace", "reject"]),
  replaceId: controllerId.optional(),
}).strict().refine((body) => body.action !== "replace" || body.replaceId !== undefined, {
  message: "replaceId: required for replace",
  path: ["replaceId"],
});

export const ControllerIdParam = controllerId;
export const ControllerListQuerySchema = z.object({ host: remoteDeviceId }).strict();
export const HostControllerListQuerySchema = z.object({
  state: z.enum(["pending", "active", "revoked", "all"]).default("pending"),
}).strict();

export const RemoteLeaseCheckSchema = z.object({
  leases: z.array(z.object({
    userId: z.number().int().positive(),
    deviceId: remoteDeviceId,
    sessionId: z.string().regex(/^[0-9a-f]{64}$/).optional(),
  }).strict()).min(1).max(16),
}).strict();

export const RemoteAttachmentUploadSchema = z.object({
  objectId: z.string().regex(/^[0-9a-f]{64}$/),
  ticket: z.string().regex(/^[0-9a-f]{64}$/),
  ciphertextBytes: z.number().int().positive().max(20 * 1024 * 1024),
  ciphertextSha256: z.string().regex(/^[0-9a-f]{64}$/),
}).strict();

export const RemoteAttachmentDownloadSchema = z.object({
  objectId: z.string().regex(/^[0-9a-f]{64}$/),
  ticket: z.string().regex(/^[0-9a-f]{64}$/),
}).strict();

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

// The envelope is base64 so the body stays JSON like every other route; the
// decoded size is checked again after decoding.
export const ConfigBackupUploadSchema = z.object({
  label: z.string().trim().max(80),
  format: z.number().int().positive().max(1000),
  appVersion: z.string().trim().max(64),
  platform: z.string().trim().max(64),
  categories: z.array(z.enum(["settings", "extensions", "memory", "automation", "secrets"])).min(1).max(5),
  envelope: z.string().min(8).max(Math.ceil((CONFIG_BACKUP_MAX_BYTES * 4) / 3) + 4),
}).strict();

export const ConfigBackupIdSchema = z.string().regex(/^[0-9a-f]{64}$/);
