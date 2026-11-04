// @vitest-environment jsdom
import {act,cleanup,render,screen,waitFor} from "@testing-library/react";
import "./testkit";
import userEvent from "@testing-library/user-event";
import {afterEach,beforeEach,expect,it,vi} from "vitest";
import {MockPort} from "../port/mock";
import {HttpError} from "../port/port";
import {AgentCompositionPicker} from "./AgentCompositionPicker";
import {CompositionNewContext} from "./CompositionNewContext";
import {RunLine} from "./RunLine";
beforeEach(() => { localStorage.setItem("rx-show-agent-composition", "on"); });
afterEach(() => { cleanup(); localStorage.removeItem("rx-show-agent-composition"); });

it("shows the restored persona before the module catalog finishes", () => {
  const port = new MockPort();
  vi.spyOn(port, "agentPresets").mockImplementation(() => new Promise(() => {}));
  vi.spyOn(port, "agentComposition").mockImplementation(() => new Promise(() => {}));
  const binding = { id: "project:reviewer", name: "Saved reviewer", revision: "saved", digest: "saved", started: true };
  render(<AgentCompositionPicker port={port} binding={binding} locked running={false} onChanged={()=>{}} onSettings={()=>{}} onError={()=>{}}/>);
  expect(screen.getByRole("button", { name: /^Agent 组合.*Saved reviewer/ }).textContent).toBe("Saved reviewer");
});

it("keeps a known persona visible and management reachable when the catalog fails", async () => {
  const port = new MockPort();
  vi.spyOn(port, "agentPresets").mockRejectedValue(new Error("catalog unavailable"));
  const settings = vi.fn();
  const binding = { id: "project:reviewer", name: "Saved reviewer", revision: "saved", digest: "saved", started: true };
  render(<AgentCompositionPicker port={port} binding={binding} locked running={false} onChanged={()=>{}} onSettings={settings} onError={()=>{}}/>);
  await userEvent.click(screen.getByRole("button", { name: /^Agent 组合.*Saved reviewer/ }));
  await userEvent.click(screen.getByRole("menuitem", { name: "管理 Agent 预设…" }));
  expect(settings).toHaveBeenCalledOnce();
});

it("uses an explicit unbound status instead of another binding read", async () => {
  const port = new MockPort();
  const read = vi.spyOn(port, "agentComposition");
  const binding = { id: "project:reviewer", name: "Saved reviewer", revision: "saved", digest: "saved", started: true };
  const draw = (value: typeof binding | null) => <AgentCompositionPicker port={port} binding={value} running={false} onChanged={()=>{}} onSettings={()=>{}} onError={()=>{}}/>;
  const view = render(draw(binding));
  view.rerender(draw(null));
  expect(screen.getByRole("button", { name: /^Agent 组合.*日常搭档/ }).textContent).toBe("日常搭档");
  expect(read).not.toHaveBeenCalled();
});

it("does not replace an authoritative restored binding with a stale default read", async () => {
  const port = new MockPort();
  let finish!: (value: null) => void;
  const read = vi.spyOn(port, "agentComposition").mockImplementation(() => new Promise(resolve => { finish = resolve; }));
  const binding = { id: "user:reviewer", name: "Saved reviewer", revision: "saved", digest: "saved", started: true };
  const draw = (value?: typeof binding) => <AgentCompositionPicker port={port} binding={value} locked running={false} onChanged={()=>{}} onSettings={()=>{}} onError={()=>{}}/>;
  const view = render(draw());
  await waitFor(() => expect(read).toHaveBeenCalledOnce());
  view.rerender(draw(binding));
  await act(async () => finish(null));
  expect(screen.getByRole("button", { name: /^Agent 组合.*Saved reviewer/ }).textContent).toBe("Saved reviewer");
  expect(read).toHaveBeenCalledOnce();
});

it.each([false, true])("labels saved snapshots without labeling live bindings: live=%s", async (live) => {
  const port = new MockPort();
  vi.spyOn(port, "agentComposition").mockResolvedValue({
    id: "builtin:minimal", name: "轻装搭档", revision: "fixture", digest: "fixture", started: true,
    ...(live ? {formatVersion: 2, resourceMode: "live" as const} : {}),
  });
  render(<AgentCompositionPicker port={port} locked running={false} onChanged={()=>{}} onSettings={()=>{}} onError={()=>{}}/>);
  await userEvent.click(await screen.findByRole("button", {name: /^Agent 组合.*轻装搭档/}));
  expect(screen.queryByText("已固定资源快照") !== null).toBe(!live);
});

