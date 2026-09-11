import { useEffect, useState } from "react";
import { app } from "../lib/bridge";
import { useCommittedCommand } from "../lib/useCommittedCommand";

export type ExtensionSurfaceView = { pluginId: string; surfaceId: string };
export type ExtensionNotificationView = { severity?: string; title: string; body?: string };

/**
 * Owns the extension form surface: submitting delivers the structured values
 * to the owning sidecar, cancel reports values{"cancelled": true} over the
 * same channel (a failed cancel still dismisses), and queued notifications
 * drain into toasts from per-tab reducer state the toast context cannot read.
 */
export function useExtensionSurface(input: {
  activeTabId: string | undefined;
  form: ExtensionSurfaceView | undefined;
  notifications: readonly ExtensionNotificationView[] | undefined;
  dismissForm(): void;
  drainNotifications(): void;
  showToast(message: string, level: "info" | "warn" | "error"): void;
}) {
  const { activeTabId, form, notifications, dismissForm, drainNotifications, showToast } = input;
  const [extensionFormBusy, setExtensionFormBusy] = useState(false);

  useEffect(() => {
    const pending = notifications;
    if (!pending || pending.length === 0) return;
    for (const notification of pending) {
      const level = notification.severity === "error" ? "error" : notification.severity === "warn" ? "warn" : "info";
      showToast(notification.body ? `${notification.title} — ${notification.body}` : notification.title, level);
    }
    drainNotifications();
  }, [drainNotifications, notifications, showToast]);

  const submitExtensionForm = useCommittedCommand(async (values: Record<string, unknown>) => {
    const pending = form;
    if (!pending || !activeTabId || extensionFormBusy) return;
    setExtensionFormBusy(true);
    try {
      await app.SubmitExtensionForm(activeTabId, pending.pluginId, pending.surfaceId, values);
      dismissForm();
    } catch (err) {
      showToast(err instanceof Error ? err.message : String(err), "error");
    } finally {
      setExtensionFormBusy(false);
    }
  });

  const cancelExtensionForm = useCommittedCommand(async () => {
    const pending = form;
    if (!pending || extensionFormBusy) return;
    setExtensionFormBusy(true);
    try {
      if (activeTabId) {
        await app.SubmitExtensionForm(activeTabId, pending.pluginId, pending.surfaceId, { cancelled: true }).catch(() => {});
      }
      dismissForm();
    } finally {
      setExtensionFormBusy(false);
    }
  });

  return { extensionFormBusy, submitExtensionForm, cancelExtensionForm };
}
