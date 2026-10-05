import { useSyncExternalStore } from "react";
import type { DeviceSelf } from "../port/share";

// Which screen this page is: a paired device, or null for the window and a
// networked serve's browser. The device bar learns it from the kernel once;
// everything that says where an action came from reads it from here.
let self: DeviceSelf | null = null;
const subs = new Set<() => void>();

export function setViewer(next: DeviceSelf | null) {
  if (next?.id === self?.id && next?.ordinal === self?.ordinal) return;
  self = next;
  subs.forEach((fn) => fn());
}

export function useViewer(): DeviceSelf | null {
  return useSyncExternalStore(
    (fn) => {
      subs.add(fn);
      return () => subs.delete(fn);
    },
    () => self,
  );
}
