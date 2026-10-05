import type { KeyboardEvent } from "react";

// A tablist that does not answer the arrow keys is a tablist in name only, and
// the two in this app must not behave differently.
export function arrowTabs(e: KeyboardEvent<HTMLElement>) {
  const tabs = [...e.currentTarget.querySelectorAll<HTMLElement>('[role="tab"]')];
  const i = tabs.indexOf(document.activeElement as HTMLElement);
  if (i < 0) return;
  const to = e.key === "ArrowRight" || e.key === "ArrowDown" ? i + 1 : e.key === "ArrowLeft" || e.key === "ArrowUp" ? i - 1 : -1;
  if (to < 0 || to >= tabs.length) return;
  e.preventDefault();
  tabs[to].focus();
  tabs[to].click();
}

export function arrowRadios(e: KeyboardEvent<HTMLElement>) {
  if (e.altKey || e.ctrlKey || e.metaKey) return;
  const radios = [...e.currentTarget.querySelectorAll<HTMLButtonElement>('button[role="radio"]:not(:disabled)')];
  const index = radios.indexOf(document.activeElement as HTMLButtonElement);
  if (index < 0) return;
  const step = e.key === "ArrowRight" || e.key === "ArrowDown" ? 1 : e.key === "ArrowLeft" || e.key === "ArrowUp" ? -1 : 0;
  if (!step) return;
  e.preventDefault();
  e.stopPropagation();
  const next = radios[(index + step + radios.length) % radios.length];
  next.focus();
  next.click();
}
