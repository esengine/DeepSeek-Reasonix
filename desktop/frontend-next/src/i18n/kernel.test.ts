// @vitest-environment jsdom
//
// jsdom only because boot() reads the stored choice and stamps <html lang>.
// Nothing here renders: this is the last hop of a refusal, and it is a pure
// function of the error and the installed catalogue.
import { beforeEach, describe, expect, it } from "vitest";
import { boot, STORAGE } from "./index";
import { ACCOUNT_SIGNIN_DISABLED, PROVIDER_EDIT_DISABLED, SAVED_NOT_APPLIED, codes, reason } from "./kernel";
import { HttpError, KernelBusyError } from "../port/port";

// Pinned, not defaulted: with nothing stored the window follows the machine, so
// on an English runner every assertion about a Chinese sentence would be about
// the runner rather than about reason(). The browser guards pin it for the same
// reason.
beforeEach(() => {
  localStorage.setItem(STORAGE, "zh");
  boot();
});

const coded = (message: string, code: string, params?: Record<string, string | number>) =>
  new HttpError(409, message, { code, error: message, params });

// Where a refusal stops being the kernel's and becomes the reader's. Everything
// upstream of this — dotted codes, sentinels, typed families — buys nothing if
// the last hop prints the English that rode along for the log.
describe("what a reader is told a refusal was", () => {
  it("renders shell refusal causes in Chinese and English", () => {
    for (const code of ["shell.destructive_target", "shell.analysis_unknown", "shell.delete_sequence", "shell.delete_nonliteral", "shell.delete_option", "shell.syntax_error", "shell.parser_unavailable", "shell.parser_timeout", "shell.command_line_too_long"]) {
      localStorage.setItem(STORAGE, "zh"); boot();
      const chinese = reason(coded("fixture fallback", code));
      expect(chinese).not.toBe("fixture fallback");
      localStorage.setItem(STORAGE, "en"); boot();
      const english = reason(coded("fixture fallback", code));
      expect(english).not.toBe(chinese);
      expect(english).not.toBe("fixture fallback");
    }
  });
  it("says a coded refusal in the window's own language", () => {
    expect(reason(coded("inbox item not found", "inbox.not_found"))).toBe("该条已不在待送达队列中");
  });

  // The gate the codes were for. The kernel rewords its own English whenever a
  // sentence reads better in a log; if that moves what a reader sees, wording
  // authority never actually left the kernel.
  it("explains the project limit in Chinese and English", () => {
    const error = coded("storage limit", "workspace.limit_reached");
    expect(reason(error)).toBe("项目列表已满（32 个），请先移除一个项目再添加");
    localStorage.setItem(STORAGE, "en");
    boot();
    expect(reason(error)).toBe("The project list is full (32 projects); remove one before adding another");
  });

  it("does not move when the kernel rewords the same code", () => {
    const before = reason(coded("inbox item not found", "inbox.not_found"));
    const after = reason(coded("no such entry in this session's inbox", "inbox.not_found"));
    expect(after).toBe(before);
  });

  // The pane map is one window's, so a holder in another process leaves nothing
  // in this window to close. The pid is the only actionable fact.
  it("names the process holding a conversation open", () => {
    const said = reason(coded("another process holds this conversation open", "session.in_use_by", { pid: 77941, host: "mac-mini.local" }));
    expect(said).toContain("77941");
    expect(said).toContain("mac-mini.local");
  });

  it("fills a code's sentence from the params, not from the kernel's prose", () => {
    const said = reason(coded("workspace has 3 open panes", "workspace.has_open_panes", { n: 3 }));
    expect(said).toContain("3");
    expect(said).not.toContain("workspace");
  });

  // A dead process and a proxy answer with neither a code nor a body, and then
  // message is a path and a number — true of the failure, useless to the person
  // reading it.
  it("does not put a path and a status in front of the reader", () => {
    const said = reason(new HttpError(502, "/skills/enabled: 502", undefined, false));
    expect(said).toBe("请求未能送达内核（HTTP 502）");
    expect(said).not.toContain("/skills/enabled");
  });

  it("names a decision the kernel never answered as busy or unreachable", () => {
    const said = reason(new KernelBusyError());
    expect(said).toBe("内核繁忙或无法连接，这次回答可能没有被收到，重试前请先确认");
  });

  it("keeps a detailed answer the kernel had no code for", () => {
    expect(reason(new HttpError(400, "unknown skill: explore"))).toBe("unknown skill: explore");
  });

  it("keeps an ordinary failure as itself", () => {
    expect(reason(new TypeError("cannot read properties of null"))).toBe("cannot read properties of null");
  });

  // A catch block catches whatever was thrown, and nothing guarantees it is an
  // Error. The answer has to be a string either way — a panel that renders
  // undefined reads as the operation having succeeded.
  it("answers with a string for anything else that was thrown", () => {
    for (const thrown of ["plain string", null, undefined, 42, { code: "not-an-error" }]) {
      expect(typeof reason(thrown)).toBe("string");
    }
  });

  it("says the same refusal in English when the window is English", () => {
    localStorage.setItem(STORAGE, "en");
    boot();
    expect(reason(coded("inbox item not found", "inbox.not_found"))).toBe(
      "That entry is no longer in the pending queue",
    );
  });
});

// A code a caller branches on is spelled twice: once as the exported constant,
// once as a literal key the kernel's parity guard can read as text. Nothing in
// either language notices when one of them moves.
describe("codes a caller branches on", () => {
  it("spells the provider refusal the same in the constant and the catalogue", () => {
    expect(codes[PROVIDER_EDIT_DISABLED]).toBeTruthy();
  });

  it("spells the account refusal the same in the constant and the catalogue", () => {
    expect(codes[ACCOUNT_SIGNIN_DISABLED]).toBeTruthy();
  });

  it("spells the saved-but-not-applied answers the same in the constant and the catalogue", () => {
    for (const code of SAVED_NOT_APPLIED) expect(codes[code]).toBeTruthy();
  });
});
