// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import type { ModelEntry, ProviderEntry } from "../port/port";
import type { Port } from "./Providers";
import { EditConn } from "./EditConn";
import { ModelLimits } from "./ModelLimits";
import { ModelUsage } from "./ModelUsage";
import { modelMenu } from "./modelmenu";

afterEach(cleanup);

const routed = [
  { ref: "mag/group/deepseek-v4.1-flash", provider: "mag", vendor: "m.example", model: "group/deepseek-v4.1-flash", modelShort: "deepseek-v4.1-flash", kind: "openai" },
  { ref: "mag/group/glm-5", provider: "mag", vendor: "m.example", model: "group/glm-5", modelShort: "glm-5", modelAlias: "My GLM", kind: "openai" },
] as ModelEntry[];

it("draws the model menu and the usage list without the shared prefix, a typed alias still first, and keeps the ref exact", () => {
  const items = modelMenu(routed);
  const rows = items.filter((it) => !it.header && !it.plain);
  expect(rows.map((r) => r.label)).toEqual(["deepseek-v4.1-flash", "My GLM"]);
  expect(rows.map((r) => r.value)).toEqual(["mag/group/deepseek-v4.1-flash", "mag/group/glm-5"]);
  expect(rows[0].desc).toBeUndefined();
  expect(rows[1].desc).toBe("group/glm-5");
  render(<ModelUsage models={routed} roles={null} main="mag/group/deepseek-v4.1-flash" busy="" protocol={{}} onMain={vi.fn()} onRole={vi.fn()} />);
  const options = [...(screen.getByRole("combobox", { name: "默认模型" }) as HTMLSelectElement).options].map((o) => o.textContent);
  expect(options).toEqual(["deepseek-v4.1-flash", "My GLM"]);
});

it("shows the short name on a limits row and keeps the full id in the tooltip", () => {
  render(<ModelLimits models={["group/deepseek-v4.1-flash", "other"]} value={{}} onChange={vi.fn()} inherited={() => null}
    labels={{ "group/deepseek-v4.1-flash": "deepseek-v4.1-flash" }} />);
  const row = document.querySelector('[data-model="group/deepseek-v4.1-flash"]')!;
  const name = within(row as HTMLElement).getByText(/deepseek/);
  expect(name.textContent).toBe("deepseek-v4.1-flash");
  expect(name.getAttribute("title")).toBe("group/deepseek-v4.1-flash");
  expect(document.querySelector('[data-model="other"] .me-name')?.textContent).toBe("other");
});

function drawEdit(entry: Partial<ProviderEntry> = {}) {
  const editProvider = vi.fn().mockResolvedValue(undefined);
  const full = {
    name: "mag", kind: "openai", baseUrl: "https://m.example/v1",
    models: ["group/a", "group/b"], default: "group/a", hasKey: true, inUse: false, preset: false, ...entry,
  } as ProviderEntry;
  const port = { editProvider, protocols: vi.fn(async () => []), checkProvider: vi.fn(async () => ({ ok: true, models: [] })) } as unknown as Port;
  render(<EditConn entry={full} port={port} busy="" setBusy={() => {}} onDone={vi.fn()} onRevert={() => {}} />);
  return { editProvider, group: screen.getByRole("group", { name: "mag 的模型名前缀" }) };
}

it("starts off, turns on with a click, and sends the choice with the model ids untouched", async () => {
  const { editProvider, group } = drawEdit();
  expect(within(group).getByRole("button", { name: "关" }).getAttribute("aria-pressed")).toBe("true");
  await userEvent.click(within(group).getByRole("button", { name: "开" }));
  await userEvent.click(screen.getByRole("button", { name: /保存/ }));
  expect(editProvider.mock.calls[0]?.[0]).toMatchObject({ hideModelPrefix: true, models: ["group/a", "group/b"] });
});

it("shows a stored choice as on and sends it off when turned off", async () => {
  const { editProvider, group } = drawEdit({ hideModelPrefix: true, modelShorts: { "group/a": "a", "group/b": "b" } });
  expect(within(group).getByRole("button", { name: "开" }).getAttribute("aria-pressed")).toBe("true");
  await userEvent.click(within(group).getByRole("button", { name: "关" }));
  await userEvent.click(screen.getByRole("button", { name: /保存/ }));
  expect(editProvider.mock.calls[0]?.[0]).toMatchObject({ hideModelPrefix: false });
});
