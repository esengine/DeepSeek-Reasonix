// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "../testkit";
import { boot, STORAGE } from "../../i18n";
import { MockHub } from "../../port/mock_hub";
import { TeamRail } from "./TeamRail";

const CROPPED = "data:image/webp;base64,UklGRg==";
vi.mock("./photo", () => ({ cropPhoto: vi.fn(async () => CROPPED) }));

beforeEach(() => {
  localStorage.setItem(STORAGE, "zh");
  boot();
  HTMLDialogElement.prototype.showModal = function () { this.open = true; };
  HTMLDialogElement.prototype.close = function () { this.open = false; };
});
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

const file = () => new File([new Uint8Array([1, 2, 3])], "me.png", { type: "image/png" });

async function seeded() {
  const hub = new MockHub();
  const team = hub.team();
  await team.bootstrap("Chief");
  await team.createAgent({ name: "Ada" });
  render(<TeamRail hub={hub} active="" onAdoptPane={vi.fn()} />);
  return { hub, team };
}

it("a photo is cropped, shown in the preview, sent on save, and worn in the rail", async () => {
  const { team } = await seeded();
  await userEvent.click(await screen.findByRole("button", { name: "Ada 的设置" }));
  const dialog = await screen.findByRole("dialog", { name: "Agent 设置" });
  await userEvent.upload(within(dialog).getByLabelText("上传照片"), file());
  await waitFor(() => expect(within(dialog).getByRole("group", { name: "头像" }).querySelector("img")?.getAttribute("src")).toBe(CROPPED));
  expect(team.calls.some((c) => c.method === "setPhoto")).toBe(false);
  await userEvent.click(within(dialog).getByRole("button", { name: "保存" }));
  await waitFor(() => expect(team.calls.some((c) => c.method === "setPhoto")).toBe(true));
  expect(team.calls.find((c) => c.method === "setPhoto")?.args).toEqual(["a-1", CROPPED]);
  const row = (await screen.findAllByRole("button", { name: /^Ada(\s|$)/ })).find((b) => b.dataset.actionClick === "team.agent-open")!;
  await waitFor(() => expect(row.querySelector("img")?.getAttribute("src")).toBe(CROPPED));
});

it("a worn photo can be taken off", async () => {
  const { team } = await seeded();
  await team.setPhoto("a-1", CROPPED);
  await userEvent.click(await screen.findByRole("button", { name: "Ada 的设置" }));
  const dialog = await screen.findByRole("dialog", { name: "Agent 设置" });
  await userEvent.click(within(dialog).getByRole("button", { name: "移除照片" }));
  await userEvent.click(within(dialog).getByRole("button", { name: "保存" }));
  await waitFor(() => expect(team.calls.at(-1)).toEqual({ method: "clearPhoto", args: ["a-1"] }));
});

it("a new agent gets its photo once it exists", async () => {
  const { team } = await seeded();
  await userEvent.click(await screen.findByRole("button", { name: "新建 Agent" }));
  const dialog = await screen.findByRole("dialog", { name: "新建 Agent" });
  await userEvent.upload(within(dialog).getByLabelText("上传照片"), file());
  await userEvent.type(within(dialog).getByLabelText("名字"), "Cy");
  await userEvent.click(within(dialog).getByRole("button", { name: "创建" }));
  await waitFor(() => expect(team.calls.map((c) => c.method).slice(-2)).toEqual(["createAgent", "setPhoto"]));
  expect(team.calls.at(-1)?.args).toEqual(["a-2", CROPPED]);
});

it("a photo that will not load falls back to the glyph", async () => {
  const { team } = await seeded();
  await team.setPhoto("a-1", "/team/avatars/gone.webp");
  const row = (await screen.findAllByRole("button", { name: /^Ada(\s|$)/ })).find((b) => b.dataset.actionClick === "team.agent-open")!;
  const img = await waitFor(() => row.querySelector("img")!);
  img.dispatchEvent(new Event("error"));
  await waitFor(() => expect(row.querySelector("img")).toBeNull());
  expect(row.querySelector("svg")).toBeTruthy();
});
