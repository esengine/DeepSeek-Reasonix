export type Platform = "ios" | "android" | "web";

/** Detect host platform for chrome adaptation (Capacitor-aware, UA fallback). */
export function detectPlatform(): Platform {
  const cap = (window as unknown as { Capacitor?: { getPlatform?: () => string } })
    .Capacitor;
  const fromCap = cap?.getPlatform?.();
  if (fromCap === "ios") return "ios";
  if (fromCap === "android") return "android";

  const ua = navigator.userAgent || "";
  if (/iPhone|iPad|iPod/i.test(ua)) return "ios";
  if (/Android/i.test(ua)) return "android";
  // Desktop browsers: prefer iOS chrome for demos on Apple hardware, else Android-ish density.
  if (/Mac/i.test(navigator.platform || "")) return "ios";
  return "web";
}

export function applyPlatform(platform: Platform = detectPlatform()): void {
  document.documentElement.setAttribute("data-platform", platform);
}
