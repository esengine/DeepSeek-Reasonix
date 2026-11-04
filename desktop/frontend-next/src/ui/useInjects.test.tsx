// @vitest-environment jsdom
import { renderHook } from "@testing-library/react";
import type { Dispatch, MutableRefObject, SetStateAction } from "react";
import { describe, expect, it, vi } from "vitest";
import type { PickedSnapshot } from "./composerChips";
import { useInjects, type Injects } from "./useInjects";

function element(tag: string): PickedSnapshot {
  return {
    url: "https://example.test/",
    title: "Example",
    tag,
    selector: `${tag}#one`,
    xpath: `/${tag}[1]`,
    html: `<${tag}></${tag}>`,
    attributes: {},
    rect: { x: 0, y: 0, width: 10, height: 10 },
    style: { color: "#000", display: "block", fontFamily: "sans-serif", fontSize: "12px", fontWeight: "400" },
  };
}

function rig() {
  return {
    caret: 0,
    addElement: vi.fn(),
    attach: vi.fn(),
    setText: vi.fn() as unknown as Dispatch<SetStateAction<string>>,
    addSession: vi.fn(),
    focus: vi.fn(),
    pending: { current: null } as MutableRefObject<number | [number, number] | null>,
  };
}

type Rig = ReturnType<typeof rig>;

// The composer mounts with n: 0 and learns about an inject by the number going
// up; the first value on mount is treated as already applied.
function mount(r: Rig) {
  return renderHook(({ inject }: { inject: Injects }) => useInjects({ ...r, inject }), {
    initialProps: { inject: { n: 0 } as Injects },
  });
}

describe("the composer's injects", () => {
  it("applies a pick's elements and its picture together", () => {
    const r = rig();
    const file = new File(["x"], "shot.png", { type: "image/png" });
    const { rerender } = mount(r);
    rerender({ inject: { n: 1, pick: { elements: [element("img")], files: [file] } } });
    expect(r.addElement).toHaveBeenCalledWith(element("img"));
    expect(r.attach).toHaveBeenCalledWith([file]);
  });

  it("does not replay the previous pick's picture on the next pick", () => {
    const r = rig();
    const file = new File(["x"], "shot.png", { type: "image/png" });
    const { rerender } = mount(r);
    rerender({ inject: { n: 1, pick: { elements: [element("img")], files: [file] } } });
    rerender({ inject: { n: 2, pick: { elements: [element("button")], files: [] } } });
    expect(r.addElement).toHaveBeenLastCalledWith(element("button"));
    expect(r.attach).toHaveBeenCalledTimes(1);
  });

  it("still delivers text after a pick", () => {
    const r = rig();
    const { rerender } = mount(r);
    rerender({ inject: { n: 1, pick: { elements: [element("img")], files: [] } } });
    rerender({ inject: { n: 2, text: "ref" } });
    expect(r.setText).toHaveBeenCalledWith(expect.any(Function));
  });
});
