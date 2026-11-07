// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { useState } from "react";
import "./testkit";
import { SettingDeep, SettingDeepContext } from "./SettingDeep";

afterEach(cleanup);

function Frame({ go = vi.fn() }: { go?: (id: string | null) => void }) {
  const [open, setOpen] = useState<string | null>(null);
  return (
    <SettingDeepContext.Provider value={{ pages: true, open, section: "权限与安全", go: (id) => { go(id); setOpen(id); } }}>
      <SettingDeep id="rules" title="明确的规则" hint="哪些操作始终禁止" now="3 条"><p>规则内容</p></SettingDeep>
      <SettingDeep id="approval" title="工具批准"><p>批准内容</p></SettingDeep>
    </SettingDeepContext.Provider>
  );
}

it("shows a block the catalogue gives a page as one row, and mounts its contents only once opened", () => {
  render(<Frame />);
  expect(screen.queryByText("规则内容")).toBeNull();
  expect(screen.getByText("批准内容")).toBeTruthy();
  const row = screen.getByRole("button", { name: /明确的规则/ });
  expect(row.textContent).toContain("3 条");
  fireEvent.click(row);
  expect(screen.getByText("规则内容")).toBeTruthy();
  expect(screen.getByRole("navigation", { name: "当前位置" }).textContent).toContain("权限与安全/明确的规则");
});

it("goes back to the section from the breadcrumb", () => {
  const go = vi.fn();
  render(<Frame go={go} />);
  fireEvent.click(screen.getByRole("button", { name: /明确的规则/ }));
  fireEvent.click(screen.getByRole("button", { name: "权限与安全" }));
  expect(go).toHaveBeenLastCalledWith(null);
  expect(screen.queryByText("规则内容")).toBeNull();
});

it("draws the block in place outside the settings screen", () => {
  render(<SettingDeep id="rules" title="明确的规则"><p>规则内容</p></SettingDeep>);
  expect(screen.getByText("规则内容")).toBeTruthy();
});

it("opens the page a landing belongs to: the block's own, or the one it is drawn on", async () => {
  const { pageFor } = await import("./prefsnav");
  expect(pageFor("rules")).toBe("rules");
  expect(pageFor("persona")).toBe("agent-presets");
  expect(pageFor("composition-picker")).toBe("agent-presets");
  expect(pageFor("approval")).toBeNull();
});

it("draws a block that names its action as a setting row with a pill, and opens the same page", () => {
  const go = vi.fn();
  render(
    <SettingDeepContext.Provider value={{ pages: true, open: null, section: "权限与安全", go }}>
      <SettingDeep id="rules" title="明确的规则" hint="哪些操作始终禁止" act="编辑规则（3 条）"><p>规则内容</p></SettingDeep>
    </SettingDeepContext.Provider>,
  );
  const row = document.querySelector('.srow[data-setting="rules"]')!;
  expect(row.querySelector(".srow-name")?.firstChild?.textContent).toBe("明确的规则");
  expect(row.querySelector(".srow-name .scope-tag")?.textContent).toBe("重建运行时");
  expect(row.querySelector(".srow-desc")?.textContent).toBe("哪些操作始终禁止");
  expect(row.querySelector(".apply")?.textContent).toContain("本机");
  fireEvent.click(screen.getByRole("button", { name: "编辑规则（3 条）" }));
  expect(go).toHaveBeenCalledWith("rules");
  expect(screen.queryByText("规则内容")).toBeNull();
});
