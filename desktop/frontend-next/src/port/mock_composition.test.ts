// @vitest-environment jsdom
import { describe, expect, it } from "vitest";
import { MockHub } from "./mock_hub";
import { builtinCompositions } from "./composition";

describe("remote composition ownership", () => {
  it("uses each remote host's own catalog and selected composition", async () => {
    const hub = new MockHub();
    const local = await hub.open({root:"/same"});
    const remote = await hub.openRemote({host:"gpu-box",workspace:"/same",agentComposition:"builtin:minimal"});
    const another = await hub.openRemote({host:"builder",workspace:"/same"});
    const custom = {...builtinCompositions()[0],id:"user:only-remote",name:"Remote only"};
    await hub.portFor(remote).saveAgentPreset(custom,"");
    expect(remote.agentComposition?.id).toBe("builtin:minimal");
    expect((await hub.portFor(local).agentPresets()).presets.some(v=>v.definition.id===custom.id)).toBe(false);
    expect((await hub.portFor(another).agentPresets()).presets.some(v=>v.definition.id===custom.id)).toBe(false);
    const sibling = await hub.openRemote({host:"gpu-box",workspace:"/same",agentComposition:custom.id});
    expect(sibling.agentComposition?.id).toBe(custom.id);
  });
});
