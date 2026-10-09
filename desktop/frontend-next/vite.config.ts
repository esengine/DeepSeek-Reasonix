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
  "/adjudications", "/execution-graph", "/jobs",
  "/plan", "/plan-decision", "/goal", "/resume", "/models", "/tool-approval-mode", "/workspace-trust", "/preset",
  "/model", "/default-model", "/effort", "/new", "/sessions", "/delete-session", "/provider-setup",
  "/inbox", "/trajectory", "/mcp", "/skills", "/complete", "/prompt", "/workspace", "/capability-scope",
  "/providers", "/decision-models", "/roles", "/account", "/backups", "/hooks", "/memory", "/network", "/shell", "/todos",
  "/changes", "/commit", "/attachments", "/drop", "/checkpoints", "/branches", "/compact", "/compaction", "/rewind",
  "/extensions", "/themes", "/plugins", "/market", "/surfaces", "/feedback",
  "/fork", "/summarize", "/forget", "/bypass", "/auto-approve-tools",
  "/permissions", "/sandbox", "/progress-watch", "/display-currency", "/auto-archive", "/context", "/storage", "/tray", "/browser", "/browser-host", "/asks", "/update",
  "/host", "/notifications", "/share", "/pair", "/device",
  "/slash", "/workspaces", "/welcome", "/usage", "/config", "/studio",
];

// REASONIX_SERVE points at a running `reasonix serve`; without it the app boots
// on MockPort so the UI can be developed with no Go process at all.
export default defineConfig(({ mode }) => {
  const serve = loadEnv(mode, ".", "REASONIX").REASONIX_SERVE;
  return {
    base: "./",
    plugins: [react()],
    // Stylesheets are stubbed for tests by default, `?raw` included, so a
    // guard that reads app.css to check a rule gets an empty string and passes
    // on nothing. Processing it is what makes that guard able to fail.
    //
    // Threads rather than forks: a worker per file either way, but a thread is
    // cheaper to start, which is most of what a suite of small files pays.
    //
    // Sharing a worker across files was tried and taken back out. It was fast —
    // about 9s against 15 — and it made the suite intermittent: vi.mock cannot
    // see a module another file already imported unmocked, and a render test
    // that passed alone failed in one full run and passed in the next. A gate
    // that goes red by arrangement teaches people to re-run it, not read it.
    test: {
      css: true,
      pool: "threads",
    },
    server: {
      port: 5273,
      // The dev server is cross-origin to serve, which its CSRF guard rejects.
      // Rewriting Origin makes the hop look same-origin, matching production
      // where the UI really is served from the kernel.
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
    build: {
      outDir: "dist",
      emptyOutDir: true,
      rolldownOptions: {
        output: {
          codeSplitting: {
            groups: [
              {
                name: "react-runtime",
                test: /node_modules[\\/](?:react|react-dom|scheduler)[\\/]/,
                priority: 10,
              },
            ],
          },
        },
      },
    },
  };
});
