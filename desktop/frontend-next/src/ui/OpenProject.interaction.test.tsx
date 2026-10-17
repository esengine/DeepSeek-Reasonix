// @vitest-environment jsdom
import "./testkit";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { OpenProject } from "./OpenProject";

afterEach(cleanup);

function draw() {
  const add = vi.fn();
  const onOpen = vi.fn().mockResolvedValue(undefined);
  const onClose = vi.fn();
  const remoteDirs = vi.fn().mockResolvedValue({ path: "/srv/project", folders: [] });
  render(<OpenProject hub={{ remoteDirs } as never} adder={{ add } as never}
    hosts={[{ name: "gpu", target: "dev@gpu", status: "idle" }]} onOpen={onOpen} onClose={onClose} onError={vi.fn()} />);
  return { add, onOpen, onClose, remoteDirs };
}

it("keeps local folder selection behind the shared project entry", async () => {
  const { add, onClose } = draw();
  await userEvent.click(screen.getByRole("button", { name: "选择本机文件夹" }));
  expect(onClose).toHaveBeenCalledOnce();
  expect(add).toHaveBeenCalledOnce();
});

it("chooses a remote directory before opening the project on its own host", async () => {
  const { onOpen, remoteDirs } = draw();
  await userEvent.click(screen.getByRole("button", { name: "gpu · SSH" }));
  await userEvent.click(await screen.findByRole("button", { name: "使用此目录" }));
  expect(remoteDirs).toHaveBeenCalledWith("gpu", "");
  expect(onOpen).toHaveBeenCalledWith("gpu", "/srv/project");
});

it("focuses a location and dismisses with Escape without opening anything", async () => {
  const { add, onOpen, onClose } = draw();
  expect(document.activeElement).toBe(screen.getByRole("button", { name: "选择本机文件夹" }));
  await userEvent.keyboard("{Escape}");
  expect(onClose).toHaveBeenCalledOnce();
  expect(add).not.toHaveBeenCalled();
  expect(onOpen).not.toHaveBeenCalled();
});

it("handles Escape once after moving into the SSH directory picker", async () => {
  const { onOpen, onClose } = draw();
  await userEvent.click(screen.getByRole("button", { name: "gpu · SSH" }));
  await screen.findByRole("button", { name: "使用此目录" });
  await userEvent.keyboard("{Escape}");
  expect(onClose).toHaveBeenCalledOnce();
  expect(onOpen).not.toHaveBeenCalled();
});
