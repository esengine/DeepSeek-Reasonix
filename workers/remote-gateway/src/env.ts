export interface RateLimiter {
  limit(opts: { key: string }): Promise<{ success: boolean }>;
}

export interface Env {
  ACCOUNT_ORIGIN: string;
  REMOTE_GATEWAY_TOKEN?: string;
  REMOTE_SESSIONS: DurableObjectNamespace;
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
