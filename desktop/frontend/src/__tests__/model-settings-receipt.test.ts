import assert from "node:assert/strict";
import type { AppBindings } from "../lib/bridge";
import type { ModelSettingsResult } from "../lib/types";
import { baseSettings } from "../test-support/settingsTestFixtures";
import { saveModelSettings } from "../lib/modelSettings";

let writes = 0, reads = 0;
const receipt: ModelSettingsResult = {requestId:"",persisted:true,revision:"revision-1",application:"failed",targets:[{tabId:"tab",application:"failed",appliedRevision:"old",desiredRevision:"new"}],issues:[{code:"apply_failed",message:"Saved; retry applying to this session."}],appliedCatalogs:[]};
const bindings: Partial<AppBindings> = {
  async ApplyModelSettings(change) {
    writes++;
    receipt.requestId = change.requestId;
    throw new Error("bridge response lost");
  },
  async GetModelSettingsRequest(id) {
    reads++;
    assert.equal(id,receipt.requestId);
    return receipt;
  },
};
Object.assign(globalThis,{window:{go:{main:{App:bindings}}}});
const saved = await saveModelSettings(baseSettings(),{kind:"credential",name:"provider",key:"test-key"});
assert.equal(saved.persisted,true);
assert.equal(saved.application,"failed","saved-but-not-applied remains distinct from a write failure");
assert.equal(writes,1,"lost response is never replayed");
assert.equal(reads,1,"lost response reads its original receipt");
receipt.persisted=false;
receipt.issues=[{code:"validation",message:"Provider no longer exists"}];
await assert.rejects(saveModelSettings(baseSettings(),{kind:"credential",name:"provider",key:"test-key"}),/Provider no longer exists/);
bindings.GetModelSettingsRequest=async()=>{throw new Error("bridge down");};
await assert.rejects(saveModelSettings(baseSettings(),{kind:"credential",name:"provider",key:"test-key"}),/could not be confirmed/);
assert.equal(writes,3,"each user operation writes once despite all lost responses");
console.log("PASS: receipt readback, no write replay, saved application failure and unknown outcome");
