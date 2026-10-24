import type { UIAction } from "./actions";

export const ISOLATION_ACTIONS: UIAction[] = [
  { id: "isolation.refresh", kind: "repeatable", target: "none", proof: "interaction" },
  { id: "isolation.root-recheck", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "isolation.root-retry", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "isolation.root-recover", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "isolation.root-cancel", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "isolation.recover", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "isolation.recover-result", kind: "kernel-mutation", target: "entity", proof: "authority-effect" },
  { id: "isolation.discard-preview", kind: "view", target: "entity", proof: "interaction" },
  { id: "isolation.discard-confirm", kind: "kernel-mutation", target: "none", proof: "authority-effect" },
  { id: "isolation.discard-cancel", kind: "view", target: "none", proof: "interaction" },
];
