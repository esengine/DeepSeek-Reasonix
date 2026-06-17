import { mcpServerLifecycleActions } from "../lib/mcpServerLifecycle";
import type { ServerView } from "../lib/types";

function ok(value: unknown, message: string) {
  if (!value) throw new Error(message);
}

function server(status: ServerView["status"]): ServerView {
  return {
    name: "codegraph",
    transport: "stdio",
    status,
    configured: true,
    autoStart: true,
    tier: "background",
    tools: 0,
    prompts: 0,
    resources: 0,
  };
}

const initializing = mcpServerLifecycleActions(server("initializing"));
ok(initializing.enabled, "initializing server should still be treated as enabled");
ok(initializing.showRetryInRow, "initializing server row should expose a retry/reset action");
ok(initializing.canReconnect, "initializing server details should expose reconnect");
ok(!initializing.canConnectNow, "initializing server should not use the deferred connect-now action");

const connected = mcpServerLifecycleActions(server("connected"));
ok(!connected.showRetryInRow, "connected server row should keep the toggle UI");
ok(connected.canReconnect, "connected server details should expose reconnect");

const deferred = mcpServerLifecycleActions(server("deferred"));
ok(deferred.canConnectNow, "deferred server should expose connect now");
ok(!deferred.canReconnect, "deferred server should not show reconnect separately");

console.log("capabilities panel MCP actions");