it.each([null, "off", "unexpected"])("hides the picker and avoids catalog reads for visibility %s", async (stored) => {
  if (stored === null) localStorage.removeItem("rx-show-agent-composition");
  else localStorage.setItem("rx-show-agent-composition", stored);
  const port = new MockPort();
  const catalog = vi.spyOn(port, "agentPresets");
  const select = vi.spyOn(port, "selectAgentComposition");
  const drawPicker = () => <AgentCompositionPicker port={port} running={false} onChanged={()=>{}} onSettings={()=>{}} onError={()=>{}}/>;
  const view = render(<RunLine label="空闲" running={false} blocked={false} sent={0} received={0} estimated={false}>
    {drawPicker()}
  </RunLine>);
  expect(screen.getByRole("status")).toBeTruthy();
  expect(document.querySelector(".compose-context")).toBeNull();
  expect(screen.queryByRole("button", { name: /^Agent 组合/ })).toBeNull();
  view.rerender(drawPicker());
  expect(screen.queryByRole("button", { name: /^Agent 组合/ })).toBeNull();
  expect(catalog).not.toHaveBeenCalled();
  expect(select).not.toHaveBeenCalled();
});

it("shows the initial default only after opt-in without applying a server preference", async () => {
  const port = new MockPort();
  await port.setDefaultAgentPreset("builtin:minimal", false);
  const select = vi.spyOn(port, "selectAgentComposition");
  render(<RunLine label="空闲" running={false} blocked={false} sent={0} received={0} estimated={false}>
    <AgentCompositionPicker port={port} running={false} onChanged={()=>{}} onSettings={()=>{}} onError={()=>{}}/>
  </RunLine>);
  const picker = await screen.findByRole("button", { name: /^Agent 组合/ });
  expect(picker.textContent).toBe("日常搭档");
  expect(screen.getByRole("status").parentElement?.contains(picker)).toBe(true);
  expect(await port.agentComposition()).toBeNull();
  expect(select).not.toHaveBeenCalled();
});

it("shows an unavailable state with a management entry when the catalog fails", async () => {
  const port = new MockPort();
  vi.spyOn(port, "agentPresets").mockRejectedValue(new Error("catalog unavailable"));
  const settings = vi.fn();
  render(<AgentCompositionPicker port={port} running={false} onChanged={()=>{}} onSettings={settings} onError={()=>{}}/>);
  const failed = await screen.findByRole("button", { name: "Agent 预设暂不可用" });
  await userEvent.click(failed);
  expect(settings).toHaveBeenCalledOnce();
});

