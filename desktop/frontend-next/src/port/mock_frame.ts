// A frame document for the dev fixture, speaking reasonix.frame.v1 the way a
// plugin's own document would. Served as a blob URL: there is no kernel route
// behind the mock, and the dev build is the only place this module loads.
const BOARD = `<!doctype html>
<meta charset="utf-8">
<style>
  body { margin: 0; padding: 12px 14px; font: 13px/1.5 var(--ui, system-ui); color: var(--text); background: transparent; }
  .cols { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 10px; }
  .col h3 { margin: 0 0 6px; font-size: 12px; color: var(--muted); font-weight: 500; }
  .card { padding: 8px 10px; border: 1px solid var(--border); border-radius: var(--r-md, 8px); background: var(--raised); margin-bottom: 6px; cursor: pointer; }
  .card:hover { border-color: var(--accent); }
  button { font: inherit; color: var(--accent-fg); background: var(--accent); border: 0; border-radius: var(--r-sm, 6px); padding: 4px 10px; }
</style>
<div class="cols" id="cols"></div>
<p><button id="refresh">Refresh</button></p>
<script>
  let port;
  const cols = document.getElementById("cols");
  const paint = (props) => {
    cols.textContent = "";
    for (const [name, cards] of Object.entries(props.columns || {})) {
      const col = document.createElement("div");
      col.className = "col";
      const h = document.createElement("h3");
      h.textContent = name + " · " + cards.length;
      col.append(h);
      for (const card of cards) {
        const el = document.createElement("div");
        el.className = "card";
        el.textContent = card.title;
        el.onclick = () => port && port.postMessage({ type: "action", actionId: "advance", args: { card: card.id } });
        col.append(el);
      }
      cols.append(col);
    }
    port && port.postMessage({ type: "size", height: document.documentElement.scrollHeight });
  };
  const theme = (t) => { for (const [k, v] of Object.entries(t.tokens || {})) document.documentElement.style.setProperty(k, v); };
  document.getElementById("refresh").onclick = () => port && port.postMessage({ type: "action", actionId: "refresh" });
  addEventListener("message", (e) => {
    if (!e.data || e.data.type !== "connect" || !e.ports[0]) return;
    port = e.ports[0];
    port.onmessage = (m) => {
      if (m.data.type === "init") { theme(m.data.theme); paint(m.data.props); }
      if (m.data.type === "props") paint(m.data.props);
      if (m.data.type === "theme") theme(m.data.theme);
    };
    port.postMessage({ type: "ready" });
  });
</script>`;

let url = "";

export function mockFrameUrl(): string {
  url ||= URL.createObjectURL(new Blob([BOARD], { type: "text/html" }));
  return url;
}
