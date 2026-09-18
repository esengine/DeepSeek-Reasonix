import type { WireImageIdentity } from "./imageRecoveryTypes";

export interface ImageRecoveryBindings {
  ResolveImageRecovery(id: string, selected: WireImageIdentity[]): Promise<void>;
  ResolveImageRecoveryForTab(tabID: string, id: string, selected: WireImageIdentity[]): Promise<void>;
}

export function makeMockImageRecoveryBindings(): ImageRecoveryBindings {
  return {
    async ResolveImageRecovery(_id, _selected) {},
    async ResolveImageRecoveryForTab(_tabID, _id, _selected) {},
  };
}
