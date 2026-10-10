// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Packages } from "./Packages";
import { MockPort } from "../port/mock";
import { boot, STORAGE } from "../i18n";
import type { AgentPort, PluginPackage } from "../port/port";

afterEach(() => { cleanup(); localStorage.setItem(STORAGE, "zh"); boot(); });

const categories = [
  { field: "skills", zh: "技能贡献", en: "Skills" },
  { field: "commands", zh: "命令贡献", en: "Commands" },
  { field: "agents", zh: "子代理贡献", en: "Subagents" },
  { field: "prompts", zh: "提示词贡献", en: "Prompts" },
  { field: "themes", zh: "主题贡献", en: "Themes" },
] as const;

async function fixture(lang: "zh" | "en") {
  localStorage.setItem(STORAGE, lang);
  boot();
  const port = new MockPort() as unknown as AgentPort;
  const [pkg] = await port.plugins();
  const mixed: PluginPackage = { ...pkg, skills: [], commands: [], agents: [], prompts: [], themes: [] };
  for (const { field } of categories) mixed[field] = [{ name: "shared", description: field + " description", invocation: field === "themes" ? undefined : "/" + field + ":shared" }];
  return { port, pkg: mixed, onChanged: vi.fn(), updating: "", onUpdate: vi.fn() };
}

function packageRow() {
  return within(document.querySelector('[data-extension-name="review-kit"]') as HTMLElement);
}

describe("installed package contribution categories", () => {
 for (const lang of ["zh", "en"] as const) {
  it.each([true, false])(`identifies same-name contribution kinds in ${lang}, enabled=%s`, async (enabled) => {
   const { pkg, ...props } = await fixture(lang);
   render(<Packages {...props} packages={[{ ...pkg, enabled }]} />);
   const row = packageRow();
   const summary = row.getByText("review-kit").closest("summary") as HTMLElement;
   await userEvent.click(summary);
   expect(summary.closest("details")?.open).toBe(true);
   for (const category of categories) {
    const label = category[lang];
    const group = within(row.getByRole("group", { name: label }));
    expect(group.getByText(label)).toBeTruthy();
    expect(group.getByText(category.field + " description")).toBeTruthy();
    for (const other of categories) if (other.field !== category.field) expect(group.queryByText(other.field + " description")).toBeNull();
    expect(group.getByText(category.field === "themes" ? "shared" : "/" + category.field + ":shared")).toBeTruthy();
   }
   expect(row.getAllByRole("group")).toHaveLength(5);
   expect(props.onChanged).not.toHaveBeenCalled();
   expect(props.onUpdate).not.toHaveBeenCalled();
  });

  it(`removes empty groups and follows inventory updates in ${lang}`, async () => {
   const { pkg, ...props } = await fixture(lang);
   const view = render(<Packages {...props} packages={[pkg]} />);
   const row = packageRow();
   const summary = row.getByText("review-kit").closest("summary") as HTMLElement;
   await userEvent.click(summary);
   for (const category of categories) expect(row.getByRole("group", { name: category[lang] })).toBeTruthy();
   const updated = { ...pkg, skills: [], commands: undefined, agents: [], prompts: [{ name: "next", description: "Current prompt" }], themes: undefined };
   view.rerender(<Packages {...props} packages={[updated]} />);
   const prompts = within(row.getByRole("group", { name: categories[3][lang] }));
   expect(prompts.getByText("next")).toBeTruthy();
   expect(prompts.getByText("Current prompt")).toBeTruthy();
   expect(row.getAllByRole("group")).toHaveLength(1);
   expect(summary.closest("details")?.open).toBe(true);
   for (const category of categories) expect(row.queryByText(category.field + " description")).toBeNull();
   view.rerender(<Packages {...props} packages={[{ ...updated, prompts: [] }]} />);
   expect(row.queryAllByRole("group")).toHaveLength(0);
   expect(row.queryByText("Current prompt")).toBeNull();
   expect(summary.closest("details")?.open).toBe(true);
  });
 }

 it("leaves absent categories empty without inventing readiness or calls", async () => {
  const { pkg, ...props } = await fixture("zh");
  render(<Packages {...props} packages={[{ ...pkg, skills: [], commands: undefined, agents: undefined, prompts: [], themes: [] }]} />);
  const row = packageRow();
  await userEvent.click(row.getByText("review-kit").closest("summary") as HTMLElement);
  expect(row.queryAllByRole("group")).toHaveLength(0);
  expect(row.queryByText("可使用")).toBeNull();
  expect(props.onChanged).not.toHaveBeenCalled();
 });
});
