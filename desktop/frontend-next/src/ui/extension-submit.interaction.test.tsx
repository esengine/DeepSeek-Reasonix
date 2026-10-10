// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import "./testkit";
import { Pane } from "./Pane";
import { MockPort } from "../port/mock";
import { HttpError } from "../port/http_error";
import type { WireEvent } from "../port/wire";

afterEach(cleanup);

async function mount() {
  const port = new MockPort();
  vi.spyOn(port, "history").mockResolvedValue([]);
  let emit: (ev: WireEvent) => void = () => {};
  vi.spyOn(port, "subscribe").mockImplementation((onEvent) => {
    emit = onEvent;
    return () => true;
  });
  const submit = vi.spyOn(port, "submitExtensionForm");
  render(<Pane port={port} rt={{ id: "r1", base: "/rt/r1", root: "/w", name: "w" }}
    title="w" active visible sideHost={null} side={false} onFocus={() => {}} onReport={() => {}}
    onSessionChanged={() => {}} pulse={0} findPulse={0} onSettings={() => {}} needsProject={false}
    onOpenProject={() => {}} onKeepHere={() => {}} theme="light" dockW={320} dockMax={640} onDockW={() => {}} />);
  await act(async () => {});
  act(() => emit({ kind: "extension_surface", extension: {
    pluginId: "fixture", surfaceId: "form", sessionId: "s1", generation: 3, kind: "form",
    form: { title: "Extension form", fields: [{ key: "name", label: "Fixture name", kind: "input", required: true }] },
  } }));
  fireEvent.change(field(), { target: { value: "Original draft" } });
  return submit;
}

const field = () => screen.getByRole("textbox", { name: "Fixture name" }) as HTMLInputElement;
const button = () => document.querySelector<HTMLButtonElement>('[data-action="extensions.submit"]');

it.each([
  ["sidecar refusal", new HttpError(422, "not accepted", {
    code: "extension.form_rejected", params: { detail: "Fixture declined this value" },
  }), "Fixture declined this value"],
  ["transport failure", new TypeError("Fixture connection lost"), "Fixture connection lost"],
] as const)("retains the draft after a %s and submits again only on a new click", async (_kind, error, detail) => {
  const submit = await mount();
  submit.mockRejectedValueOnce(error).mockResolvedValueOnce(undefined);
  fireEvent.click(button()!);
  await waitFor(() => expect(screen.getByText(new RegExp(detail))).toBeTruthy());
  expect(screen.getAllByText(new RegExp(detail))).toHaveLength(1);
  expect(field().value).toBe("Original draft");
  expect(field().readOnly).toBe(false);
  expect(button()?.disabled).toBe(false);
  await act(async () => {});
  expect(submit).toHaveBeenCalledTimes(1);
  fireEvent.change(field(), { target: { value: "Corrected draft" } });
  fireEvent.click(button()!);
  await waitFor(() => expect(button()).toBeNull());
  expect(field().readOnly).toBe(true);
  expect(submit.mock.calls).toEqual([
    ["fixture", "form", { name: "Original draft" }],
    ["fixture", "form", { name: "Corrected draft" }],
  ]);
});

it("keeps one request pending and seals only after the port acknowledges it", async () => {
  const submit = await mount();
  let resolve = () => {};
  submit.mockImplementation(() => new Promise<void>((done) => { resolve = done; }));
  fireEvent.click(button()!);
  expect(button()).not.toBeNull();
  expect(button()?.disabled).toBe(true);
  expect(screen.getByText("正在提交…")).toBeTruthy();
  expect(document.querySelector(".extform")?.hasAttribute("data-sealed")).toBe(false);
  expect(field().readOnly).toBe(true);
  fireEvent.click(button()!);
  expect(submit).toHaveBeenCalledExactlyOnceWith("fixture", "form", { name: "Original draft" });
  await act(async () => resolve());
  expect(button()).toBeNull();
  expect(document.querySelector(".extform")?.hasAttribute("data-sealed")).toBe(true);
});
