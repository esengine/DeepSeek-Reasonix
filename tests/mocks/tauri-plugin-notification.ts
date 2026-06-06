import { vi } from "vitest";

export const isPermissionGranted = vi.fn(async () => false);
export const requestPermission = vi.fn(async () => "denied" as const);
export const sendNotification = vi.fn();
