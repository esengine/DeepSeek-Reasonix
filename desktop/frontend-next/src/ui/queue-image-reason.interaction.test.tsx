// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render } from "@testing-library/react";
import "./testkit";
import { Queue } from "./Queue";
import type { Queue as QueueSnapshot, QueueItem } from "../port/port";
import { t } from "../i18n";

afterEach(() => { cleanup(); vi.restoreAllMocks(); });

const item = (over: Partial<QueueItem> = {}): QueueItem => ({
  id: "i1", intent: "steer", state: "queued", preview: "@.reasonix/attachments/shot.png look", createdAt: "2026-10-17T10:00:00Z", ...over,
});

function rows(items: QueueItem[]) {
  const queue: QueueSnapshot = { revision: 1, paused: false, items, capacity: { items: items.length, maxItems: 64, bytes: 10, maxBytes: 64 << 20 } };
  const noop = () => {};
  const { container } = render(
    <Queue queue={queue} running onSendNow={noop} onToSteer={noop} onCancel={noop} onRetry={noop} onPause={noop} />,
  );
  return container;
}

describe("why a queued line has not been delivered", () => {
  const reason = t("composer.has_an_image_sent_when");

  it("says so on a line that carries a picture", () => {
    rows([item({ images: true })]);
    expect(document.querySelector(".queue")?.textContent).toContain(reason);
  });

  it("keeps the ordinary state word on a text-only line, next to a picture line", () => {
    rows([item({ id: "a", images: true }), item({ id: "b", preview: "words only" })]);
    const text = document.querySelector(".queue")?.textContent ?? "";
    expect(text.split(reason).length - 1).toBe(1);
    expect(text).toContain(t("composer.steer_queued"));
  });

  it("does not claim a picture wait for a steer the turn already took", () => {
    rows([item({ images: true, state: "steer_accepted" })]);
    expect(document.querySelector(".queue")?.textContent).not.toContain(reason);
    expect(document.querySelector(".queue")?.textContent).toContain(t("composer.steer_accepted"));
  });
});
