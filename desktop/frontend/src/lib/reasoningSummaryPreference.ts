// Compatibility re-export for extensions and older tests. The desktop UI now
// owns one reasoning-display preference instead of a separate summary toggle.
export {
  getReasoningSummaryEnabled,
  setReasoningSummaryEnabled,
  useReasoningSummaryEnabled,
} from "./reasoningDisplayPreference";
