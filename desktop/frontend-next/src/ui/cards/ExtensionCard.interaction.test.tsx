// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { ExtensionCard } from "./ExtensionCard";
import type { ExtensionSurface } from "../../port/wire";

afterEach(cleanup);

const ext: ExtensionSurface = {
  pluginId: "fixture", surfaceId: "form", sessionId: "s1", generation: 1, kind: "form",
  form: { title: "Fixture form", fields: [
    { key: "name", label: "Name", kind: "input", default: "Ada", required: true },
    { key: "env", label: "Environment", kind: "select", options: ["prod", "dev"], default: "dev" },
    { key: "regions", label: "Regions", kind: "multiselect", options: ["east", "west"], default: ["east"] },
    { key: "confirmed", label: "Confirm", kind: "confirm", required: true },
  ] },
};

const submitButton = () => document.querySelector<HTMLButtonElement>('[data-action="extensions.submit"]');
const field = () => screen.getByRole("textbox", { name: "Name" }) as HTMLInputElement;
const fill = () => fireEvent.click(screen.getByRole("button", { name: /^Confirm/ }));

it("validates required fields and holds all field kinds until success", async () => {
  let resolve = () => {};
  const submit = vi.fn(() => new Promise<void>((done) => { resolve = done; }));
  render(<ExtensionCard ext={ext} onSubmit={submit} />);
  expect(submitButton()?.disabled).toBe(true);
  fill();
  fireEvent.change(field(), { target: { value: " " } });
  expect(submitButton()?.disabled).toBe(true);
  fireEvent.change(field(), { target: { value: "Grace" } });
  fireEvent.click(screen.getByRole("button", { name: "prod" }));
  fireEvent.click(screen.getByRole("button", { name: "west" }));
  fireEvent.click(submitButton()!);
  expect(submit).toHaveBeenCalledExactlyOnceWith("fixture", "form", {
    name: "Grace", env: "prod", regions: ["east", "west"], confirmed: true,
  });
  expect(field().readOnly).toBe(true);
  for (const b of screen.getAllByRole("button")) expect((b as HTMLButtonElement).disabled).toBe(true);
  await act(async () => resolve());
  expect(submitButton()).toBeNull();
  expect(field().value).toBe("Grace");
});

it("does not seal a form without a submission callback", () => {
  render(<ExtensionCard ext={ext} />);
  fill();
  expect(submitButton()?.disabled).toBe(true);
  fireEvent.click(submitButton()!);
  expect(field().readOnly).toBe(false);
});

it("accepts a synchronous callback after it completes", async () => {
  const submit = vi.fn(() => {});
  render(<ExtensionCard ext={ext} onSubmit={submit} />);
  fill();
  await act(async () => fireEvent.click(submitButton()!));
  expect(submit).toHaveBeenCalledTimes(1);
  expect(submitButton()).toBeNull();
});

it("keeps the draft editable if a callback throws synchronously", async () => {
  const submit = vi.fn(() => { throw new Error("refused"); });
  render(<ExtensionCard ext={ext} onSubmit={submit} />);
  fill();
  await act(async () => fireEvent.click(submitButton()!));
  expect(submit).toHaveBeenCalledTimes(1);
  expect(field().readOnly).toBe(false);
  expect(submitButton()?.disabled).toBe(false);
});

it("preserves an in-flight draft when the same surface is republished", async () => {
  let resolve = () => {};
  const submit = vi.fn(() => new Promise<void>((done) => { resolve = done; }));
  const mounted = render(<ExtensionCard ext={ext} onSubmit={submit} />);
  fill();
  fireEvent.change(field(), { target: { value: "Retained" } });
  fireEvent.click(submitButton()!);
  mounted.rerender(<ExtensionCard ext={{ ...ext, form: { ...ext.form!, message: "Updated message" } }} onSubmit={submit} />);
  expect(field().value).toBe("Retained");
  expect(submitButton()?.disabled).toBe(true);
  expect(screen.getByText("Updated message")).toBeTruthy();
  await act(async () => resolve());
  expect(submitButton()).toBeNull();
  expect(submit).toHaveBeenCalledTimes(1);
});

it.each(["success", "failure"] as const)("does not apply an old %s to a remounted form", async (outcome) => {
  let resolve = () => {};
  let reject: (e: Error) => void = () => {};
  const pending = new Promise<void>((done, fail) => { resolve = done; reject = fail; });
  void pending.catch(() => {});
  const submit = vi.fn(() => pending);
  const mounted = render(<ExtensionCard key="old" ext={ext} onSubmit={submit} />);
  fill();
  fireEvent.click(submitButton()!);
  mounted.rerender(<ExtensionCard key="new" ext={{ ...ext, sessionId: "s2" }} onSubmit={submit} />);
  await act(async () => outcome === "success" ? resolve() : reject(new Error("old request failed")));
  expect(field().value).toBe("Ada");
  expect(field().readOnly).toBe(false);
  fill();
  expect(submitButton()?.disabled).toBe(false);
  expect(submit).toHaveBeenCalledTimes(1);
});
