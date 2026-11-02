// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";

afterEach(() => { vi.useRealTimers(); document.querySelectorAll('iframe[title="Mermaid rendering sandbox"]').forEach((frame) => frame.remove()); });

it("releases a failed initial frame so a later diagram can initialize", async () => {
  vi.useFakeTimers();
  vi.resetModules();
  // A fresh module gives this test sole ownership of the frame's startup lifecycle.
  const { renderMermaid } = await import("./mermaid");
  const first = renderMermaid("flowchart TD\nA --> B", "light");
  const failed = expect(first).rejects.toThrow("Mermaid renderer did not load");
  const original = document.querySelector("iframe") as HTMLIFrameElement;
  await vi.advanceTimersByTimeAsync(30_000);
  await failed;
  expect(original.isConnected).toBe(false);
  const recovered = renderMermaid("flowchart TD\nC --> D", "dark");
  const replacement = document.querySelector("iframe") as HTMLIFrameElement;
  expect(replacement).not.toBe(original);
  const posted = vi.spyOn(replacement.contentWindow!, "postMessage");
  window.dispatchEvent(new MessageEvent("message", { source: original.contentWindow, origin: location.origin, data: { type: "mermaid.ready" } }));
  await Promise.resolve();
  expect(posted).not.toHaveBeenCalled();
  window.dispatchEvent(new MessageEvent("message", { source: replacement.contentWindow, origin: location.origin, data: { type: "mermaid.ready" } }));
  await Promise.resolve();
  const request = posted.mock.calls[0]![0] as { id: number; source: string; theme: string };
  expect(request.source).toContain("C --> D");
  expect(request.theme).toBe("dark");
  window.dispatchEvent(new MessageEvent("message", { source: replacement.contentWindow, origin: location.origin, data: { type: "mermaid.result", id: request.id, svg: "<svg/>" } }));
  await expect(recovered).resolves.toBe("<svg/>");
});
