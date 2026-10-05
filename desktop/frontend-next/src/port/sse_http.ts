import { HttpError, KernelBusyError } from "./port";

type Refusal = { code?: string; error?: string; params?: Record<string, string | number> };

// How long a decision waits for the kernel before the card gets control back.
const DECISION_WAIT_MS = 20_000;

// One idempotent read per path in flight. `next` is the single re-read owed to
// every caller that arrived while `flight` was already on the wire.
interface ReadSlot {
  flight: Promise<unknown>;
  next?: Promise<unknown>;
}

// The transport half of SsePort: where the kernel is, and the five shapes every
// call to it takes. Split out because the port itself is the whole AgentPort
// surface — a hundred endpoint methods — and none of them should have to be
// read past to find how a request is actually made.
export class SseHttp {
  private readonly reads = new Map<string, ReadSlot>();

  // rt names the pane this port speaks for. The shell's bus carries every
  // pane's frames, so a channel per runtime is what keeps two live
  // conversations out of each other's transcript.
  constructor(
    protected readonly base = "",
    protected readonly rt = "",
  ) {}

  // A refusal carries a code; only when it does not do we fall back to text.
  // Throwing HttpError with the reason attached keeps that choice at the point
  // that renders it, instead of flattening it to a string here.
  protected static async fail(path: string, res: Response): Promise<never> {
    // Read once as text: a refusal envelope is JSON, but http.Error writes the
    // reason as plain text, and parsing first threw that account away and left
    // the panel showing only a path and a number.
    const raw = (await res.text().catch(() => "")).trim();
    let body: Refusal | null = null;
    try {
      const parsed: unknown = raw === "" ? null : JSON.parse(raw);
      if (parsed && typeof parsed === "object") body = parsed as Refusal;
    } catch {
      // Plain text, which is the whole message.
    }
    const detail = body?.error || raw.slice(0, 400);
    throw new HttpError(res.status, detail || `${path}: ${res.status}`, body ?? undefined, detail !== "");
  }

  protected async post(path: string, body?: unknown): Promise<void> {
    const res = await fetch(this.base + path, {
      method: "POST",
      headers: { "content-type": "application/json" },
      credentials: "same-origin",
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    if (!res.ok) await SseHttp.fail(path, res);
  }

  // A POST whose answer is the payload, not a status code.
  protected async post0<T>(path: string, body?: unknown): Promise<T> {
    const res = await fetch(this.base + path, {
      method: "POST",
      headers: { "content-type": "application/json" },
      credentials: "same-origin",
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    if (!res.ok) await SseHttp.fail(path, res);
    return (await res.json()) as T;
  }

  // A partial update of one resource. Distinct from post because the kernel
  // reads the verb: the same path answers a different question under each.
  protected async patch(path: string, body?: unknown): Promise<void> {
    const res = await fetch(this.base + path, {
      method: "PATCH",
      headers: { "content-type": "application/json" },
      credentials: "same-origin",
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    if (!res.ok) await SseHttp.fail(path, res);
  }

  protected async put0<T>(path: string, body?: unknown): Promise<T> {
    const res = await fetch(this.base + path, {
      method: "PUT",
      headers: { "content-type": "application/json" },
      credentials: "same-origin",
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    if (!res.ok) await SseHttp.fail(path, res);
    return (await res.json()) as T;
  }

  protected async del(path: string): Promise<void> {
    const res = await fetch(this.base + path, { method: "DELETE", credentials: "same-origin" });
    if (!res.ok) await SseHttp.fail(path, res);
  }

  // A DELETE whose answer is the payload. post0's counterpart, and it exists
  // for the same reason: hand-rolling one drops the refusal's code.
  protected async del0<T>(path: string): Promise<T> {
    const res = await fetch(this.base + path, { method: "DELETE", credentials: "same-origin" });
    if (!res.ok) await SseHttp.fail(path, res);
    return (await res.json()) as T;
  }

  // Reads refuse with a code the same way writes do, so this goes through the
  // same fail(). It used to throw a bare Error carrying a path and a number,
  // which spent every refusal the kernel had spelled out — "this server does
  // not open account sign-in" reached the panel as "/account: 403".
  // An idempotent read joins the one in flight for its path and, if it arrived
  // late, the single trailing re-read: a slow endpoint polled faster than it
  // answers would otherwise fill the browser's per-origin connection pool.
  protected get<T>(path: string): Promise<T> {
    const slot = this.reads.get(path);
    if (!slot) return this.startRead<T>(path);
    slot.next ??= slot.flight.then(
      () => this.startRead<T>(path),
      () => this.startRead<T>(path),
    );
    return slot.next as Promise<T>;
  }

  private startRead<T>(path: string): Promise<T> {
    const flight = this.fetchRead<T>(path);
    const slot: ReadSlot = { flight };
    this.reads.set(path, slot);
    const release = () => {
      if (this.reads.get(path) === slot) this.reads.delete(path);
    };
    flight.then(release, release);
    return flight;
  }

  private async fetchRead<T>(path: string): Promise<T> {
    const res = await fetch(this.base + path, { credentials: "same-origin" });
    if (!res.ok) await SseHttp.fail(path, res);
    return (await res.json()) as T;
  }

  // An approval, plan decision or answer: the card is waiting on this one call,
  // so it gets its own bounded wait and a typed failure it can recover from.
  protected async postDecision(path: string, body?: unknown): Promise<void> {
    const ctl = new AbortController();
    const timer = setTimeout(() => ctl.abort(), DECISION_WAIT_MS);
    let answered = false;
    try {
      const res = await fetch(this.base + path, {
        method: "POST",
        headers: { "content-type": "application/json" },
        credentials: "same-origin",
        body: body === undefined ? undefined : JSON.stringify(body),
        signal: ctl.signal,
      });
      answered = true;
      if (!res.ok) await SseHttp.fail(path, res);
    } catch (e) {
      if (!answered && ctl.signal.aborted) throw new KernelBusyError();
      throw e;
    } finally {
      clearTimeout(timer);
    }
  }
}

// A capability scope rides as a query rather than a path segment: every one of
// these endpoints answers for the running workspace when it is absent.
export function rootQuery(root?: string): string {
  return root ? "?root=" + encodeURIComponent(root) : "";
}
