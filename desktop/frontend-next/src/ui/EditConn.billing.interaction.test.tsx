// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { EditConn } from "./EditConn";
import { MockPort } from "../port/mock";
import type { ProviderEdit, ProviderEntry } from "../port/port";

afterEach(() => { cleanup(); vi.restoreAllMocks(); });

const entry = (over: Partial<ProviderEntry> = {}): ProviderEntry => ({
  name: "gw", kind: "openai", baseUrl: "https://gw.example/v1", models: ["alpha"], default: "alpha", hasKey: true,
  keyEnv: "GW_KEY", inUse: false, preset: false, ...over,
} as ProviderEntry);

async function open(e: ProviderEntry) {
  const port = new MockPort();
  const edit = vi.spyOn(port, "editProvider").mockResolvedValue(undefined as never);
  render(<EditConn entry={e} port={port as never} busy="" setBusy={() => {}} onDone={() => {}} onRevert={() => {}} />);
  await screen.findByText("按模型设置价格");
  return { edit };
}
const mode = () => screen.getByRole<HTMLSelectElement>("combobox", { name: "计费方式" });
const save = () => screen.getByRole<HTMLButtonElement>("button", { name: /保存/ });

describe("how a connection is billed", () => {
  it("reads pay-as-you-go when the kernel names no mode, and sends nothing for it", async () => {
    const { edit } = await open(entry());
    expect(mode().value).toBe("payg");
    await userEvent.type(screen.getByRole("textbox", { name: "最大输出" }), "100");
    await userEvent.click(save());
    await waitFor(() => expect(edit).toHaveBeenCalled());
    expect("billingMode" in (edit.mock.calls[0]![0] as ProviderEdit)).toBe(false);
  });

  it("sends the chosen mode only when it differs from the saved one", async () => {
    const { edit } = await open(entry({ billingMode: "payg" }));
    await userEvent.selectOptions(mode(), "subscription_equivalent");
    await userEvent.click(save());
    await waitFor(() => expect(edit).toHaveBeenCalled());
    expect((edit.mock.calls[0]![0] as ProviderEdit).billingMode).toBe("subscription_equivalent");
  });

  it("shows a saved subscription and makes switching back a change", async () => {
    const { edit } = await open(entry({ billingMode: "subscription_equivalent" }));
    expect(mode().value).toBe("subscription_equivalent");
    expect(save().disabled).toBe(true);
    await userEvent.selectOptions(mode(), "payg");
    expect(save().disabled).toBe(false);
    await userEvent.click(save());
    await waitFor(() => expect(edit).toHaveBeenCalled());
    expect((edit.mock.calls[0]![0] as ProviderEdit).billingMode).toBe("payg");
  });

  it("says what the subscription figures mean", async () => {
    await open(entry());
    expect(screen.getByText("订阅套餐的用量按上面的价格折算成估算值，单独显示，不会计入实际花费。")).toBeTruthy();
  });
});
