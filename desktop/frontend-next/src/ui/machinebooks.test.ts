// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, renderHook, waitFor } from "@testing-library/react";
import { useMachineBooks } from "./machinebooks";
import type { HubPort, TreeWorkspace } from "../port/hub";
import type { RemoteHost } from "../port/remote";

const tree: TreeWorkspace[] = [{ root: "/root", name: "root", sessions: [] } as unknown as TreeWorkspace];
const host = (name: string, status: string) => ({ name, status, target: "root@h" }) as unknown as RemoteHost;
const hubWith = (remoteTree: (h: string) => Promise<TreeWorkspace[] | null>) => ({ remoteTree }) as unknown as HubPort;

afterEach(() => localStorage.clear());

describe("a machine's book", () => {
  it("is read on demand while nothing is open on it, and remembered", async () => {
    const remoteTree = vi.fn(async () => tree);
    const { result } = renderHook(() => useMachineBooks(hubWith(remoteTree), [host("my-server", "idle")]));
    expect(remoteTree).not.toHaveBeenCalled();
    await act(() => result.current.read("my-server"));
    expect(result.current.trees["my-server"]).toEqual(tree);
    expect(JSON.parse(localStorage.getItem("rx-remote-books") ?? "[]")).toEqual(["my-server"]);
  });

  it("is read again on start for a machine read before, and not for one never read", async () => {
    localStorage.setItem("rx-remote-books", JSON.stringify(["my-server"]));
    const remoteTree = vi.fn(async () => tree);
    renderHook(() => useMachineBooks(hubWith(remoteTree), [host("my-server", "idle"), host("other", "idle")]));
    await waitFor(() => expect(remoteTree).toHaveBeenCalledWith("my-server"));
    expect(remoteTree).not.toHaveBeenCalledWith("other");
  });

  it("hands a failed read to the caller", async () => {
    const remoteTree = vi.fn(async () => {
      throw new Error("remote.unreachable");
    });
    const { result } = renderHook(() => useMachineBooks(hubWith(remoteTree), [host("my-server", "idle")]));
    await expect(result.current.read("my-server")).rejects.toThrow("remote.unreachable");
    expect(localStorage.getItem("rx-remote-books")).toBeNull();
  });
});
