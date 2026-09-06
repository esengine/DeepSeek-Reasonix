import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { ServerRow } from "./ServerRow";
import type { AgentPort, McpEntry } from "../port/port";

const entry = (over: Partial<McpEntry> = {}): McpEntry => ({
  name: "github",
  state: "failed",
  enabled: true,
  transport: "http",
  tools: 0,
  ...over,
});

const draw = (m: McpEntry) =>
  renderToStaticMarkup(
    <ServerRow m={m} port={{} as AgentPort} onDone={() => {}} root="/w" live />,
  );

// What a row may say about a failure is what the host answered, not what the
// server wrote about itself. The second is text this machine sanitised and
// truncated on the way through, and it used to decide which repair the row
// offered.
describe("what a failed server's row claims", () => {
  it("offers no authorisation repair on the strength of the server's own words", () => {
    const html = draw(entry({ error: "401 unauthorized forbidden auth please login again" }));
    expect(html).not.toContain("重新授权");
    expect(html).toContain("重连");
  });

  it("says the status the host answered with, without saying what to do about it", () => {
    const html = draw(entry({ httpStatus: 401, error: "banana" }));
    expect(html).toContain("HTTP 401");
    // 401 is a fact. Whether the reader has to authorise again depends on
    // whether the automatic refresh ran, and nothing here knows that yet.
    expect(html).not.toContain("重新授权");
  });

  it("keeps the server's own words as detail", () => {
    expect(draw(entry({ httpStatus: 403, error: "banana" }))).toContain("banana");
  });

  it("says nothing about a status when the failure was not one", () => {
    const html = draw(entry({ error: "spawn ENOENT" }));
    expect(html).not.toContain("HTTP");
  });
});
