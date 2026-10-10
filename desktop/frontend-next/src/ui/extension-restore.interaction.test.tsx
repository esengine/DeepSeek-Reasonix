// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import "./testkit";
import { Pane } from "./Pane";
import { MockPort } from "../port/mock";
import type { HistoryMessage } from "../port/port";
import type { WireEvent } from "../port/wire";

afterEach(cleanup);

const record: HistoryMessage[] = [
  { role: "user", content: "Recorded request" },
  { role: "assistant", content: "Recorded answer" },
];

async function mount(mode: "mount" | "gap") {
  const port = new MockPort();
  let resolve!: (messages: HistoryMessage[]) => void;
  const pending = new Promise<HistoryMessage[]>((done) => { resolve = done; });
  const history = vi.spyOn(port, "history").mockResolvedValue([]);
  if (mode === "mount") history.mockReturnValueOnce(pending);
  let emit: (ev: WireEvent) => void = () => {};
  let gap = () => {};
  vi.spyOn(port, "subscribe").mockImplementation((onEvent, onGap) => {
    emit = onEvent;
    gap = onGap ?? (() => {});
    return () => true;
  });
  const invoke = vi.spyOn(port, "invokeExtensionAction").mockResolvedValue("invoked");
  const submit = vi.spyOn(port, "submitExtensionForm").mockResolvedValue(undefined);
  const view = (key: string) => (
    <Pane key={key} port={port} rt={{ id: key, base: "/rt/" + key, root: "/w", name: "w" }}
      title="w" active visible sideHost={null} side={false} onFocus={() => {}} onReport={() => {}}
      onSessionChanged={() => {}} pulse={0} findPulse={0} onSettings={() => {}} needsProject={false}
      onOpenProject={() => {}} onKeepHere={() => {}} theme="light" dockW={320} dockMax={640} onDockW={() => {}} />
  );
  const mounted = render(view("r1"));
  await act(async () => {});
  const send = (ev: WireEvent) => act(() => emit(ev));
  const restore = async () => {
    if (mode === "mount") await act(async () => resolve(record));
    else {
      history.mockResolvedValue(record);
      await act(async () => gap());
    }
    await waitFor(() => expect(screen.getByText("Recorded answer")).toBeTruthy());
  };
  return { send, restore, invoke, submit, remount: () => mounted.rerender(view("r2")) };
}

it.each(["mount", "gap"] as const)("keeps extension controls when the %s history read settles", async (mode) => {
  const pane = await mount(mode);
  pane.send({ kind: "extension_surface", extension: {
    pluginId: "fixture", surfaceId: "card", sessionId: "s1", generation: 3, kind: "card",
    card: { title: "Extension result", text: "Accepted publication", actions: [{ actionId: "run", label: "Run fixture" }] },
  } });
  pane.send({ kind: "extension_surface", extension: {
    pluginId: "fixture", surfaceId: "form", sessionId: "s1", generation: 3, kind: "form",
    form: { title: "Extension form", fields: [{ key: "name", label: "Fixture name", kind: "input" }] },
  } });
  const field = screen.getByRole("textbox", { name: "Fixture name" });
  fireEvent.change(field, { target: { value: "Retained draft" } });
  await pane.restore();
  expect(screen.getByText("Accepted publication")).toBeTruthy();
  expect((screen.getByRole("textbox", { name: "Fixture name" }) as HTMLInputElement).value).toBe("Retained draft");
  fireEvent.click(screen.getByRole("button", { name: "Run fixture" }));
  expect(pane.invoke).toHaveBeenCalledExactlyOnceWith("/fixture:run");
  fireEvent.click(document.querySelector<HTMLButtonElement>('[data-action="extensions.submit"]')!);
  expect(pane.submit).toHaveBeenCalledExactlyOnceWith("fixture", "form", { name: "Retained draft" });
  pane.remount();
  await act(async () => {});
  expect(screen.queryByText("Accepted publication")).toBeNull();
  expect(screen.queryByRole("textbox", { name: "Fixture name" })).toBeNull();
});
