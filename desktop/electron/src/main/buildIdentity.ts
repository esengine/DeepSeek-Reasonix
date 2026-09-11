import { readFileSync } from "node:fs";
import { join } from "node:path";

export interface BuildIdentity {
  version: string;
  channel: string;
  commit: string;
}

// Native version resources are numeric and may discard both the tag prefix
// and prerelease suffix. The package manifest owns the RPC build identity.
export function loadBuildIdentity(packaged: boolean, resourcesPath: string, env: NodeJS.ProcessEnv): BuildIdentity {
  if (!packaged) {
    return { version: "dev", channel: env.REASONIX_CHANNEL || "dev", commit: env.REASONIX_COMMIT || "dev" };
  }
  const value: unknown = JSON.parse(readFileSync(join(resourcesPath, "build.json"), "utf8"));
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("Invalid packaged build identity");
  const info = value as Record<string, unknown>;
  if (info.schemaVersion !== 1) throw new Error("Unsupported packaged build identity schema");
  for (const field of ["version", "channel", "commit"] as const) {
    if (typeof info[field] !== "string" || info[field].trim() === "") throw new Error(`Missing packaged build identity: ${field}`);
  }
  const { version, channel, commit } = info as unknown as BuildIdentity;
  if (!/^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?$/.test(version)) {
    throw new Error("Invalid packaged build version");
  }
  return { version, channel, commit };
}
