// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { HttpError, type ProviderEntry } from "../port/port";
import type { Port } from "./Providers";
import { EditConn } from "./EditConn";

afterEach(cleanup);

it("keeps vision capability discovered while refreshing a saved source", async () => {
  const visionModel = "deepseek-flash-vision-exp";
  const editProvider = vi.fn(async () => {});
  const port = {
    checkProvider: vi.fn(async () => ({
      ok: true,
      models: ["deepseek-flash", visionModel],
      vision: [visionModel],
    })),
    editProvider,
  } as unknown as Port;
  const entry: ProviderEntry = {
    name: "deepseek-flash",
    kind: "responses",
    baseUrl: "https://api.deepseek.com",
    models: ["deepseek-flash"],
    default: "deepseek-flash",
    hasKey: true,
    inUse: false,
    preset: true,
    canSetVision: false,
    visionModels: [],
    visionSettable: [],
  };

  render(<EditConn entry={entry} port={port} busy="" setBusy={() => {}} onDone={() => {}} onRevert={() => {}} />);
  await userEvent.click(screen.getByRole("button", { name: "从服务商读取可用模型" }));

  const modelName = await screen.findByText(visionModel);
  const row = modelName.closest(".mline") as HTMLElement;
  await userEvent.click(within(row).getByRole("checkbox"));
  const vision = within(row).getByRole("button", { name: `${visionModel} 的图片输入` });
  expect((vision as HTMLButtonElement).disabled).toBe(false);
  expect(vision.getAttribute("aria-pressed")).toBe("true");

  await userEvent.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() => expect(editProvider).toHaveBeenCalled());
  expect(editProvider).toHaveBeenCalledWith(expect.objectContaining({ vision: [visionModel] }));
});

it("keeps an exact unlisted model id and verifies it independently of the catalog", async () => {
  const hidden = "deepseek-preview-private-202609";
  const editProvider = vi.fn(async () => {});
  const checkProviderModel = vi.fn(async () => ({ model: hidden, status: "available" as const }));
  const port = { editProvider, checkProviderModel } as unknown as Port;
  const entry: ProviderEntry = {
    name: "deepseek",
    kind: "openai",
    baseUrl: "https://api.deepseek.com",
    models: ["deepseek-chat"],
    default: "deepseek-chat",
    hasKey: true,
    inUse: false,
    preset: true,
    canSetVision: false,
  };

  render(<EditConn entry={entry} port={port} busy="" setBusy={() => {}} onDone={() => {}} onRevert={() => {}} />);
  await userEvent.type(screen.getByRole("searchbox", { name: "搜索或添加模型" }), `${hidden}{enter}`);

  const row = screen.getAllByText(hidden).map((el) => el.closest(".mline")).find(Boolean) as HTMLElement;
  expect(within(row).getByText("用户添加")).toBeTruthy();
  expect(within(row).getByText("待验证")).toBeTruthy();
  await userEvent.click(within(row).getByRole("button", { name: `验证模型 ${hidden}` }));

  await waitFor(() => expect(within(row).getByText("已验证可用")).toBeTruthy());
  expect(checkProviderModel).toHaveBeenCalledWith(expect.objectContaining({ model: hidden }));

  await userEvent.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() => expect(editProvider).toHaveBeenCalled());
  expect(editProvider).toHaveBeenCalledWith(expect.objectContaining({
    models: expect.arrayContaining(["deepseek-chat", hidden]),
  }));
});

it("shows the HTTP status and the endpoint's own words when a model check is refused", async () => {
  const checkProviderModel = vi.fn(async () => ({
    model: "clef:27b",
    status: "unknown" as const,
    reason: "rejected" as const,
    httpStatus: 400,
    detail: "clef:27b does not support tools",
  }));
  const port = { checkProviderModel } as unknown as Port;
  const entry: ProviderEntry = {
    name: "ollama_local",
    kind: "openai",
    baseUrl: "http://localhost:11434/v1",
    models: ["clef:27b"],
    default: "clef:27b",
    hasKey: false,
    inUse: false,
    preset: false,
  };

  render(<EditConn entry={entry} port={port} busy="" setBusy={() => {}} onDone={() => {}} onRevert={() => {}} />);
  const row = screen.getAllByText("clef:27b").map((el) => el.closest(".mline")).find(Boolean) as HTMLElement;
  await userEvent.click(within(row).getByRole("button", { name: "验证模型 clef:27b" }));

  await waitFor(() => expect(within(row).getByText(/请求被拒绝，尚未确认/)).toBeTruthy());
  expect(within(row).getByText("HTTP 400")).toBeTruthy();
  expect(within(row).getByText("clef:27b does not support tools")).toBeTruthy();
});

