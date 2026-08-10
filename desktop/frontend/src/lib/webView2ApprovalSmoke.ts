declare global {
  interface Window {
    __REASONIX_WEBVIEW2_APPROVAL_SMOKE__?: boolean;
  }
}

export function webView2ApprovalSmokeEnabled(): boolean {
  return typeof window !== "undefined" && window.__REASONIX_WEBVIEW2_APPROVAL_SMOKE__ === true;
}
