import mermaid from "mermaid";
import { sanitizeMermaidSvg } from "./mermaidSvg";

let queue = Promise.resolve();
window.addEventListener("message", (event: MessageEvent) => {
  if (event.source !== parent || event.origin !== location.origin) return;
  const request = event.data;
  if (request?.type !== "mermaid.render" || typeof request.id !== "number" || typeof request.source !== "string" || !["light", "dark"].includes(request.theme)) return;
  queue = queue.then(async () => {
    try {
      if (request.source.length > 50_000) throw new Error("Mermaid source exceeds the 50,000 character rendering limit");
      mermaid.initialize({
        startOnLoad: false,
        securityLevel: "strict",
        secure: Object.keys(mermaid.mermaidAPI.defaultConfig),
        htmlLabels: false,
        flowchart: { htmlLabels: false },
        theme: request.theme === "dark" ? "dark" : "default",
        suppressErrorRendering: true,
      });
      const { svg } = await mermaid.render(`mermaid-${request.id}`, request.source);
      parent.postMessage({ type: "mermaid.result", id: request.id, svg: sanitizeMermaidSvg(svg) }, location.origin);
    } catch (error) {
      parent.postMessage({ type: "mermaid.result", id: request.id, error: error instanceof Error ? error.message : String(error) }, location.origin);
    } finally {
      document.querySelectorAll("body > :not(script)").forEach((node) => node.remove());
    }
  });
});
parent.postMessage({ type: "mermaid.ready" }, location.origin);
