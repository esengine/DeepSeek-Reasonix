import { SseHook } from "./sse_hook";
import type { NetworkProbe, NetworkSettings } from "./port";

// How the kernel reaches the outside, and what probing that found.
export class SseNetwork extends SseHook {
  network() {
    return this.get<NetworkSettings>("/network");
  }
  async saveNetwork(settings: NetworkSettings, password: string, clearPassword: boolean) {
    const res = await fetch(this.base + "/network", {
      method: "POST",
      headers: { "content-type": "application/json" },
      credentials: "same-origin",
      body: JSON.stringify({ ...settings, password, clearPassword }),
    });
    const body = (await res.json().catch(() => ({}))) as NetworkSettings & { error?: string };
    if (!res.ok) throw new Error(body.error || `/network: ${res.status}`);
    return body;
  }
  async diagnoseNetwork() {
    const r = await this.post0<{ probes?: NetworkProbe[] }>("/network/diagnose");
    return r.probes ?? [];
  }
}
