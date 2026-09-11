import { useEffect } from "react";
import { desktopHost } from "../lib/desktopHost";
import { useAppNavigationStore } from "../store/appNavigation";

export function useNativeSettingsEvent(input: {
  closeTransientOverlays: () => void;
  setSettingsTarget: (target: ReturnType<typeof useAppNavigationStore.getState>["lastSettingsTarget"]) => void;
}) {
  const { closeTransientOverlays, setSettingsTarget } = input;
  useEffect(() => {
    const host = desktopHost();
    if (host.kind === "none") return;
    return host.events.on("app:open-settings", () => {
      closeTransientOverlays();
      setSettingsTarget(useAppNavigationStore.getState().lastSettingsTarget);
    });
  }, [closeTransientOverlays, setSettingsTarget]);
}
