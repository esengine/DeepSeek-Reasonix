// @vitest-environment jsdom
import { act, renderHook } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { useNewConversation } from "./useNewConversation";
import type { RuntimeView } from "../port/hub";

it.each([undefined, "gpu"])("starts in the focused project's environment: %s", async (host) => {
  const open = vi.fn().mockResolvedValue(undefined);
  const remote = vi.fn().mockResolvedValue(undefined);
  const runtime = { id: "active", root: "/shared/project", host } as RuntimeView;
  const { result } = renderHook(() => useNewConversation("active", [runtime], "/other", open, remote));
  await act(() => result.current());
  if (host) {
    expect(remote).toHaveBeenCalledWith("gpu", "/shared/project");
    expect(open).not.toHaveBeenCalled();
  } else {
    expect(open).toHaveBeenCalledWith({ root: "/shared/project" });
    expect(remote).not.toHaveBeenCalled();
  }
});
