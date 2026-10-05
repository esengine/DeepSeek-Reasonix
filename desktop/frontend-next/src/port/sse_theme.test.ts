import { afterEach, expect, it, vi } from "vitest";
import { SseTheme } from "./sse_theme";
import type { ThemeImport } from "./look";

const pack = { id: "dusk", name: "Dusk", tokens: { light: { bg: "#FFFFFF" } } };

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

function reply(body: ThemeImport) {
  const fetch = vi.fn().mockResolvedValue(Response.json(body));
  vi.stubGlobal("fetch", fetch);
  return fetch;
}

it.each(["loose-first", "archive-first", "two-archives"])("rejects an ambiguous ZIP selection before reading or uploading any file (%s)", async (order) => {
  const zip = new File(["archive fixture"], "Dusk.ZIP");
  const other = new File(["other fixture"], order === "two-archives" ? "other.zip" : "theme.json");
  const readZip = vi.spyOn(zip, "arrayBuffer");
  const readOther = vi.spyOn(other, "arrayBuffer");
  const fetch = reply({ pack, ignored: ["theme.json"] });
  await expect(new SseTheme().importTheme(order === "loose-first" ? [other, zip] : [zip, other])).rejects.toThrow();
  expect(readZip).not.toHaveBeenCalled();
  expect(readOther).not.toHaveBeenCalled();
  expect(fetch).not.toHaveBeenCalled();
});

it.each([{ ignored: undefined }, { ignored: ["notes.txt"] }])("preserves a single ZIP's complete canonical result (%j)", async ({ ignored }) => {
  const response = ignored ? { pack, ignored } : { pack };
  const fetch = reply(response);
  expect(await new SseTheme().importTheme([new File(["archive fixture"], "Dusk.ZIP")])).toEqual(response);
  expect(fetch).toHaveBeenCalledTimes(1);
  expect(JSON.parse(fetch.mock.calls[0][1].body)).toEqual({ name: "Dusk.ZIP", zip: btoa("archive fixture") });
});

it("uploads every loose file and preserves the kernel's ignored-file list without reinterpretation", async () => {
  const response = { pack, ignored: ["notes.txt", "notes.txt"] };
  const fetch = reply(response);
  const got = await new SseTheme().importTheme([
    new File(["manifest fixture"], "theme.json"),
    new File(["image fixture"], "preview.png"),
  ]);
  expect(got).toEqual(response);
  expect(JSON.parse(fetch.mock.calls[0][1].body)).toEqual({
    files: { "theme.json": btoa("manifest fixture"), "preview.png": btoa("image fixture") },
  });
});

it("propagates a single-ZIP upload failure without a successful receipt", async () => {
  const failure = new Error("theme import unavailable");
  vi.stubGlobal("fetch", vi.fn().mockRejectedValue(failure));
  await expect(new SseTheme().importTheme([new File(["archive fixture"], "dusk.zip")])).rejects.toBe(failure);
});
