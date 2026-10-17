import { describe, expect, it } from "vitest";
import { certifyRoots } from "./roots";

describe("abort receiver certification", () => {
  it("places explicitly typed AbortSignal listeners outside user inputs", () => {
    const result = certifyRoots(new Map([["src/upload.ts", `
      function upload(request: Request) {
        const uploadSignal: AbortSignal = request.signal;
        uploadSignal.addEventListener("abort", cancel);
        uploadSignal.removeEventListener("abort", cancel);
      }
    `]]), {});
    expect(result.roots).toEqual([]);
    expect(result.uncertified).toEqual([]);
    expect(result.nonUser.map(note => note.why)).toEqual(["NOT_A_DOM_TARGET:AbortSignal"]);
  });

  it("leaves an unproven receiver unresolved even when the event is abort", () => {
    const result = certifyRoots(new Map([["src/upload.ts", `
      function upload(request: unknown) {
        const uploadSignal = request.signal;
        uploadSignal.addEventListener("abort", cancel);
      }
    `]]), {});
    expect(result.uncertified.map(note => note.why)).toEqual(["UNRESOLVED_RECEIVER"]);
  });

  it("does not borrow an outer signal's type for a shadowing parameter", () => {
    const result = certifyRoots(new Map([["src/upload.ts", `
      const uploadSignal: AbortSignal = request.signal;
      function other(uploadSignal: unknown) {
        uploadSignal.addEventListener("abort", cancel);
      }
    `]]), {});
    expect(result.uncertified.map(note => note.why)).toEqual(["UNRESOLVED_RECEIVER"]);
  });
});
