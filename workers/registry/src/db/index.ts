import type { Bindings } from "../env";
import { PackageRepo } from "./packages";
import { EventRepo } from "./events";

export function repos(env: Bindings): { packages: PackageRepo; events: EventRepo } {
  return {
    packages: new PackageRepo(env.DB),
    events: new EventRepo(env.DB),
  };
}
