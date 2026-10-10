// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import "./testkit";
import { Pane } from "./Pane";
import { MockPort } from "../port/mock";
import type { ExtensionFormField, ExtensionSurface, WireEvent } from "../port/wire";

afterEach(cleanup);

const keeper: ExtensionFormField = { key: "name", label: "Name", kind: "input", default: "Initial" };
const textbox = (name: string) => screen.getByRole("textbox", { name }) as HTMLInputElement;
const submitButton = () => document.querySelector<HTMLButtonElement>('[data-action="extensions.submit"]');
const option = (name: string) => screen.getByRole("button", { name });

async function mount() {
  const port = new MockPort();
  vi.spyOn(port, "history").mockResolvedValue([]);
  let emit: (ev: WireEvent) => void = () => {};
  vi.spyOn(port, "subscribe").mockImplementation((onEvent) => {
    emit = onEvent;
    return () => true;
  });
  const submit = vi.spyOn(port, "submitExtensionForm").mockResolvedValue(undefined);
  render(<Pane port={port} rt={{ id: "r1", base: "/rt/r1", root: "/w", name: "w" }}
    title="w" active visible sideHost={null} side={false} onFocus={() => {}} onReport={() => {}}
    onSessionChanged={() => {}} pulse={0} findPulse={0} onSettings={() => {}} needsProject={false}
    onOpenProject={() => {}} onKeepHere={() => {}} theme="light" dockW={320} dockMax={640} onDockW={() => {}} />);
  await act(async () => {});
  const publish = (body: Pick<ExtensionSurface, "kind" | "form" | "status">) => act(() => emit({
    kind: "extension_surface", extension: {
      pluginId: "fixture", surfaceId: "form", sessionId: "s1", generation: 3, ...body,
    },
  }));
  return { submit, publish, fields: (fields: ExtensionFormField[]) => publish({ kind: "form", form: { title: "Published form", fields } }) };
}

it.each([
  { key: "extra", label: "Extra", kind: "input", default: "Published" },
  { key: "extra", label: "Extra", kind: "select", options: ["east", "west"], default: "east" },
  { key: "extra", label: "Extra", kind: "multiselect", options: ["east", "west"], default: ["east"] },
  { key: "extra", label: "Extra", kind: "confirm", default: true },
] satisfies ExtensionFormField[])("uses the default of a newly published $kind field without losing the draft", async (extra) => {
  const pane = await mount();
  pane.fields([keeper]);
  fireEvent.change(textbox("Name"), { target: { value: "Retained draft" } });
  pane.fields([keeper, extra]);
  expect(document.querySelectorAll(".extform")).toHaveLength(1);
  expect(textbox("Name").value).toBe("Retained draft");
  if (extra.kind === "input") expect(textbox("Extra").value).toBe("Published");
  else expect(option(extra.kind === "confirm" ? "Extra" : "east").hasAttribute("data-on")).toBe(true);
  await act(async () => fireEvent.click(submitButton()!));
  expect(pane.submit).toHaveBeenCalledExactlyOnceWith("fixture", "form", { name: "Retained draft", extra: extra.default });
});

it("submits only fields in the current publication", async () => {
  const pane = await mount();
  pane.fields([keeper, { key: "retired", label: "Retired", kind: "input", default: "Old" }]);
  fireEvent.change(textbox("Retired"), { target: { value: "Edited old field" } });
  pane.fields([keeper]);
  expect(screen.queryByRole("textbox", { name: "Retired" })).toBeNull();
  await act(async () => fireEvent.click(submitButton()!));
  expect(pane.submit).toHaveBeenCalledExactlyOnceWith("fixture", "form", { name: "Initial" });
});

it("seeds a form that replaces a status at the same surface", async () => {
  const pane = await mount();
  pane.publish({ kind: "status", status: { label: "Waiting for configuration" } });
  pane.fields([{ ...keeper, required: true }]);
  expect(textbox("Name").value).toBe("Initial");
  expect(submitButton()?.disabled).toBe(false);
  await act(async () => fireEvent.click(submitButton()!));
  expect(pane.submit).toHaveBeenCalledExactlyOnceWith("fixture", "form", { name: "Initial" });
});

it("keeps explicit empty and false user choices when fields are republished", async () => {
  const pane = await mount();
  const fields: ExtensionFormField[] = [keeper,
    { key: "env", label: "Environment", kind: "select", options: ["prod", "dev"], default: "prod" },
    { key: "regions", label: "Regions", kind: "multiselect", options: ["east", "west"], default: ["east"] },
    { key: "confirmed", label: "Confirm", kind: "confirm", default: true },
  ];
  pane.fields(fields);
  fireEvent.change(textbox("Name"), { target: { value: "" } });
  fireEvent.click(option("dev"));
  fireEvent.click(option("east"));
  fireEvent.click(option("Confirm"));
  pane.fields(fields.map((field) => ({ ...field })));
  expect(textbox("Name").value).toBe("");
  expect(option("dev").hasAttribute("data-on")).toBe(true);
  expect(option("east").hasAttribute("data-on")).toBe(false);
  expect(option("Confirm").hasAttribute("data-on")).toBe(false);
  await act(async () => fireEvent.click(submitButton()!));
  expect(pane.submit).toHaveBeenCalledExactlyOnceWith("fixture", "form", { name: "", env: "dev", regions: [], confirmed: false });
});

it("validates newly required fields and retains the completed form on a publication", async () => {
  const pane = await mount();
  pane.fields([keeper]);
  const extra: ExtensionFormField = { key: "extra", label: "Extra", kind: "input", required: true };
  pane.fields([keeper, extra]);
  expect(submitButton()?.disabled).toBe(true);
  fireEvent.change(textbox("Extra"), { target: { value: "Filled" } });
  expect(submitButton()?.disabled).toBe(false);
  await act(async () => fireEvent.click(submitButton()!));
  pane.fields([keeper, { ...extra }]);
  expect(submitButton()).toBeNull();
  expect(textbox("Extra").readOnly).toBe(true);
  expect(pane.submit).toHaveBeenCalledExactlyOnceWith("fixture", "form", { name: "Initial", extra: "Filled" });
});
