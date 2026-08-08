/**
 * Documents that the default desktop product path remains Wails (`bridge.ts`).
 * HttpSseHost is opt-in for Electron PoC / serve clients and does not replace
 * window.go.main.App bindings.
 */
export const DEFAULT_DESKTOP_HOST = "wails" as const;
export const ALTERNATE_HOSTS = ["http-sse"] as const;

/** True when Vite/build injects HTTP host mode (Electron PoC). */
export function isHttpSseHostMode(): boolean {
  try {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const env = (import.meta as any)?.env;
    return env?.VITE_REASONIX_HOST === "http-sse";
  } catch {
    return false;
  }
}
