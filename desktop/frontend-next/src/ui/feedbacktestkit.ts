import { fireEvent, screen } from "@testing-library/react";

/** The list row naming a report. */
export const rowOf = (receipt: string) => document.querySelector<HTMLElement>(`[data-action="feedback.pick"][data-target="${receipt}"]`)!;

/** Puts a report on the right and returns its page. */
export function pick(receipt: string): HTMLElement {
  fireEvent.click(rowOf(receipt));
  return document.querySelector<HTMLElement>(`.fbk-detail[data-receipt="${receipt}"]`)!;
}

/** Resolves once the list names the report. */
export const listed = (receipt: string) => screen.findByRole("button", { name: new RegExp(receipt) });
