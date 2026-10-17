// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  CLOSE_IDLE,
  CLOSE_REAUTH_REQUIRED,
  CLOSE_REVOKED,
  closeAction,
  linkCodec,
  maxRequestBody,
  RemoteLink,
  RemoteLinkError,
  type RemoteConnection,
  type RemoteEnd,
} from "./cloud_link";

class FakeSocket extends EventTarget {
  readyState: number = WebSocket.OPEN;
  readonly sent: Array<Record<string, unknown>> = [];

  send(wire: string) {
    this.sent.push(JSON.parse(wire) as Record<string, unknown>);
  }

  close(code = 1000, reason = "") {
    if (this.readyState === WebSocket.CLOSED) return;
    this.readyState = WebSocket.CLOSED;
    this.dispatchEvent(Object.assign(new Event("close"), { code, reason }));
  }

  reply(id: string, body: string, extra: Record<string, unknown> = {}) {
    this.dispatchEvent(new MessageEvent("message", { data: JSON.stringify({
      type: "desktop.response", id, index: 0, done: true, status: 200,
      body: linkCodec.bytesToBase64(new TextEncoder().encode(body)), ...extra,
    }) }));
  }
}

function connection(features: string[] = [], instance = "desktop-1"): { conn: RemoteConnection; socket: FakeSocket } {
  const socket = new FakeSocket();
  return {
    socket,
    conn: {
      socket: socket as unknown as WebSocket,
      features,
      instance,
      listen(onMessage, onClose) {
        socket.addEventListener("message", (event) => onMessage(String((event as MessageEvent).data)));
        socket.addEventListener("close", (event) => {
          const closed = event as Event & { code: number; reason: string };
          onClose(closed.code, closed.reason);
        });
      },
      channel: {
        seal: async (value) => JSON.stringify(value),
        open: async (payload) => JSON.parse(payload) as Record<string, unknown>,
      },
    },
  };
}

function request(method = "GET", path = "/rt/one/events/replay") {
  return new Request(`${location.origin}${path}`, { method, body: method === "GET" ? undefined : "{}" });
}

let ends: RemoteEnd[];

beforeEach(() => {
  vi.useFakeTimers();
  ends = [];
});

afterEach(() => vi.useRealTimers());

describe("relay close codes", () => {
  it("reconnects on idle, legacy expiry and dropped sockets, and stops on revocation or sign-in", () => {
    expect(closeAction(CLOSE_IDLE)).toBe("reconnect");
    expect(closeAction(4003)).toBe("reconnect");
    expect(closeAction(1006)).toBe("reconnect");
    expect(closeAction(1012)).toBe("reconnect");
    expect(closeAction(CLOSE_REAUTH_REQUIRED)).toBe("reauth");
    expect(closeAction(CLOSE_REVOKED)).toBe("end");
    expect(closeAction(4004)).toBe("end");
  });
});

