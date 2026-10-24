// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { PresentFiles } from "./PresentFiles";
import { MockPort } from "../port/mock";
import type { PresentFilesSettings } from "../port/port";

afterEach(cleanup);

const toggle = () => screen.findByRole("switch", { name: "启用 AI 文件交付卡片" });

it("saves both choices and rereads the saved state after reopening", async () => {
  const port = new MockPort();
  const save = vi.spyOn(port, "savePresentFiles");
  const view = render(<PresentFiles port={port} />);
  const sw = await toggle();
  expect(sw.getAttribute("aria-checked")).toBe("false");
  await userEvent.click(sw);
  await waitFor(() => expect(sw.getAttribute("aria-checked")).toBe("true"));
  expect(save).toHaveBeenCalledWith(true);
  view.unmount();
  render(<PresentFiles port={port} />);
  const reopened = await toggle();
  expect(reopened.getAttribute("aria-checked")).toBe("true");
  await userEvent.click(reopened);
  await waitFor(() => expect(reopened.getAttribute("aria-checked")).toBe("false"));
  expect(save).toHaveBeenLastCalledWith(false);
});

it("disables repeat saves while pending and retains the saved choice on failure", async () => {
  const port = new MockPort();
  let reject!: (error: Error) => void;
  const save = vi.spyOn(port, "savePresentFiles").mockImplementation(() =>
    new Promise<PresentFilesSettings>((_, fail) => { reject = fail; }));
  render(<PresentFiles port={port} />);
  const sw = await toggle();
  await userEvent.click(sw);
  expect((sw as HTMLButtonElement).disabled).toBe(true);
  await userEvent.click(sw);
  expect(save).toHaveBeenCalledTimes(1);
  await act(async () => reject(new Error("disk full")));
  expect((await screen.findByRole("alert")).textContent).toContain("disk full");
  expect(sw.getAttribute("aria-checked")).toBe("false");
  expect((sw as HTMLButtonElement).disabled).toBe(false);
  save.mockResolvedValue({ enabled: true, path: "/config.toml" });
  await userEvent.click(sw);
  await waitFor(() => expect(sw.getAttribute("aria-checked")).toBe("true"));
  expect(screen.queryByRole("alert")).toBeNull();
});

it("shows read failures without offering a switch with a guessed value", async () => {
  const port = new MockPort();
  vi.spyOn(port, "presentFiles").mockRejectedValue(new Error("offline"));
  render(<PresentFiles port={port} />);
  expect((await screen.findByRole("alert")).textContent).toContain("offline");
  expect(screen.getByText("无法读取文件交付设置。")).toBeTruthy();
  expect(screen.queryByRole("switch")).toBeNull();
});
