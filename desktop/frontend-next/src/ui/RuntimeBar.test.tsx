// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { RuntimeBar } from "./RuntimeBar";
import type { RuntimeNotice } from "../state/session";

afterEach(cleanup);

const notice = (over: Partial<RuntimeNotice> = {}): RuntimeNotice => ({
  id: "r1",
  level: "warn",
  code: "verification_stalled",
  text: "The same check has failed 3 rounds running and still reports the same thing.",
  detail: "verification stalled: 3 rounds, 5 change(s) landed against it without moving it",
  ...over,
});

// This is where a fact about the run is said, so it is said the way the rest of
// the window says things: the kernel's code in the reader's language, and the
// kernel's own English diagnostic beside it rather than run into it.
describe("what the runtime has to say about itself", () => {
  it("says the kernel's code in this window's language", () => {
    render(<RuntimeBar notices={[notice()]} onSeen={() => {}} />);
    const bar = screen.getByRole("status");
    expect(bar.textContent).toContain("同一个检查连着几轮都报同样的结果，是否继续由你定");
    expect(bar.textContent).not.toContain("The same check has failed");
  });

  it("keeps the diagnostic beside the sentence, in its own half", () => {
    render(<RuntimeBar notices={[notice()]} onSeen={() => {}} />);
    expect(document.querySelector(".rtbar .t")?.textContent).toContain("同一个检查");
    expect(document.querySelector(".rtbar .why")?.textContent).toContain("3 rounds");
  });

  it("carries the severity the bar is coloured by", () => {
    render(<RuntimeBar notices={[notice()]} onSeen={() => {}} />);
    expect(screen.getByRole("status").getAttribute("data-lvl")).toBe("warn");
  });

  // A code this build has no sentence for still reads: the kernel wrote one.
  it("falls back to the kernel's own English", () => {
    render(<RuntimeBar notices={[notice({ code: "some_future_code", text: "recovered a forked session" })]} onSeen={() => {}} />);
    expect(screen.getByRole("status").textContent).toContain("recovered a forked session");
  });

  it("hands back the id of the one dismissed", async () => {
    const onSeen = vi.fn();
    render(<RuntimeBar notices={[notice(), notice({ id: "r2" })]} onSeen={onSeen} />);
    await userEvent.click(screen.getAllByRole("button", { name: "知道了" })[1]);
    expect(onSeen).toHaveBeenCalledWith("r2");
  });
});
