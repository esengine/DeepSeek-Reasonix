// @vitest-environment jsdom
import { act } from "react";
import { createRoot } from "react-dom/client";
import { describe, expect, it, vi } from "vitest";
import { RemoteNotice } from "./RemoteNotice";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

function mount(props: Partial<Parameters<typeof RemoteNotice>[0]>) {
  const host = document.createElement("div");
  document.body.append(host);
  const onAction = vi.fn();
  const root = createRoot(host);
  act(() => root.render(<RemoteNotice title="t" body="b" action="Go" onAction={onAction} {...props} />));
  return { onAction, host, stop: () => act(() => root.unmount()) };
}

describe("remote notice", () => {
  it("retries by itself when the network returns, only if asked to", () => {
    const waiting = mount({ retryWhenOnline: true });
    act(() => { dispatchEvent(new Event("online")); });
    expect(waiting.onAction).toHaveBeenCalledOnce();
    waiting.stop();
    act(() => { dispatchEvent(new Event("online")); });
    expect(waiting.onAction).toHaveBeenCalledOnce();

    const plain = mount({});
    act(() => { dispatchEvent(new Event("online")); });
    expect(plain.onAction).not.toHaveBeenCalled();
    plain.stop();
  });

  it("holds the button until the wait is over", () => {
    vi.useFakeTimers();
    const view = mount({ waitS: 2 });
    const button = view.host.querySelector("button")!;
    expect(button.disabled).toBe(true);
    act(() => { vi.advanceTimersByTime(2000); });
    expect(button.disabled).toBe(false);
    view.stop();
    vi.useRealTimers();
  });
});