it("preserves configured models that a refreshed catalog no longer returns", async () => {
  const port = {
    checkProvider: vi.fn(async () => ({ ok: true, models: ["deepseek-new"], vision: [] })),
  } as unknown as Port;
  const entry: ProviderEntry = {
    name: "deepseek",
    kind: "openai",
    baseUrl: "https://api.deepseek.com",
    models: ["deepseek-hidden"],
    default: "deepseek-hidden",
    hasKey: true,
    inUse: false,
    preset: true,
  };

  render(<EditConn entry={entry} port={port} busy="" setBusy={() => {}} onDone={() => {}} onRevert={() => {}} />);
  await userEvent.click(screen.getByRole("button", { name: "从服务商读取可用模型" }));

  expect(await screen.findByText("deepseek-new")).toBeTruthy();
  const preserved = screen.getByText("deepseek-hidden").closest(".mline") as HTMLElement;
  expect(within(preserved).getByText("本次未返回")).toBeTruthy();
  expect(document.querySelector(".mdiff")?.textContent).toContain("有 1 个已配置模型本次未返回，已为你保留。");
});

it("opens on the reasoning fields when sent to declare effort levels, and saves them", async () => {
  const editProvider = vi.fn(async () => {});
  const port = { editProvider } as unknown as Port;
  const entry: ProviderEntry = {
    name: "relay",
    kind: "openai",
    baseUrl: "https://relay.invalid/v1",
    models: ["gpt-x"],
    default: "gpt-x",
    hasKey: true,
    inUse: true,
    preset: false,
  };

  render(<EditConn entry={entry} port={port} busy="" setBusy={() => {}} onDone={() => {}} onRevert={() => {}} declare />);
  const think = screen.getByRole("combobox", { name: /^思考参数/ });
  expect(document.activeElement).toBe(think);

  await userEvent.type(screen.getByRole("textbox", { name: /^推理档位/ }), "Low, medium，auto xhigh");
  await userEvent.selectOptions(screen.getByRole("combobox", { name: /^默认档位/ }), "medium");
  await userEvent.click(screen.getByRole("button", { name: "保存" }));

  await waitFor(() => expect(editProvider).toHaveBeenCalled());
  expect(editProvider).toHaveBeenCalledWith(expect.objectContaining({
    supportedEfforts: ["low", "medium", "xhigh"],
    defaultEffort: "medium",
  }));
});

it("keeps the reasoning fields folded when the form was opened by hand", () => {
  const port = { editProvider: vi.fn() } as unknown as Port;
  const entry: ProviderEntry = {
    name: "relay", kind: "openai", baseUrl: "https://relay.invalid/v1", models: ["gpt-x"],
    default: "gpt-x", hasKey: true, inUse: false, preset: false,
  };
  render(<EditConn entry={entry} port={port} busy="" setBusy={() => {}} onDone={() => {}} onRevert={() => {}} />);
  expect(screen.queryByRole("textbox", { name: /^推理档位/ })).toBeNull();
});

it("refreshes the list when the save landed but the conversation keeps its settings", async () => {
  const editProvider = vi.fn(async () => {
    throw new HttpError(409, "saved", { code: "provider.saved_while_running", error: "saved" });
  });
  const port = { editProvider } as unknown as Port;
  const entry: ProviderEntry = {
    name: "rich", kind: "openai", baseUrl: "https://gateway.invalid/v1",
    models: ["alpha"], default: "alpha", hasKey: true, inUse: true, preset: false, canSetVision: false,
  };
  const onDone = vi.fn();
  const onSaved = vi.fn();

  render(<EditConn entry={entry} port={port} busy="" setBusy={() => {}} onDone={onDone} onRevert={() => {}} onSaved={onSaved} />);
  await userEvent.type(screen.getByLabelText("上下文窗口"), "1");
  await userEvent.click(screen.getByRole("button", { name: "保存" }));

  await waitFor(() => expect(onSaved).toHaveBeenCalledTimes(1));
  expect(onDone).not.toHaveBeenCalled();
  expect(screen.getByText(/已保存。当前对话还有未结束的工作/)).toBeTruthy();
});

const rejecting = (status: number, code: string, error: string) => {
  const editProvider = vi.fn(async () => {
    throw new HttpError(status, error, { code, error });
  });
  const entry: ProviderEntry = {
    name: "rich", kind: "openai", baseUrl: "https://gateway.invalid/v1",
    models: ["alpha"], default: "alpha", hasKey: true, inUse: true, preset: false, canSetVision: false,
  };
  render(<EditConn entry={entry} port={{ editProvider } as unknown as Port} busy="" setBusy={() => {}} onDone={() => {}} onRevert={() => {}} onSaved={() => {}} />);
  return userEvent.type(screen.getByLabelText("上下文窗口"), "1").then(() => userEvent.click(screen.getByRole("button", { name: "保存" })));
};

