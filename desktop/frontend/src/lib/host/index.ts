/**
 * Pluggable Reasonix hosts.
 * - Wails remains the default product path via bridge.ts (unchanged).
 * - HttpSseHost is for Electron PoC / serve clients.
 */
export type { ReasonixHost, HostCapabilities } from "./types";
export {
  POC_CAPABILITIES,
  WAILS_DESKTOP_CAPABILITIES,
  isCapabilityEnabled,
  unsupportedDesktopFeatures,
} from "./capabilities";
export { SERVE_ROUTES } from "./routes";
export { mapServeEventToWire, parseSseChunk } from "./mapEvent";
export { HttpSseHost, createHttpSseHost } from "./httpSseHost";
export { DEFAULT_DESKTOP_HOST, ALTERNATE_HOSTS, isHttpSseHostMode } from "./wailsDefault";
