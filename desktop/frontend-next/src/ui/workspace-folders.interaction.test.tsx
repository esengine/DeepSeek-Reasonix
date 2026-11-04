// @vitest-environment jsdom
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { WorkspaceFolders } from "./WorkspaceFolders";
import { MockPort } from "../port/mock";
import { HttpError } from "../port/port";
import { boot, STORAGE, t } from "../i18n";
import type { AgentPort } from "../port/port";

beforeEach(() => {
  localStorage.setItem(STORAGE, "zh");
  boot();
});
afterEach(() => { cleanup(); vi.restoreAllMocks(); });

function draw(port = new MockPort() as unknown as AgentPort) {
  const onChanged = vi.fn();
  render(<WorkspaceFolders port={port} onChanged={onChanged} />);
  return { port, onChanged };
}

it("starts with no extra folder and says approval does not widen where writes can land", async () => {
  draw();
  expect(await screen.findByText(t("session.extra_folders_for_this_project"))).toBeTruthy();
  expect(screen.queryAllByRole("button", { name: /移除文件夹/ })).toHaveLength(0);
  expect(screen.getByText(/「完全放行」只决定是否询问/)).toBeTruthy();
  expect((screen.getByRole("button", { name: t("common.add") }) as HTMLButtonElement).disabled).toBe(true);
});

it("adds a folder, lists it, tells the window to rebuild, and removes it again", async () => {
  const { onChanged } = draw();
  const input = await screen.findByLabelText(t("session.folder_path_to_add"));
  await userEvent.type(input, "/Users/you/code/shared-lib");
  await userEvent.click(screen.getByRole("button", { name: t("common.add") }));
  expect(await screen.findByText("/Users/you/code/shared-lib")).toBeTruthy();
  expect(onChanged).toHaveBeenCalledTimes(1);
  expect((input as HTMLInputElement).value).toBe("");
  await userEvent.click(screen.getByRole("button", { name: t("session.remove_folder_path", { path: "/Users/you/code/shared-lib" }) }));
  await waitFor(() => expect(screen.queryByText("/Users/you/code/shared-lib")).toBeNull());
  expect(onChanged).toHaveBeenCalledTimes(2);
});

it.each([
  ["relative/path", "需要一个已存在的文件夹的绝对路径"],
  ["/Users/you/code/site/sub", "这个文件夹已经在当前工作区里"],
  ["/Users/you", "这个文件夹范围太大"],
  ["/Users/you/.ssh/keys", "这个文件夹含有或位于密钥"],
  ["/var/root/stuff", "这个文件夹不属于当前用户"],
  ["/tmp", "和已有的可写位置"],
])("explains a refused folder %s in words and keeps what was typed", async (path, words) => {
  const { onChanged } = draw();
  const input = await screen.findByLabelText(t("session.folder_path_to_add"));
  await userEvent.type(input, path);
  await userEvent.click(screen.getByRole("button", { name: t("common.add") }));
  expect((await screen.findByRole("alert")).textContent).toContain(words);
  expect((input as HTMLInputElement).value).toBe(path);
  expect(onChanged).not.toHaveBeenCalled();
});

it("says so when the kernel is busy, and when other panes will take the change later", async () => {
  const port = new MockPort() as unknown as AgentPort;
  vi.spyOn(port, "addWorkspaceRoot").mockRejectedValueOnce(new HttpError(409, "busy", { code: "workspace_roots.busy" }))
    .mockResolvedValueOnce({ primary: "/Users/you/code/site", extra: ["/x/y"], pending: 2 });
  draw(port);
  const input = await screen.findByLabelText(t("session.folder_path_to_add"));
  await userEvent.type(input, "/x/y");
  await userEvent.click(screen.getByRole("button", { name: t("common.add") }));
  expect((await screen.findByRole("alert")).textContent).toContain("当前还有任务在运行");
  await userEvent.click(screen.getByRole("button", { name: t("common.add") }));
  expect(await screen.findByText(/还有 2 个窗格正在运行/)).toBeTruthy();
  expect(screen.queryByRole("alert")).toBeNull();
});

// Granting write access is the Add button's decision, not the dialog's.
it("puts the folder chosen in the system picker in the field and grants nothing until Add", async () => {
  const port = new MockPort() as unknown as AgentPort;
  vi.spyOn(port, "pickFolder").mockResolvedValue("/Users/you/code/picked");
  const added = vi.spyOn(port, "addWorkspaceRoot");
  draw(port);
  await userEvent.click(await screen.findByRole("button", { name: t("common.choose_a_folder") }));
  expect((screen.getByLabelText(t("session.folder_path_to_add")) as HTMLInputElement).value).toBe("/Users/you/code/picked");
  expect(added).not.toHaveBeenCalled();
  await userEvent.click(screen.getByRole("button", { name: t("common.add") }));
  expect(await screen.findByText("/Users/you/code/picked")).toBeTruthy();
});

it("lists the path the kernel recorded, not the spelling that was typed", async () => {
  draw();
  const input = await screen.findByLabelText(t("session.folder_path_to_add"));
  await userEvent.type(input, "/Users/you/code/shared-lib/");
  await userEvent.click(screen.getByRole("button", { name: t("common.add") }));
  expect(await screen.findByText("/Users/you/code/shared-lib")).toBeTruthy();
  await userEvent.type(input, "/Users/you/link");
  await userEvent.click(screen.getByRole("button", { name: t("common.add") }));
  expect(await screen.findByText("/Users/you/real")).toBeTruthy();
  expect(screen.queryByText("/Users/you/link")).toBeNull();
});

it("states the consequence of adding and of removing", async () => {
  draw();
  expect(await screen.findByText("添加后，agent 将能修改并删除该文件夹里的任何文件。")).toBeTruthy();
  const input = await screen.findByLabelText(t("session.folder_path_to_add"));
  await userEvent.type(input, "/Users/you/code/x");
  await userEvent.click(screen.getByRole("button", { name: t("common.add") }));
  expect(await screen.findByText(/移除后，已经打开的窗格/)).toBeTruthy();
});

it("shows a retry instead of vanishing when the folders cannot be read", async () => {
  const port = new MockPort() as unknown as AgentPort;
  vi.spyOn(port, "workspaceRoots").mockRejectedValueOnce(new Error("down")).mockResolvedValue({ primary: "/Users/you/code/site", extra: [] });
  draw(port);
  expect(await screen.findByText(t("session.could_not_read_this_projects"))).toBeTruthy();
  await userEvent.click(screen.getByRole("button", { name: t("common.retry") }));
  expect(await screen.findByText(t("session.project_folders"))).toBeTruthy();
});
