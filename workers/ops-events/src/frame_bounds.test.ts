// @ts-expect-error the Worker package carries no Node typings; vitest runs under Node.
import { spawnSync } from "node:child_process";
// @ts-expect-error the Worker package carries no Node typings; vitest runs under Node.
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import { MAX_FRAME_BYTES, type OpsEvent } from "./events";
import { HubCore, type Store } from "./hub_core";
import { validateEmit } from "./index";

const EVENTS_TS = fileURLToPath(new URL("./events.ts", (import.meta as unknown as { url: string }).url));
const CHILD = `
import { frame } from ${JSON.stringify(EVENTS_TS)};
const events = JSON.parse(await new Response(process.stdin).text());
const enc = new TextEncoder();
process.stdout.write(JSON.stringify(events.map((e) => { const t = frame(e); return { bytes: enc.encode(t).length, parsed: JSON.parse(t) }; })));
`;

// frame() runs in a child process so a non-terminating loop fails by timeout instead of hanging the suite.
function framesOf(events: OpsEvent[]): { bytes: number; parsed: OpsEvent }[] {
  const run = spawnSync(process.execPath, ["--experimental-strip-types", "--input-type=module", "-e", CHILD], { input: JSON.stringify(events), timeout: 8000, encoding: "utf8" });
  if (run.error) throw new Error(`frame() did not return: ${run.error.message}`);
  expect(run.status).toBe(0);
  return JSON.parse(run.stdout);
}

const frameOf = (event: OpsEvent) => framesOf([event])[0]!;

const cjk = (n: number) => "路".repeat(n);
const emoji = (n: number) => "🧪".repeat(n);
const accepted = (body: unknown) => {
  const e = validateEmit(body);
  if (!e) throw new Error("validateEmit rejected the body");
  return { ...e, id: 123456789012, ts: "2026-10-06T04:13:05.000Z" } as OpsEvent;
};
const registry = (extra: Record<string, string>, title = "registry submission pending") => accepted({ src: "registry", t: "pending", title, extra });

const cases: Record<string, OpsEvent> = {
  "cjk key, source and summary": registry({ key: `alice/${"b".repeat(40)}@1.0.0`, kind: "skill", source: `https://a.io/${cjk(67)}`, summary: cjk(80) }),
  "emoji summary and source": registry({ key: "alice/devkit@0.1.0", kind: "skill", source: emoji(80), summary: emoji(80) }),
  "long ascii extra": registry({ key: "k".repeat(80), kind: "skill", source: "s".repeat(80), summary: "m".repeat(80), a: "x".repeat(80), b: "x".repeat(80) }),
  "title already at its floor and extra over budget": registry({ key: cjk(80), kind: cjk(80), source: cjk(80), summary: cjk(80) }, "t".repeat(9)),
  "long title only": accepted({ src: "github", t: "issue.opened", title: cjk(200), url: `https://github.com/${"u".repeat(170)}`, by: "b".repeat(60), n: 1 }),
  "max type names": accepted({ src: "s".repeat(40), t: "t".repeat(40), by: "b".repeat(60), url: `https://${"u".repeat(180)}`, title: emoji(120), extra: { key: cjk(80) } }),
};

describe("frame bounds", () => {
  for (const [name, event] of Object.entries(cases)) {
    it(`returns within the byte limit: ${name}`, () => {
      const out = frameOf(event);
      expect(out.bytes).toBeLessThanOrEqual(MAX_FRAME_BYTES);
      expect(out.parsed).toMatchObject({ id: event.id, src: event.src, t: event.t });
    });
  }

  it("keeps a frame that is exactly at the limit untouched", () => {
    const base = { src: "registry", t: "pending", id: 1, ts: "2026-10-06T04:13:05.000Z", extra: { key: "" } } as OpsEvent;
    const room = MAX_FRAME_BYTES - new TextEncoder().encode(JSON.stringify(base)).length;
    const exact = { ...base, extra: { key: "k".repeat(room) } } as OpsEvent;
    const out = frameOf(exact);
    expect(out.bytes).toBe(MAX_FRAME_BYTES);
    expect(out.parsed).toEqual(exact);
    const over = frameOf({ ...exact, extra: { key: "k".repeat(room + 1) } } as OpsEvent);
    expect(over.bytes).toBeLessThanOrEqual(MAX_FRAME_BYTES);
  });

  it("drops extra before it shrinks a title", () => {
    const out = frameOf(cases["cjk key, source and summary"]!);
    expect(out.parsed.extra).toBeUndefined();
    expect(out.parsed.title).toBe("registry submission pending");
  });

  it("replays an already stored oversized event without spinning", async () => {
    const data = new Map<string, unknown>();
    const store: Store = {
      async get<T>(k: string) { return data.get(k) as T | undefined; },
      async put(k: string, v: unknown) { data.set(k, v); },
      async delete(keys: string | string[]) { for (const k of Array.isArray(keys) ? keys : [keys]) data.delete(k); },
      async list<T>(o: { prefix: string; start?: string; limit?: number }) {
        const keys = [...data.keys()].filter((k) => k.startsWith(o.prefix) && (!o.start || k >= o.start)).sort().slice(0, o.limit ?? 1000);
        return new Map(keys.map((k) => [k, data.get(k) as T]));
      },
    };
    const poison = validateEmit({ src: "registry", t: "pending", title: "registry submission pending", extra: cases["cjk key, source and summary"]!.extra });
    const core = new HubCore(store);
    const pushed = await core.push(poison!, { rateKey: "emit" });
    const backlog = await core.replay("0");
    expect(backlog.map((e) => e.id)).toEqual([pushed.event!.id]);
    const out = frameOf(backlog[0]!);
    expect(out.bytes).toBeLessThanOrEqual(MAX_FRAME_BYTES);
  });
});
