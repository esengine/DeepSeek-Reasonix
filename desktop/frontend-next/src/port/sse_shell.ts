import { SseExtensions } from "./sse_ext";
import type { ContextBreakdown, ShellSettings } from "./port";

// What the turn runs inside: the shell it shells out to, and how much of the
// window the context has left.
export class SseShell extends SseExtensions {
  context() {
    return this.get<ContextBreakdown>("/context");
  }
  // post0 so the refusal keeps its code: "not while a turn is running" and "this
  // server does not let you edit sources" send the reader to different places.
  setContextWindow(window: number) {
    return this.post0<ContextBreakdown>("/context/window", { window });
  }
  shell() {
    return this.get<ShellSettings>("/shell");
  }
  // post0 for the same reason setContextWindow uses it: a refusal here carries
  // a code, and hand-rolling the fetch threw it away — "this server does not
  // let you set the shell" reached the panel in the kernel's English.
  saveShell(prefer: string, path: string) {
    return this.post0<ShellSettings>("/shell", { prefer, path });
  }
}
