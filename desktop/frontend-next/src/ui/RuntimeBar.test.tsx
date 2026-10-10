// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import "./testkit";
import { boot, STORAGE } from "../i18n";
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

  it("words a recovered inbox from its count, not the payload", () => {
    render(<RuntimeBar notices={[notice({ code: "inbox_recovered", text: "Recovered 3 pending instruction(s).", detail: '{"count":3}' })]} onSeen={() => {}} />);
    const bar = screen.getByRole("status");
    expect(bar.textContent).toContain("已恢复 3 条未完成的指令");
    expect(bar.textContent).not.toContain("count");
  });

  it("hands back the id of the one dismissed", async () => {
    const onSeen = vi.fn();
    render(<RuntimeBar notices={[notice(), notice({ id: "r2" })]} onSeen={onSeen} />);
    await userEvent.click(screen.getAllByRole("button", { name: "知道了" })[1]);
    expect(onSeen).toHaveBeenCalledWith("r2");
  });
});

describe("an extension left out because its sidecar is not running", () => {
  const skipped = (over: Partial<RuntimeNotice> = {}): RuntimeNotice => notice({
    id: "x1",
    code: "extension_skipped",
    text: "Extension aipush-ask-bridge's sidecar is not running, so it was skipped at tool.before.",
    detail: JSON.stringify({ extension: "aipush-ask-bridge", point: "tool.before", reason: "no_live_sidecar" }),
    ...over,
  });

  afterEach(() => { localStorage.setItem(STORAGE, "zh"); boot(); });

  it("names the extension and says what to do, in full, without the raw payload", () => {
    render(<RuntimeBar notices={[skipped()]} onSeen={() => {}} onSettings={() => {}} />);
    const bar = screen.getByRole("status");
    expect(document.querySelector(".rtbar .t")?.textContent).toBe(
      "扩展 aipush-ask-bridge 的配套后台程序没有运行，该扩展本次（在 tool.before）已被跳过；到「工具与集成」里查看并启动它，或停用该扩展");
    expect(bar.textContent).not.toContain("reason");
    expect(bar.textContent).not.toContain("no_live_sidecar");
    expect(document.querySelector(".rtbar .why")).toBeNull();
  });

  it("says it in English too", () => {
    localStorage.setItem(STORAGE, "en");
    boot();
    render(<RuntimeBar notices={[skipped()]} onSeen={() => {}} onSettings={() => {}} />);
    expect(document.querySelector(".rtbar .t")?.textContent).toContain("extension aipush-ask-bridge is not running");
    expect(screen.getByRole("button", { name: "Open Tools and integrations" })).toBeTruthy();
  });

  it("opens Tools and integrations from the notice", async () => {
    const onSettings = vi.fn();
    render(<RuntimeBar notices={[skipped()]} onSeen={() => {}} onSettings={onSettings} />);
    await userEvent.click(screen.getByRole("button", { name: "打开「工具与集成」" }));
    expect(onSettings).toHaveBeenCalledWith("ext");
  });

  it("can still be dismissed", async () => {
    const onSeen = vi.fn();
    render(<RuntimeBar notices={[skipped()]} onSeen={onSeen} onSettings={() => {}} />);
    await userEvent.click(screen.getByRole("button", { name: "知道了" }));
    expect(onSeen).toHaveBeenCalledWith("x1");
  });

  it("keeps the kernel's English when the payload cannot be read", () => {
    render(<RuntimeBar notices={[skipped({ detail: "not json" })]} onSeen={() => {}} onSettings={() => {}} />);
    expect(screen.getByRole("status").textContent).toContain("sidecar is not running, so it was skipped");
    expect(screen.queryByRole("button", { name: "打开「工具与集成」" })) .toBeNull();
  });

  it("leaves other notices without the integrations entry", () => {
    render(<RuntimeBar notices={[notice()]} onSeen={() => {}} onSettings={() => {}} />);
    expect(screen.queryByRole("button", { name: "打开「工具与集成」" })).toBeNull();
  });
});