describe("remote link", () => {
  it("resends an in-flight read on the next connection and resolves it there", async () => {
    const first = connection();
    const second = connection();
    const dial = vi.fn(async () => second.conn);
    const link = new RemoteLink(first.conn, dial, (end) => ends.push(end));

    const answer = link.fetch(request());
    await vi.advanceTimersByTimeAsync(0);
    const id = String(first.socket.sent[0]?.id);
    first.socket.close(CLOSE_IDLE, "Idle timeout");
    await vi.advanceTimersByTimeAsync(1_000);

    expect(dial).toHaveBeenCalledOnce();
    expect(second.socket.sent[0]?.id).toBe(id);
    second.socket.reply(id, "late but whole");
    await expect((await answer).text()).resolves.toBe("late but whole");
    expect(ends).toEqual([]);
  });

  it("queues requests made while reconnecting and sends them once connected", async () => {
    const first = connection();
    const second = connection();
    const link = new RemoteLink(first.conn, async () => second.conn, (end) => ends.push(end));

    first.socket.close(4003, "Admission expired");
    const answer = link.fetch(request("POST", "/rt/one/send"));
    await vi.advanceTimersByTimeAsync(0);
    expect(first.socket.sent).toEqual([]);
    await vi.advanceTimersByTimeAsync(1_000);
    const id = String(second.socket.sent[0]?.id);
    second.socket.reply(id, "sent");
    await expect((await answer).text()).resolves.toBe("sent");
  });

  it("never resends a state change to a desktop that cannot recognise the resend", async () => {
    const first = connection();
    const second = connection();
    const link = new RemoteLink(first.conn, async () => second.conn, (end) => ends.push(end));

    const answer = link.fetch(request("POST", "/rt/one/send"));
    const rejected = answer.catch((error: Error) => error);
    await vi.advanceTimersByTimeAsync(0);
    first.socket.close(1006, "");
    await vi.advanceTimersByTimeAsync(1_000);

    expect(await rejected).toBeInstanceOf(Error);
    expect(second.socket.sent).toEqual([]);
  });

  it("resends a state change under the same id when the desktop replays answers", async () => {
    const first = connection(["replay"]);
    const second = connection(["replay"]);
    const link = new RemoteLink(first.conn, async () => second.conn, (end) => ends.push(end));

    const answer = link.fetch(request("POST", "/rt/one/send"));
    await vi.advanceTimersByTimeAsync(0);
    const id = String(first.socket.sent[0]?.id);
    first.socket.close(1006, "");
    await vi.advanceTimersByTimeAsync(1_000);

    expect(second.socket.sent[0]?.id).toBe(id);
    second.socket.reply(id, "once");
    await expect((await answer).text()).resolves.toBe("once");
  });

  it("does not resend a state change to a restarted desktop, whose replay cache is empty", async () => {
    const first = connection(["replay"], "desktop-1");
    const second = connection(["replay"], "desktop-2");
    const link = new RemoteLink(first.conn, async () => second.conn, (end) => ends.push(end));

    const rejected = link.fetch(request("POST", "/rt/one/send")).catch((error: Error) => error);
    const read = link.fetch(request());
    await vi.advanceTimersByTimeAsync(0);
    first.socket.close(1006, "");
    await vi.advanceTimersByTimeAsync(1_000);

    expect(await rejected).toBeInstanceOf(Error);
    expect(second.socket.sent).toHaveLength(1);
    expect(second.socket.sent[0]?.method).toBe("GET");
    second.socket.reply(String(second.socket.sent[0]?.id), "read again");
    await expect((await read).text()).resolves.toBe("read again");
  });

  it("sends commands in the order they were sealed, however long sealing takes", async () => {
    const { conn, socket } = connection();
    let slow = true;
    conn.channel.seal = async (value) => {
      if (slow) {
        slow = false;
        await new Promise((resolve) => setTimeout(resolve, 50));
      }
      return JSON.stringify(value);
    };
    const link = new RemoteLink(conn, vi.fn(), (end) => ends.push(end));

    void link.fetch(request("POST", "/first"));
    void link.fetch(request("POST", "/second"));
    await vi.advanceTimersByTimeAsync(100);

    expect(socket.sent.map((command) => command.path)).toEqual(["/first", "/second"]);
  });

  it("asks for a sign-in instead of reconnecting when the relay says so", async () => {
    const first = connection();
    const dial = vi.fn();
    const link = new RemoteLink(first.conn, dial, (end) => ends.push(end));
    const pending = link.fetch(request()).catch((error: Error) => error);
    await vi.advanceTimersByTimeAsync(0);

    first.socket.close(CLOSE_REAUTH_REQUIRED, "Sign-in required");
    await vi.advanceTimersByTimeAsync(60_000);

    expect(dial).not.toHaveBeenCalled();
    expect(ends).toEqual([{ kind: "reauth", reason: "Sign-in required" }]);
    expect(await pending).toBeInstanceOf(Error);
  });

  it("stops for good when the device is revoked", async () => {
    const first = connection();
    const dial = vi.fn();
    new RemoteLink(first.conn, dial, (end) => ends.push(end));

    first.socket.close(CLOSE_REVOKED, "Access revoked");
    await vi.advanceTimersByTimeAsync(60_000);

    expect(dial).not.toHaveBeenCalled();
    expect(ends).toEqual([{ kind: "ended", reason: "Access revoked" }]);
  });

  it("ends with the account service's reason when a reconnect is refused", async () => {
    const first = connection();
    const dial = vi.fn()
      .mockRejectedValueOnce(new RemoteLinkError("transient", "relay busy"))
      .mockRejectedValueOnce(new RemoteLinkError("reauth", "sign in"));
    new RemoteLink(first.conn, dial, (end) => ends.push(end));

    first.socket.close(CLOSE_IDLE, "Idle timeout");
    await vi.advanceTimersByTimeAsync(5_000);

    expect(dial).toHaveBeenCalledTimes(2);
    expect(ends).toEqual([{ kind: "reauth", reason: "sign in" }]);
  });

  it("rebuilds the encrypted session when the desktop goes silent on an open socket", async () => {
    const first = connection();
    const second = connection();
    const dial = vi.fn(async () => second.conn);
    const link = new RemoteLink(first.conn, dial, (end) => ends.push(end), { requestTimeoutMs: 30_000 });

    const answer = link.fetch(request());
    await vi.advanceTimersByTimeAsync(30_000);
    await vi.advanceTimersByTimeAsync(1_000);

    expect(first.socket.readyState).toBe(WebSocket.CLOSED);
    expect(dial).toHaveBeenCalledOnce();
    const id = String(second.socket.sent[0]?.id);
    second.socket.reply(id, "fresh session");
    await expect((await answer).text()).resolves.toBe("fresh session");
  });

  it("gives up after its retries and says the connection ended", async () => {
    const first = connection();
    const dial = vi.fn(async () => { throw new RemoteLinkError("transient", "offline"); });
    new RemoteLink(first.conn, dial, (end) => ends.push(end), { retryDelaysMs: [10, 10] });

    first.socket.close(1006, "");
    await vi.advanceTimersByTimeAsync(100);

    expect(dial).toHaveBeenCalledTimes(2);
    expect(ends[0]?.kind).toBe("ended");
  });

  it("ends with the last attempt's failure code, and with idle when the relay reclaimed the lease", async () => {
    const limited = connection();
    const dialLimited = vi.fn(async () => {
      throw new RemoteLinkError("transient", "wait", { code: "rate_limited", retryAfterS: 30 });
    });
    new RemoteLink(limited.conn, dialLimited, (end) => ends.push(end), { retryDelaysMs: [10] });
    limited.socket.close(1006, "");
    await vi.advanceTimersByTimeAsync(100);
    expect(ends[0]).toMatchObject({ kind: "ended", failure: { code: "rate_limited", retryAfterS: 30 } });

    const idle = connection();
    const dialIdle = vi.fn(async () => { throw new RemoteLinkError("transient", "down"); });
    new RemoteLink(idle.conn, dialIdle, (end) => ends.push(end), { retryDelaysMs: [10] });
    idle.socket.close(CLOSE_IDLE, "Idle timeout");
    await vi.advanceTimersByTimeAsync(100);
    expect(ends[1]).toMatchObject({ kind: "ended", failure: { code: "idle" } });

    const plain = connection();
    new RemoteLink(plain.conn, dialIdle, (end) => ends.push(end), { retryDelaysMs: [10] });
    plain.socket.close(1006, "");
    await vi.advanceTimersByTimeAsync(100);
    expect(ends[2]?.failure).toBeUndefined();
  });

  it("waits out a Retry-After before dialling again and reports only the last attempt's cause", async () => {
    const first = connection();
    const dial = vi.fn()
      .mockRejectedValueOnce(new RemoteLinkError("transient", "wait", { code: "rate_limited", retryAfterS: 5 }))
      .mockRejectedValueOnce(new RemoteLinkError("transient", "down"));
    new RemoteLink(first.conn, dial, (end) => ends.push(end), { retryDelaysMs: [10, 10] });
    first.socket.close(1006, "");

    await vi.advanceTimersByTimeAsync(10);
    expect(dial).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(4_000);
    expect(dial).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(1_100);
    expect(dial).toHaveBeenCalledTimes(2);
    expect(ends[0]?.kind).toBe("ended");
    expect(ends[0]?.failure).toBeUndefined();
  });
});

