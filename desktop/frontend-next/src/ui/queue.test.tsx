import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { Queue } from "./Queue";
import { BLOCK_WHY } from "../i18n/queue_why";
import { EN } from "../i18n/en";
import type { Queue as QueueSnapshot, QueueItem } from "../port/port";

const item = (over: Partial<QueueItem> = {}): QueueItem => ({
  id: "i1",
  intent: "steer",
  state: "steer_accepted",
  preview: "把 gate_test.go 里那三个跳过的用例打开",
  createdAt: "2026-08-25T10:00:00Z",
  ...over,
});

const snapshot = (over: Partial<QueueSnapshot> = {}): QueueSnapshot => ({
  revision: 1,
  paused: false,
  items: [item()],
  capacity: { items: 1, maxItems: 64, bytes: 3174, maxBytes: 64 << 20 },
  ...over,
});

const paint = (q: QueueSnapshot, running: boolean) =>
  renderToStaticMarkup(
    <Queue
      queue={q}
      running={running}
      onRead={async () => ""}
      onEdit={() => {}}
      onMove={() => {}}
      onSendNow={() => {}}
      onCancel={() => {}}
      onRetry={() => {}}
      onRefresh={() => {}}
      onPause={() => {}}
    />,
  );

const draw = (q: QueueSnapshot) => paint(q, true);
const drawIdle = (q: QueueSnapshot) => paint(q, false);

describe("why a held entry stopped", () => {
  const held = (over: Partial<QueueItem>) => draw(snapshot({ paused: true, items: [item({ state: "uncertain", ...over })] }));

  it("words a coded stop in this build's language, not the kernel's English", () => {
    const html = held({ blockCode: "steer_unapplied", blockReason: "steer accepted but unapplied before turn exit" });
    expect(html).toContain("没来得及送达");
    expect(html).not.toContain("unapplied before turn exit");
  });

  it.each(Object.entries(BLOCK_WHY))("%s reads in Chinese and has an English sentence", (code, zh) => {
    const html = held({ blockCode: code, blockReason: "kernel diagnostic" });
    expect(html).toContain(zh);
    expect(html).not.toContain("kernel diagnostic");
    expect(EN[zh], `no English for ${code}`).toBeTruthy();
    expect(EN[zh]).not.toBe(zh);
  });

  it("covers exactly the codes the kernel sends", () => {
    expect(Object.keys(BLOCK_WHY).sort()).toEqual(
      ["manifest_salvaged", "owner_inactive", "steer_unapplied", "turn_ack_failed", "turn_snapshot_failed"],
    );
  });

  it("keeps the kernel's text for a code this build has no sentence for", () => {
    expect(held({ blockCode: "future_code", blockReason: "something new" })).toContain("something new");
  });
});

// Typing at a working turn does reach the model — the report was that nobody
// could tell. Every assertion here is on what the strip says back, because a
// steer whose only evidence is the model's next sentence reads as ignored.
describe("what the pending strip says back", () => {
  // A background job's follow-on is queued the same way a typed line is, so
  // without saying whose it is the strip reports the runtime as the user.
  // "To send" is the person's outbox. A background job's continuation is not
  // something they typed, and listing it there asked them to take back a line
  // they never wrote.
  it("keeps a host continuation out of the outbox", () => {
    expect(draw(snapshot({ items: [item({ origin: "host", state: "queued", intent: "followup" })] }))).toBe("");
  });

  // Unless it is stuck: a decision only they can make is the one case it has
  // to reach them here.
  it("shows a host continuation that needs a decision", () => {
    const html = draw(snapshot({ items: [item({ origin: "host", state: "blocked", intent: "followup" })] }));
    expect(html).toContain("受阻");
  });

  it("names an accepted steer as taken, not as waiting", () => {
    const html = draw(snapshot());
    expect(html).toContain("插话已收");
    expect(html).toContain('data-tone="accent"');
  });

  it("keeps a queued follow-up apart from a steer the kernel took", () => {
    const html = draw(snapshot({ items: [item({ intent: "followup", state: "queued" })] }));
    expect(html).toContain("尚未送达，可以改，也可以取回");
    expect(html).not.toContain("插话已收");
  });

  it("offers taking it back while that is still true", () => {
    expect(draw(snapshot())).toContain("取回");
  });

  // Past the tool boundary the text is the model's, and the kernel refuses the
  // withdrawal. The entry does not stay on as a read-only copy either: the
  // transcript has the line by then, and two of it is what read as stuck.
  it("hands a line over to the transcript once the turn has read it", () => {
    expect(draw(snapshot({ items: [item({ state: "steer_consumed" })] }))).toBe("");
  });

  // A follow-up the kernel dispatched is the running turn, and the running turn
  // is the transcript's to draw.
  it("hands a dispatched follow-up over the same way", () => {
    expect(draw(snapshot({ items: [item({ intent: "followup", state: "running" })] }))).toBe("");
  });

  // Waiting needs no button; this is the other case, and it is only meaningful
  // against a turn that is actually holding the session.
  it("offers sending now only while a turn is in the way", () => {
    expect(draw(snapshot())).toContain("立即发送");
    expect(drawIdle(snapshot())).not.toContain("立即发送");
  });

  it("keeps technical capacity out of the ordinary one-line queue", () => {
    const html = draw(snapshot());
    expect(html).not.toContain("1/64");
    expect(html).not.toContain("3.1k/64M");
    expect(html).toContain("尚未送达，可以改，也可以取回");
  });

  // Capacity becomes useful when it needs attention rather than occupying the
  // primary layout for every one-line follow-up.
  it("reveals both capacity limits when either is under pressure", () => {
    const html = draw(snapshot({ capacity: { items: 49, maxItems: 64, bytes: 3174, maxBytes: 64 << 20 } }));
    expect(html).toContain("49/64");
    expect(html).toContain("3.1k/64M");
  });

  it("renders nothing when there is nothing waiting and the queue is not held", () => {
    expect(draw(snapshot({ items: [], capacity: { items: 0, maxItems: 64, bytes: 0, maxBytes: 64 << 20 } }))).toBe("");
  });

  it("renders nothing when a held queue has no pending work", () => {
    const html = draw(snapshot({ paused: true, items: [], capacity: { items: 0, maxItems: 64, bytes: 0, maxBytes: 64 << 20 } }));
    expect(html).toBe("");
  });
});
