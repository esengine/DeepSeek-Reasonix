export interface RateLimiter {
  limit(opts: { key: string }): Promise<{ success: boolean }>;
}

export interface Env {
  ACCOUNT_ORIGIN: string;
  ALLOWED_ORIGINS: string;
  REMOTE_GATEWAY_TOKEN?: string;
  REMOTE_SESSIONS: DurableObjectNamespace;
  ATTACHMENTS: R2Bucket;
  GATEWAY_LIMITER?: RateLimiter;
}

export type RemoteCapability = "terminal" | "tasks" | "logs" | "files" | "desktop";

export interface AuthenticatedDevice {
  userId: number;
  device: {
    id: string;
    publicKey: string;
    capabilities: RemoteCapability[];
  };
}

export interface ConsumedGrant {
  grant: {
    userId: number;
    targetDeviceId: string;
    scopes: RemoteCapability[];
  };
}

export interface AuthorizedAttachment {
  attachment: {
    objectId: string;
    userId: number;
    targetDeviceId: string;
    maxBytes: number;
    ciphertextBytes: number;
    ciphertextSha256: string;
    expiresAt: string;
  };
}
