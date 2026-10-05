import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { ToolCard } from "./cards/ToolCard";
import type { Tool } from "../port/wire";

const call = (id: string, name = "task"): Tool => ({ id, name, readOnly: true });

// The run graph lands on a call by the id the kernel published for it, and the
// only thing that makes that possible is an anchor in the transcript's DOM.
// Nothing else reads data-call, so losing it fails silently: the graph's
// "find it in the activity stream" would switch tabs and do nothing.
describe("the anchor a run-graph node lands on", () => {
  it("marks a tool call with the id the graph names it by", () => {
    const html = renderToStaticMarkup(<ToolCard tool={call("probe-0")} running={false} />);
    expect(html).toContain('data-call="probe-0"');
  });

  it("marks a delegate's children too, which is where a fan-out's nodes are", () => {
    const html = renderToStaticMarkup(
      <ToolCard
        tool={{ ...call("probe-0"), profile: { name: "fleet", count: 2 } }}
        running={false}
        children={[call("probe-0/fleet-1"), call("probe-0/fleet-2")]}
      />,
    );
    for (const id of ["probe-0", "probe-0/fleet-1", "probe-0/fleet-2"]) {
      expect(html).toContain(`data-call="${id}"`);
    }
  });

  // A call the provider never gave an id gets no anchor rather than an empty
  // one: `[data-call=""]` would match it for every lookup that finds nothing.
  it("leaves an id-less call unanchored", () => {
    const html = renderToStaticMarkup(<ToolCard tool={{ name: "read_file", readOnly: true }} running={false} />);
    expect(html).not.toContain("data-call");
  });
});

// The group above a call already says work is happening. A call that opens
// itself moves the transcript under the reader at every step of a long turn,
// and leaves it in a state nobody chose. Failure is the exception: that is the
// one thing a reader is being asked to look at. Fixing this once by changing
// the initial state left the effect still forcing it open, so both are pinned.
describe("whether a call opens itself", () => {
  const opened = (html: string) => /<details[^>]*\bopen\b/.test(html);

  it("stays shut while it runs", () => {
    expect(opened(renderToStaticMarkup(<ToolCard tool={call("t1", "todo_write")} running />))).toBe(false);
  });

  it("opens when the call failed, which is what a reader is being shown", () => {
    const failed: Tool = { id: "t2", name: "bash", readOnly: false, err: "boom" };
    expect(opened(renderToStaticMarkup(<ToolCard tool={failed} running={false} />))).toBe(true);
  });
});
