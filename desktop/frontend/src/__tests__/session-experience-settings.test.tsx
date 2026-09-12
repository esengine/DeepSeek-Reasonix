import assert from "node:assert/strict";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import { JSDOM } from "jsdom";
import { SessionExperienceSettings } from "../components/SessionExperienceSettings";
import { LocaleProvider, t } from "../lib/i18n";
import type { SettingsView } from "../lib/types";
import { installDesktopHostStub } from "./desktopHostStub";

const dom = new JSDOM("<div id='root'></div>", { url: "http://localhost" });
Object.assign(globalThis, { window: dom.window, document: dom.window.document, localStorage: dom.window.localStorage,
  CustomEvent: dom.window.CustomEvent, IS_REACT_ACT_ENVIRONMENT: true });
const writes: string[] = [];
let legacyWrites = 0;
installDesktopHostStub({ SetSessionExperience: async () => { legacyWrites++; },
  SetDefaultToolApprovalMode: async (mode: string) => { writes.push(mode); } });
const root = createRoot(document.getElementById("root")!);
const apply = async (write: () => Promise<unknown>) => { await write(); return true; };
try {
  for (const mode of ["standard", "deep"] as const) {
    await act(async () => root.render(<LocaleProvider><SessionExperienceSettings snapshot={{ sessionExperience: mode, defaultToolApprovalMode: "workspace-write" } as SettingsView} busy={false} apply={apply} /></LocaleProvider>));
    const groups = [...document.querySelectorAll<HTMLElement>("[role=radiogroup]")];
    assert.ok(!groups.some(group => group.getAttribute("aria-label") === t("settings.sessionExperience")), "legacy display preference has no settings control");
    const approval = groups.find(group => group.getAttribute("aria-label") === t("settings.defaultToolApprovalMode"))!;
    assert.equal(approval.querySelectorAll("[role=radio]").length, 3);
    await act(async () => approval.querySelectorAll<HTMLButtonElement>("button")[0].click());
  }
  assert.deepEqual(writes, ["read-only", "read-only"], "permission settings send the selected preset to the host");
  assert.equal(legacyWrites, 0, "opening settings never rewrites the persisted legacy preference");

  await act(async () => root.render(<LocaleProvider><SessionExperienceSettings snapshot={{ defaultToolApprovalMode: "workspace-write" } as SettingsView} busy={false} apply={apply} /></LocaleProvider>));
  const approval = [...document.querySelectorAll<HTMLElement>("[role=radiogroup]")]
    .find(group => group.getAttribute("aria-label") === t("settings.defaultToolApprovalMode"))!;
  await act(async () => approval.querySelectorAll<HTMLButtonElement>("button")[2].click());
  assert.deepEqual(writes, ["read-only", "read-only"], "selecting a Full access default does not persist before confirmation");
  const dialog = document.querySelector<HTMLElement>('[role="dialog"]')!;
  assert.ok(dialog.textContent?.includes(t("permission.fullAccessConfirm.title")));
  const enable = [...dialog.querySelectorAll<HTMLButtonElement>("button")]
    .find(button => button.textContent?.includes(t("permission.fullAccessConfirm.enable")))!;
  assert.equal(enable.disabled, true, "default Full access stays disabled until risk acknowledgement");
  await act(async () => dialog.querySelector<HTMLInputElement>('input[type="checkbox"]')!.click());
  assert.equal(enable.disabled, false);
  await act(async () => enable.click());
  assert.deepEqual(writes, ["read-only", "read-only", "danger-full-access"], "confirmed Full access becomes the new-session default");

  await act(async () => root.render(<LocaleProvider><SessionExperienceSettings snapshot={{} as SettingsView} busy apply={apply} /></LocaleProvider>));
  assert.ok([...document.querySelectorAll<HTMLButtonElement>("[role=radio]")].every(button => button.disabled));
  console.log("settings: retired conversation display control, preserved approval and busy states passed");
} finally { await act(async () => root.unmount()); dom.window.close(); }