it("says a refused save failed, as an error alert", async () => {
  await rejecting(400, "provider.endpoint_required", "no endpoint");
  const alert = await screen.findByRole("alert");
  expect(alert.getAttribute("data-lvl")).toBe("err");
  expect(within(alert).getByText("保存失败")).toBeTruthy();
  expect(screen.queryByText("没保存成功")).toBeNull();
});

it("does not call a saved-but-not-applied outcome a failed save", async () => {
  await rejecting(409, "provider.saved_while_running", "saved");
  const note = (await screen.findByText(/已保存。当前对话还有未结束的工作/)).closest(".find")!;
  expect(note.getAttribute("data-lvl")).toBe("warn");
  expect(note.getAttribute("role")).toBe("status");
  expect(screen.queryByRole("alert")).toBeNull();
  expect(screen.queryByText("保存失败")).toBeNull();
});

const idleEntry = (idleTimeoutSeconds?: number): ProviderEntry => ({
  name: "rich", kind: "openai", baseUrl: "https://gateway.invalid/v1",
  models: ["alpha"], default: "alpha", hasKey: true, inUse: false, preset: false, canSetVision: false,
  idleTimeoutSeconds, idleTimeoutDefault: 300,
});

async function openIdle(entry: ProviderEntry) {
  const editProvider = vi.fn(async () => {});
  render(<EditConn entry={entry} port={{ editProvider } as unknown as Port} busy="" setBusy={() => {}} onDone={() => {}} onRevert={() => {}} />);
  await userEvent.click(screen.getByText("思考参数与推理档位"));
  return { editProvider, field: (await screen.findByLabelText("无响应超时")) as HTMLInputElement };
}

it("shows the stored no-answer timeout in the advanced section and leaves it unchanged when untouched", async () => {
  const { editProvider, field } = await openIdle(idleEntry(90));
  expect(field.value).toBe("90");
  await userEvent.type(screen.getByLabelText("上下文窗口"), "1");
  await userEvent.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() => expect(editProvider).toHaveBeenCalledWith(expect.objectContaining({ idleTimeoutSeconds: 90 })));
});

it("sends an edited no-answer timeout through the provider edit path", async () => {
  const { editProvider, field } = await openIdle(idleEntry(90));
  await userEvent.clear(field);
  await userEvent.type(field, "600");
  await userEvent.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() => expect(editProvider).toHaveBeenCalledWith(expect.objectContaining({ idleTimeoutSeconds: 600 })));
});

it("sends 0, the default, when the field is emptied", async () => {
  const { editProvider, field } = await openIdle(idleEntry(90));
  expect(field.placeholder).toBe("默认 300");
  await userEvent.clear(field);
  await userEvent.click(screen.getByRole("button", { name: "保存" }));
  await waitFor(() => expect(editProvider).toHaveBeenCalledWith(expect.objectContaining({ idleTimeoutSeconds: 0 })));
});

it("accepts both bounds", async () => {
  for (const edge of ["1", "32767"]) {
    cleanup();
    const { editProvider, field } = await openIdle(idleEntry());
    await userEvent.type(field, edge);
    await userEvent.click(screen.getByRole("button", { name: "保存" }));
    await waitFor(() => expect(editProvider).toHaveBeenCalledWith(expect.objectContaining({ idleTimeoutSeconds: Number(edge) })));
  }
});

it("refuses a no-answer timeout the kernel would refuse, and says why", async () => {
  for (const bad of ["0", "32768", "1.5", "abc"]) {
    cleanup();
    const { editProvider, field } = await openIdle(idleEntry());
    await userEvent.type(field, bad);
    expect(field.getAttribute("aria-invalid")).toBe("true");
    expect(screen.getByText("须是 1 到 32767 之间的整数秒；留空使用默认值。")).toBeTruthy();
    const save = screen.getByRole("button", { name: "保存" }) as HTMLButtonElement;
    expect(save.disabled).toBe(true);
    await userEvent.click(save);
    expect(editProvider).not.toHaveBeenCalled();
  }
});

it("words the kernel's refusal of the timeout with its bounds", async () => {
  const editProvider = vi.fn(async () => {
    throw new HttpError(400, "bad", { code: "provider.bad_idle_timeout", error: "bad", params: { min: 1, max: 32767 } });
  });
  render(<EditConn entry={idleEntry()} port={{ editProvider } as unknown as Port} busy="" setBusy={() => {}} onDone={() => {}} onRevert={() => {}} />);
  await userEvent.type(screen.getByLabelText("上下文窗口"), "1");
  await userEvent.click(screen.getByRole("button", { name: "保存" }));
  expect(await screen.findByText("无响应超时须在 1 到 32767 秒之间；留空使用默认值")).toBeTruthy();
});
