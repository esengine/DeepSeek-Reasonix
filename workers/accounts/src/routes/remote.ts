import { Hono } from "hono";
import type { AppEnv } from "../env";
import { repos } from "../db";
import { ApiError } from "../http/errors";
import {
  parseBody,
  RemoteAttachmentDownloadSchema,
  RemoteAttachmentUploadSchema,
  RemoteDeviceAuthenticateSchema,
  RemoteGrantConsumeSchema,
} from "../lib/validation";

const remote = new Hono<AppEnv>();

function equalSecret(supplied: string, expected: string): boolean {
  if (supplied.length !== expected.length) return false;
  let difference = 0;
  for (let index = 0; index < expected.length; index += 1) {
    difference |= supplied.charCodeAt(index) ^ expected.charCodeAt(index);
  }
  return difference === 0;
}

remote.use("*", async (c, next) => {
  const expected = c.env.REMOTE_GATEWAY_TOKEN;
  const supplied = c.req.header("x-reasonix-gateway-token");
  if (!expected) throw new ApiError(503, "remote_unavailable", "Remote access is not configured.");
  if (!supplied || !equalSecret(supplied, expected)) {
    throw new ApiError(401, "unauthorized_gateway", "Gateway authentication failed.");
  }
  await next();
});

remote.post("/devices/authenticate", async (c) => {
  const { deviceId, deviceCredential } = await parseBody(c, RemoteDeviceAuthenticateSchema);
  const authenticated = await repos(c.env).remoteDevices.authenticate(deviceId, deviceCredential);
  if (!authenticated) throw new ApiError(401, "invalid_device", "The device credential is invalid or revoked.");
  return c.json(authenticated);
});

remote.post("/grants/consume", async (c) => {
  const { ticket } = await parseBody(c, RemoteGrantConsumeSchema);
  const grant = await repos(c.env).remoteDevices.consumeGrant(ticket);
  if (!grant) throw new ApiError(401, "invalid_grant", "The connection grant is invalid, expired, or already used.");
  return c.json({ grant });
});

remote.post("/attachments/upload", async (c) => {
  const input = await parseBody(c, RemoteAttachmentUploadSchema);
  const attachment = await repos(c.env).remoteAttachments.consumeUpload(input);
  if (!attachment) throw new ApiError(401, "invalid_attachment_grant", "The upload grant is invalid or expired.");
  return c.json({ attachment });
});

remote.post("/attachments/download", async (c) => {
  const { objectId, ticket } = await parseBody(c, RemoteAttachmentDownloadSchema);
  const attachment = await repos(c.env).remoteAttachments.authorizeDownload(objectId, ticket);
  if (!attachment) throw new ApiError(401, "invalid_attachment_grant", "The download grant is invalid or expired.");
  return c.json({ attachment });
});

export default remote;
