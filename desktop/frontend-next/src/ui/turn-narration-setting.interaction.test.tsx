// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { TurnNarrationSetting } from "./TurnNarrationSetting";
import type { TurnNarrationSettings } from "../port/boundary";

afterEach(cleanup);

function portAt(level: TurnNarrationSettings["level"]) {
  const port = {
    turnNarrationSettings: vi.fn(async () => ({ level, path: "/c.toml" })),
    saveTurnNarrationSettings: vi.fn(async (next: TurnNarrationSettings["level"]) => ({ level: next, path: "/c.toml" })),
  };
  return port;
}

it("shows the stored level and changes it on request", async () => {
  const port = portAt("brief");
  render(<TurnNarrationSetting port={port as never} />);
  await waitFor(() => expect(screen.getByRole("radio", { name: "简短" }).getAttribute("aria-checked")).toBe("true"));
  expect(screen.getByRole("radio", { name: "详细" }).getAttribute("aria-checked")).toBe("false");
  await userEvent.click(screen.getByRole("radio", { name: "详细" }));
  expect(port.saveTurnNarrationSettings).toHaveBeenCalledExactlyOnceWith("detailed");
  await waitFor(() => expect(screen.getByRole("radio", { name: "详细" }).getAttribute("aria-checked")).toBe("true"));
});

it("offers no choice until the stored level is known, and says when the save fails", async () => {
  const port = portAt("detailed");
  port.saveTurnNarrationSettings.mockRejectedValueOnce(new Error("disk full"));
  render(<TurnNarrationSetting port={port as never} />);
  expect((screen.getByRole("radio", { name: "简短" }) as HTMLButtonElement).disabled).toBe(true);
  await waitFor(() => expect(screen.getByRole("radio", { name: "详细" }).getAttribute("aria-checked")).toBe("true"));
  await userEvent.click(screen.getByRole("radio", { name: "简短" }));
  expect(await screen.findByText(/disk full/)).toBeTruthy();
  expect(screen.getByRole("radio", { name: "详细" }).getAttribute("aria-checked")).toBe("true");
});
