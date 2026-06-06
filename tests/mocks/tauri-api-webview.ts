import { vi } from "vitest";
export const getCurrentWebview = vi.fn(() => ({
  setZoomScale: vi.fn(),
}));
