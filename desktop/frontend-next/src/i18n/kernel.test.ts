// @vitest-environment jsdom
//
// jsdom only because boot() reads the stored choice and stamps <html lang>.
// Nothing here renders: this is the last hop of a refusal, and it is a pure
// function of the error and the installed catalogue.
import { beforeEach, describe, expect, it } from "vitest";
import { boot, STORAGE } from "./index";
import { reason } from "./kernel";
import { HttpError } from "../port/port";

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
  it("says a coded refusal in the window's own language", () => {
    expect(reason(coded("inbox item not found", "inbox.not_found"))).toBe("这一条已经不在待送达里了");
  });

  // The gate the codes were for. The kernel rewords its own English whenever a
  // sentence reads better in a log; if that moves what a reader sees, wording
  // authority never actually left the kernel.
  it("does not move when the kernel rewords the same code", () => {
    const before = reason(coded("inbox item not found", "inbox.not_found"));
    const after = reason(coded("no such entry in this session's inbox", "inbox.not_found"));
    expect(after).toBe(before);
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
    expect(said).toBe("这次请求没能送到内核（HTTP 502）");
    expect(said).not.toContain("/skills/enabled");
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
