import type { Bindings } from "../env";
import { PackageRepo } from "./packages";
import { EventRepo } from "./events";
import { VoteRepo } from "./votes";
import { InstallRepo } from "./installs";

export function repos(env: Bindings): {
  packages: PackageRepo;
  events: EventRepo;
  votes: VoteRepo;
  installs: InstallRepo;
} {
  return {
    packages: new PackageRepo(env.DB),
    events: new EventRepo(env.DB),
    votes: new VoteRepo(env.DB),
    installs: new InstallRepo(env.DB),
  };
}
