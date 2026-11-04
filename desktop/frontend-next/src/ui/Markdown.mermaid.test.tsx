// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { Markdown } from "./Markdown";

const engine = vi.hoisted(() => ({ render: vi.fn(async () => '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 100"><text>Browser → App</text></svg>') }));
vi.mock("./mermaid", () => ({ renderMermaid: engine.render }));
afterEach(() => { cleanup(); vi.clearAllMocks(); });
const source = "flowchart TD\n  Browser --> App\n";
const fence = (s: string) => "```mermaid\n" + s + "```";

it("routes only Mermaid fences to a lazy preview and keeps source/copy available", async () => {
  const user = userEvent.setup();
  const write = vi.spyOn(navigator.clipboard, "writeText").mockResolvedValue();
  const { container } = render(<Markdown text={fence(source) + "\n\n```go\nfunc main() {}\n```\n\n**Next paragraph** [Web](https://example.com)"} />);
  expect(await screen.findByRole("img", { name: "Mermaid 图表" })).toBeTruthy();
  expect(engine.render).toHaveBeenCalledWith(source, "light");
  await user.click(screen.getByRole("button", { name: "源代码" }));
  expect(container.querySelector(".mermaid-source")?.textContent).toBe(source);
  await user.click(screen.getByRole("button", { name: "复制 Mermaid 源代码" }));
  expect(write).toHaveBeenCalledWith(source);
  await user.click(screen.getByRole("button", { name: "预览" }));
  expect(screen.getByRole("img", { name: "Mermaid 图表" })).toBeTruthy();
  expect(container.querySelector('.code-wrap[data-lang="go"] pre')?.textContent).toBe("func main() {}\n");
  expect(container.querySelector("strong")?.textContent).toBe("Next paragraph");
  expect(container.querySelector('a[href="https://example.com"]')?.getAttribute("rel")).toBe("noreferrer noopener");
});

it("keeps invalid definitions readable and can recover when the source changes", async () => {
  engine.render.mockRejectedValueOnce(new Error("Parse error on line 2"));
  const { container, rerender } = render(<Markdown text={fence("not a diagram\n")} />);
  expect((await screen.findByRole("alert")).textContent).toContain("Parse error on line 2");
  expect(container.querySelector(".mermaid-source")?.textContent).toBe("not a diagram\n");
  rerender(<Markdown text={fence(source)} />);
  expect(await screen.findByRole("img", { name: "Mermaid 图表" })).toBeTruthy();
});

it("defers rendering the live tail and follows document theme changes", async () => {
  const { rerender, container } = render(<Markdown text={fence(source)} streaming />);
  expect(engine.render).not.toHaveBeenCalled();
  expect(container.textContent).toContain("Browser");
  rerender(<Markdown text={fence(source)} />);
  await screen.findByRole("img", { name: "Mermaid 图表" });
  document.documentElement.dataset.theme = "dark";
  await waitFor(() => expect(engine.render).toHaveBeenLastCalledWith(source, "dark"));
  delete document.documentElement.dataset.theme;
});
