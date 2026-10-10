// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Settings } from "./Settings";
import { HttpError, type AgentPort, type SessionStatus } from "../port/port";
import { MockPort } from "../port/mock";
import { MockHub } from "../port/mock_hub";

afterEach(() => { cleanup(); vi.restoreAllMocks(); });

function draw(port: AgentPort) {
  render(
    <Settings
      hub={new MockHub() as never}
      port={port}
      status={{ preset: "balanced", toolApprovalMode: "ask" } as SessionStatus}
      theme="light" onTheme={() => {}} contrast="" onContrast={() => {}} weight="" onWeight={() => {}}
      look={{} as never} onLook={() => {}} reloadThemes={() => {}}
      onClose={() => {}} onChanged={() => {}} onError={() => {}}
      at="model"
      account={null} accountUnread="" reloadAccount={() => {}}
    />,
  );
}

it("a role saved while the conversation is busy stays shown, since the kernel wrote it", async () => {
  const port = new MockPort() as unknown as AgentPort;
  await port.setRole("subagent", "");
  const real = port.setRole.bind(port);
  vi.spyOn(port, "setRole").mockImplementation(async (role, ref) => {
    await real(role, ref);
    throw new HttpError(409, "saved", { code: "runtime.saved_while_running", error: "saved" });
  });
  draw(port);
  const select = (await screen.findByRole("combobox", { name: "子代理" })) as HTMLSelectElement;
  expect(select.value).toBe("");
  const other = Array.from(select.options).find((o) => o.value)!.value;
  await userEvent.selectOptions(select, other);
  await waitFor(() => expect(port.setRole).toHaveBeenCalledWith("subagent", other));
  await waitFor(() => expect((screen.getByRole("combobox", { name: "子代理" }) as HTMLSelectElement).value).toBe(other));
});

it("says a role saved while busy is saved and not applied yet, as the provider form does", async () => {
  const port = new MockPort() as unknown as AgentPort;
  await port.setRole("subagent", "");
  const real = port.setRole.bind(port);
  vi.spyOn(port, "setRole").mockImplementation(async (role, ref) => {
    await real(role, ref);
    throw new HttpError(409, "saved", { code: "runtime.saved_while_running", error: "saved" });
  });
  draw(port);
  const select = (await screen.findByRole("combobox", { name: "子代理" })) as HTMLSelectElement;
  await userEvent.selectOptions(select, Array.from(select.options).find((o) => o.value)!.value);
  const note = (await screen.findByText("已保存，尚未生效")).closest(".find")!;
  expect(note.getAttribute("data-lvl")).toBe("warn");
  expect(note.getAttribute("role")).toBe("status");
  expect(screen.queryByRole("alert")).toBeNull();
  expect(screen.queryByText("操作未完成")).toBeNull();
});

it("a role saved while idle shows no notice and keeps the new value", async () => {
  const port = new MockPort() as unknown as AgentPort;
  await port.setRole("subagent", "");
  draw(port);
  const select = (await screen.findByRole("combobox", { name: "子代理" })) as HTMLSelectElement;
  const other = Array.from(select.options).find((o) => o.value)!.value;
  await userEvent.selectOptions(select, other);
  await waitFor(() => expect((screen.getByRole("combobox", { name: "子代理" }) as HTMLSelectElement).value).toBe(other));
  expect(screen.queryByText("已保存，尚未生效")).toBeNull();
  expect(screen.queryByRole("alert")).toBeNull();
});

it("a really refused role write is still a failure, not a saved notice", async () => {
  const port = new MockPort() as unknown as AgentPort;
  vi.spyOn(port, "setRole").mockRejectedValue(new HttpError(400, "no", { code: "roles.model_unknown", error: "no" }));
  draw(port);
  const select = (await screen.findByRole("combobox", { name: "子代理" })) as HTMLSelectElement;
  await userEvent.selectOptions(select, Array.from(select.options).find((o) => o.value)!.value);
  const alert = await screen.findByRole("alert");
  expect(alert.textContent).toContain("操作未完成");
  expect(screen.queryByText("已保存，尚未生效")).toBeNull();
});

it("saving one role while busy leaves the other rows and a reopened page on the saved values", async () => {
  const port = new MockPort() as unknown as AgentPort;
  await port.setRole("subagent", "");
  await port.setRole("planner", "");
  const real = port.setRole.bind(port);
  vi.spyOn(port, "setRole").mockImplementation(async (role, ref) => {
    await real(role, ref);
    throw new HttpError(409, "saved", { code: "runtime.saved_while_running", error: "saved" });
  });
  draw(port);
  const select = (await screen.findByRole("combobox", { name: "子代理" })) as HTMLSelectElement;
  const other = Array.from(select.options).find((o) => o.value)!.value;
  await userEvent.selectOptions(select, other);
  await screen.findByText("已保存，尚未生效");
  expect((screen.getByRole("combobox", { name: "计划" }) as HTMLSelectElement).value).toBe("");
  cleanup();
  draw(port);
  await waitFor(async () => expect(((await screen.findByRole("combobox", { name: "子代理" })) as HTMLSelectElement).value).toBe(other));
});

it("a default model switch refused while busy leaves the row on the kernel's default", async () => {
  const port = new MockPort() as unknown as AgentPort;
  vi.spyOn(port, "setModel").mockRejectedValue(new HttpError(409, "busy", { code: "busy.switch_model", error: "busy" }));
  draw(port);
  const select = (await screen.findByRole("combobox", { name: "默认模型" })) as HTMLSelectElement;
  const before = select.value;
  const other = Array.from(select.options).find((o) => o.value && o.value !== before)!.value;
  await userEvent.selectOptions(select, other);
  await screen.findByRole("alert");
  expect((screen.getByRole("combobox", { name: "默认模型" }) as HTMLSelectElement).value).toBe(before);
});

it("a refused role write leaves the row on what the kernel still holds", async () => {
  const port = new MockPort() as unknown as AgentPort;
  vi.spyOn(port, "setRole").mockRejectedValue(new HttpError(400, "no", { code: "roles.model_unknown", error: "no" }));
  draw(port);
  const select = (await screen.findByRole("combobox", { name: "子代理" })) as HTMLSelectElement;
  const before = select.value;
  const other = Array.from(select.options).find((o) => o.value && o.value !== before)!.value;
  await userEvent.selectOptions(select, other);
  await waitFor(() => expect(port.setRole).toHaveBeenCalled());
  expect((screen.getByRole("combobox", { name: "子代理" }) as HTMLSelectElement).value).toBe(before);
});