describe("a request too large for the desktop's read limit", () => {
  const upload = (bytes: number, path = "/rt/one/attachments") =>
    new Request(`${location.origin}${path}`, { method: "POST", body: "x".repeat(bytes) });

  it("is refused here, says why, and leaves the link and later requests alone", async () => {
    const { conn, socket } = connection();
    const link = new RemoteLink(conn, async () => conn, (end) => ends.push(end));
    const room = maxRequestBody("/rt/one/attachments");
    expect(room).toBeGreaterThan(20_000);
    await expect(link.fetch(upload(room + 1))).rejects.toThrow(/太大|too large/);
    expect(socket.sent).toHaveLength(0);
    expect(ends).toEqual([]);
    const small = link.fetch(upload(100));
    await vi.advanceTimersByTimeAsync(0);
    expect(socket.sent).toHaveLength(1);
    socket.reply(String(socket.sent[0].id), "{}");
    expect((await small).status).toBe(200);
  });

  it("is judged by what the sealed message would weigh, which the room stays well under", async () => {
    const { conn, socket } = connection();
    const link = new RemoteLink(conn, async () => conn, (end) => ends.push(end));
    const room = maxRequestBody("/rt/one/attachments");
    void link.fetch(upload(room)).catch(() => {});
    await vi.advanceTimersByTimeAsync(0);
    expect(socket.sent).toHaveLength(1);
    const wire = JSON.stringify(socket.sent[0]).length;
    expect(Math.ceil(((wire + 16 + 96) * 4) / 3) + 120).toBeLessThan(64 * 1024);
  });
});

