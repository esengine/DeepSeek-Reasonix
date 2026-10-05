// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { AddWorkspacePrompt } from "./AddWorkspacePrompt";

afterEach(cleanup);

it("keeps the server path editable and submits it", async () => {
  const submit = vi.fn();
  const close = vi.fn();
  render(<AddWorkspacePrompt busy={false} onSubmit={submit} onClose={close} />);

  expect(screen.getByRole("dialog", { name: "添加工作区" })).toBeTruthy();
  const input = screen.getByRole("textbox", { name: "工作区路径" });
  expect(screen.getByRole("button", { name: "添加" }).hasAttribute("disabled")).toBe(true);
  await userEvent.type(input, "/srv/project");
  await userEvent.click(screen.getByRole("button", { name: "添加" }));

  expect(submit).toHaveBeenCalledWith("/srv/project");
  expect(close).not.toHaveBeenCalled();
});
