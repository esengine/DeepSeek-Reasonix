import { act } from "react";

export async function selectSettingsValue(control: HTMLButtonElement, value: string) {
  await act(async () => control.click());
  const option = Array.from(document.querySelectorAll<HTMLElement>('.settings-select-menu [role="option"]')).find(item => item.dataset.value === value);
  if (!option) throw new Error(`Settings option not found: ${value}`);
  await act(async () => option.click());
}
export async function settingsOptionValues(control: HTMLButtonElement) {
  await act(async () => control.click());
  const values = Array.from(document.querySelectorAll<HTMLElement>('.settings-select-menu [role="option"]')).map(item => item.dataset.value);
  await act(async () => control.click());
  return values;
}
