type Pending = { resolve: (svg: string) => void; reject: (error: Error) => void; timer: number };
let frame: HTMLIFrameElement | undefined;
let ready: Promise<void> | undefined;
let unlisten: (() => void) | undefined;
let sequence = 0;
const pending = new Map<number, Pending>();

/** The renderer's CSP blocks graph resources before Mermaid inserts any measuring DOM. */
export async function renderMermaid(source: string, theme: "light" | "dark"): Promise<string> {
  if (!ready) {
    ready = new Promise<void>((resolve, reject) => {
      const timer = window.setTimeout(() => reject(new Error("Mermaid renderer did not load")), 30_000);
      frame = document.createElement("iframe");
      frame.title = "Mermaid rendering sandbox";
      frame.setAttribute("sandbox", "allow-scripts allow-same-origin");
      frame.setAttribute("aria-hidden", "true");
      frame.tabIndex = -1;
      frame.style.cssText = "position:fixed;left:-10000px;top:0;width:1200px;height:900px;visibility:hidden;pointer-events:none";
      const currentFrame = frame;
      const receive = (event: MessageEvent) => {
        if (event.source !== currentFrame.contentWindow || event.origin !== location.origin) return;
        if (event.data?.type === "mermaid.ready") {
          clearTimeout(timer);
          resolve();
        } else if (event.data?.type === "mermaid.result") {
          const request = pending.get(event.data.id);
          if (!request) return;
          clearTimeout(request.timer);
          pending.delete(event.data.id);
          if (typeof event.data.svg === "string") request.resolve(event.data.svg);
          else request.reject(new Error(String(event.data.error)));
        }
      };
      window.addEventListener("message", receive);
      unlisten = () => window.removeEventListener("message", receive);
      frame.src = new URL("./mermaid-frame.html", document.baseURI).href;
      document.body.append(frame);
    });
  }
  const loading = ready;
  try {
    await loading;
  } catch (error) {
    if (ready === loading) {
      unlisten?.();
      unlisten = undefined;
      frame?.remove();
      frame = undefined;
      ready = undefined;
    }
    throw error;
  }
  return new Promise<string>((resolve, reject) => {
    const id = ++sequence;
    const timer = window.setTimeout(() => {
      pending.delete(id);
      reject(new Error("Mermaid rendering timed out"));
    }, 30_000);
    pending.set(id, { resolve, reject, timer });
    frame!.contentWindow!.postMessage({ type: "mermaid.render", id, source, theme }, location.origin);
  });
}
