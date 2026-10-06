import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { announceRegistryPending, registryDropped, resetRegistryGate, type RegistryPending } from "./ops_registry";
import { validateEmit } from "../../ops-events/src/index";

const relay = { OPS_EVENTS_URL: "https://ops.test", OPS_EMIT_TOKEN: "emit-secret" };
let fetchMock: ReturnType<typeof vi.fn>;
let pending: Promise<unknown>[];
const ctx = { waitUntil: (p: Promise<unknown>) => void pending.push(p) };
const item = (over: Partial<RegistryPending> = {}): RegistryPending => ({ slug: "alice/devkit", version: "0.1.0", kind: "skill", source: "https://github.com/o/r", summary: "s", ...over });
const announce = (over: Partial<RegistryPending> = {}) => announceRegistryPending(ctx, relay, item(over));
const sentBodies = () => fetchMock.mock.calls.map(([, init]) => (init as RequestInit).body as string);
const flush = () => Promise.all(pending);

beforeEach(() => {
  vi.useFakeTimers();
  vi.setSystemTime(new Date("2026-10-06T00:00:00Z"));
  resetRegistryGate();
  pending = [];
  fetchMock = vi.fn(async () => new Response(null, { status: 202 }));
  vi.stubGlobal("fetch", fetchMock);
});
afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe("registry event payload", () => {
  it("keeps slug@version whole when the slug is too long for the relay", async () => {
    const slug = `${"h".repeat(30)}/${"n".repeat(64)}`;
    announce({ slug, version: "1.2.3-rc.1" });
    await flush();
    const extra = JSON.parse(sentBodies()[0]!).extra as Record<string, string>;
    expect(extra.key.length).toBeLessThanOrEqual(80);
    expect(extra.key.endsWith("@1.2.3-rc.1")).toBe(true);
    expect(extra.key.startsWith("hhh")).toBe(true);
  });

  it("fits the relay frame in bytes for multi-byte fields and keeps key and kind", async () => {
    announce({ slug: `${"a".repeat(30)}/${"b".repeat(40)}`, version: "路".repeat(40), source: `https://a.io/${"路".repeat(90)}`, summary: "🧪".repeat(80) });
    await flush();
    const body = JSON.parse(sentBodies()[0]!);
    const event = validateEmit(body)!;
    const wire = JSON.stringify({ ...event, id: 999999999999, ts: "2026-10-06T00:00:00.000Z" });
    expect(new TextEncoder().encode(wire).length).toBeLessThanOrEqual(600);
    expect(event.extra!.key).toBe(body.extra.key);
    expect(event.extra!.kind).toBe("skill");
    expect(String(event.extra!.key).endsWith("路")).toBe(true);
  });

  it("strips control characters from free text", async () => {
    announce({ summary: "hi\u001b[31m\u0000 there x\u0085y", source: "https://a.io/\u0007x" });
    await flush();
    const extra = JSON.parse(sentBodies()[0]!).extra as Record<string, string>;
    expect(extra.summary).toBe("hi [31m there x y");
    expect(extra.source).toBe("https://a.io/ x");
  });
});

describe("registry event gate", () => {
  it("sends one event per key within an hour and again after it", async () => {
    announce();
    announce();
    await flush();
    expect(fetchMock).toHaveBeenCalledTimes(1);
    vi.setSystemTime(Date.now() + 61 * 60 * 1000);
    announce();
    await flush();
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it("caps events per minute, counts the rest and recovers next minute", async () => {
    for (let i = 0; i < 14; i++) announce({ slug: `alice/p${i}` });
    await flush();
    expect(fetchMock).toHaveBeenCalledTimes(10);
    expect(registryDropped()).toBe(4);
    vi.setSystemTime(Date.now() + 61 * 1000);
    announce({ slug: "alice/later" });
    await flush();
    expect(fetchMock).toHaveBeenCalledTimes(11);
  });

  it("does not mark a throttled event as sent", async () => {
    for (let i = 0; i < 10; i++) announce({ slug: `alice/p${i}` });
    announce({ slug: "alice/late" });
    await flush();
    expect(fetchMock).toHaveBeenCalledTimes(10);
    vi.setSystemTime(Date.now() + 61 * 1000);
    announce({ slug: "alice/late" });
    await flush();
    expect(fetchMock).toHaveBeenCalledTimes(11);
  });
});
