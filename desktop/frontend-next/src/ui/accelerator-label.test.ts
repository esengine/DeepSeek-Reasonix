import { afterEach, expect, it, vi } from "vitest";

afterEach(() => { vi.unstubAllGlobals(); vi.resetModules(); });

it.each([
  ["Macintosh", "⌘⇧Space", "⌃⌥↑"],
  ["Windows", "Ctrl+Shift+Space", "Ctrl+Alt+↑"],
  ["Linux", "Ctrl+Shift+Space", "Ctrl+Alt+↑"],
])("presents accelerators in %s notation", async (userAgent, primary, explicit) => {
  vi.stubGlobal("navigator", { userAgent });
  vi.resetModules();
  // Platform detection happens at module load, after this test supplies the agent.
  const { acceleratorLabel } = await import("./keys");
  expect(acceleratorLabel("CommandOrControl+Shift+Space")).toBe(primary);
  expect(acceleratorLabel("Control+Alt+Up")).toBe(explicit);
  expect(acceleratorLabel("")).toBe("");
});
