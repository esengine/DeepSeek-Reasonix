/** `/mcp` slash modal — single hub with two tabs: Live (attached servers) + Marketplace (registry). */

import { Box, Text } from "ink";
import React, { useState } from "react";
import { McpBrowser } from "./McpBrowser.js";
import { McpMarketplace } from "./McpMarketplace.js";
import type { PickerBroadcastPorts } from "./dashboard/use-picker-broadcast.js";
import { useKeystroke } from "./keystroke-context.js";
import type { ApplyAppend } from "./mcp-reconnect-kickoff.js";
import type { McpServerSummary } from "./slash/types.js";
import { COLOR } from "./theme.js";

export type McpHubTab = "live" | "marketplace";

export interface McpHubProps {
  initialTab: McpHubTab;
  liveServers: McpServerSummary[];
  configPath: string;
  onClose: () => void;
  postInfo: (text: string) => void;
  applyAppend?: ApplyAppend;
  reloadMcp?: () => Promise<{
    added: string[];
    removed: string[];
    failed: Array<{ spec: string; reason: string }>;
  }>;
  /** Forwarded to the marketplace tab so the web dashboard can drive install / uninstall / refine / load-more. */
  pickerPorts?: PickerBroadcastPorts;
}

export function McpHub({
  initialTab,
  liveServers,
  configPath,
  onClose,
  postInfo,
  applyAppend,
  reloadMcp,
  pickerPorts,
}: McpHubProps) {
  const [tab, setTab] = useState<McpHubTab>(initialTab);

  // Hub-level: Tab key cycles tabs. Inner components don't bind Tab
  // (Marketplace rebound to PgDn for load-more) so no conflict.
  useKeystroke((ev) => {
    if (ev.paste) return;
    if (ev.tab) setTab((t) => (t === "live" ? "marketplace" : "live"));
  });

  return (
    <Box flexDirection="column">
      <Box paddingX={1}>
        <Text bold color={COLOR.brand}>
          ◈ MCP
        </Text>
        <Text>{"  "}</Text>
        <TabPill label="Live" count={liveServers.length} active={tab === "live"} />
        <Text>{"  "}</Text>
        <TabPill label="Marketplace" active={tab === "marketplace"} />
        <Text dimColor>{"   tab to switch"}</Text>
      </Box>
      {tab === "live" ? (
        <McpBrowser
          servers={liveServers}
          configPath={configPath}
          onClose={onClose}
          postInfo={postInfo}
          applyAppend={applyAppend}
        />
      ) : (
        <McpMarketplace
          onClose={onClose}
          postInfo={postInfo}
          reloadMcp={reloadMcp}
          pickerPorts={pickerPorts}
        />
      )}
    </Box>
  );
}

function TabPill({ label, count, active }: { label: string; count?: number; active: boolean }) {
  const text = count !== undefined ? `${label} (${count})` : label;
  if (active) {
    return (
      <Text bold color={COLOR.brand}>
        [{text}]
      </Text>
    );
  }
  return <Text dimColor>{` ${text} `}</Text>;
}
