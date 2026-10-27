// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, renderHook } from "@testing-library/react";
import { NEW_TASK_PRESET_KEY, newTaskComposition, newTaskPreset, setNewTaskPreset, useNewTaskPreset } from "./newTaskPreset";

afterEach(() => { cleanup(); vi.restoreAllMocks(); localStorage.removeItem(NEW_TASK_PRESET_KEY); });

it("leaves old installations unselected and ignores unsupported stored identities", () => {
  expect(newTaskComposition()).toBeUndefined();
  for (const raw of ["", "default", "user:", "user:has space", "project:review", "builtin:retired", "{bad"]) {
    localStorage.setItem(NEW_TASK_PRESET_KEY, raw);
    expect(newTaskPreset()).toBeUndefined();
  }
  expect(setNewTaskPreset("project:review")).toBe(false);
});

it("accepts a personal preset as the default, which a project's shared preset cannot be", () => {
  expect(setNewTaskPreset("user:review-pro")).toBe(true);
  expect(newTaskPreset()).toBe("user:review-pro");
  expect(newTaskComposition()).toBe("user:review-pro");
  expect(setNewTaskPreset("project:review")).toBe(false);
  expect(newTaskPreset()).toBe("user:review-pro");
});

it("round-trips explicit built-in choices while preserving request and history choices", () => {
  for (const id of ["builtin:general", "builtin:minimal"]) {
    expect(setNewTaskPreset(id)).toBe(true);
    expect(newTaskComposition()).toBe(id);
    expect(newTaskComposition("user:explicit")).toBe("user:explicit");
    expect(newTaskComposition(undefined, "/history/task.jsonl")).toBeUndefined();
  }
});

it("publishes local changes and cross-window storage changes to the card", () => {
  const { result } = renderHook(useNewTaskPreset);
  act(() => { setNewTaskPreset("builtin:minimal"); });
  expect(result.current).toBe("builtin:minimal");
  act(() => {
    localStorage.setItem(NEW_TASK_PRESET_KEY, "builtin:general");
    window.dispatchEvent(new StorageEvent("storage", { key: NEW_TASK_PRESET_KEY }));
  });
  expect(result.current).toBe("builtin:general");
});

it("refuses an unpersisted selection when local storage is unavailable", () => {
  setNewTaskPreset("builtin:general");
  vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => { throw new Error("storage denied"); });
  expect(setNewTaskPreset("builtin:minimal")).toBe(false);
  expect(newTaskPreset()).toBe("builtin:general");
});
