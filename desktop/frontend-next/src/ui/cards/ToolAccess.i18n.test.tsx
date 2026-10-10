// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { boot, STORAGE } from "../../i18n";
import type { Tool } from "../../port/wire";
import { ReadsCard } from "./ReadsCard";
import { NestedCall, ToolCard } from "./ToolCard";

const ZH = "无法读取目标文件：没有权限访问。";
const EN = "Cannot read target file: permission denied.";

afterEach(() => {
  cleanup();
  localStorage.setItem(STORAGE, "zh");
  boot();
});

function show(shape: string, tool: Tool) {
  if (shape === "nested") return render(<NestedCall tool={tool} />);
  if (shape === "grouped") {
    const view = render(<ReadsCard tools={[tool]} />);
    fireEvent.click(view.container.querySelector('[data-call="denied"]')!);
    return view;
  }
  return render(<ToolCard tool={tool} running={false} />);
}

describe.each(["zh", "en"])("file access refusals in %s", (lang) => {
  it.each(["workspace.read_forbidden", "workspace.read_outside_scope"].flatMap((code) =>
    ["single", "nested", "grouped"].map((shape) => [code, shape]),
  ))("localizes %s in the %s card without the raw echo", (code, shape) => {
    localStorage.setItem(STORAGE, lang); boot();
    // The English window also covers a restored, previously Chinese result.
    const raw = lang === "zh" ? EN : ZH;
    const expected = lang === "zh" ? ZH : EN;
    const view = show(shape, { id: "denied", name: "read_file", args: '{"path":"private.txt"}',
      err: raw, output: raw, refusalCode: code, readOnly: true });
    expect(view.container.querySelector("[data-refusal]")?.textContent).toBe(expected);
    expect(screen.queryAllByText(expected)).toHaveLength(1);
    expect(screen.queryByText(raw)).toBeNull();
  });
});

it.each(["single", "nested", "grouped"])("keeps unknown error details in the %s card", (shape) => {
  localStorage.setItem(STORAGE, "en"); boot();
  show(shape, { id: "denied", name: "read_file", args: '{"path":"missing.txt"}',
    err: "unknown failure detail", output: "unknown failure detail", refusalCode: "fixture.unknown", readOnly: true });
  expect(screen.queryAllByText("unknown failure detail")).toHaveLength(1);
});

describe.each(["zh", "en"])("overwrite refusals in %s", (lang) => {
  it.each(["workspace.overwrite_read_forbidden", "workspace.overwrite_read_outside_scope"].flatMap((code) =>
    ["single", "nested", "grouped"].map((shape) => [code, shape]),
  ))("localizes %s in the %s card without the read-only wording", (code, shape) => {
    const zh = "无法覆盖目标文件：没有权限读取原文件内容。";
    const en = "Cannot overwrite target file: permission to read existing contents is required.";
    localStorage.setItem(STORAGE, lang); boot();
    const raw = lang === "zh" ? en : zh;
    const expected = lang === "zh" ? zh : en;
    const view = show(shape, { id: "denied", name: "write_file", args: '{"path":".env"}',
      err: raw, output: raw, refusalCode: code, readOnly: false });
    expect(view.container.querySelector("[data-refusal]")?.textContent).toBe(expected);
    expect(screen.queryAllByText(expected)).toHaveLength(1);
    expect(screen.queryByText(raw)).toBeNull();
  });
});
