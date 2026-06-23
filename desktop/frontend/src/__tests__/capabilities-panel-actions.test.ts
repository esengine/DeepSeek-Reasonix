import { mcpServerLifecycleActions, mcpServerRetryableFromAvailableList } from "../lib/mcpServerLifecycle";
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
ok(!initializing.showRetryInRow, "initializing server should not expose retry until it fails");
ok(!initializing.canReconnect, "initializing server should not expose reconnect while already connecting");
ok(!initializing.canConnectNow, "initializing server should not use the deferred connect-now action");

const connected = mcpServerLifecycleActions(server("connected"));
ok(!connected.showRetryInRow, "connected server row should keep the toggle UI");
ok(connected.canReconnect, "connected server details should expose reconnect");

const manuallyConnected = mcpServerLifecycleActions({ ...server("connected"), autoStart: false, startIntent: "off", runtimeState: "ready" });
ok(manuallyConnected.enabled, "connected manual server should still render as enabled");
ok(!manuallyConnected.canConnectNow, "connected manual server should not expose connect-now");
ok(manuallyConnected.canReconnect, "connected manual server should expose reconnect");

const automaticIdle = mcpServerLifecycleActions({ ...server("deferred"), startIntent: "automatic" });
ok(!automaticIdle.canConnectNow, "automatic idle server should not look like a manual connector");
ok(!automaticIdle.canReconnect, "automatic idle server should wait for background connection or failure");

const failed = mcpServerLifecycleActions({ ...server("failed"), runtimeState: "issue" });
ok(failed.showRetryInRow, "failed server row should expose retry");

ok(mcpServerRetryableFromAvailableList(server("initializing")), "connecting server should be included in available-list retry all");
ok(mcpServerRetryableFromAvailableList({ ...server("deferred"), startIntent: "automatic" }), "automatic idle server should be included in available-list retry all");
ok(!mcpServerRetryableFromAvailableList(server("connected")), "connected server should be excluded from available-list retry all");
ok(!mcpServerRetryableFromAvailableList({ ...server("disabled"), startIntent: "off" }), "disabled server should be excluded from available-list retry all");
ok(!mcpServerRetryableFromAvailableList({ ...server("failed"), runtimeState: "issue" }), "failed server is handled by the failure banner retry all");

console.log("capabilities panel MCP actions");
