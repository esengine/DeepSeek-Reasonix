import { useEffect, useRef, useState } from "react";
import { app } from "../lib/bridge";
import { useCommittedCommand } from "../lib/useCommittedCommand";
import { hasSessionGeneration } from "../lib/sessionIdentity";

export type ExtensionSurfaceView = {
  pluginId: string;
  surfaceId: string;
  sessionId?: string;
  generation?: number;
  formInstanceId: string;
  formInstanceExact: boolean;
};
export type ExtensionNotificationView = { severity?: string; title: string; body?: string };

/**
 * Owns the extension form surface: submitting delivers the structured values
 * to the owning sidecar, cancel reports values{"cancelled": true} over the
 * same channel (a failed cancel still dismisses), and queued notifications
 * drain into toasts from per-tab reducer state the toast context cannot read.
 */
export function useExtensionSurface(input: {
  activeTabId: string | undefined;
  hostId?: string;
  sessionId?: string;
  sessionGeneration?: number;
  form: ExtensionSurfaceView | undefined;
  notifications: readonly ExtensionNotificationView[] | undefined;
  dismissForm(tabId?: string, identity?: Pick<ExtensionSurfaceView, "pluginId" | "surfaceId" | "formInstanceId">): void;
  drainNotifications(): void;
  showToast(message: string, level: "info" | "warn" | "error"): void;
}) {
  const { activeTabId, form, notifications, dismissForm, drainNotifications, showToast } = input;
  const formKey = form ? JSON.stringify([activeTabId ?? "", form.pluginId, form.surfaceId, form.formInstanceId]) : "";
  const visibleFormKeyRef = useRef(formKey);
  visibleFormKeyRef.current = formKey;
  const [busyFormKey, setBusyFormKey] = useState("");
  const extensionFormBusy = Boolean(formKey && busyFormKey === formKey);

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
    if (!pending || !activeTabId || busyFormKey === formKey) return;
    const target = {
      tabId: activeTabId,
      hostId: input.hostId ?? "local",
      sessionId: pending.sessionId ?? input.sessionId ?? "",
      sessionGeneration: input.sessionGeneration ?? 0,
      pluginId: pending.pluginId,
      surfaceId: pending.surfaceId,
      pluginGeneration: pending.generation ?? 0,
      formInstanceId: pending.formInstanceId,
    };
    const identity = { pluginId: pending.pluginId, surfaceId: pending.surfaceId, formInstanceId: pending.formInstanceId };
    const requestKey = formKey;
    setBusyFormKey(requestKey);
    try {
      if (!app.SubmitExtensionFormExact || !pending.formInstanceExact || !target.sessionId || !hasSessionGeneration(input.sessionGeneration) || !target.pluginGeneration || !target.formInstanceId) {
        throw new Error("Exact extension form submission is unavailable; refresh or upgrade Reasonix.");
      }
      await app.SubmitExtensionFormExact(target, values);
      dismissForm(target.tabId, identity);
    } catch (err) {
      if (visibleFormKeyRef.current === requestKey) showToast(err instanceof Error ? err.message : String(err), "error");
    } finally {
      setBusyFormKey((current) => current === requestKey ? "" : current);
    }
  });

  const cancelExtensionForm = useCommittedCommand(async () => {
    const pending = form;
    if (!pending || busyFormKey === formKey) return;
    const target = {
      tabId: activeTabId ?? "",
      hostId: input.hostId ?? "local",
      sessionId: pending.sessionId ?? input.sessionId ?? "",
      sessionGeneration: input.sessionGeneration ?? 0,
      pluginId: pending.pluginId,
      surfaceId: pending.surfaceId,
      pluginGeneration: pending.generation ?? 0,
      formInstanceId: pending.formInstanceId,
    };
    const identity = { pluginId: pending.pluginId, surfaceId: pending.surfaceId, formInstanceId: pending.formInstanceId };
    const requestKey = formKey;
    setBusyFormKey(requestKey);
    try {
      if (app.SubmitExtensionFormExact && pending.formInstanceExact && target.tabId && target.sessionId && hasSessionGeneration(input.sessionGeneration) && target.pluginGeneration && target.formInstanceId) {
        await app.SubmitExtensionFormExact(target, { cancelled: true }).catch(() => {});
      }
      dismissForm(target.tabId, identity);
    } finally {
      setBusyFormKey((current) => current === requestKey ? "" : current);
    }
  });

  return { extensionFormBusy, submitExtensionForm, cancelExtensionForm };
}
