import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { Mcp } from "./Mcp";
import type { McpEntry } from "../../port/port";

const SECRET = "dial tcp 127.0.0.1:8931: connectex: No connection could be made";

const entry = (over: Partial<McpEntry> = {}): McpEntry =>
  ({ name: "figma", state: "failed", error: SECRET, ...over }) as McpEntry;

const rail = (servers: McpEntry[]) =>
  renderToStaticMarkup(<Mcp servers={servers} onOpen={() => {}} />);

describe("the external services block on the rail", () => {
  // The endpoint writes for a terminal. Settings prints it beside the switch
  // and the retry; here it only crowds out the one thing that can be done.
  it("never prints the endpoint's error text", () => {
    expect(rail([entry()])).not.toContain(SECRET);
  });

  it("names the way out on every broken row", () => {
    const html = rail([entry(), entry({ name: "linear" })]);
    expect(html).toContain("figma");
    expect(html).toContain("linear");
    expect(html.match(/去修复/g)).toHaveLength(2);
  });

  // A row that can be pressed and does not say so reads as a dead status line,
  // so the way out is printed, not left to a hover.
  it("keeps the row pressable and titled with where it goes", () => {
    const html = rail([entry()]);
    expect(html).toContain("<button");
    expect(html).toContain("在设置的 MCP 面板中修复");
  });

  it("draws nothing while every service is answering", () => {
    expect(rail([entry({ state: "ready", error: undefined })])).toBe("");
  });
});
