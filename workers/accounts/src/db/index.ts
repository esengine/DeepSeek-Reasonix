import type { Bindings } from "../env";
import { UserRepo } from "./users";
import { SessionRepo } from "./sessions";
import { EmailTokenRepo } from "./emailTokens";
import { DeviceGrantRepo } from "./deviceGrants";
import { RemoteDeviceRepo } from "./remoteDevices";
import { RemoteAttachmentRepo } from "./remoteAttachments";
import { RemoteControllerRepo } from "./remoteControllers";
import { RemoteChallengeRepo } from "./remoteChallenges";
import { RateCounterRepo } from "./rateCounters";
import { FeedbackErasureRepo } from "./feedbackErasures";

export interface Repos {
  users: UserRepo;
  sessions: SessionRepo;
  emailTokens: EmailTokenRepo;
  deviceGrants: DeviceGrantRepo;
  remoteDevices: RemoteDeviceRepo;
  remoteAttachments: RemoteAttachmentRepo;
  remoteControllers: RemoteControllerRepo;
  remoteChallenges: RemoteChallengeRepo;
  rateCounters: RateCounterRepo;
  feedbackErasures: FeedbackErasureRepo;
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
    remoteDevices: new RemoteDeviceRepo(env.DB, pepper),
    remoteAttachments: new RemoteAttachmentRepo(env.DB, pepper),
    remoteControllers: new RemoteControllerRepo(env.DB),
    remoteChallenges: new RemoteChallengeRepo(env.DB, pepper),
    rateCounters: new RateCounterRepo(env.DB),
    feedbackErasures: new FeedbackErasureRepo(env.DB),
  };
}

export {
  UserRepo, SessionRepo, EmailTokenRepo, DeviceGrantRepo, RemoteDeviceRepo, RemoteAttachmentRepo,
  RemoteControllerRepo, RemoteChallengeRepo, RateCounterRepo, FeedbackErasureRepo,
};
