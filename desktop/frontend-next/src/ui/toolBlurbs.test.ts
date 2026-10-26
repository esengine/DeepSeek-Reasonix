import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";
import { toolBlurb } from "./toolBlurbs";

const catalogue = new URL("../../../../internal/runtime/usecap/testdata/capability_list.json", import.meta.url);
const peerTools = ["dispatch_to_project", "list_peers", "open_projects", "send_peer_message"];
const configTools = ["advise", "best_of_n", "run_script", "read_mcp_resource", "present_files"];
const mediaTools = ["web_search", "read_video", "import_web_image", "computer_act", "computer_read"];

describe("the tool blurbs", () => {
  it("cover every built-in tool the kernel's capability catalogue declares, so the settings list never shows model-facing English", () => {
    const entries: { kind: string; name: string }[] = JSON.parse(readFileSync(catalogue, "utf8")).capabilities;
    const tools = entries.filter((e) => e.kind === "tool").map((e) => e.name);
    expect(tools.length).toBeGreaterThan(20);
    expect(tools.filter((name) => toolBlurb(name) === undefined)).toEqual([]);
  });

  it("cover the peer, media, web and config-gated tools that assembly adds outside the catalogue", () => {
    expect([...peerTools, ...mediaTools, ...configTools].filter((name) => toolBlurb(name) === undefined)).toEqual([]);
  });
});