it("groups composition controls with the run state without announcing controls as live status",async()=>{
  const port=new MockPort();const changed=vi.fn();const settings=vi.fn();
  const user=userEvent.setup();
  render(<RunLine label="空闲" running={false} blocked={false} sent={0} received={0} estimated={false}>
    <AgentCompositionPicker port={port} running={false} onChanged={changed} onSettings={settings} onError={()=>{}}/>
  </RunLine>);
  const picker=await screen.findByRole("button",{name:/^Agent 组合/});
  const status=screen.getByRole("status");
  expect(status.parentElement?.contains(picker)).toBe(true);
  expect(status.contains(picker)).toBe(false);
  expect(picker.compareDocumentPosition(status) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  expect(picker.textContent).toBe("日常搭档");
  expect(screen.queryByRole("button",{name:"管理 Agent 预设"})).toBeNull();
  await user.click(picker);
  const review=screen.getByRole("menuitem",{name:/轻装搭档/});
  expect(review.getAttribute("data-action")).toBe("agent-composition.select");
  await user.click(review);
  expect(changed).toHaveBeenCalledOnce();
  expect(picker.getAttribute("aria-label")).toContain("轻装搭档");
  await user.click(picker);
  const manage=screen.getByRole("menuitem",{name:"管理 Agent 预设…"});
  expect(manage.getAttribute("data-action")).toBe("agent-composition.manage");
  await user.click(manage);
  expect(settings).toHaveBeenCalledOnce();
});

it("keeps started conversations locked and opens the chosen composition in a new conversation",async()=>{
  const port=new MockPort();const create=vi.fn();const select=vi.spyOn(port,"selectAgentComposition");
  const user=userEvent.setup();
  render(<CompositionNewContext.Provider value={create}><AgentCompositionPicker port={port} locked running={false} onChanged={()=>{}} onSettings={()=>{}} onError={()=>{}}/></CompositionNewContext.Provider>);
  const picker=await screen.findByRole("button",{name:/^Agent 组合/});
  expect(picker.textContent).toBe("日常搭档");
  expect(picker.getAttribute("aria-label")).toContain("已锁定");
  await user.click(picker);
  expect(screen.getByText("该会话已开始，模式已锁定。请用其他模式新建会话。")).toBeTruthy();
  const review=screen.getByRole("menuitem",{name:/轻装搭档/});
  expect(review.getAttribute("data-action")).toBe("agent-composition.new");
  await user.click(review);
  expect(create).toHaveBeenCalledWith("builtin:minimal");
  expect(select).not.toHaveBeenCalled();
});

it("supports keyboard selection and Escape with focus returning to the one trigger",async()=>{
  const port=new MockPort();const select=vi.spyOn(port,"selectAgentComposition");
  const user=userEvent.setup();
  render(<AgentCompositionPicker port={port} running={false} onChanged={()=>{}} onSettings={()=>{}} onError={()=>{}}/>);
  const picker=await screen.findByRole("button",{name:/^Agent 组合/});
  picker.focus();await user.keyboard("{Enter}{ArrowDown}{Enter}");
  await waitFor(()=>expect(select).toHaveBeenCalledWith("builtin:minimal"));
  expect(document.activeElement).toBe(picker);
  await user.click(picker);await user.keyboard("{Escape}");
  expect(screen.queryByRole("menu")).toBeNull();
  expect(document.activeElement).toBe(picker);
});

it("keeps a failed selection unchanged and prevents duplicate submissions while pending",async()=>{
  const port=new MockPort();const error=vi.fn();const changed=vi.fn();
  let reject!:(error:Error)=>void;
  const select=vi.spyOn(port,"selectAgentComposition").mockImplementation(()=>new Promise((_,fail)=>{reject=fail;}));
  const user=userEvent.setup();
  render(<AgentCompositionPicker port={port} running={false} onChanged={changed} onSettings={()=>{}} onError={error}/>);
  const picker=await screen.findByRole("button",{name:/^Agent 组合/});
  await user.click(picker);await user.click(screen.getByRole("menuitem",{name:/轻装搭档/}));
  await user.click(picker);
  expect((screen.getByRole("menuitem",{name:/日常搭档/}) as HTMLButtonElement).disabled).toBe(true);
  expect(document.activeElement).toBe(screen.getByRole("menuitem",{name:"管理 Agent 预设…"}));
  await user.click(screen.getByRole("menuitem",{name:/日常搭档/}));
  expect(select).toHaveBeenCalledOnce();
  reject(new Error("fixture failure"));
  await waitFor(()=>expect(error).toHaveBeenCalledOnce());
  expect(picker.textContent).toContain("日常搭档");
  expect(changed).not.toHaveBeenCalled();
});

it("reports the failed MCP dependency and lets the user retry without changing selection", async () => {
  const port = new MockPort();
  const error = vi.fn();
  const changed = vi.fn();
  vi.spyOn(port, "selectAgentComposition").mockRejectedValueOnce(new HttpError(409, "fallback", {
    code: "agent_composition.mcp_unavailable",
    params: { module: "docs", detail: "http 401" },
  }));
  const user = userEvent.setup();
  render(<AgentCompositionPicker port={port} running={false} onChanged={changed} onSettings={()=>{}} onError={error}/>);
  const picker = await screen.findByRole("button", { name: /^Agent 组合/ });
  await user.click(picker);
  await user.click(screen.getByRole("menuitem", { name: /轻装搭档/ }));
  await waitFor(() => expect(error).toHaveBeenCalledWith(expect.stringContaining("MCP 服务 docs")));
  expect(error.mock.calls[0][0]).toContain("http 401");
  expect(picker.textContent).toContain("日常搭档");
  expect(changed).not.toHaveBeenCalled();
  await user.click(picker);
  await user.click(screen.getByRole("menuitem", { name: /轻装搭档/ }));
  await waitFor(() => expect(changed).toHaveBeenCalledOnce());
});

it("keeps management accessible when a locked host cannot create conversations",async()=>{
  const port=new MockPort();const settings=vi.fn();const select=vi.spyOn(port,"selectAgentComposition");
  const user=userEvent.setup();
  render(<AgentCompositionPicker port={port} locked running={false} onChanged={()=>{}} onSettings={settings} onError={()=>{}}/>);
  await user.click(await screen.findByRole("button",{name:/^Agent 组合.*已锁定/}));
  expect(screen.getAllByRole("menuitem")).toHaveLength(1);
  await user.click(screen.getByRole("menuitem",{name:"管理 Agent 预设…"}));
  expect(settings).toHaveBeenCalledOnce();
  expect(select).not.toHaveBeenCalled();
});

it("distinguishes equal names by source and retains a deleted binding's name",async()=>{
  const port=new MockPort();const catalog=await port.agentPresets();
  const original=catalog.presets[0];
  vi.spyOn(port,"agentPresets").mockResolvedValue({...catalog,presets:[
    {...original,definition:{...original.definition,id:"user:review",name:"同名审查"}},
    {...original,definition:{...original.definition,id:"project:review",name:"同名审查"}},
  ]});
  vi.spyOn(port,"agentComposition").mockResolvedValue({id:"user:deleted",name:"已删除但绑定的预设",digest:"frozen",revision:"1",started:false});
  const user=userEvent.setup();
  render(<AgentCompositionPicker port={port} running={false} onChanged={()=>{}} onSettings={()=>{}} onError={()=>{}}/>);
  const trigger=await screen.findByRole("button",{name:/^Agent 组合.*已删除但绑定的预设/});
  await user.click(trigger);
  const rows=screen.getAllByRole("menuitem",{name:/同名审查/});
  expect(rows.map(row=>row.textContent)).toEqual([expect.stringContaining("个人"),expect.stringContaining("项目")]);
});
