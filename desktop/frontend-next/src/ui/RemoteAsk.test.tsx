// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import type { RemoteAsk as Ask } from "../port/remote";
import { RemoteAsk } from "./RemoteAsk";

afterEach(cleanup);
const base: Ask = {epoch:"epoch",operationId:"connect",askId:"first",createdAt:"2026-11-02T00:00:00Z",kind:"keyboard-interactive",host:"loopback",name:"Server title",instruction:"Exact instruction\nSecond line",question:"Unclassified prompt:",echo:false,answerRequired:true};

it("renders exact server text, echo flags, and clears answers between prompts",()=>{
 const answer=vi.fn();
 const view=render(<RemoteAsk ask={base} onAnswer={answer}/>);
 expect(screen.getByRole("heading").textContent).toBe(base.name);
 expect(screen.getByText("Exact instruction",{exact:false}).textContent).toBe(base.instruction);
 const hidden=screen.getByLabelText(base.question!);
 expect(hidden.getAttribute("type")).toBe("password");
 fireEvent.change(hidden,{target:{value:"one-time"}});
 fireEvent.keyDown(hidden,{key:"Enter"});
 expect(answer).toHaveBeenLastCalledWith("first",true,"one-time");
 view.rerender(<RemoteAsk ask={{...base,askId:"second",question:"Visible identifier:",echo:true}} onAnswer={answer}/>);
 const visible=screen.getByLabelText("Visible identifier:") as HTMLInputElement;
 expect(visible.type).toBe("text");
 expect(visible.value).toBe("");
 fireEvent.change(visible,{target:{value:"identifier"}});
 fireEvent.click(screen.getByRole("button",{name:/继续|Continue/}));
 expect(answer).toHaveBeenLastCalledWith("second",true,"identifier");
 fireEvent.keyDown(window,{key:"Escape"});
 expect(answer).toHaveBeenLastCalledWith("second",false,"");
});

it("supports instruction-only rounds without inventing a question",()=>{
 const answer=vi.fn();
 render(<RemoteAsk ask={{...base,answerRequired:false,question:undefined}} onAnswer={answer}/>);
 expect(document.querySelector("input")).toBeNull();
 fireEvent.click(screen.getByRole("button",{name:/继续|Continue/}));
 expect(answer).toHaveBeenCalledWith("first",true,"");
});

it.each(["password","passphrase"] as const)("preserves %s dialogs and cancellation",kind=>{
 const answer=vi.fn();
 render(<RemoteAsk ask={{...base,kind,identityFile:kind==="passphrase"?"id_ed25519":undefined}} onAnswer={answer}/>);
 const input=document.querySelector("input")!;
 expect(input.type).toBe("password");
 fireEvent.change(input,{target:{value:"reusable"}});
 fireEvent.keyDown(input,{key:"Enter"});
 expect(answer).toHaveBeenLastCalledWith("first",true,"reusable");
 fireEvent.click(screen.getByRole("button",{name:/取消|Cancel/}));
 expect(answer).toHaveBeenLastCalledWith("first",false,"");
});

it("keeps host-key verification explicit and focused on cancellation",()=>{
 const answer=vi.fn();
 render(<RemoteAsk ask={{...base,kind:"hostkey",fingerprint:"SHA256:complete-fingerprint",keyType:"ssh-ed25519",address:"127.0.0.1:22"}} onAnswer={answer}/>);
 expect(screen.getByText("SHA256:complete-fingerprint")).toBeTruthy();
 expect(document.querySelector("input")).toBeNull();
 expect(document.activeElement?.textContent).toMatch(/取消|Cancel/);
 fireEvent.click(screen.getByRole("button",{name:/指纹一致|fingerprint/i}));
 expect(answer).toHaveBeenCalledWith("first",true,"");
});
