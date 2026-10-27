// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import type { AgentPort } from "../port/port";
import { useAgentHistory } from "./useAgentHistory";

afterEach(cleanup);

it("loads only the latest page until earlier history is requested", async () => {
  const read = vi.fn(async (ref: string, after: number) => ({ run: {ref,state:"completed"}, messages: [], after: after<0?900:after, next:after<0?1000:after+100,total:1000,live:false }));
  const port = {subagentTranscript:read} as unknown as AgentPort;
  const view = renderHook(() => useAgentHistory(port,"child",true));
  await waitFor(() => expect(view.result.current.before).toBe(900));
  expect(read).toHaveBeenCalledTimes(1);
  expect(read).toHaveBeenCalledWith("child",-1,100);
  await act(async () => { await view.result.current.earlier(); });
  expect(read).toHaveBeenLastCalledWith("child",800,100);
  expect(view.result.current.before).toBe(800);
});

it("drops an earlier-page reply after switching children and preserves failed cursors", async () => {
  let resolve!: (value: unknown) => void;
  const read = vi.fn(async (ref: string, after: number) => {
    if (after>=0) return new Promise(done => {resolve=done});
    return {run:{ref,state:"completed"},messages:[],after:100,next:200,total:200,live:false};
  });
  const port = {subagentTranscript:read} as unknown as AgentPort;
  const view = renderHook(({ref}) => useAgentHistory(port,ref,true),{initialProps:{ref:"one"}});
  await waitFor(() => expect(view.result.current.before).toBe(100));
  act(() => {void view.result.current.earlier();});
  view.rerender({ref:"two"});
  await waitFor(() => expect(view.result.current.info?.ref).toBe("two"));
  await act(async () => {resolve({run:{ref:"one"},messages:[],after:0,next:100,total:200,live:false});});
  expect(view.result.current.info?.ref).toBe("two");
  expect(view.result.current.before).toBe(100);
});
