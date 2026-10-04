// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { SsePort } from "./sse";
import { SseExtensions } from "./sse_ext";
import { HttpError } from "./http_error";
import { host } from "./host";
import { boot, STORAGE } from "../i18n";
import { reason } from "../i18n/kernel";

afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals(); localStorage.clear(); });

const root = "/rt/project";
const path = "/plugins/notes-kit/export";
const callers = [
  ["Studio", () => new SsePort(root)],
  ["extension port", () => new SseExtensions(root)],
] as const;
const replies = [
  { name: "export refusal", status: 422, body: { code: "plugin.export_failed", error: "archive cannot be written", params: { detail: "installed folder missing" } } },
  { name: "unknown package", status: 404, body: { code: "plugin.not_installed", error: "that plugin is not installed" } },
  { name: "plain text", status: 503, body: "export service unavailable" },
  { name: "empty response", status: 502, body: "" },
] as const;

it.each(callers.flatMap(([caller, make]) => replies.map((reply) => ({ caller, make, ...reply }))))(
  "keeps a $name from the $caller export endpoint", async ({ make, status, body }) => {
    const raw = typeof body === "string" ? body : JSON.stringify(body);
    const response = new Response(raw, { status });
    const fetch = vi.fn().mockResolvedValue(response);
    vi.stubGlobal("fetch", fetch);
    const save = vi.spyOn(host(), "saveBytes");
    const readArchive = vi.spyOn(response, "blob");
    const error = await make().exportPlugin("notes-kit").catch((e: unknown) => e);
    expect(error).toBeInstanceOf(HttpError);
    const typed = error as HttpError;
    expect(typed.status).toBe(status);
    expect(typed.message).toBe(typeof body === "string" ? body || path + ": " + status : body.error);
    expect(typed.reason).toEqual(typeof body === "string" ? undefined : body);
    expect(typed.detailed).toBe(raw !== "");
    expect(fetch).toHaveBeenCalledExactlyOnceWith(root + path, { credentials: "same-origin" });
    expect(readArchive).not.toHaveBeenCalled();
    expect(save).not.toHaveBeenCalled();
    expect(response.bodyUsed).toBe(true);
  },
);

it.each(callers)("renders the %s export refusal with the kernel's detail", async (_caller, make) => {
  localStorage.setItem(STORAGE, "zh");
  boot();
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(replies[0].body), { status: 422 })));
  const error = await make().exportPlugin("notes-kit").catch((e: unknown) => e);
  expect(reason(error)).toBe("未能导出该插件：installed folder missing");
});

const archive = new Uint8Array([80, 75, 3, 4]);

function exportResponse() {
  return new Response(archive, { headers: { "content-type": "application/zip", "X-Reasonix-Required-Env": "NOTES_TOKEN,DOCS_KEY" } });
}

it.each(["/exports/notes-kit.zip", ""])("keeps the native archive save answer %j", async (savedTo) => {
  const response = exportResponse();
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(response));
  const save = vi.spyOn(host(), "saveBytes").mockResolvedValue(savedTo);
  const click = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => {});
  expect(await new SsePort(root).exportPlugin("notes-kit")).toEqual({
    required: ["NOTES_TOKEN", "DOCS_KEY"], savedTo: savedTo || undefined,
  });
  expect(save).toHaveBeenCalledExactlyOnceWith("notes-kit.zip", archive);
  expect(click).not.toHaveBeenCalled();
  expect(response.bodyUsed).toBe(true);
});

it.each(callers)("keeps the %s browser archive download", async (_caller, make) => {
  const response = exportResponse();
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(response));
  vi.spyOn(host(), "saveBytes").mockResolvedValue(null);
  const create = vi.fn().mockReturnValue("blob:notes-archive");
  const revoke = vi.fn();
  vi.stubGlobal("URL", class extends URL {
    static createObjectURL = create;
    static revokeObjectURL = revoke;
  });
  let clicked!: HTMLAnchorElement;
  const click = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(function (this: HTMLAnchorElement) { clicked = this; });
  expect(await make().exportPlugin("notes-kit")).toEqual({ required: ["NOTES_TOKEN", "DOCS_KEY"] });
  expect(create).toHaveBeenCalledTimes(1);
  expect(new Uint8Array(await create.mock.calls[0][0].arrayBuffer())).toEqual(archive);
  expect(click).toHaveBeenCalledTimes(1);
  expect(clicked.href).toBe("blob:notes-archive");
  expect(clicked.download).toBe("notes-kit.zip");
  expect(revoke).toHaveBeenCalledExactlyOnceWith("blob:notes-archive");
});
