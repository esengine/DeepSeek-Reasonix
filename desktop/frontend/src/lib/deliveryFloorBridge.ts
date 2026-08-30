import type { QualityFloor, TabMeta } from "./types";

export interface QualityFloorBindings {
  SetQualityFloor(floor: string): Promise<void>;
  SetQualityFloorForTab(tabID: string, floor: string): Promise<void>;
  // Model-free exit from the delivery pause; see desktop/delivery_accept.go.
  AcceptDelivery(): Promise<void>;
  AcceptDeliveryToTab(tabID: string): Promise<void>;
}

export function normalizeQualityFloor(floor: string): QualityFloor {
  return floor === "delivery" ? "delivery" : "standard";
}

// The mock stores the floor on the tab list so the browser shell reflects the
// toggle the way the Wails host does; the host derives it from the session.
export function makeMockQualityFloorBindings(
  tabs: () => TabMeta[],
  setTabs: (next: TabMeta[]) => void,
): QualityFloorBindings {
  const applyToTab = (tabID: string, floor: string) => {
    const next = normalizeQualityFloor(floor);
    setTabs(tabs().map((tab) => (tab.id === tabID ? { ...tab, qualityFloor: next } : tab)));
  };
  return {
    async SetQualityFloor(floor: string) {
      const active = tabs().find((tab) => tab.active);
      if (active) applyToTab(active.id, floor);
    },
    async SetQualityFloorForTab(tabID: string, floor: string) {
      applyToTab(tabID, floor);
    },
    async AcceptDelivery() {},
    async AcceptDeliveryToTab(_tabID: string) {},
  };
}
