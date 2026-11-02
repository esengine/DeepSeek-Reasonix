import { afterEach, expect, it, vi } from "vitest";
import { SseHub } from "./hub";
import type { RemoteAsk } from "./remote";

afterEach(()=>{vi.useRealTimers();vi.unstubAllGlobals();});
const ask:RemoteAsk={epoch:"live-kernel",operationId:"supervisor-reconnect",askId:"fresh-otp",createdAt:"2026-11-02T00:00:00Z",kind:"keyboard-interactive",host:"gpu",name:"Verify",instruction:"Current code",question:"Challenge:",answerRequired:true};

it("delivers a fresh supervisor challenge after the initiating request has completed",async()=>{
 vi.useFakeTimers();
 let pending:RemoteAsk[]=[];
 const fetch=vi.fn(async(path:string)=>new Response(JSON.stringify(path==="/asks"?{asks:pending}:{id:"pane",host:"gpu",root:"/project",base:"/rt/pane",name:"GPU"}),{status:200}));
 vi.stubGlobal("fetch",fetch);
 const hub=new SseHub();
 const receive=vi.fn();
 const stop=hub.onRemoteAsk(receive);
 await hub.openRemote({host:"gpu",workspace:"/project"});
 pending=[ask];
 await vi.advanceTimersByTimeAsync(500);
 expect(receive).toHaveBeenCalledExactlyOnceWith(ask);
 hub.answerRemote(ask.askId,true,"fresh-answer");
 const [,init]=fetch.mock.calls.find(([path])=>path.includes("/answer"))! as unknown as [string,RequestInit];
 expect(JSON.parse(init.body as string)).toEqual({epoch:ask.epoch,operationId:ask.operationId,answer:{ok:true,text:"fresh-answer"}});
 pending=[];
 stop();
 const calls=fetch.mock.calls.length;
 await vi.advanceTimersByTimeAsync(1500);
 expect(fetch.mock.calls.length).toBe(calls);
});

it("finds unanswered reconnect challenges on a new window subscription",async()=>{
 vi.useFakeTimers();
 vi.stubGlobal("fetch",vi.fn(async()=>new Response(JSON.stringify({asks:[ask]}),{status:200})));
 const hub=new SseHub();
 const first=vi.fn();
 const stop=hub.onRemoteAsk(first);
 await vi.advanceTimersByTimeAsync(500);
 expect(first).toHaveBeenCalledExactlyOnceWith(ask);
 stop();
 const second=vi.fn();
 const stopAgain=hub.onRemoteAsk(second);
 await vi.advanceTimersByTimeAsync(500);
 expect(second).toHaveBeenCalledExactlyOnceWith(ask);
 stopAgain();
});
