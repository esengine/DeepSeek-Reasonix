import type { Bindings } from "../env";
import { UserRepo } from "./users";
import { SessionRepo } from "./sessions";
import { EmailTokenRepo } from "./emailTokens";
import { DeviceGrantRepo } from "./deviceGrants";

export interface Repos {
  users: UserRepo;
  sessions: SessionRepo;
  emailTokens: EmailTokenRepo;
  deviceGrants: DeviceGrantRepo;
}

// Builds the repository layer from request bindings. The session pepper is a
// secret; absent (e.g. first local run) it degrades to an empty pepper.
export function repos(env: Bindings): Repos {
  const pepper = env.SESSION_PEPPER ?? "";
  return {
    users: new UserRepo(env.DB),
    sessions: new SessionRepo(env.DB, pepper),
    emailTokens: new EmailTokenRepo(env.DB, pepper),
    deviceGrants: new DeviceGrantRepo(env.DB, pepper),
  };
}

export { UserRepo, SessionRepo, EmailTokenRepo, DeviceGrantRepo };
