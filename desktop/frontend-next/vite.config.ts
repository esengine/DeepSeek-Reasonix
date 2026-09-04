import { defineConfig, loadEnv } from "vite";
import react from "@vitejs/plugin-react";

interface ProxyEvents {
  on(event: "proxyReq", cb: (req: { setHeader(k: string, v: string): void }) => void): void;
}

// Prefixes: vite matches a proxy key against the head of the path, so "/mcp"
// also carries /mcp/reconnect. A route missing here answers with the SPA shell
// instead of JSON, which is indistinguishable from a broken endpoint.
const ROUTES = [
  // The hub's own surface, plus /rt — every pane's requests carry that prefix,
  // so without it a second session's traffic answers with the SPA shell.
  "/runtimes", "/tree", "/rt", "/remotes",
  "/appearance",
  "/events", "/history", "/status", "/balance", "/submit", "/cancel", "/approve", "/answer",
  "/adjudications", "/execution-graph",
  "/plan", "/plan-decision", "/goal", "/resume", "/models", "/tool-approval-mode", "/preset",
  "/model", "/effort", "/new", "/sessions", "/delete-session", "/provider-setup",
  "/inbox", "/trajectory", "/mcp", "/skills", "/complete", "/workspace", "/capability-scope",
  "/providers", "/roles", "/account", "/hooks", "/memory", "/network", "/shell", "/todos",
  "/changes", "/attachments", "/drop", "/checkpoints", "/branches", "/compact", "/compaction", "/rewind",
  "/extensions", "/themes", "/plugins", "/surfaces",
  "/fork", "/summarize", "/forget", "/bypass", "/auto-approve-tools",
  "/permissions", "/sandbox", "/context", "/storage", "/tray", "/asks", "/update",
  "/slash", "/workspaces", "/welcome", "/usage", "/config", "/studio",
];

// REASONIX_SERVE points at a running `reasonix serve`; without it the app boots
// on MockPort so the UI can be developed with no Go process at all.
export default defineConfig(({ mode }) => {
  const serve = loadEnv(mode, ".", "REASONIX").REASONIX_SERVE;
  return {
    base: "./",
    plugins: [react()],
    server: {
      port: 5273,
      // The dev server is cross-origin to serve, which its CSRF guard rejects.
      // Rewriting Origin makes the hop look same-origin, matching production
      // and the Wails shell where the UI really is served from the kernel.
      proxy: serve
        ? Object.fromEntries(
            ROUTES.map((r) => [
              r,
              {
                target: serve,
                changeOrigin: true,
                configure: (proxy: unknown) => {
                  (proxy as ProxyEvents).on("proxyReq", (req) => req.setHeader("origin", serve));
                },
              },
            ]),
          )
        : undefined,
    },
    build: { outDir: "dist", emptyOutDir: true },
  };
});
