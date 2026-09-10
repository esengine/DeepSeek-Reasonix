import { useEffect } from "react";

import type { BrowserLayoutRect, DesktopBrowserHost } from "./browserHost";

export type BrowserSurfaceHost = Pick<DesktopBrowserHost, "setLayout" | "setOverlay">;
type View = Window & typeof globalThis;

/** Marks a portaled application overlay root; while one exists every native website view is hidden. */
export const APP_OVERLAY_SELECTOR = "[data-app-overlay]";

const sameRect = (a: BrowserLayoutRect, b: BrowserLayoutRect) =>
  a.x === b.x && a.y === b.y && a.width === b.width && a.height === b.height;

/** Reports the surface's viewport rectangle to the shell until detached, then reports null. */
export function attachBrowserSurfaceLayout(host: Pick<DesktopBrowserHost, "setLayout">, surface: HTMLElement, view: View = window): () => void {
  let frame = 0;
  let last: BrowserLayoutRect | null = null;
  const measure = () => {
    frame = 0;
    const box = surface.getBoundingClientRect();
    const rect = { x: Math.round(box.left), y: Math.round(box.top), width: Math.round(box.width), height: Math.round(box.height) };
    if (last && sameRect(last, rect)) return;
    last = rect;
    host.setLayout(rect);
  };
  const schedule = () => {
    if (!frame) frame = view.requestAnimationFrame(measure);
  };
  const observer = typeof view.ResizeObserver === "function" ? new view.ResizeObserver(schedule) : null;
  observer?.observe(surface);
  const scrollers: EventTarget[] = [view];
  for (let node = surface.parentElement; node; node = node.parentElement) scrollers.push(node);
  for (const scroller of scrollers) scroller.addEventListener("scroll", schedule, { passive: true });
  view.addEventListener("resize", schedule);
  view.visualViewport?.addEventListener("resize", schedule);
  view.visualViewport?.addEventListener("scroll", schedule);
  measure();
  return () => {
    if (frame) view.cancelAnimationFrame(frame);
    observer?.disconnect();
    for (const scroller of scrollers) scroller.removeEventListener("scroll", schedule);
    view.removeEventListener("resize", schedule);
    view.visualViewport?.removeEventListener("resize", schedule);
    view.visualViewport?.removeEventListener("scroll", schedule);
    host.setLayout(null);
  };
}

const carriesOverlay = (node: Node) =>
  node.nodeType === 1 && ((node as Element).matches(APP_OVERLAY_SELECTOR) || (node as Element).querySelector(APP_OVERLAY_SELECTOR) !== null);

/** Mirrors the presence of any `[data-app-overlay]` element into the shell's overlay state. */
export function attachAppOverlayGate(host: Pick<DesktopBrowserHost, "setOverlay">, view: View = window): () => void {
  const document = view.document;
  let frame = 0;
  let last: boolean | null = null;
  const check = () => {
    frame = 0;
    const active = document.querySelector(APP_OVERLAY_SELECTOR) !== null;
    if (active === last) return;
    last = active;
    host.setOverlay(active);
  };
  const schedule = () => {
    if (!frame) frame = view.requestAnimationFrame(check);
  };
  // Transcript streaming mutates the DOM constantly; only records that can
  // change overlay presence are allowed to schedule the document query.
  const observer = new view.MutationObserver((records) => {
    if (records.some((record) => record.type === "attributes"
      || [...record.addedNodes, ...record.removedNodes].some(carriesOverlay))) schedule();
  });
  observer.observe(document.body, { childList: true, subtree: true, attributes: true, attributeFilter: ["data-app-overlay"] });
  check();
  return () => {
    if (frame) view.cancelAnimationFrame(frame);
    observer.disconnect();
    if (last) host.setOverlay(false);
  };
}

export function useBrowserSurfaceLayout(host: BrowserSurfaceHost | undefined, surface: HTMLElement | null): void {
  useEffect(() => (host && surface ? attachBrowserSurfaceLayout(host, surface) : undefined), [host, surface]);
  useEffect(() => (host ? attachAppOverlayGate(host) : undefined), [host]);
}
