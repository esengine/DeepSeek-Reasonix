import { vi } from "vitest";
export const getCurrentWindow = vi.fn(() => ({
  setTitle: vi.fn(),
  onCloseRequested: vi.fn(),
}));
