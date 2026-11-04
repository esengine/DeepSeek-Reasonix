// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Appearance } from "./Appearance";
import { MockPort } from "../port/mock";
import { boot, STORAGE, t } from "../i18n";
import type { AgentPort, ThemePack } from "../port/port";

const mine: ThemePack = { id: "mine", name: "Mine", removable: true, tokens: { light: { bg: "#F6F3EE" } } };
const shipped: ThemePack = { id: "paper", name: "Paper", tokens: { light: { bg: "#FFFFFF" } } };

beforeEach(() => {
  localStorage.setItem(STORAGE, "zh");
  boot();
  vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockReturnValue(null);
});
afterEach(() => { cleanup(); vi.restoreAllMocks(); });

function draw(port: AgentPort, reload = vi.fn()) {
  render(<Appearance port={port} theme="light" onTheme={() => {}} contrast="" onContrast={() => {}}
    weight="" onWeight={() => {}} look={{}} onLook={() => {}} reloadThemes={reload} />);
  return reload;
}

it("offers deletion only on packs the user's library holds", async () => {
  const port = new MockPort() as unknown as AgentPort;
  vi.spyOn(port, "themes").mockResolvedValue([mine, shipped]);
  draw(port);
  await screen.findByRole("button", { name: /Paper/ });
  expect(screen.getAllByRole("button", { name: t("look.delete_theme_name", { name: "Mine" }) })).toHaveLength(1);
  expect(screen.queryByRole("button", { name: t("look.delete_theme_name", { name: "Paper" }) })).toBeNull();
});

it("asks twice, then removes the pack and re-reads the inventory", async () => {
  const port = new MockPort() as unknown as AgentPort;
  const themes = vi.spyOn(port, "themes").mockResolvedValueOnce([mine, shipped]).mockResolvedValue([shipped]);
  const remove = vi.spyOn(port, "removeTheme").mockResolvedValue();
  const reload = draw(port);
  const del = await screen.findByRole("button", { name: t("look.delete_theme_name", { name: "Mine" }) });
  await userEvent.click(del);
  expect(remove).not.toHaveBeenCalled();
  expect(del.textContent).toBe(t("common.confirm_delete"));
  await userEvent.click(del);
  expect(remove).toHaveBeenCalledExactlyOnceWith("mine");
  await waitFor(() => expect(screen.queryByRole("button", { name: /Mine/ })).toBeNull());
  expect(themes).toHaveBeenCalledTimes(2);
  expect(reload).toHaveBeenCalledTimes(1);
});

it("backs out of the arming when focus leaves, and never removes", async () => {
  const port = new MockPort() as unknown as AgentPort;
  vi.spyOn(port, "themes").mockResolvedValue([mine]);
  const remove = vi.spyOn(port, "removeTheme").mockResolvedValue();
  draw(port);
  const del = await screen.findByRole("button", { name: t("look.delete_theme_name", { name: "Mine" }) });
  await userEvent.click(del);
  await userEvent.tab();
  expect(del.textContent).toBe(t("common.delete"));
  expect(remove).not.toHaveBeenCalled();
});

it("keeps the pack and says why when the kernel refuses", async () => {
  const port = new MockPort() as unknown as AgentPort;
  vi.spyOn(port, "themes").mockResolvedValue([mine]);
  vi.spyOn(port, "removeTheme").mockRejectedValue(new Error("disk is read-only"));
  draw(port);
  const del = await screen.findByRole("button", { name: t("look.delete_theme_name", { name: "Mine" }) });
  await userEvent.click(del);
  await userEvent.click(del);
  expect((await screen.findByRole("alert")).textContent).toBe("disk is read-only");
  expect(screen.getByRole("button", { name: t("look.delete_theme_name", { name: "Mine" }) })).toBeTruthy();
  expect(del.textContent).toBe(t("common.delete"));
});
