import { useEffect, useRef } from "react";
import { app } from "./bridge";
import type { WireApproval } from "./types";
import { webView2ApprovalSmokeEnabled } from "./webView2ApprovalSmoke";

type NativeAnimate = typeof Element.prototype.animate;

export function WebView2ApprovalSmoke({
  activeTabId,
  approval,
}: {
  activeTabId: string | undefined;
  approval: WireApproval | undefined;
}) {
  useWebView2ApprovalSmoke(activeTabId, approval);
  return null;
}

export function useWebView2ApprovalSmoke(activeTabId: string | undefined, approval: WireApproval | undefined) {
  const started = useRef(false);
  const sawApproval = useRef(false);
  const reported = useRef(false);
  const nativeCalls = useRef(0);
  const nativeEasing = useRef("");

  const report = (ok: boolean, detail: string) => {
    if (reported.current) return;
    reported.current = true;
    const complete = window.go?.main?.WebView2ApprovalSmokeBridge?.Complete;
    if (typeof complete === "function") void complete(ok, detail.slice(0, 240));
  };

  useEffect(() => {
    if (!webView2ApprovalSmokeEnabled()) return;
    const original = Element.prototype.animate as NativeAnimate | undefined;
    if (typeof original !== "function") {
      report(false, "Element.animate is unavailable in WebView2");
      return;
    }
    const onError = () => report(false, "window error during approval smoke");
    const onRejection = () => report(false, "unhandled rejection during approval smoke");
    window.addEventListener("error", onError);
    window.addEventListener("unhandledrejection", onRejection);
    Element.prototype.animate = function (frames, options) {
      if (this instanceof HTMLElement && this.querySelector(".prompt-shelf")) {
        nativeCalls.current += 1;
        nativeEasing.current = typeof options === "object" && options ? String(options.easing ?? "") : "";
        if (!nativeEasing.current || /power\d?\./i.test(nativeEasing.current)) {
          report(false, `approval used invalid easing: ${nativeEasing.current || "<empty>"}`);
        }
      }
      return original.call(this, frames, options);
    };
    return () => {
      Element.prototype.animate = original;
      window.removeEventListener("error", onError);
      window.removeEventListener("unhandledrejection", onRejection);
    };
  }, []);

  useEffect(() => {
    if (!webView2ApprovalSmokeEnabled() || !activeTabId || started.current) return;
    started.current = true;
    void app.SubmitToTab(activeTabId, "/mock-tool-approval").catch(() => {
      report(false, "mock approval submit failed");
    });
  }, [activeTabId]);

  useEffect(() => {
    if (!webView2ApprovalSmokeEnabled() || !approval || sawApproval.current) return;
    sawApproval.current = true;
    window.setTimeout(() => {
      const action = document.querySelector<HTMLButtonElement>(".prompt-shelf__actions .prompt-action");
      const confirm = document.querySelector<HTMLButtonElement>(".decision-confirm-bar__confirm");
      if (!action || !confirm) {
        report(false, "approval controls did not render");
        return;
      }
      action.click();
      confirm.click();
    }, 0);
  }, [approval]);

  useEffect(() => {
    if (!webView2ApprovalSmokeEnabled() || !sawApproval.current || approval || reported.current) return;
    window.setTimeout(() => {
      if (nativeCalls.current !== 1) {
        report(false, `approval native animation count was ${nativeCalls.current}, expected 1`);
        return;
      }
      report(true, `approval completed with native easing ${nativeEasing.current}`);
    }, 0);
  }, [approval]);
}
