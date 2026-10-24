// @vitest-environment jsdom
import { beforeEach, expect, it } from "vitest";
import { onSettingsWideChange, setSettingsWide, settingsWide } from "./settingsWide";

beforeEach(() => localStorage.clear());

it("is the default size until this machine chooses otherwise", () => {
  expect(settingsWide()).toBe(false);
});

it("reads back a choice and tells subscribers", () => {
  let heard = 0;
  const stop = onSettingsWideChange(() => heard++);
  setSettingsWide(true);
  expect(settingsWide()).toBe(true);
  setSettingsWide(false);
  stop();
  expect(settingsWide()).toBe(false);
  expect(heard).toBe(2);
});

it("reads anything unreadable as the default size", () => {
  for (const raw of ["", "yes", "1", "{}"]) {
    localStorage.setItem("rx-settings-wide", raw);
    expect(settingsWide(), raw).toBe(false);
  }
});
