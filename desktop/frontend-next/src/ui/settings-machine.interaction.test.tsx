// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Settings } from "./Settings";
import { MockPort } from "../port/mock";
import { MockHub } from "../port/mock_hub";
import { HttpError, type MemoryCatalog } from "../port/port";

afterEach(() => { cleanup(); vi.restoreAllMocks(); });

function settings(remote: MockPort, local: MockPort, at = "memory") {
  return <Settings hub={new MockHub()} onError={() => {}} port={remote} networkPort={local}
    status={null} theme="" reloadThemes={() => {}} onTheme={() => {}} contrast="" onContrast={() => {}}
    weight="" onWeight={() => {}} look={{}} onLook={() => {}} onClose={() => {}} onChanged={() => {}}
    at={at} account={null} accountUnread="" reloadAccount={() => {}} />;
}

it("reads and forgets memory on this machine while a remote pane is active", async () => {
  const remote = new MockPort(), local = new MockPort();
  const catalog: MemoryCatalog = { memories: [{ name: "local-fact", title: "Local fact", activation: "relevant" }], recallQuery: "" };
  vi.spyOn(local, "memories").mockResolvedValue(catalog);
  const remoteRead = vi.spyOn(remote, "memories");
  const localForget = vi.spyOn(local, "forgetMemory").mockResolvedValue();
  const remoteForget = vi.spyOn(remote, "forgetMemory");
  render(settings(remote, local));
  await screen.findByText("Local fact");
  await userEvent.click(screen.getByRole("button", { name: "忘记" }));
  expect(localForget).toHaveBeenCalledWith("local-fact");
  expect(remoteRead).not.toHaveBeenCalled();
  expect(remoteForget).not.toHaveBeenCalled();
});

it("reads machine totals locally but keeps project inventory on the active remote", async () => {
  const remote = new MockPort(), local = new MockPort();
  const storage = vi.spyOn(local, "storage");
  const usage = vi.spyOn(local, "usage");
  const remoteStorage = vi.spyOn(remote, "storage");
  const remoteUsage = vi.spyOn(remote, "usage");
  const skills = vi.spyOn(remote, "skills");
  const localSkills = vi.spyOn(local, "skills");
  render(settings(remote, local, "ext"));
  await screen.findByRole("heading", { name: "扩展" });
  await waitFor(() => expect(storage).toHaveBeenCalled(), { timeout: 4000 });
  expect(usage).toHaveBeenCalled();
  expect(remoteStorage).not.toHaveBeenCalled();
  expect(remoteUsage).not.toHaveBeenCalled();
  expect(skills).toHaveBeenCalled();
  expect(localSkills).not.toHaveBeenCalled();
});

it("keeps the current machine memory after an old owner's read settles late", async () => {
  const remote = new MockPort(), first = new MockPort(), current = new MockPort();
  let finish!: (catalog: MemoryCatalog) => void;
  vi.spyOn(first, "memories").mockReturnValue(new Promise((resolve) => { finish = resolve; }));
  vi.spyOn(current, "memories").mockResolvedValue({ memories: [{ name: "current", title: "Current fact", activation: "relevant" }], recallQuery: "" });
  const forget = vi.spyOn(current, "forgetMemory").mockResolvedValue();
  const oldForget = vi.spyOn(first, "forgetMemory");
  const view = render(settings(remote, first));
  view.rerender(settings(remote, current));
  await screen.findByText("Current fact");
  await act(async () => finish({ memories: [{ name: "old", title: "Old fact", activation: "relevant" }], recallQuery: "" }));
  expect(screen.queryByText("Old fact")).toBeNull();
  await userEvent.click(screen.getByRole("button", { name: "忘记" }));
  expect(forget).toHaveBeenCalledWith("current");
  expect(oldForget).not.toHaveBeenCalled();
});

it("reports a missing local owner and retries locally without falling back to the remote", async () => {
  const remote = new MockPort(), local = new MockPort();
  const read = vi.spyOn(local, "memories").mockRejectedValue(new HttpError(503, "local required", { code: "settings.local_runtime_required" }));
  const remoteRead = vi.spyOn(remote, "memories");
  render(settings(remote, local));
  expect((await screen.findByRole("alert")).textContent).toContain("请先打开一个本机会话");
  read.mockResolvedValue({ memories: [], recallQuery: "" });
  await userEvent.click(screen.getByRole("button", { name: "重试" }));
  await screen.findByText("暂无记录。");
  expect(remoteRead).not.toHaveBeenCalled();
});