describe("a request too large for one message", () => {
  const upload = (bytes: number) => new Request(`${location.origin}/rt/one/attachments`, { method: "POST", body: new Uint8Array(bytes).fill(7) });
  const partsOf = (socket: FakeSocket) => socket.sent.filter((m) => m.type === "desktop.request.part");

  it("crosses as numbered parts to a desktop that can put them back together, and resolves on its answer", async () => {
    const { conn, socket } = connection(["replay", "upload"]);
    const link = new RemoteLink(conn, async () => conn, (end) => ends.push(end));
    const size = 24 * 1024 * 5 + 321;
    const sent = link.fetch(upload(size));
    await vi.advanceTimersByTimeAsync(0);
    const parts = partsOf(socket);
    expect(parts).toHaveLength(6);
    expect(socket.sent.filter((m) => m.type === "desktop.request")).toHaveLength(0);
    expect(parts.map((p) => p.index)).toEqual([0, 1, 2, 3, 4, 5]);
    expect(new Set(parts.map((p) => p.id)).size).toBe(1);
    expect(parts.every((p) => p.total === 6)).toBe(true);
    expect(parts[0]).toMatchObject({ method: "POST", path: "/rt/one/attachments" });
    expect(parts.slice(1).every((p) => p.method === undefined && p.path === undefined)).toBe(true);
    const bytes = parts.flatMap((p) => [...linkCodec.base64ToBytes(String(p.body))]);
    expect(bytes).toHaveLength(size);
    expect(bytes.every((b) => b === 7)).toBe(true);
    for (const p of parts) expect(JSON.stringify(p).length).toBeLessThan(40 * 1024);
    socket.reply(String(parts[0].id), "{}");
    expect((await sent).status).toBe(200);
  });

  it("is refused here, with nothing sent, by a desktop that does not say it can reassemble", async () => {
    const { conn, socket } = connection(["replay"]);
    const link = new RemoteLink(conn, async () => conn, (end) => ends.push(end));
    await expect(link.fetch(upload(200 * 1024))).rejects.toThrow(/太大|too large/);
    expect(socket.sent).toHaveLength(0);
  });

  it("is refused here past what the desktop would hold", async () => {
    const { conn, socket } = connection(["upload"]);
    const link = new RemoteLink(conn, async () => conn, (end) => ends.push(end));
    await expect(link.fetch(upload(16 * 1024 * 1024 + 1))).rejects.toThrow(/16 MB|16 MB/);
    expect(socket.sent).toHaveLength(0);
  });

  it("stops reading an oversized upload stream at the relay budget", async () => {
    const { conn, socket } = connection(["upload"]);
    const link = new RemoteLink(conn, async () => conn, (end) => ends.push(end));
    let chunks = 0;
    const canceled = vi.fn();
    const body = new ReadableStream({
      pull(controller) { chunks++; controller.enqueue(new Uint8Array(1024 * 1024)); },
      cancel: canceled,
    });
    const request = new Request(`${location.origin}/rt/one/attachments/upload?name=large.apk`, { method: "POST", body, duplex: "half" } as RequestInit);
    await expect(link.fetch(request)).rejects.toThrow(/16 MB/);
    expect(chunks).toBeLessThanOrEqual(18);
    expect(canceled).toHaveBeenCalledOnce();
    expect(socket.sent).toHaveLength(0);
  });

  it("starts again from the first part, under the same id, when the link is lost midway", async () => {
    const first = connection(["replay", "upload"], "desktop-1");
    const second = connection(["replay", "upload"], "desktop-1");
    const link = new RemoteLink(first.conn, async () => second.conn, (end) => ends.push(end), { retryDelaysMs: [10] });
    void link.fetch(upload(24 * 1024 * 3)).catch(() => {});
    await vi.advanceTimersByTimeAsync(0);
    expect(partsOf(first.socket)).toHaveLength(3);
    first.socket.close(1006, "gone");
    await vi.advanceTimersByTimeAsync(50);
    const again = partsOf(second.socket);
    expect(again.map((p) => p.index)).toEqual([0, 1, 2]);
    expect(again[0].id).toBe(partsOf(first.socket)[0].id);
  });
});
