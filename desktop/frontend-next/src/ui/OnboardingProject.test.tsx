// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { OnboardingProject } from "./OnboardingProject";

afterEach(() => { cleanup(); vi.restoreAllMocks(); });

function phone(pick = vi.fn(async (): Promise<string | null> => null)) {
  vi.spyOn(window, "matchMedia").mockImplementation((query) => ({ matches: query === "(pointer: coarse)" }) as MediaQueryList);
  const project = { current: "/data/user/0/reasonix/files/workspace", pick, add: vi.fn(async (_dir: string) => {}) };
  const onDone = vi.fn();
  render(<OnboardingProject project={project} onDone={onDone} onSkip={vi.fn()} />);
  return { project, onDone, user: userEvent.setup() };
}

it("lets a phone start in the current workspace without adding it again", async () => {
  const { user, project, onDone } = phone();
  expect(screen.queryByText("把文件夹拖到这里")).toBeNull();
  await user.click(screen.getByRole("button", { name: "开始" }));
  await waitFor(() => expect(onDone).toHaveBeenCalledOnce());
  expect(project.add).not.toHaveBeenCalled();
});

it("keeps the current workspace after cancelling the phone picker", async () => {
  const { user, project, onDone } = phone();
  await user.click(screen.getByRole("button", { name: "换一个…" }));
  expect(project.pick).toHaveBeenCalledOnce();
  expect(screen.queryByRole("alert")).toBeNull();
  await user.click(screen.getByRole("button", { name: "开始" }));
  await waitFor(() => expect(onDone).toHaveBeenCalledOnce());
});

it("opens the selected phone folder and lets a failed open be retried", async () => {
  const { user, project, onDone } = phone(vi.fn(async () => "/storage/emulated/0/Documents/demo"));
  project.add.mockRejectedValueOnce(new Error("Cannot open this folder"));
  await user.click(screen.getByRole("button", { name: "换一个…" }));
  await screen.findByText("demo");
  await user.click(screen.getByRole("button", { name: "开始" }));
  await screen.findByRole("alert");
  expect(onDone).not.toHaveBeenCalled();
  await user.click(screen.getByRole("button", { name: "开始" }));
  await waitFor(() => expect(onDone).toHaveBeenCalledOnce());
  expect(project.add).toHaveBeenLastCalledWith("/storage/emulated/0/Documents/demo");
});
